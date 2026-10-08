// Package db opens and manages a service's SQLite database.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	// Register the pure-Go SQLite driver for the database/sql pools.
	_ "modernc.org/sqlite"
)

// Readers is the number of concurrent read transactions per handle.
const Readers = 4

// Config names the database, its embedded migrations, and its migration clock.
type Config struct {
	Path       string
	Migrations fs.FS
	Now        func() time.Time
	Service    string
	Stderr     io.Writer
}

// DB routes transactions through separate reader and writer pools.
type DB struct {
	reader, writer *sql.DB
	failing        atomic.Bool
	closed         atomic.Bool
	closeOnce      sync.Once
}

// Open prepares the file and applies migrations before admitting readers.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := validatePath(cfg.Path)
	if err != nil {
		return nil, err
	}
	migrations, err := loadMigrations(cfg.Migrations)
	if err != nil {
		return nil, err
	}
	if _, err = inspectPath(path); err != nil {
		return nil, err
	}
	if err = makeParents(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// Probe filesystem access before SQLite can create journal sidecars.
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	probe, err := os.CreateTemp(filepath.Dir(path), ".db-write-")
	if err != nil {
		return nil, err
	}
	probeName := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probeName)
	if err = errors.Join(closeErr, removeErr); err != nil {
		return nil, err
	}
	writer, err := sql.Open("sqlite", databaseURI(path, false))
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	unknown, err := applyMigrations(ctx, writer, migrations, cfg.Now)
	if err != nil {
		return nil, errors.Join(err, writer.Close())
	}
	reader, err := sql.Open("sqlite", databaseURI(path, true))
	if err != nil {
		return nil, errors.Join(err, writer.Close())
	}
	reader.SetMaxOpenConns(Readers)
	reader.SetMaxIdleConns(Readers)
	if err = reader.PingContext(ctx); err != nil {
		return nil, errors.Join(err, reader.Close(), writer.Close())
	}
	if unknown != nil && cfg.Stderr != nil {
		_, _ = fmt.Fprintf(cfg.Stderr, "%s: unknown migration version %04d: database is ahead of this binary\n", cfg.Service, *unknown)
	}
	return &DB{reader: reader, writer: writer}, nil
}

func validatePath(path string) (string, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") || strings.Contains(path, "?") {
		return "", errors.New("database path must be an ordinary filesystem path")
	}
	return filepath.Abs(path)
}

func inspectPath(path string) (bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("database is not a regular file: %s", path)
	}
	if info.Size() != 0 {
		header := make([]byte, 16)
		if _, err := io.ReadFull(f, header); err != nil {
			return false, fmt.Errorf("invalid SQLite database: %w", err)
		}
		if string(header) != "SQLite format 3\x00" {
			return false, errors.New("invalid SQLite database header")
		}
	}
	return true, nil
}

func makeParents(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("database parent is not a directory: %s", path)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err = makeParents(filepath.Dir(path)); err != nil {
		return err
	}
	if err = os.Mkdir(path, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.Chmod(filepath.Base(path), 0700)
}

func databaseURI(path string, read bool) string {
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"_pragma": {"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)"}}
	if read {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Set("_txlock", "immediate")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Read runs fn once within a read-only transaction.
func (d *DB) Read(ctx context.Context, fn func(*sql.Tx) error) error {
	return d.transaction(ctx, d.reader, fn)
}

// Write runs fn once within the handle's single writer transaction.
func (d *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	return d.transaction(ctx, d.writer, fn)
}

func (d *DB) transaction(ctx context.Context, pool *sql.DB, fn func(*sql.Tx) error) error {
	if d.closed.Load() {
		return errors.New("database is closed")
	}
	if d.failing.Load() {
		return errors.New("database failure enabled")
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = fn(tx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

// Close closes both pools; repeated calls succeed.
func (d *DB) Close() error {
	var err error
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		err = errors.Join(d.reader.Close(), d.writer.Close())
	})
	return err
}

// SetFailing switches the handle's test failure seam.
func (d *DB) SetFailing(failing bool) { d.failing.Store(failing) }
