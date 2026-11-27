package linux

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

const sqliteHeader = "SQLite format 3\x00"

func openBackupDirectory(fd int, name string) (*os.File, error) {
	child, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(child), name), nil
}

func missingBackupDirectory(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP)
}

func (h *Host) walkBackupDatabases(slug string) (backupWalk, error) {
	srv, err := openBackupDirectory(unix.AT_FDCWD, h.path("/srv"))
	if err != nil {
		if missingBackupDirectory(err) {
			return backupWalk{}, nil
		}
		return backupWalk{}, hostError("/srv", err)
	}
	account, err := openBackupDirectory(int(srv.Fd()), slug)
	_ = srv.Close()
	if err != nil {
		if missingBackupDirectory(err) {
			return backupWalk{}, nil
		}
		return backupWalk{}, hostError("/srv/"+slug, err)
	}
	data, err := openBackupDirectory(int(account.Fd()), "data")
	_ = account.Close()
	if err != nil {
		if missingBackupDirectory(err) {
			return backupWalk{}, nil
		}
		return backupWalk{}, hostError("/srv/"+slug+"/data", err)
	}
	defer func() { _ = data.Close() }()
	var walker backupWalker
	err = walker.directory(data, "data", 0)
	if errors.Is(err, machine.ErrTooLarge) {
		return backupWalk{Error: machine.DataTooLarge}, nil
	}
	if walker.walk.Total > MaxDatabasesPerAccount {
		walker.walk.Error = machine.TooManyDatabases
	}
	slices.SortFunc(walker.walk.Databases, func(a, b backupDatabase) int { return cmp.Compare(a.Path, b.Path) })
	return walker.walk, err
}

type backupWalker struct {
	walk    backupWalk
	visited int
}

type backupDirectory interface {
	ReadDir(int) ([]fs.DirEntry, error)
	Fd() uintptr
}

func (w *backupWalker) directory(dir backupDirectory, path string, depth int) error {
	var failures error
	for {
		entries, err := dir.ReadDir(min(ReadBatch, MaxVisit-w.visited+1))
		w.visited += len(entries)
		if w.visited > MaxVisit {
			return machine.ErrTooLarge
		}
		for _, entry := range entries {
			p := filepath.Join(path, entry.Name())
			if err := w.visit(int(dir.Fd()), entry, p, depth); err != nil {
				if errors.Is(err, machine.ErrTooLarge) {
					return err
				}
				// One filesystem failure suffices while the remaining walk proceeds.
				if failures == nil {
					failures = hostError(p, err)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return failures
		}
		if err != nil {
			return errors.Join(failures, err)
		}
	}
}

func (w *backupWalker) visit(fd int, entry fs.DirEntry, path string, depth int) error {
	if entry.IsDir() {
		if depth >= MaxDepth {
			return machine.ErrTooLarge
		}
		child, err := openBackupDirectory(fd, entry.Name())
		if missingBackupDirectory(err) {
			return nil
		}
		if err != nil {
			return err
		}
		defer func() { _ = child.Close() }()
		return w.directory(child, path, depth+1)
	}
	if !entry.Type().IsRegular() || valid.DatabasePath(path) != nil {
		return nil
	}
	database, wal, err := backupDBHeader(fd, entry.Name())
	if err != nil || !database {
		return err
	}
	w.walk.Total++
	if len(w.walk.Databases) < MaxDatabasesPerAccount {
		w.walk.Databases = append(w.walk.Databases, backupDatabase{Database: protocol.Database{Path: path}, WAL: wal})
	}
	return nil
}

func backupDBHeader(fd int, name string) (database, wal bool, err error) {
	fileFD, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if missingBackupDirectory(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	f := os.NewFile(uintptr(fileFD), name)
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, false, err
	}
	var header [20]byte
	n, err := io.ReadFull(f, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, false, err
	}
	if n < len(sqliteHeader) || !bytes.Equal(header[:len(sqliteHeader)], []byte(sqliteHeader)) {
		return false, false, nil
	}
	return true, n == len(header) && header[18] == 2 && header[19] == 2, nil
}

func (h *Host) saveBackupWalk(slug string, walk backupWalk) error {
	return h.updateAccountBackup(slug, func(facts *backupAccountFacts) {
		facts.WalkRecorded = true
		if walk.Error == machine.DataTooLarge {
			facts.Walk.Error = walk.Error
			return
		}
		for i := range walk.Databases {
			db := &walk.Databases[i]
			idx, found := slices.BinarySearchFunc(facts.Walk.Databases, db.Path, func(d backupDatabase, path string) int {
				return cmp.Compare(d.Path, path)
			})
			if found && facts.Walk.Databases[idx].WAL == db.WAL {
				db.Database = facts.Walk.Databases[idx].Database
			}
		}
		facts.Walk = walk
	})
}
