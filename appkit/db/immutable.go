package db

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
)

// Immutable reads a finished SQLite file that the caller promises not to change.
type Immutable struct {
	reader    *sql.DB
	closed    atomic.Bool
	closeOnce sync.Once
}

// OpenImmutable opens an existing database without locks or writable sidecars.
func OpenImmutable(ctx context.Context, path string) (*Immutable, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := validatePath(path)
	if err != nil {
		return nil, err
	}
	exists, err := inspectPath(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &os.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"mode": {"ro"}, "immutable": {"1"}, "_pragma": {"query_only(1)"}}.Encode()
	reader, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	reader.SetMaxOpenConns(Readers)
	reader.SetMaxIdleConns(Readers)
	// Reading the schema forces SQLite to validate the file before returning.
	var count int
	if err = reader.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&count); err != nil {
		return nil, errors.Join(err, reader.Close())
	}
	return &Immutable{reader: reader}, nil
}

// Read runs fn once in a read transaction, abandoning it on error or panic.
func (i *Immutable) Read(ctx context.Context, fn func(*sql.Tx) error) error {
	if i.closed.Load() {
		return errors.New("database is closed")
	}
	tx, err := i.reader.BeginTx(ctx, nil)
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

// Close closes the reader pool; repeated calls succeed.
func (i *Immutable) Close() error {
	var err error
	i.closeOnce.Do(func() {
		i.closed.Store(true)
		err = i.reader.Close()
	})
	return err
}
