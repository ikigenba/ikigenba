package linux

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

func (h *Host) restoreDatabase(ctx context.Context, b *protocol.Backup, credentials protocol.Credentials, slug, path, output string, flags []string) (result error) {
	work := filepath.Dir(output)
	dir, err := h.openRestoreDir(work, false)
	if err != nil {
		return err
	}
	_ = dir.Close()
	config := filepath.Join(work, "litestream.yml")
	if _, err := h.writeAtomic(config, backupLitestreamYAML(b, slug, []string{path}), 0o600); err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(h.path(config)); err != nil {
			result = errors.Join(result, hostError(config, err))
		}
	}()

	args := []string{"restore", "-config", h.path(config), "-no-expand-env"}
	args = append(args, flags...)
	args = append(args, "-o", h.path(output), h.path("/srv/"+slug+"/"+path))
	_, err = h.litestreamCommand(ctx, credentials, b.Region, args, io.Discard)
	return err
}

func parseRestoreManifest(line string) (string, restorePermissions, bool) {
	fields := strings.SplitN(line, " ", 4)
	if len(fields) != 4 || len(fields[2]) != 4 || valid.DatabasePath(fields[3]) != nil {
		return "", restorePermissions{}, false
	}
	if !onlyBytes(fields[0], "0123456789") || !onlyBytes(fields[1], "0123456789") || !onlyBytes(fields[2], "01234567") {
		return "", restorePermissions{}, false
	}
	uid, uidErr := strconv.ParseUint(fields[0], 10, 32)
	gid, gidErr := strconv.ParseUint(fields[1], 10, 32)
	if uidErr != nil || gidErr != nil || uid == 1<<32-1 || gid == 1<<32-1 {
		return "", restorePermissions{}, false
	}
	mode, _ := strconv.ParseUint(fields[2], 8, 12)
	return fields[3], restorePermissions{int64(uid), int64(gid), modeFromUnixRestore(uint32(mode))}, true
}

func (h *Host) restoreManifest(ctx context.Context, repo backupRestic, snapshot restoreSnapshot, slug string) (map[string]restorePermissions, error) {
	manifest := make(map[string]restorePermissions)
	if !snapshot.manifest {
		return manifest, nil
	}
	lines := 0
	writer := lineWriter{limit: MaxJournalLine, line: make([]byte, 0, MaxJournalLine), emit: func(line []byte) {
		if lines >= MaxDatabasesPerAccount {
			return
		}
		lines++
		path, permissions, ok := parseRestoreManifest(string(line))
		if _, exists := manifest[path]; ok && !exists {
			manifest[path] = permissions
		}
	}}
	_, err := repo.run(ctx, []string{"dump", snapshot.id, h.path(BackupDir + "/manifest/" + slug)}, &writer, 0)
	return manifest, err
}

func (h *Host) restoreWholeDatabases(ctx context.Context, b *protocol.Backup, account protocol.RestoreAccount, credentials protocol.Credentials, manifest map[string]restorePermissions, work string) error {
	paths := make([]string, 0, len(account.Databases)+len(manifest))
	paths = append(paths, account.Databases...)
	for path := range manifest {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range slices.Compact(paths) {
		permissions, ok := manifest[path]
		if !ok {
			permissions = restorePermissions{account.GID, account.GID, 0o660}
		}
		if err := h.restoreWholeDatabase(ctx, b, credentials, account.Slug, path, work, permissions); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) restoreWholeDatabase(ctx context.Context, b *protocol.Backup, credentials protocol.Credentials, slug, path, work string, permissions restorePermissions) error {
	destination := filepath.Join(work, slug, path)
	_, err := h.restoreEntry(destination)
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	output := filepath.Join(work, ".database")
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := h.removeRestoreWork(output + suffix); err != nil {
			return err
		}
	}
	if err := h.restoreDatabase(ctx, b, credentials, slug, path, output, []string{"-if-replica-exists"}); err != nil {
		return err
	}
	stat, err := h.restoreEntry(output)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case stat.Mode&unix.S_IFMT != unix.S_IFREG:
		return nil
	}
	if err := h.restoreDatabaseOwner(ctx, output, permissions); err != nil {
		return err
	}
	dir, err := h.openRestoreDir(filepath.Dir(destination), true)
	if err != nil {
		return err
	}
	_ = dir.Close()
	return h.moveRestoreEntry(output, destination, false)
}
