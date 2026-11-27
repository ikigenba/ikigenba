package linux

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

const sqliteHeader = "SQLite format 3\x00"

const backupWalkFallbackDir = BackupDir + "/walk"

type backupWalkFallback struct {
	backupWalk
	Token string `json:",omitempty"`
}

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

func (h *Host) pruneBackupWalks(slugs []string) {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	for slug := range h.backupWalks {
		if _, active := slices.BinarySearch(slugs, slug); !active {
			delete(h.backupWalks, slug)
		}
	}
}

func (h *Host) recordBackupWalk(slug string, walk backupWalk, directoryReady bool) error {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	// A finished walk supersedes its predecessor even if the write fails.
	previous, attempted := h.backupWalks[slug]
	limited := walk.Error == machine.DataTooLarge
	publication := backupWalkPublication{readiness: walk.readiness()}
	if limited {
		publication = backupWalkPublication{
			readiness:            previous.readiness,
			fallback:             previous.fallback,
			fallbackFromPrevious: !attempted || previous.fallbackFromPrevious,
		}
	}
	h.backupWalks[slug] = publication
	if !directoryReady {
		return nil
	}
	if limited {
		return h.recordLimitedBackupWalk(slug, publication)
	}
	facts := h.readAccountBackup(slug)
	for i := range walk.Databases {
		db := &walk.Databases[i]
		idx, found := slices.BinarySearchFunc(facts.Walk.Databases, db.Path, func(d backupDatabase, path string) int {
			return cmp.Compare(d.Path, path)
		})
		if found && facts.Walk.Databases[idx].WAL == db.WAL {
			db.Database = facts.Walk.Databases[idx].Database
		}
	}
	fallback, fallbackErr := h.saveBackupWalkFallback(slug, walk)
	h.backupWalks[slug] = backupWalkPublication{readiness: walk.readiness(), fallback: fallback}
	// A primary publication must never be newer than its recovery copy.
	if fallbackErr != nil {
		return fallbackErr
	}
	facts.Walk = walk
	facts.WalkRecorded = true
	return h.publishBackupWalk(slug, facts)
}

func (h *Host) recordLimitedBackupWalk(slug string, publication backupWalkPublication) error {
	path := backupStatusDir + "/" + slug + ".json"
	facts, err := loadBackupFacts[backupAccountFacts](h, path)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	var walk backupWalk
	var retained bool
	var fallbackErr error
	if publication.fallbackFromPrevious {
		fallback, readErr := h.loadBackupWalkFallback(slug)
		fallbackErr = readErr
		missingWalk := !facts.WalkRecorded && facts.Walk.Error == machine.DataTooLarge
		if errors.Is(readErr, fs.ErrNotExist) {
			fallbackErr = nil
			if err == nil && !missingWalk {
				// Older releases have only the primary status file.
				if facts.WalkRecorded {
					walk = facts.Walk
				}
				publication.fallback, fallbackErr = h.saveBackupWalkFallback(slug, walk)
			}
		} else if readErr == nil && (!missingWalk || fallback.Token != facts.RejectedRecoveryToken) {
			walk = fallback.backupWalk
			publication.fallback, fallbackErr = walk.digest()
		}
		if publication.fallback != (backupWalkDigest{}) && fallbackErr == nil {
			publication.fallbackFromPrevious = false
			publication.readiness = walk.readiness()
			h.backupWalks[slug] = publication
			retained = true
		}
	} else {
		walk, retained, fallbackErr = h.readBackupWalkFallback(slug, facts, publication.fallback)
	}
	if !retained {
		// Report the new bound without making stale paths available to
		// restic. Keep the fallback marker so a later pass can recover it.
		facts.Walk = backupWalk{Error: machine.DataTooLarge}
		facts.WalkRecorded, facts.WalkToken = false, ""
		if fallback, loadErr := h.loadBackupWalkFallback(slug); loadErr == nil {
			facts.RejectedRecoveryToken = fallback.Token
		}
		return errors.Join(err, fallbackErr, saveBackupFacts(h, path, facts))
	}
	walk.Error = machine.DataTooLarge
	facts.Walk, facts.WalkRecorded = walk, true
	return errors.Join(err, fallbackErr, h.publishBackupWalk(slug, facts))
}

func (h *Host) saveBackupWalkFallback(slug string, walk backupWalk) (backupWalkDigest, error) {
	walk.Error = ""
	digest, err := walk.digest()
	if err != nil {
		return backupWalkDigest{}, err
	}
	path := backupWalkFallbackDir + "/" + slug + ".json"
	temp, err := h.stageBackupPublication(path, func(token string) ([]byte, error) {
		return json.Marshal(backupWalkFallback{backupWalk: walk, Token: token})
	})
	if err != nil {
		return digest, err
	}
	defer func() { _ = os.Remove(temp) }()
	if err := os.Rename(temp, h.path(path)); err != nil {
		return digest, hostError(path, err)
	}
	return digest, nil
}

func (h *Host) loadBackupWalkFallback(slug string) (backupWalkFallback, error) {
	return loadBackupFacts[backupWalkFallback](h, backupWalkFallbackDir+"/"+slug+".json")
}

func (h *Host) readBackupWalkFallback(slug string, facts backupAccountFacts, expected backupWalkDigest) (backupWalk, bool, error) {
	if expected == (backupWalkDigest{}) {
		return backupWalk{}, false, nil
	}
	if facts.WalkRecorded {
		walk := facts.Walk
		walk.Error = ""
		if digest, err := walk.digest(); err == nil && digest == expected {
			return walk, true, nil
		}
	}
	walk, err := h.loadBackupWalkFallback(slug)
	if errors.Is(err, fs.ErrNotExist) {
		return backupWalk{}, false, nil
	}
	if err != nil {
		return backupWalk{}, false, err
	}
	digest, err := walk.digest()
	return walk.backupWalk, digest == expected && err == nil, err
}

func (h *Host) stageBackupPublication(path string, marshal func(token string) ([]byte, error)) (string, error) {
	if err := h.privateBackupDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	return h.stageFileWithWriter(path, 0o600, func(file *os.File) error {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fs.ErrInvalid
		}
		// Bind the record to this atomic publication across process
		// restarts and across status directories with identical walks.
		source := fmt.Sprintf("%s:%d:%d:%d:%d", filepath.Base(file.Name()), stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec)
		data, err := marshal(fmt.Sprintf("%x", sha256.Sum256([]byte(source))))
		if err != nil {
			return err
		}
		return writeFileBytes(file, data)
	})
}

func (h *Host) publishBackupWalk(slug string, facts backupAccountFacts) error {
	path := backupStatusDir + "/" + slug + ".json"
	temp, err := h.stageBackupPublication(path, func(token string) ([]byte, error) {
		facts.WalkToken = token
		facts.RejectedRecoveryToken = ""
		return json.Marshal(facts)
	})
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp) }()
	identity, err := facts.walkIdentity()
	if err != nil {
		return err
	}
	if err := os.Rename(temp, h.path(path)); err != nil {
		return hostError(path, err)
	}
	publication := h.backupWalks[slug]
	publication.identity, publication.readiness = identity, facts.Walk.readiness()
	h.backupWalks[slug] = publication
	return nil
}
