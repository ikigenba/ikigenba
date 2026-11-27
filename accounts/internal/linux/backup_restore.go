package linux

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

type backupRestoreAttempt struct {
	Step    protocol.Step
	Error   string
	Runs    int
	Checked bool
	Moved   []string
}

func restoreAttemptFor(r backupRestoreAttempt, id string) backupRestoreAttempt {
	if r.Step.ID != id {
		return backupRestoreAttempt{Step: reportedBackupStep(id, protocol.Step{})}
	}
	return r
}

func restoreFinished(step protocol.Step) bool {
	return step.State == protocol.StepDone || step.State == protocol.StepFailed
}

func (h *Host) finishRestoreAttempt(r *backupRestoreAttempt, err error) {
	switch {
	case err == nil:
		r.Error = ""
		r.Step = h.backupOperationStep(r.Step.ID, "")
	case errors.Is(err, errRestoreTargetOccupied):
		r.Step = h.backupOperationStep(r.Step.ID, protocol.CodeNotEmpty)
	default:
		r.Error = boundBackupError(err.Error())
		r.Runs++
		if r.Runs >= RestoreAttempts {
			r.Step = h.backupOperationStep(r.Step.ID, protocol.CodeFailed)
		}
	}
}

func validHostRestore(r *protocol.HostRestore) bool {
	if len(r.Accounts) > protocol.MaxBackupAccounts {
		return false
	}
	seen := make(map[string]bool, len(r.Accounts))
	for _, account := range r.Accounts {
		if account.GID < 1000 || valid.Slug(account.Slug) != nil || seen[account.Slug] {
			return false
		}
		seen[account.Slug] = true
		for _, path := range account.Databases {
			if valid.DatabasePath(path) != nil {
				return false
			}
		}
	}
	return true
}

func (h *Host) backupWholeRestore(ctx context.Context, s protocol.DesiredState) error {
	restore := s.Backup.Restore
	if !h.restoreReady(restore.ID) {
		return nil
	}
	host := restoreAttemptFor(h.readHostBackup().Operation.Restore, restore.ID)
	if host.Step.State == protocol.StepFailed {
		return nil
	}
	marker, err := h.readRestoreMarker()
	switch {
	case err != nil || string(marker) != "restore "+restore.ID+"\n":
		host.Step = h.backupOperationStep(restore.ID, protocol.CodeNotEmpty)
		host.Error = ""
		return h.saveHostRestore(host)
	case !validHostRestore(restore):
		host.Step = h.backupOperationStep(restore.ID, protocol.CodeFailed)
		host.Error = machine.ErrInvalidState.Error()
		return h.saveHostRestore(host)
	}
	if host.Step.State != protocol.StepDone {
		if !hostBackupCredentials(s.Backup) {
			host.Error = machine.NoCredentials
			return h.saveHostRestore(host)
		}
		repo := h.resticCommand(s.Backup, s.Backup.Host, s.Backup.HostPrefix, ".host")
		repo.restoreOnly = true
		attemptErr := h.restoreHostKeys(ctx, repo)
		h.finishRestoreAttempt(&host, attemptErr)
		if err := errors.Join(attemptErr, h.saveHostRestore(host)); err != nil || host.Step.State != protocol.StepDone {
			return err
		}
	}
	var passErr error
	for _, account := range restore.Accounts {
		passErr = errors.Join(passErr, h.runWholeAccountRestore(ctx, s.Backup, account))
	}
	return passErr
}

func (h *Host) readRestoreMarker() ([]byte, error) {
	if err := h.backupDirectory(DataDir, "", false); err != nil {
		return nil, err
	}
	return h.readBounded(RestoreMarker)
}

func (h *Host) saveHostRestore(r backupRestoreAttempt) error {
	return h.updateHostBackup(func(f *backupHostFacts) {
		f.Operation.Restore = r
	})
}

