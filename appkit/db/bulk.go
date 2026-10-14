package db

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Bulk builds a new SQLite file without a journal or synchronous writes.
// A failed Write spoils the build; callers must close and discard its file.
type Bulk struct {
	pool      *sql.DB
	writeSlot chan struct{}
	spoiled   atomic.Bool
	closed    atomic.Bool
	closeOnce sync.Once
}

// OpenBulk exclusively creates path and prepares one bulk writer connection.
func OpenBulk(ctx context.Context, path string) (*Bulk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := validatePath(path)
	if err != nil {
		return nil, err
	}
	if err = makeParents(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": {"journal_mode(OFF)", "synchronous(OFF)", "foreign_keys(1)"}}.Encode()
	pool, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	if err = pool.PingContext(ctx); err != nil {
		return nil, errors.Join(err, pool.Close())
	}
	return &Bulk{pool: pool, writeSlot: make(chan struct{}, 1)}, nil
}

// Write calls fn once in a transaction and commits its successful result.
// Any error or panic makes subsequent writes fail without calling fn.
func (b *Bulk) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	succeeded := false
	acquired := false
	defer func() {
		if !succeeded {
			b.spoiled.Store(true)
		}
		if acquired {
			<-b.writeSlot
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case b.writeSlot <- struct{}{}:
		acquired = true
	case <-ctx.Done():
		return ctx.Err()
	}
	if b.closed.Load() {
		return errors.New("bulk database is closed")
	}
	if b.spoiled.Load() {
		return errors.New("bulk database is spoiled")
	}
	tx, err := b.pool.BeginTx(ctx, nil)
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
	if err = tx.Commit(); err != nil {
		return err
	}
	succeeded = true
	return nil
}

// Close releases the connection without syncing the file; repeated calls succeed.
func (b *Bulk) Close() error {
	var err error
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		err = b.pool.Close()
	})
	return err
}
