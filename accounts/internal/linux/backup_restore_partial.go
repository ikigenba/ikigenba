package linux

import (
	"cmp"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

const partialRestoreFactsPath = backupStatusDir + "/.requests.json"

func (h *Host) reportPartialRestores(requests []protocol.RestoreRequest, out *protocol.Report) {
	if len(requests) == 0 {
		return
	}
	for _, result := range readBackupFacts[[]protocol.RestoreResult](h, partialRestoreFactsPath) {
		if restoreRequested(requests, result.ID) {
			out.Restores = append(out.Restores, result)
		}
	}
}

func restoreRequested(requests []protocol.RestoreRequest, id string) bool {
	return slices.ContainsFunc(requests, func(q protocol.RestoreRequest) bool { return q.ID == id })
}

func (h *Host) prunePartialRestores(s protocol.DesiredState) error {
	kept := readBackupFacts[[]protocol.RestoreResult](h, partialRestoreFactsPath)
	prior := len(kept)
	kept = slices.DeleteFunc(kept, func(result protocol.RestoreResult) bool {
		return !restoreRequested(s.Backup.Restores, result.ID)
	})
	if len(kept) == prior {
		return nil
	}
	return saveBackupFacts(h, partialRestoreFactsPath, kept)
}

func (h *Host) backupPartialRestores(ctx context.Context, s protocol.DesiredState) error {
	requests := slices.Clone(s.Backup.Restores)
	slices.SortStableFunc(requests, func(a, b protocol.RestoreRequest) int { return cmp.Compare(a.ID, b.ID) })
	kept := readBackupFacts[[]protocol.RestoreResult](h, partialRestoreFactsPath)
	var passErr error
	for _, q := range requests {
		if slices.ContainsFunc(kept, func(r protocol.RestoreResult) bool { return r.ID == q.ID }) {
			continue
		}
		result, err := h.runPartialRestore(ctx, s, q)
		passErr = errors.Join(passErr, err)
		if result.State == "" {
			continue
		}
		kept = append(kept, result)
		slices.SortFunc(kept, func(a, b protocol.RestoreResult) int { return cmp.Compare(a.ID, b.ID) })
		passErr = errors.Join(passErr, saveBackupFacts(h, partialRestoreFactsPath, kept))
	}
	return passErr
}

func partialRestoreTarget(s protocol.DesiredState, q protocol.RestoreRequest) (protocol.User, bool) {
	if !validBackupID(q.ID) || valid.Slug(q.Account) != nil {
		return protocol.User{}, false
	}
	switch q.Kind {
	case protocol.RestoreDatabase:
		return protocol.User{}, valid.DatabasePath(q.Path) == nil
	case protocol.RestoreFiles:
		if valid.RestorePath(q.Path) == nil {
			if q.User == "" {
				return protocol.User{}, true
			}
			for _, user := range s.Users {
				if user.Name == q.User && user.Account == q.Account && valid.Name(user.Name) == nil &&
					(user.State == protocol.UserActive || user.State == protocol.UserLocked) {
					return user, true
				}
			}
		}
	}
	return protocol.User{}, false
}

func (h *Host) runPartialRestore(ctx context.Context, s protocol.DesiredState, q protocol.RestoreRequest) (protocol.RestoreResult, error) {
	result := protocol.RestoreResult{ID: q.ID, State: protocol.StepFailed}
	user, ok := partialRestoreTarget(s, q)
	if !ok {
		result.Error = machine.ErrInvalidState.Error()
		return result, nil
	}
	credentials, present := accountBackupCredentials(s.Backup, q.Account)
	account := slices.IndexFunc(s.Accounts, func(account protocol.Account) bool {
		return account.Slug == q.Account && account.State == protocol.AccountActive
	})
	if !present || account < 0 {
		return protocol.RestoreResult{}, nil
	}
	gid := s.Accounts[account].GID
	var copyName string
	var err error
	if q.Kind == protocol.RestoreDatabase {
		copyName, err = h.partialDatabaseRestore(ctx, s.Backup, q, credentials, gid)
	} else {
		repo := h.resticCommand(s.Backup, credentials, protocol.FilesPrefix(q.Account), q.Account)
		copyName, err = h.partialFilesRestore(ctx, repo, q, user, gid)
	}
	if err != nil {
		result.Error = boundBackupError(err.Error())
	} else {
		result.State, result.Copy = protocol.StepDone, copyName
	}
	if errors.Is(err, machine.ErrNoSnapshot) {
		err = nil
	}
	return result, err
}

func (h *Host) partialDatabaseRestore(ctx context.Context, b *protocol.Backup, q protocol.RestoreRequest, credentials protocol.Credentials, gid int64) (copyName string, result error) {
	base := "/srv/" + q.Account
	source := filepath.Join(base, q.Path)
	permissions := restorePermissions{gid, gid, 0o660}
	stat, err := h.restoreEntry(source)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err == nil && stat.Mode&unix.S_IFMT == unix.S_IFREG {
		permissions = restorePermissions{int64(stat.Uid), int64(stat.Gid), modeFromUnixRestore(stat.Mode)}
	}

	copyName, target, err := h.restoreCopyCandidate(q, base)
	if err != nil {
		return "", err
	}
	work, err := h.temporaryRestoreWork(ctx, "/srv")
	if err != nil {
		return "", err
	}
	defer func() { result = errors.Join(result, h.removeRestoreWork(work)) }()
	output := work + "/database"
	timestamp := q.AsOf.Add(time.Second).UTC().Format(time.RFC3339)
	if err := h.restoreDatabase(ctx, b, credentials, q.Account, q.Path, output, []string{"-timestamp", timestamp}); err != nil {
		return "", err
	}
	if err := h.requireRestoreEntry(output, unix.S_IFREG); err != nil {
		return "", err
	}
	if err := h.restoreDatabaseOwner(ctx, output, permissions); err != nil {
		return "", err
	}
	if err := h.moveRestoreEntry(output, target, false); err != nil {
		return "", err
	}
	return copyName, nil
}

func (h *Host) requireRestoreEntry(path string, kind uint32) error {
	stat, err := h.restoreEntry(path)
	if err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != kind {
		return hostError(path, fs.ErrInvalid)
	}
	return nil
}

// A files restore installs one new timestamp directory containing the
// requested relative path. Collision checks use that top-level directory.
func (h *Host) restoreCopyCandidate(q protocol.RestoreRequest, base string) (string, string, error) {
	for attempt := 1; ; attempt++ {
		copyName := machine.CopyName(q, attempt)
		entry := copyName
		if q.Kind == protocol.RestoreFiles && q.Path != "" {
			entry = strings.TrimSuffix(copyName, "/"+q.Path)
		}
		target := filepath.Join(base, entry)
		_, err := h.restoreEntry(target)
		if errors.Is(err, fs.ErrNotExist) {
			return copyName, target, nil
		}
		if err != nil {
			return "", "", err
		}
	}
}

func (h *Host) partialFilesRestore(ctx context.Context, repo backupRestic, q protocol.RestoreRequest, user protocol.User, gid int64) (copyName string, result error) {
	snapshot, err := h.findRestoreSnapshot(ctx, repo, q.Account, &q.AsOf)
	if err != nil {
		return "", err
	}
	if snapshot.id == "" {
		return "", machine.ErrNoSnapshot
	}

	base := "/srv/" + q.Account
	permissions := restorePermissions{gid, gid, 0o770 | fs.ModeSetgid}
	if user.Name != "" {
		base = "/home/" + user.Name
		permissions = restorePermissions{user.UID, user.UID, 0o700}
	}
	dir, err := h.openRestoreDir(base, false)
	if err != nil {
		return "", err
	}
	_ = dir.Close()

	work, err := h.temporaryRestoreWork(ctx, filepath.Dir(base))
	if err != nil {
		return "", err
	}
	defer func() { result = errors.Join(result, h.removeRestoreWork(work)) }()
	restored := work + "/r"
	if err := os.Mkdir(h.path(restored), 0o700); err != nil {
		return "", hostError(restored, err)
	}
	args := []string{"restore", snapshot.id + ":" + h.path(base), "--target", h.path(restored)}
	if q.Path != "" {
		args = append(args, "--include", "/"+escapeResticPattern(q.Path))
	}
	if _, err := repo.run(ctx, args, io.Discard, 0); err != nil {
		return "", err
	}
	if err := h.requireRestoreEntry(restored, unix.S_IFDIR); err != nil {
		return "", err
	}
	if err := h.restoreDatabaseOwner(ctx, restored, permissions); err != nil {
		return "", err
	}

	copyName, target, err := h.restoreCopyCandidate(q, base)
	if err != nil {
		return "", err
	}
	parent := base + "/restored"
	if err := h.requireRestoreEntry(parent, unix.S_IFDIR); errors.Is(err, fs.ErrNotExist) {
		if err := h.publishNewRestoredDir(ctx, permissions, work, target, parent); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if err := h.moveRestoreEntry(restored, target, false); err != nil {
		return "", err
	}
	return copyName, nil
}

func (h *Host) publishNewRestoredDir(ctx context.Context, permissions restorePermissions, work, target, parent string) error {
	from := work + "/restored"
	if err := os.Mkdir(h.path(from), 0o700); err != nil {
		return hostError(from, err)
	}
	if err := h.restoreDatabaseOwner(ctx, from, permissions); err != nil {
		return err
	}
	if err := h.moveRestoreEntry(work+"/r", filepath.Join(from, filepath.Base(target)), false); err != nil {
		return err
	}
	return h.moveRestoreEntry(from, parent, false)
}
