package store

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

type savedEntry struct {
	name string
	mode os.FileMode
	data []byte
	link string
}

func (s *Store) retainEntry(ctx context.Context, id string) error {
	c := s.scope(ctx)
	if c == nil {
		return nil
	}
	path := s.Dir(id)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := removable(path); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.cfg.Root)
	if err != nil {
		return err
	}
	retained := false
	defer func() {
		if !retained {
			_ = root.Close()
		}
	}()
	var entries []savedEntry
	err = filepath.WalkDir(path, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name, err := filepath.Rel(s.cfg.Root, path)
		if err != nil {
			return err
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		e := savedEntry{name: name, mode: info.Mode()}
		switch {
		case e.mode.IsRegular():
			if e.mode.Perm()&0400 == 0 {
				if err = root.Chmod(name, permissionMode(e.mode)|0400); err != nil {
					return err
				}
				e.data, err = root.ReadFile(name)
				err = errors.Join(err, root.Chmod(name, permissionMode(e.mode)))
			} else {
				e.data, err = root.ReadFile(name)
			}
		case e.mode&os.ModeSymlink != 0:
			e.link, err = root.Readlink(name)
		case e.mode.IsDir(), e.mode&os.ModeNamedPipe != 0, e.mode&os.ModeSocket != 0:
		default:
			err = errors.New("repository entry cannot be retained")
		}
		if err != nil {
			return err
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	retained = true
	c.finish = append(c.finish, dir.Close, root.Close)
	c.undo = append(c.undo, func() error {
		return withDirectory(dir, func() error { return restoreEntry(root, id, entries) })
	})
	return nil
}

func restoreEntry(root *os.Root, id string, entries []savedEntry) error {
	if err := removeOwnedEntry(root, id+".git"); err != nil {
		return err
	}
	var err error
	for _, e := range entries {
		switch {
		case e.mode.IsDir():
			err = root.Mkdir(e.name, 0700)
		case e.mode&os.ModeSymlink != 0:
			err = root.Symlink(e.link, e.name)
		case e.mode.IsRegular():
			var file *os.File
			file, err = root.OpenFile(e.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, e.mode.Perm())
			if err == nil {
				_, err = file.Write(e.data)
				err = errors.Join(err, file.Close())
			}
		case e.mode&os.ModeNamedPipe != 0:
			err = syscall.Mkfifo(filepath.Join(root.Name(), e.name), 0600)
		case e.mode&os.ModeSocket != 0:
			err = syscall.Mknod(filepath.Join(root.Name(), e.name), syscall.S_IFSOCK|0600, 0)
		}
		if err != nil {
			return err
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.mode&os.ModeSymlink == 0 {
			if err = root.Chmod(e.name, permissionMode(e.mode)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Removing an entry during rollback must also work when a late fault changed
// permissions inside it. No links are followed, and these entries are removed.
func removeOwnedEntry(root *os.Root, name string) error {
	if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	err := fs.WalkDir(root.FS(), name, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		// Group/ACL access can already make a directory removable even when
		// owner bits are absent. Do not require chmod authority unnecessarily.
		if dir, openErr := root.Open(name); openErr == nil {
			accessErr := syscall.Faccessat(int(dir.Fd()), ".", 7, 0)
			_ = dir.Close()
			if accessErr == nil {
				return nil
			}
		}
		return root.Chmod(name, permissionMode(info.Mode())|0700)
	})
	if err != nil {
		return err
	}
	return root.RemoveAll(name)
}
