package linux

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

// Backup installation paths, limits, and release pins.
const (
	MaxRedirects      = 5
	MaxInstallBytes   = 64 << 20
	InstallTimeout    = 10 * time.Minute
	InstallRetry      = time.Hour
	ResticPath        = "/usr/local/bin/restic"
	LitestreamPath    = "/usr/local/bin/litestream"
	BackupDir         = "/var/lib/accountsd/backup"
	ResticCache       = "/var/cache/accountsd/restic"
	LitestreamUnit    = "accountsd-litestream@.service"
	CheckSubset       = "10%"
	ResticVersion     = "0.20.0"
	ResticDigest      = "sha256:1188daded239bccb5952a01d2ad33e77a0d3dc34f9d9fc1e7fdda618d44938ef"
	LitestreamVersion = "0.5.17"
	LitestreamDigest  = "sha256:f8ca4a050095c1efbda2c4365172e61bf9d955ea0d9ac42f448b52e51819baa5"
)

// Backup traversal and restore limits and the fresh-volume marker.
const (
	ReadBatch              = 256
	MaxDepth               = 8
	MaxVisit               = 100000
	MaxDatabasesPerAccount = 100
	RestoreAttempts        = 3
	FinalSyncTimeout       = 2 * time.Minute
	ShutdownSyncTimeout    = 30 * time.Second
	RestartBefore          = 20 * time.Minute
	RestoreMarker          = "/data/.accountsd-restore"
)

const (
	resticStamp     = BackupDir + "/restic.digest"
	litestreamStamp = BackupDir + "/litestream.digest"
	resticURL       = "https://github.com/restic/restic/releases/download/v%s/restic_%s_linux_arm64.bz2"
	litestreamURL   = "https://github.com/benbjohnson/litestream/releases/download/v%s/litestream-%s-linux-arm64.tar.gz"
)

// InstallRestic installs the checked ARM64 restic executable from its bzip2 release.
func InstallRestic(ctx context.Context, env Env, version, digest string) error {
	return installBackupProgram(ctx, env, ResticPath, resticStamp, fmt.Sprintf(resticURL, version, version), digest,
		func(dst io.Writer, src io.Reader, buffer []byte) error {
			return copyExecutable(dst, bzip2.NewReader(src), buffer)
		})
}

// InstallLitestream installs the checked ARM64 litestream executable from its tar release.
func InstallLitestream(ctx context.Context, env Env, version, digest string) error {
	return installBackupProgram(ctx, env, LitestreamPath, litestreamStamp, fmt.Sprintf(litestreamURL, version, version), digest, unpackLitestream)
}

func installBackupProgram(ctx context.Context, env Env, path, stamp, url, digest string, unpack func(io.Writer, io.Reader, []byte) error) error {
	h := &Host{env: env}
	parent := filepath.Dir(path)
	if err := errors.Join(h.backupDirectory(parent, "", false), h.backupDirectory(BackupDir, "", false)); err != nil {
		return err
	}
	staging := filepath.Join(parent, "."+filepath.Base(path)+".new")
	dest, staged := h.path(path), h.path(staging)
	if err := h.recoverBackupProgram(staged, dest, stamp); err != nil {
		return err
	}
	stampBytes := []byte(digest + "\n")
	if info, err := os.Lstat(dest); err == nil && info.Mode().IsRegular() {
		if match, _ := sameContents(h.path(stamp), stampBytes); match {
			return nil
		}
	}

	var buffer [32 << 10]byte
	archive, err := h.downloadBackupProgram(ctx, url, digest, buffer[:])
	if err != nil {
		return err
	}
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()

	if err := h.stageExecutable(staging, archive, buffer[:], unpack); err != nil {
		return err
	}

	stampTemp, err := h.stageFile(stamp, stampBytes, 0o600)
	if err != nil {
		return errors.Join(err, os.Remove(staged))
	}
	defer func() { _ = os.Remove(stampTemp) }()
	return h.publishBackupProgram(staged, dest, stampTemp, stamp)
}

func (h *Host) downloadBackupProgram(ctx context.Context, url, digest string, buffer []byte) (*os.File, error) {
	if h.env.Fetch == nil {
		return nil, errors.New("no backup program download configured")
	}
	fetchCtx, cancel := context.WithTimeout(ctx, InstallTimeout)
	defer cancel()
	body, err := h.env.Fetch(fetchCtx, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	if err := h.privateBackupDir(BackupDir); err != nil {
		return nil, err
	}
	archive, err := os.CreateTemp(h.path(BackupDir), ".download-")
	if err != nil {
		return nil, hostError(BackupDir, err)
	}
	if err := storeBackupProgram(archive, body, digest, buffer); err != nil {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
		return nil, err
	}
	return archive, nil
}

func storeBackupProgram(archive *os.File, body io.Reader, digest string, buffer []byte) error {
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(archive, hash), io.LimitReader(body, MaxInstallBytes+1), buffer)
	if err != nil {
		return err
	}
	if n > MaxInstallBytes {
		return machine.ErrTooLarge
	}
	if fmt.Sprintf("sha256:%x", hash.Sum(nil)) != digest {
		return machine.ErrDigestMismatch
	}
	_, err = archive.Seek(0, io.SeekStart)
	return err
}