func (h *Host) saveAccountRestore(ctx context.Context, slug string, r backupRestoreAttempt) error {
	return h.updateAccountBackup(ctx, slug, func(f *backupAccountFacts) {
		f.Operation.Restore = r
	})
}

func (h *Host) restoreHostKeys(ctx context.Context, repo backupRestic) error {
	snapshot, err := h.findRestoreSnapshot(ctx, repo, "", nil)
	if err != nil || snapshot.id == "" {
		return err
	}
	path := h.path(HostKeyDir)
	_, err = repo.run(ctx, []string{"restore", snapshot.id + ":" + path, "--target", path}, io.Discard, 0)
	return err
}

func (h *Host) runWholeAccountRestore(ctx context.Context, b *protocol.Backup, account protocol.RestoreAccount) error {
	attempt := restoreAttemptFor(h.readAccountBackupForPass(ctx, account.Slug).Operation.Restore, b.Restore.ID)
	if restoreFinished(attempt.Step) {
		return nil
	}
	credentials, present := accountBackupCredentials(b, account.Slug)
	if !present {
		attempt.Error = machine.NoCredentials
		return h.saveAccountRestore(ctx, account.Slug, attempt)
	}

	repo := h.resticCommand(b, credentials, protocol.FilesPrefix(account.Slug), account.Slug)
	repo.restoreOnly = true
	err := h.restoreWholeAccount(ctx, b, account, credentials, repo, &attempt)
	h.finishRestoreAttempt(&attempt, err)
	if errors.Is(err, errRestoreTargetOccupied) {
		err = nil
	}
	return errors.Join(err, h.saveAccountRestore(ctx, account.Slug, attempt))
}

type restoreSnapshot struct {
	id       string
	at       time.Time
	homes    []string
	manifest bool
}

func (h *Host) findRestoreSnapshot(ctx context.Context, repo backupRestic, slug string, asOf *time.Time) (restoreSnapshot, error) {
	args := []string{"snapshots"}
	if slug != "" {
		args = append(args, "--host", slug)
	}
	args = append(args, "--json")
	var result, current restoreSnapshot
	homePrefix := h.path("/home") + "/"
	manifest := h.path(BackupDir + "/manifest/" + slug)
	fields := []string{"time", "id"}
	emit := func(fields map[string]string) {
		at, err := time.Parse(time.RFC3339Nano, fields["time"])
		id := fields["id"]
		if err == nil && id != "" && (asOf == nil && (result.id == "" || at.After(result.at)) ||
			asOf != nil && backupTime(at).Equal(*asOf)) {
			current.id, current.at = id, at
			result = current
		}
		current = restoreSnapshot{}
	}
	var listing *backupJSONArray
	if slug != "" && asOf == nil {
		listing = newBackupJSONArrayWithStrings(fields, "paths", func(value string) {
			if value == manifest {
				current.manifest = true
			}
			if home, ok := strings.CutPrefix(value, homePrefix); ok && valid.Name(home) == nil {
				// Repository snapshots carry paths from a state's bounded user set.
				if !slices.Contains(current.homes, home) && len(current.homes) < valid.MaxUsersPerHost {
					current.homes = append(current.homes, home)
				}
			}
		}, emit)
	} else {
		listing = newBackupJSONArray(fields, emit)
	}
	status, err := repo.run(ctx, args, listing, 0, 10)
	listing.finish()
	if err != nil || status.ExitCode == 10 {
		return restoreSnapshot{}, err
	}
	return result, nil
}

type wholeRestoreTarget struct {
	path string
	work string
}

func wholeRestoreTargets(slug string, homes []string) []wholeRestoreTarget {
	targets := make([]wholeRestoreTarget, 0, len(homes)+1)
	targets = append(targets, wholeRestoreTarget{path: "/srv/" + slug, work: "/srv/" + restoreWorkPrefix + slug})
	for _, home := range homes {
		targets = append(targets, wholeRestoreTarget{path: "/home/" + home, work: "/home/" + restoreWorkPrefix + home})
	}
	return targets
}

