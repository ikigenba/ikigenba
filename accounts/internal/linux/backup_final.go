package linux

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

type backupOperationFacts struct {
	Final        protocol.Step
	FinalVersion int64
	Restore      backupRestoreAttempt
}

type backupFinalRuns struct {
	Accounts map[string]bool
	Host     bool
}

type backupCloseLock struct {
	version int64
	at      time.Time
}

func (h *Host) readBackupCloseLock(id string) (backupCloseLock, bool) {
	data, err := h.readBounded(closeLockPath)
	if err != nil {
		return backupCloseLock{}, false
	}
	fields := strings.Fields(string(data))
	if len(fields) != 3 || fields[0] != id {
		return backupCloseLock{}, false
	}
	version, versionErr := strconv.ParseInt(fields[1], 10, 64)
	at, timeErr := time.Parse(time.RFC3339, fields[2])
	return backupCloseLock{version: version, at: at}, versionErr == nil && timeErr == nil
}

func reportedBackupStep(id string, step protocol.Step) protocol.Step {
	switch {
	case id == "":
		return protocol.Step{}
	case step.ID != id:
		return protocol.Step{ID: id, State: protocol.StepPending}
	default:
		return step
	}
}

func (f backupOperationFacts) finalSettled(id string, lock backupCloseLock) bool {
	return f.Final.ID == id && (f.Final.State == protocol.StepDone ||
		f.Final.State == protocol.StepFailed && lock.version <= f.FinalVersion)
}

func (h *Host) backupOperationStep(id, code string) protocol.Step {
	step := protocol.Step{ID: id, State: protocol.StepDone, At: backupTime(h.env.Now()), Code: code}
	if code != "" {
		step.State = protocol.StepFailed
	}
	return step
}

func (h *Host) backupFinals(ctx context.Context, s protocol.DesiredState) (backupFinalRuns, error) {
	var runs backupFinalRuns
	id := s.Backup.Close
	if id == "" {
		return runs, nil
	}
	lock, locked := h.readBackupCloseLock(id)
	if !locked {
		return runs, nil
	}
	runs.Accounts = make(map[string]bool)
	allSettled := true
	var passErr error
	for _, slug := range backupAccountSlugs(s) {
		facts := h.readAccountBackup(slug)
		if facts.Operation.finalSettled(id, lock) {
			continue
		}
		settled, err := h.finalizeAccountBackup(ctx, s, slug, facts, lock)
		passErr = errors.Join(passErr, err)
		runs.Accounts[slug] = settled
		allSettled = allSettled && settled
	}
	if !allSettled {
		return runs, passErr
	}
	if h.readHostBackup().Operation.finalSettled(id, lock) {
		return runs, passErr
	}
	runs.Host = true
	return runs, errors.Join(passErr, h.finalizeHostBackup(ctx, s, lock))
}

func (h *Host) finalizeAccountBackup(ctx context.Context, s protocol.DesiredState, slug string, facts backupAccountFacts, lock backupCloseLock) (bool, error) {
	credentials, present := accountBackupCredentials(s.Backup, slug)
	_, walked := h.backupWalked.Load(slug)
	synced := facts.WalkRecorded && !slices.ContainsFunc(facts.Walk.Databases, func(db backupDatabase) bool {
		return db.WAL && !db.Synced.After(lock.at)
	})
	var failure string
	var backupErr error
	switch {
	case present && synced && !walked:
		// A persisted walk can prove synchronization, while the snapshot
		// still requires a walk by this process.
		return false, nil
	case present && synced:
		check := backupCheckDue(facts, h.env.Now(), s.Backup.Settings)
		backupErr = h.backupAccount(ctx, s, slug, credentials, true, check)
		failure = h.readAccountBackup(slug).Error
		if failure == "" && backupErr != nil {
			failure = boundBackupError(backupErr.Error())
		}
	case h.env.Now().Before(lock.at.Add(FinalSyncTimeout)):
		return false, nil
	case !present:
		failure = machine.NoCredentials
	default:
		failure = machine.ErrNotSynced.Error()
	}
	step := h.backupOperationStep(s.Backup.Close, backupFailureCode(failure))
	saveErr := h.updateAccountBackup(slug, func(facts *backupAccountFacts) {
		facts.Operation.Final, facts.Operation.FinalVersion, facts.Error = step, lock.version, failure
	})
	return saveErr == nil, errors.Join(backupErr, saveErr)
}

func (h *Host) finalizeHostBackup(ctx context.Context, s protocol.DesiredState, lock backupCloseLock) error {
	var failure string
	var backupErr error
	if !hostBackupCredentials(s.Backup) {
		failure = machine.NoCredentials
	} else if backupErr = h.backupHostKeys(ctx, s, true); backupErr != nil {
		failure = boundBackupError(backupErr.Error())
	}
	step := h.backupOperationStep(s.Backup.Close, backupFailureCode(failure))
	saveErr := h.updateHostBackup(func(facts *backupHostFacts) {
		facts.Operation.Final, facts.Operation.FinalVersion, facts.Error = step, lock.version, failure
	})
	return errors.Join(backupErr, saveErr)
}

func backupFailureCode(failure string) string {
	if failure != "" {
		return protocol.CodeFailed
	}
	return ""
}

func (h *Host) clearWholeRestores() error {
	if err := os.RemoveAll(h.path(RestoreMarker)); err != nil {
		return hostError(RestoreMarker, err)
	}
	if h.readHostBackup().Operation.Restore.Step.ID == "" {
		return nil
	}

	status, err := os.Open(h.path(backupStatusDir))
	if err != nil {
		return hostError(backupStatusDir, err)
	}
	defer func() { _ = status.Close() }()
	for {
		names, err := status.Readdirnames(ReadBatch)
		for _, name := range names {
			slug, ok := strings.CutSuffix(name, ".json")
			if !ok || h.readAccountBackup(slug).Operation.Restore.Step.ID == "" {
				continue
			}
			if err := h.updateAccountBackup(slug, func(f *backupAccountFacts) {
				f.Operation.Restore = backupRestoreAttempt{}
			}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return hostError(backupStatusDir, err)
		}
	}
	return h.updateHostBackup(func(f *backupHostFacts) {
		f.Operation.Restore = backupRestoreAttempt{}
	})
}
