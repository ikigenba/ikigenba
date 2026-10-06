package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func (s *Store) nameTaken(ctx context.Context, tx *sql.Tx, owner, name, except string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM repos WHERE owner=? AND name=? AND id<>?", owner, name, except).Scan(&n)
	return n != 0, err
}

// Create makes an empty bare repository and its catalog identity together.
func (s *Store) Create(ctx context.Context, owner, name string) (Repo, error) {
	var undo func() error
	r, err := transact(ctx, s, true, func(tx *sql.Tx) (Repo, error) {
		var err error
		if owner == "" || !ValidName(name) {
			return Repo{}, errors.New("invalid repository identity")
		}
		taken, err := s.nameTaken(ctx, tx, owner, name, "")
		if err != nil {
			return Repo{}, err
		}
		if taken {
			return Repo{}, ErrNameTaken
		}
		var id string
		for {
			var bytes [8]byte
			if _, err = io.ReadFull(s.cfg.Rand, bytes[:]); err != nil {
				return Repo{}, errors.New("repository id randomness: " + err.Error())
			}
			id = IDPrefix + hex.EncodeToString(bytes[:])
			if _, err = s.byID(ctx, tx, id); err == nil {
				continue
			} else if !errors.Is(err, ErrNotFound) {
				return Repo{}, err
			}
			if _, err = os.Lstat(s.Dir(id)); err == nil {
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return Repo{}, err
			}
			break
		}
		r := Repo{ID: id, Name: name, Owner: owner, Created: s.cfg.Now().UTC().Truncate(time.Second), Available: true}
		if err = s.retainCreation(ctx, id); err != nil {
			return Repo{}, err
		}

		if _, err = tx.ExecContext(ctx, "INSERT INTO repos ("+columns+") VALUES(?,?,?,?,?)", r.ID, r.Name, r.Owner, r.Created.Format(createdLayout), true); err != nil {
			return Repo{}, err
		}
		dir, err := filepath.Abs(s.Dir(id))
		if err != nil {
			return Repo{}, err
		}
		// No template content is copied, including hooks from a host's template directory.
		if _, err = s.cfg.Git.Output(ctx, "/", "init", "--bare", "--template=", "--object-format=sha1", "--initial-branch="+DefaultBranch, dir); err != nil {
			_ = os.RemoveAll(dir)
			return Repo{}, err
		}
		success := false
		defer func() {
			if !success {
				_ = os.RemoveAll(dir)
			}
		}()
		settings := [][2]string{{"pack.windowMemory", "64m"}, {"pack.threads", "1"}, {"core.bigFileThreshold", "16m"}, {"ikigenba.id", r.ID}, {"ikigenba.name", r.Name}, {"ikigenba.owner", r.Owner}, {"ikigenba.created", r.Created.Format(createdLayout)}}
		for _, setting := range settings {
			if _, err = s.cfg.Git.Output(ctx, "/", "config", "--file", filepath.Join(dir, "config"), setting[0], setting[1]); err != nil {
				return Repo{}, err
			}
		}

		success = true
		undo = func() error { return os.RemoveAll(dir) }
		return r, nil
	})
	if err != nil && undo != nil {
		err = errors.Join(err, undo())
	}
	return r, err
}

// Rename changes the catalog name and, for an available repository, its config.
func (s *Store) Rename(ctx context.Context, id, name string) (Repo, error) {
	var undo func() error
	r, err := transact(ctx, s, true, func(tx *sql.Tx) (Repo, error) {
		var err error
		r, err := s.byID(ctx, tx, id)
		if err != nil {
			return Repo{}, err
		}
		if !ValidName(name) {
			return Repo{}, errors.New("invalid repository name")
		}
		if r.Name == name {
			return r, nil
		}
		taken, err := s.nameTaken(ctx, tx, r.Owner, name, id)
		if err != nil {
			return Repo{}, err
		}
		if taken {
			return Repo{}, ErrNameTaken
		}

		if _, err = tx.ExecContext(ctx, "UPDATE repos SET name=? WHERE id=?", name, id); err != nil {
			return Repo{}, err
		}
		var original []byte
		var mode os.FileMode
		var root *os.Root
		relative := filepath.Join(r.ID+".git", "config")
		if r.Available {
			root, err = os.OpenRoot(s.cfg.Root)
			if err != nil {
				return Repo{}, err
			}
			defer func() { _ = root.Close() }()
			original, err = root.ReadFile(relative)
			if err != nil {
				return Repo{}, err
			}
			info, err := root.Stat(relative)
			if err != nil {
				return Repo{}, err
			}
			mode = info.Mode().Perm()
			if err = s.retainConfig(ctx, id, original, mode); err != nil {
				return Repo{}, err
			}
			updated, err := namedConfig(original, name)
			if err != nil {
				return Repo{}, err
			}
			if err = s.writeNameConfig(ctx, root, relative, updated, mode, name); err != nil {
				return Repo{}, err
			}
		}

		if r.Available {
			undo = func() error {
				root, e := os.OpenRoot(s.cfg.Root)
				if e != nil {
					return e
				}
				defer func() { _ = root.Close() }()
				return replaceConfig(root, relative, original, mode, nil)
			}
		}

		r.Name = name
		return r, nil
	})
	if err != nil && undo != nil {
		err = errors.Join(err, undo())
	}
	return r, err
}

