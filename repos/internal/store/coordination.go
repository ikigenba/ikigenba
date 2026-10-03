package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

type coordinationKey struct{}

type coordination struct {
	mu       sync.Mutex
	store    *Store
	active   bool
	catalog  bool
	verified map[string]bool
	undo     []func() error
	finish   []func() error
}

// Coordinate groups committed mutations with the reads needed to answer them.
// Call existing Store methods with the supplied context, on this same receiver.
// An error restores the original state without Git or the canceled request context.
// Competing mutations, Verify, and Close wait until cleanup has completed.
// The supplied context grants coordination ownership only during the callback;
// callbacks must not call Close or recursively call Coordinate on this Store.
func (s *Store) Coordinate(ctx context.Context, callback func(context.Context) error) error {
	release, err := s.mutationLock(ctx)
	if err != nil {
		// No scope is owned here. The canceled context still reaches the
		// caller's first Store method, which refuses without taking the gate.
		if callbackErr := callback(ctx); callbackErr != nil {
			return callbackErr
		}
		return err
	}
	defer release()
	c := &coordination{store: s, active: true}
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.active = false
		for _, finish := range c.finish {
			_ = finish()
		}
	}()
	err = callback(context.WithValue(ctx, coordinationKey{}, c))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = false
	if err == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(c.undo) - 1; i >= 0; i-- {
		err = errors.Join(err, c.undo[i]())
	}
	return err
}

func (s *Store) scope(ctx context.Context) *coordination {
	c, _ := ctx.Value(coordinationKey{}).(*coordination)
	if c != nil && c.store == s && c.active {
		return c
	}
	return nil
}

func (s *Store) mutationLock(ctx context.Context) (func(), error) {
	if c, ok := ctx.Value(coordinationKey{}).(*coordination); ok && c.store == s {
		c.mu.Lock()
		if c.active {
			return c.mu.Unlock, nil
		}
		c.mu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.gate:
		return func() { s.gate <- struct{}{} }, nil
	}
}

// retainCatalog runs with both the mutation lock and Store mutex held, before
// a transaction begins. The file descriptor survives later permission changes.
func (s *Store) retainCatalog(ctx context.Context) error {
	c := s.scope(ctx)
	if c == nil || c.catalog {
		return nil
	}
	if s.cfg.Source == "" || s.cfg.Source == ":memory:" {
		rows, err := s.rows(ctx, "SELECT "+columns+" FROM repos ORDER BY id")
		if err != nil {
			return err
		}
		c.undo = append(c.undo, func() error {
			tx, err := s.db.BeginTx(context.Background(), nil)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			if _, err = tx.Exec("DELETE FROM repos"); err != nil {
				return err
			}
			for _, r := range rows {
				if available, ok := c.verified[r.ID]; ok {
					r.Available = available
				}
				if _, err = tx.Exec("INSERT INTO repos ("+columns+") VALUES(?,?,?,?,?)", r.ID, r.Name, r.Owner, r.Created.Format(createdLayout), r.Available); err != nil {
					return err
				}
			}
			return tx.Commit()
		})
	} else {
		root, err := os.OpenRoot(filepath.Dir(s.cfg.Source))
		if err != nil {
			return err
		}
		defer func() { _ = root.Close() }()
		file, err := root.OpenFile(filepath.Base(s.cfg.Source), os.O_RDWR, 0)
		if err != nil {
			return err
		}
		bytes, err := io.ReadAll(file)
		if err != nil {
			_ = file.Close()
			return err
		}
		c.finish = append(c.finish, file.Close)
		c.undo = append(c.undo, func() error {
			image := bytes
			if len(c.verified) != 0 {
				var err error
				image, err = overlayAvailability(image, c.verified)
				if err != nil {
					return err
				}
			}
			if err := s.db.Close(); err != nil {
				return err
			}
			_, writeErr := file.WriteAt(image, 0)
			truncateErr := file.Truncate(int64(len(image)))
			syncErr := file.Sync()
			source, absErr := filepath.Abs(s.cfg.Source)
			if absErr != nil {
				return errors.Join(writeErr, truncateErr, syncErr, absErr)
			}
			source = (&url.URL{Scheme: "file", Path: source}).String()
			db, openErr := sql.Open("sqlite", source)
			if openErr == nil {
				db.SetMaxOpenConns(1)
				s.db = db
			}
			return errors.Join(writeErr, truncateErr, syncErr, openErr)
		})
	}
	c.catalog = true
	return nil
}

// Successful Verify is independent of the reversible identity changes. Patch
// its last result into the saved image in memory, so rollback never needs a
// writable Source directory or an uncanceled request to preserve that result.
func overlayAvailability(image []byte, values map[string]bool) ([]byte, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	type imageConnection interface {
		Serialize() ([]byte, error)
		Deserialize([]byte) error
	}
	err = conn.Raw(func(driverConn any) error {
		return driverConn.(imageConnection).Deserialize(image)
	})
	if err != nil {
		return nil, err
	}
	for id, available := range values {
		if _, err = conn.ExecContext(context.Background(), "UPDATE repos SET available=? WHERE id=?", available, id); err != nil {
			return nil, err
		}
	}
	err = conn.Raw(func(driverConn any) error {
		image, err = driverConn.(imageConnection).Serialize()
		return err
	})
	return image, err
}

// Undo borrows write/search permission, then restores the fault-time mode,
// leaving externally injected permissions in place.
func withDirectory(file *os.File, undo func() error) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	mode := permissionMode(info.Mode())
	if err = file.Chmod(mode | 0700); err != nil {
		return err
	}
	return errors.Join(undo(), file.Chmod(mode))
}

func permissionMode(mode os.FileMode) os.FileMode {
	return mode & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
}

func (s *Store) retainCreation(ctx context.Context, id string) error {
	c := s.scope(ctx)
	if c == nil {
		return nil
	}
	root, err := os.OpenRoot(s.cfg.Root)
	if err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return err
	}
	c.finish = append(c.finish, dir.Close, root.Close)
	c.undo = append(c.undo, func() error {
		return withDirectory(dir, func() error { return removeOwnedEntry(root, id+".git") })
	})
	return nil
}

func (s *Store) retainConfig(ctx context.Context, id string, original []byte, mode os.FileMode) error {
	c := s.scope(ctx)
	if c == nil {
		return nil
	}
	root, err := os.OpenRoot(s.cfg.Root)
	if err != nil {
		return err
	}
	dir, err := root.Open(id + ".git")
	if err != nil {
		_ = root.Close()
		return err
	}
	parent, err := root.Open(".")
	if err != nil {
		_ = dir.Close()
		_ = root.Close()
		return err
	}
	relative := filepath.Join(id+".git", "config")
	_, lockErr := root.Lstat(relative + ".lock")
	hadLock := lockErr == nil
	if lockErr != nil && !errors.Is(lockErr, os.ErrNotExist) {
		_ = parent.Close()
		_ = dir.Close()
		_ = root.Close()
		return lockErr
	}
	c.finish = append(c.finish, dir.Close, parent.Close, root.Close)
	c.undo = append(c.undo, func() error {
		return withDirectory(parent, func() error {
			return withDirectory(dir, func() error {
				if !hadLock {
					if err := removeOwnedEntry(root, relative+".lock"); err != nil {
						return err
					}
				}
				current, readErr := root.ReadFile(relative)
				info, statErr := root.Stat(relative)
				if readErr == nil && statErr == nil && bytes.Equal(current, original) && info.Mode().Perm() == mode {
					return nil
				}
				return replaceConfig(root, relative, original, mode, nil)
			})
		})
	})
	return nil
}