func (h *Host) stageExecutable(path string, archive io.Reader, buffer []byte, unpack func(io.Writer, io.Reader, []byte) error) error {
	if err := h.backupDirectory(filepath.Dir(path), "", true); err != nil {
		return err
	}
	staged := h.path(path)
	if err := os.Remove(staged); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return hostError(path, err)
	}
	f, err := os.OpenFile(filepath.Clean(staged), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return hostError(path, err)
	}
	err = unpack(f, archive, buffer)
	if err == nil {
		err = errors.Join(f.Chmod(0o755), f.Sync())
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		_ = os.Remove(staged)
		return hostError(path, err)
	}
	return nil
}

type backupInstallEntry struct {
	Device      uint64
	Inode       uint64
	Size        int64
	Modified    syscall.Timespec
	Mode        uint32
	Fingerprint string
}

func examineBackupInstallEntry(path string) (backupInstallEntry, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return backupInstallEntry{}, nil
	}
	if err != nil {
		return backupInstallEntry{}, err
	}
	if info.IsDir() {
		return backupInstallEntry{}, hostError(path, fs.ErrInvalid)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return backupInstallEntry{}, hostError(path, fs.ErrInvalid)
	}
	// Linking and renaming change Ctim, but leave Mtim unchanged.
	return backupInstallEntry{Device: stat.Dev, Inode: stat.Ino, Size: stat.Size, Modified: stat.Mtim, Mode: stat.Mode}, nil
}