// Delete removes a catalogued entry and its row, preserving all other entries.
func (s *Store) Delete(ctx context.Context, id string) (err error) {
	var undo, finish func() error
	keep := false
	_, err = transact(ctx, s, true, func(tx *sql.Tx) (struct{}, error) {
		var err error
		_, err = s.byID(ctx, tx, id)
		if err != nil {
			return struct{}{}, err
		}
		if err = s.retainEntry(ctx, id); err != nil {
			return struct{}{}, err
		}

		if _, err = tx.ExecContext(ctx, "DELETE FROM repos WHERE id=?", id); err != nil {
			return struct{}{}, err
		}
		path := s.Dir(id)
		_, err = os.Lstat(path)
		exists := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return struct{}{}, err
		}
		if !exists {
			return struct{}{}, nil
		}
		// Check removal rights before changing names, so a permission failure is reversible.
		if err = removable(path); err != nil {
			return struct{}{}, err
		}
		// Keep authority to finish or reverse owned staging after a late chmod.
		// A permission fault already present at entry must still refuse the delete.
		if err = syscall.Access(filepath.Clean(s.cfg.Root), 3); err != nil {
			return struct{}{}, err
		}
		root, err := os.OpenRoot(s.cfg.Root)
		if err != nil {
			return struct{}{}, err
		}
		defer func() {
			if !keep {
				_ = root.Close()
			}
		}()
		dir, err := root.Open(".")
		if err != nil {
			return struct{}{}, err
		}
		defer func() {
			if !keep {
				_ = dir.Close()
			}
		}()
		stage, err := os.MkdirTemp(s.cfg.Root, ".delete-")
		if err != nil {
			return struct{}{}, err
		}
		stageName := filepath.Base(stage)
		stageRemoved := false
		// One owner covers every return after creation, including a rename that
		// fails before any scoped undo could be installed. Never discard cleanup.
		defer func() {
			if !stageRemoved {
				if cleanupErr := removeEmptyDeleteStage(root, dir, stageName); cleanupErr != nil {
					err = errors.Join(err, cleanupErr)
				}
			}
		}()
		staged := filepath.Join(stage, "entry")
		if err = os.Rename(path, staged); err != nil {
			return struct{}{}, err
		}
		undo = func() error { return os.Rename(staged, path) }
		finish = func() error {
			defer func() { _ = root.Close(); _ = dir.Close() }()
			return deleteStaged(root, dir, stageName)
		}
		// Keep retained descriptors until the handle has committed or rolled back.
		keep = true

		stageRemoved = true
		return struct{}{}, nil
	})
	if err != nil && undo != nil {
		err = errors.Join(err, undo())
	}
	if finish != nil {
		err = errors.Join(err, finish())
	}
	return err
}

func removeEmptyDeleteStage(root *os.Root, dir *os.File, stage string) error {
	remove := func() error {
		err := root.Remove(stage)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	err := remove()
	if errors.Is(err, os.ErrPermission) {
		return withDirectory(dir, remove)
	}
	return err
}

// Cleanup may lose Root write permission after staging has committed. Finish
// removal through the retained owned directory, then restore the fault-time
// mode. Only this delete's stage is made removable; no bytes are inspected.
func deleteStaged(root *os.Root, dir *os.File, stage string) error {
	err := root.RemoveAll(stage)
	if errors.Is(err, os.ErrPermission) {
		return withDirectory(dir, func() error {
			if err := root.RemoveAll(stage); !errors.Is(err, os.ErrPermission) {
				return err
			}
			return removeOwnedEntry(root, stage)
		})
	}
	return err
}

func removable(path string) error {
	return filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		entries, err := os.ReadDir(filepath.Clean(path))
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		return syscall.Access(filepath.Clean(path), 3)
	})
}
