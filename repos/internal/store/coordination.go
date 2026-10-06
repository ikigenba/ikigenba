package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

type coordinationKey struct{}
type coordination struct {
	store  *Store
	tx     *sql.Tx
	active bool
	undo   []func() error
	finish []func() error
	events []func()
}

// Coordinate holds the handle's writer through the mutation and response reads.
func (s *Store) Coordinate(ctx context.Context, callback func(context.Context) error) error {
	c := &coordination{store: s, active: true}
	called := false
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		called = true
		c.tx = tx
		return callback(context.WithValue(ctx, coordinationKey{}, c))
	})
	if !called {
		if e := callback(ctx); e != nil {
			err = e
		}
	}
	c.active = false
	if err != nil {
		for i := len(c.undo) - 1; i >= 0; i-- {
			err = errors.Join(err, c.undo[i]())
		}
	} else {
		for _, emit := range c.events {
			emit()
		}
	}
	for _, finish := range c.finish {
		err = errors.Join(err, finish())
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