func (h *Host) restoreWholeAccount(ctx context.Context, b *protocol.Backup, account protocol.RestoreAccount, credentials protocol.Credentials, repo backupRestic, attempt *backupRestoreAttempt) error {
	snapshot, err := h.findRestoreSnapshot(ctx, repo, account.Slug, nil)
	if err != nil {
		return err
	}
	targets := wholeRestoreTargets(account.Slug, snapshot.homes)
	if !attempt.Checked {
		for _, target := range targets {
			if err := h.checkRestoreTarget(target.path); err != nil {
				return err
			}
		}
		attempt.Checked = true
		if err := h.saveAccountRestore(ctx, account.Slug, *attempt); err != nil {
			return err
		}
	}
	if snapshot.id == "" {
		return nil
	}
	for _, target := range targets {
		if !slices.Contains(attempt.Moved, target.path) {
			if err := h.restoreTargetToWork(ctx, repo, snapshot.id, target); err != nil {
				return err
			}
		}
	}

	if !slices.Contains(attempt.Moved, targets[0].path) {
		manifest, err := h.restoreManifest(ctx, repo, snapshot, account.Slug)
		if err != nil {
			return err
		}
		if err := h.restoreWholeDatabases(ctx, b, account, credentials, manifest, targets[0].work); err != nil {
			return err
		}
	}
	for _, target := range targets {
		if slices.Contains(attempt.Moved, target.path) {
			continue
		}
		moved, err := h.moveWholeRestoreTarget(target)
		if err != nil {
			return err
		}
		if moved {
			attempt.Moved = append(attempt.Moved, target.path)
			if err := h.saveAccountRestore(ctx, account.Slug, *attempt); err != nil {
				return err
			}
		}
	}
	for _, target := range targets {
		if err := h.removeRestoreWork(target.work); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) restoreTargetToWork(ctx context.Context, repo backupRestic, snapshot string, target wholeRestoreTarget) error {
	if err := h.prepareRestoreWork(ctx, target.work); err != nil {
		return err
	}
	args := []string{
		"restore", snapshot + ":" + h.path(filepath.Dir(target.path)), "--target", h.path(target.work),
		"--include", "/" + filepath.Base(target.path), "--overwrite", "if-changed",
	}
	_, err := repo.run(ctx, args, io.Discard, 0)
	return err
}

func (h *Host) moveWholeRestoreTarget(target wholeRestoreTarget) (bool, error) {
	source := filepath.Join(target.work, filepath.Base(target.path))
	stat, err := h.restoreEntry(source)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return false, hostError(source, fs.ErrInvalid)
	}
	err = h.moveRestoreEntry(source, target.path, true)
	return err == nil, err
}

func (h *Host) operationBackupReport(s protocol.DesiredState, out *protocol.Report, host backupHostFacts, accounts map[string]backupAccountFacts) {
	if s.Backup == nil {
		return
	}
	out.HostBackup.Final = reportedBackupStep(s.Backup.Close, host.Operation.Final)
	h.reportPartialRestores(s.Backup.Restores, out)
	if restore := s.Backup.Restore; restore != nil {
		hostAttempt := restoreAttemptFor(host.Operation.Restore, restore.ID)
		out.HostBackup.Restored = hostAttempt.Step
		if hostAttempt.Error != "" || backupReady(s.Backup) && hostBackupCredentials(s.Backup) {
			out.HostBackup.Error = hostAttempt.Error
		}
		for i, current := range out.Backups {
			attempt := restoreAttemptFor(accounts[current.Account].Operation.Restore, restore.ID)
			out.Backups[i] = protocol.AccountBackup{
				Account: current.Account, Restored: attempt.Step, Error: attempt.Error,
			}
		}
		return
	}
	for i := range out.Backups {
		account := accounts[out.Backups[i].Account]
		out.Backups[i].Final = reportedBackupStep(s.Backup.Close, account.Operation.Final)
	}
}