func (h *Host) examineBackupInstallStamp(path string) (backupInstallEntry, error) {
	entry, err := examineBackupInstallEntry(h.path(path))
	if err == nil && entry != (backupInstallEntry{}) {
		// Keep the metadata guard for malformed, oversized or unreadable
		// stamps. Valid stamps fit the bounded read and can also be hashed.
		if data, readErr := h.readBounded(path); readErr == nil {
			entry.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}
	return entry, err
}

type backupInstallRecord struct {
	OldProgram, NewProgram backupInstallEntry
	OldStamp, NewStamp     backupInstallEntry
}

func backupInstallRecordPath(stamp string) string { return stamp + ".install" }

func (h *Host) prepareBackupPublish(staged, dest, stampTemp, stamp string) (backupInstallRecord, error) {
	record := backupInstallRecord{}
	var err error
	for path, entry := range map[string]*backupInstallEntry{dest: &record.OldProgram, staged: &record.NewProgram} {
		*entry, err = examineBackupInstallEntry(path)
		if err != nil {
			return record, err
		}
	}
	record.OldStamp, err = h.examineBackupInstallStamp(stamp)
	if err != nil {
		return record, err
	}
	record.NewStamp, err = h.examineBackupInstallStamp(filepath.Join(filepath.Dir(stamp), filepath.Base(stampTemp)))
	if err != nil {
		return record, err
	}
	if record.NewProgram == (backupInstallEntry{}) || record.NewStamp == (backupInstallEntry{}) {
		return record, fs.ErrNotExist
	}
	data, err := json.Marshal(record)
	if err != nil {
		return record, err
	}
	_, err = h.writeAtomic(backupInstallRecordPath(stamp), data, 0o600)
	return record, err
}

func (h *Host) publishBackupProgram(staged, dest, stampTemp, stamp string) error {
	record, err := h.prepareBackupPublish(staged, dest, stampTemp, stamp)
	if err != nil {
		return errors.Join(err, os.Remove(staged))
	}
	// Keep the old executable at the same staging path until the stamp commits.
	if record.OldProgram == (backupInstallEntry{}) {
		err = os.Rename(staged, dest)
	} else {
		err = exchangeBackupProgram(staged, dest)
	}
	if err == nil {
		err = os.Rename(stampTemp, h.path(stamp))
	}
	recovery := h.recoverBackupProgram(staged, dest, stamp)
	if err != nil {
		return errors.Join(err, recovery)
	}
	// Both renames succeeded. If cleanup was interrupted, the next install can
	// verify the committed pair and finish it before checking the requested pin.
	return nil
}

func (h *Host) recoverBackupProgram(staged, dest, stamp string) error {
	recordPath := backupInstallRecordPath(stamp)
	raw, err := h.readBounded(recordPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var record backupInstallRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return hostError(recordPath, err)
	}
	var missing backupInstallEntry
	if record.NewProgram == missing || record.NewStamp == missing {
		return hostError(recordPath, fs.ErrInvalid)
	}
	current, err := examineBackupInstallEntry(dest)
	if err != nil {
		return err
	}
	currentStamp, err := h.examineBackupInstallStamp(stamp)
	if err != nil {
		return err
	}
	temp, err := examineBackupInstallEntry(staged)
	if err != nil {
		return err
	}
	switch {
	case currentStamp == record.NewStamp && current == record.NewProgram:
		// Both renames committed.
		if temp != missing && temp != record.OldProgram {
			return hostError(staged, fs.ErrInvalid)
		}
	case currentStamp == record.OldStamp && current == record.OldProgram:
		// The executable was never replaced, or was already restored.
		if temp != missing && temp != record.NewProgram {
			return hostError(staged, fs.ErrInvalid)
		}
	case currentStamp == record.OldStamp && current == record.NewProgram:
		if temp != record.OldProgram {
			return hostError(staged, fs.ErrInvalid)
		}
		if err := rollbackBackupProgram(staged, dest, record.OldProgram); err != nil {
			return fmt.Errorf("restore previous executable at %s: %w", dest, err)
		}
	default:
		return hostError(recordPath, fs.ErrInvalid)
	}
	if err := os.Remove(staged); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_, err = h.remove(recordPath)
	return err
}

func rollbackBackupProgram(staged, dest string, original backupInstallEntry) error {
	if original == (backupInstallEntry{}) {
		return os.Remove(dest)
	}
	return exchangeBackupProgram(staged, dest)
}

func exchangeBackupProgram(staged, dest string) error {
	return unix.Renameat2(unix.AT_FDCWD, staged, unix.AT_FDCWD, dest, unix.RENAME_EXCHANGE)
}

func unpackLitestream(dst io.Writer, src io.Reader, buffer []byte) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	found := false
	for {
		entry, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if !found && entry.Name == "litestream" && entry.Typeflag == tar.TypeReg {
			if entry.Size > MaxInstallBytes {
				return machine.ErrTooLarge
			}
			if err := copyExecutable(dst, tr, buffer); err != nil {
				return err
			}
			found = true
		}
	}
	// Even a one-bit DEFLATE length code produces at most 258 bytes. The
	// archive's compressed size was already checked before opening it.
	const maxDeflateBytes = 8 * 258 * MaxInstallBytes
	remaining := io.LimitReader(gz, maxDeflateBytes+1)
	n, err := io.CopyBuffer(struct{ io.Writer }{io.Discard}, remaining, buffer)
	if err != nil {
		return err
	}
	if n > maxDeflateBytes {
		return machine.ErrTooLarge
	}
	if !found {
		return machine.ErrNotExecutable
	}
	return nil
}

func copyExecutable(dst io.Writer, src io.Reader, buffer []byte) error {
	const elf = "\x7fELF"
	var header [len(elf)]byte
	n, err := io.ReadFull(src, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	if _, err := dst.Write(header[:n]); err != nil {
		return err
	}
	nr, err := io.CopyBuffer(struct{ io.Writer }{dst}, io.LimitReader(src, MaxInstallBytes+1-int64(n)), buffer)
	if err != nil {
		return err
	}
	if nr+int64(n) > MaxInstallBytes {
		return machine.ErrTooLarge
	}
	if string(header[:]) != elf {
		return machine.ErrNotExecutable
	}
	return nil
}

type installAttempt struct {
	mu  sync.RWMutex
	at  time.Time
	err error
}

// Each backup worker owns its attempt. Inspect only holds the mutex to read the error.
func (a *installAttempt) ensure(ctx context.Context, env Env, version, digest string, install func(context.Context, Env, string, string) error) error {
	now := env.Now()
	a.mu.RLock()
	retry := a.err != nil && !now.Before(a.at) && now.Before(a.at.Add(InstallRetry))
	lastErr := a.err
	a.mu.RUnlock()
	if retry {
		return lastErr
	}
	err := redactBackupDiagnostic(backupDiagnosticRedactor(ctx), install(ctx, env, version, digest))
	now = env.Now()
	a.mu.Lock()
	a.at, a.err = now, err
	a.mu.Unlock()
	return err
}

func (a *installAttempt) errorText() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.err == nil {
		return ""
	}
	return a.err.Error()
}

func (h *Host) ensureRestic(ctx context.Context) error {
	return h.resticInstall.ensure(ctx, h.env, ResticVersion, ResticDigest, InstallRestic)
}

func (h *Host) ensureLitestream(ctx context.Context) error {
	return h.litestreamInstall.ensure(ctx, h.env, LitestreamVersion, LitestreamDigest, InstallLitestream)
}
