package store_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func readyHost(t *testing.T, s *store.Store) store.Host {
	t.Helper()
	h, op := create(t, s)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{InstanceID: h.Name + "-instance",
		DataVolume: h.Name + "-volume", AllocationID: h.Name + "-allocation"}), nil)
	return value(t, result(s.Host(ctx, h.Name)))
}

func closeHost(t *testing.T, s *store.Store, host string) (store.Host, store.Operation) {
	t.Helper()
	h, op, err := s.CloseHost(ctx, host, caller)
	checkErr(t, err, nil)
	return h, op
}

func openHost(t *testing.T, s *store.Store, host string) (store.Host, store.Operation) {
	t.Helper()
	h, op, err := s.OpenHost(ctx, host, caller)
	checkErr(t, err, nil)
	return h, op
}

func replaceHost(t *testing.T, s *store.Store, host, mode string, target store.Target) (store.Host, store.Operation, []string) {
	t.Helper()
	h, op, cancelled, err := s.ReplaceHost(ctx, host, mode, target, caller)
	checkErr(t, err, nil)
	return h, op, cancelled
}

// R-5YUW-XHMC R-ZCEG-5W4O R-5NVT-HJY3 R-ZJPU-GIKU R-ZHA1-OZ3G
// R-5RJI-MV66 R-GT8P-VIM1 R-6EPL-WI9D R-4UM8-B25F
func TestCloseRecordsStepsAndKeepsLockThroughFailureRetryAndFinish(t *testing.T) {
	_, s, now := fixture(t)
	h := readyHost(t, s)
	observe2(s.RetryHost(ctx, h.Name, identity.Caller{})).assertZero(t, store.ErrNotFailed)
	_, placing := place(t, s, "acme", h.Name)
	place(t, s, "zeta", h.Name)
	place(t, s, "removing", h.Name)
	remove(t, s, "removing")
	user := value(t, result(s.AddUser(ctx, "acme", "staff", firstKey, caller)))
	locked := value(t, result(s.AddUser(ctx, "acme", "locked-staff", firstKey, caller)))
	value(t, result(s.Lock(ctx, "acme", locked.Name)))
	h = value(t, result(s.Host(ctx, h.Name)))
	initial := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, initial.Backup, &protocol.Backup{HostPrefix: protocol.HostPrefix(h.Name, h.Seq), Restores: []protocol.RestoreRequest{}})
	for _, invalid := range []identity.Caller{{}, {UserID: strings.Repeat("x", valid.MaxUserIDBytes+1)}} {
		observe2(s.CloseHost(ctx, h.Name, invalid)).assertZero(t, store.ErrUnreachable)
	}
	observe2(s.CloseHost(ctx, "absent", caller)).assertZero(t, store.ErrNotFound)

	watch, release := s.Watch(h.Name)
	defer release()
	closing, op := closeHost(t, s, h.Name)
	closed(t, watch, true)
	want := h
	want.State, want.Operation, want.Version = store.HostClosing, op.ID, h.Version+1
	eq(t, closing, want)
	eq(t, op.Kind, store.OpClose)
	eq(t, op.Target, store.Target{})
	eq(t, op.ResumeState, store.HostRunning)
	eq(t, op.Host, h.Name)
	eq(t, value(t, result(s.Host(ctx, h.Name))), closing)
	pending := func(name string, at time.Time) store.BackupStep {
		return store.BackupStep{Account: name, State: protocol.StepPending, At: at.UTC().Truncate(time.Second)}
	}
	steps := []store.BackupStep{pending("", *now), pending("acme", *now), pending("zeta", *now)}
	eq(t, value(t, result(s.BackupSteps(ctx, op.ID))), steps)
	eq(t, value(t, result(s.BackupSteps(ctx, placing.ID))), []store.BackupStep{})
	observe(s.BackupSteps(ctx, "missing")).assertZero(t, store.ErrNotFound)
	observe2(s.CloseHost(ctx, h.Name, caller)).assertZero(t, store.ErrBusy)
	observe2(s.StartHostOp(ctx, h.Name, store.OpStop, store.Target{}, caller)).assertZero(t, store.ErrBusy)
	observe2(s.StartHostOp(ctx, h.Name, store.OpDelete, store.Target{}, caller)).assertZero(t, store.ErrUnreachable)
	observe2(s.DeleteHost(ctx, h.Name, false, false, caller)).assertZero(t, store.ErrBusy)
	overlay := value(t, result(s.Desired(ctx, h.Name)))
	for i := range initial.Users {
		initial.Users[i].State = protocol.UserLocked
	}
	initial.Backup.Close, initial.Version = op.ID, closing.Version
	eq(t, overlay, initial)
	eq(t, value(t, result(s.User(ctx, "acme", user.Name))), user)

	failedAt := now.Add(-time.Minute)
	failure := protocol.Step{ID: op.ID, State: protocol.StepFailed, At: failedAt, Code: "backup_failed"}
	_, err := s.RecordReport(ctx, h.Name, protocol.Report{
		Backups:    []protocol.AccountBackup{{Account: "acme", Final: failure, Error: "refused"}},
		HostBackup: protocol.HostBackup{Final: protocol.Step{ID: op.ID, State: protocol.StepDone, At: *now}},
	}, *now)
	checkErr(t, err, nil)
	steps[0].State = protocol.StepDone
	steps[1] = store.BackupStep{Account: "acme", State: protocol.StepFailed, At: failedAt.UTC().Truncate(time.Second), Code: failure.Code, Reason: "refused"}
	eq(t, value(t, result(s.BackupSteps(ctx, op.ID))), steps)
	checkErr(t, s.Fail(ctx, op.ID, "refused"), nil)
	afterFail := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, afterFail.Backup.Close, "")
	eq(t, afterFail.Users, overlay.Users)
	observe2(s.CloseHost(ctx, h.Name, caller)).assertZero(t, store.ErrHostFailed)
	observe2(s.RetryHost(ctx, "", identity.Caller{})).assertZero(t, store.ErrNotFound)
	failedHost := value(t, result(s.Host(ctx, h.Name)))
	failedOp := value(t, result(s.Operation(ctx, op.ID)))
	oversized := identity.Caller{UserID: strings.Repeat("x", valid.MaxUserIDBytes+1)}
	observe2(s.RetryHost(ctx, h.Name, oversized)).assertZero(t, store.ErrUnreachable)
	eq(t, value(t, result(s.Host(ctx, h.Name))), failedHost)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), failedOp)
	*now = now.Add(time.Minute)
	resumed, same, err := s.RetryHost(ctx, h.Name, identity.Caller{})
	checkErr(t, err, nil)
	eq(t, same.Caller, identity.Caller{})
	eq(t, value(t, result(s.Operation(ctx, op.ID))), same)
	eq(t, same.ID, op.ID)
	eq(t, same.Generation, op.Generation)
	eq(t, same.ResumeState, op.ResumeState)
	eq(t, same.Finished, time.Time{})
	eq(t, resumed.State, store.HostClosing)
	eq(t, resumed.Version, closing.Version+1)
	steps[1] = pending("acme", *now)
	eq(t, value(t, result(s.BackupSteps(ctx, op.ID))), steps)
	eq(t, value(t, result(s.Desired(ctx, h.Name))).Backup.Close, op.ID)
	_, err = s.RecordReport(ctx, h.Name, protocol.Report{Backups: []protocol.AccountBackup{{Account: "acme", Final: failure}}}, *now)
	checkErr(t, err, nil)
	eq(t, value(t, result(s.BackupSteps(ctx, op.ID))), steps)
	failure.State = protocol.StepDone
	_, err = s.RecordReport(ctx, h.Name, protocol.Report{Backups: []protocol.AccountBackup{{Account: "acme", Final: failure}}}, *now)
	checkErr(t, err, nil)
	eq(t, value(t, result(s.BackupSteps(ctx, op.ID)))[1].State, protocol.StepDone)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	finished := value(t, result(s.Host(ctx, h.Name)))
	eq(t, finished.State, store.HostRunning)
	eq(t, finished.Closed, now.UTC().Truncate(time.Second))
	eq(t, finished.RunningSince, finished.Closed)
	eq(t, finished.Version, resumed.Version)
	retained, noOp := closeHost(t, s, h.Name)
	eq(t, retained, finished)
	eq(t, noOp, store.Operation{})
	final := value(t, result(s.BackupOf(ctx, "acme")))
	eq(t, final.Final, protocol.StepDone)
	eq(t, final.FinalAt, failedAt.UTC().Truncate(time.Second))
}

// R-NHIK-M46H R-62IM-2SUF R-4TEB-XAEQ R-GT8P-VIM1 R-5RJI-MV66
func TestOpenCancelsRunningCloseOrClearsClosedTime(t *testing.T) {
	_, s, now := fixture(t)
	h := readyHost(t, s)
	place(t, s, "acme", h.Name)
	value(t, result(s.AddUser(ctx, "acme", "staff", firstKey, caller)))
	observe2(s.OpenHost(ctx, h.Name, caller)).assertZero(t, store.ErrNotClosed)
	observe2(s.OpenHost(ctx, h.Name, identity.Caller{})).assertZero(t, store.ErrUnreachable)
	observe2(s.OpenHost(ctx, "missing", caller)).assertZero(t, store.ErrNotFound)
	closing, closeOp := closeHost(t, s, h.Name)
	*now = now.Add(time.Minute)
	opening, op := openHost(t, s, h.Name)
	eq(t, opening.State, store.HostOpening)
	eq(t, opening.Closed, time.Time{})
	eq(t, opening.Version, closing.Version+1)
	eq(t, op.Supersedes, closeOp.ID)
	eq(t, op.ResumeState, store.HostClosing)
	eq(t, op.Kind, store.OpOpen)
	eq(t, op.Target, store.Target{})
	cancelled := value(t, result(s.Operation(ctx, closeOp.ID)))
	eq(t, cancelled.Status, store.OpCancelled)
	eq(t, cancelled.Finished, now.UTC().Truncate(time.Second))
	after := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, after.Backup.Close, "")
	eq(t, after.Users[0].State, protocol.UserActive)
	observe2(s.OpenHost(ctx, h.Name, caller)).assertZero(t, store.ErrBusy)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)

	_, closeOp = closeHost(t, s, h.Name)
	checkErr(t, s.Fail(ctx, closeOp.ID, "failed"), nil)
	_, op = openHost(t, s, h.Name)
	eq(t, op.Supersedes, "")
	eq(t, value(t, result(s.Operation(ctx, closeOp.ID))).Status, store.OpFailed)
	checkErr(t, s.Fail(ctx, op.ID, "open failed"), nil)
	eq(t, value(t, result(s.Desired(ctx, h.Name))).Users[0].State, protocol.UserActive)
	_, retry, err := s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, nil)
	eq(t, retry.ID, op.ID)
	eq(t, value(t, result(s.Host(ctx, h.Name))).State, store.HostOpening)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)

	_, closeOp = closeHost(t, s, h.Name)
	checkErr(t, s.Finish(ctx, closeOp.ID, store.Update{}), nil)
	before := value(t, result(s.Host(ctx, h.Name)))
	_, stop := start(t, s, h.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stop.ID, store.Update{}), nil)
	observe2(s.OpenHost(ctx, h.Name, caller)).assertZero(t, store.ErrNotRunning)
	stopped, noOp := closeHost(t, s, h.Name)
	eq(t, stopped.State, store.HostStopped)
	eq(t, stopped.Closed, before.Closed)
	eq(t, noOp, store.Operation{})
	_, starting := start(t, s, h.Name, store.OpStart, store.Target{})
	checkErr(t, s.Finish(ctx, starting.ID, store.Update{}), nil)
	_, resize := start(t, s, h.Name, store.OpResize, store.Target{RootGB: h.RootGB + 1})
	checkErr(t, s.Finish(ctx, resize.ID, store.Update{RootGB: h.RootGB + 1}), nil)
	eq(t, value(t, result(s.Host(ctx, h.Name))).Closed, before.Closed)
	opening, op = openHost(t, s, h.Name)
	eq(t, opening.Closed, time.Time{})
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
}

// R-ZDMC-JNVD R-ZHA1-OZ3G R-6BLI-POCE
func TestReportSettlesOnlyEligibleBackupStepsAndBoundsTheReason(t *testing.T) {
	_, s, now := fixture(t)
	h := readyHost(t, s)
	place(t, s, "initial", h.Name)
	place(t, s, "leaving", h.Name)
	_, closeOp := closeHost(t, s, h.Name)
	place(t, s, "late", h.Name)
	remove(t, s, "leaving")
	tooLong := strings.Repeat("<🙂", protocol.MaxBackupError)
	wantReason := strings.Repeat("<🙂", (protocol.MaxBackupError-2)/10)
	step := protocol.Step{ID: closeOp.ID, State: protocol.StepDone, At: *now, Code: "completed"}
	wrong := step
	wrong.ID = "unknown-operation"
	_, err := s.RecordReport(ctx, h.Name, protocol.Report{
		Backups: []protocol.AccountBackup{
			{Account: "initial", Final: wrong},
			{Account: "late", Final: step, Error: tooLong},
			{Account: "leaving", Final: step},
		},
		HostBackup: protocol.HostBackup{Final: step},
	}, *now)
	checkErr(t, err, nil)
	steps := value(t, result(s.BackupSteps(ctx, closeOp.ID)))
	eq(t, len(steps), 3)
	eq(t, steps[0].Account, "")
	eq(t, steps[0].State, protocol.StepDone)
	eq(t, steps[1].Account, "initial")
	eq(t, steps[1].State, protocol.StepPending)
	eq(t, steps[2], store.BackupStep{Account: "late", State: protocol.StepDone, At: now.UTC().Truncate(time.Second),
		Code: step.Code, Reason: wantReason})
	encoded := value(t, result(json.Marshal(steps[2].Reason)))
	if len(encoded) > protocol.MaxBackupError {
		t.Fatalf("encoded reason is %d bytes", len(encoded))
	}
	step.State = protocol.StepFailed
	step.Code = "failed"
	_, err = s.RecordReport(ctx, h.Name, protocol.Report{Backups: []protocol.AccountBackup{{Account: "late", Final: step}}}, *now)
	checkErr(t, err, nil)
	eq(t, value(t, result(s.BackupSteps(ctx, closeOp.ID))), steps)
}

// R-666B-842I R-67E7-LVT7 R-68M3-ZNJW R-ZG25-B7CR R-5SRF-0MWV
// R-4VU4-OTW4 R-6B1W-R71A R-W1AE-Y0G9 R-W2IB-BS6Y R-VSR4-9M9E R-56T8-4RKD
// R-4X21-2LMT R-4UM8-B25F R-5RJI-MV66
func TestReplacingKeepsOldResourcesAndWithholdsUntilAdmission(t *testing.T) {
	_, s, now := fixture(t)
	h := readyHost(t, s)
	oldOperation := h.Operation
	alpha, _ := place(t, s, "alpha", h.Name)
	beta, _ := place(t, s, "beta", h.Name)
	place(t, s, "removing", h.Name)
	remove(t, s, "removing")
	value(t, result(s.AddUser(ctx, "alpha", "staff", firstKey, caller)))
	_, closeOp := closeHost(t, s, h.Name)
	checkErr(t, s.Finish(ctx, closeOp.ID, store.Update{}), nil)
	_, err := s.RecordReport(ctx, h.Name, protocol.Report{Backups: []protocol.AccountBackup{
		{Account: "beta", Databases: []protocol.Database{{Path: "data/three"}}},
		{Account: "alpha", Databases: []protocol.Database{{Path: "data/two"}, {Path: "data/one"}}},
		{Account: "removing", Databases: []protocol.Database{{Path: "data/keep-out"}}},
	}}, *now)
	checkErr(t, err, nil)
	h = value(t, result(s.Host(ctx, h.Name)))
	before := value(t, result(s.Desired(ctx, h.Name)))

	observe3(s.ReplaceHost(ctx, "missing", store.ReplaceBackup, store.Target{}, caller)).assertZero(t, store.ErrNotFound)
	observe3(s.ReplaceHost(ctx, h.Name, "unknown", store.Target{}, caller)).assertZero(t, store.ErrUnreachable)
	observe3(s.ReplaceHost(ctx, h.Name, store.ReplaceBackup, store.Target{}, identity.Caller{})).assertZero(t, store.ErrUnreachable)
	observe3(s.ReplaceHost(ctx, h.Name, store.ReplaceVolume, store.Target{DataGB: 1}, caller)).assertZero(t, store.ErrUnreachable)
	observe(s.Replacement(ctx, oldOperation)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.Retire(ctx, oldOperation), store.ErrNotFound)
	checkErr(t, s.Admit(ctx, oldOperation), store.ErrNotFound)
	observe(s.KeptVolume(ctx, h.DataVolume)).assertZero(t, store.ErrNotFound)

	replacing, op, cancelled := replaceHost(t, s, h.Name, store.ReplaceBackup, store.Target{DataGB: 30})
	eq(t, cancelled, []string(nil))
	wantReplacement := store.Replacement{Operation: op.ID, Mode: store.ReplaceBackup, OldInstance: h.InstanceID, OldVolume: h.DataVolume}
	eq(t, value(t, result(s.Replacement(ctx, op.ID))), wantReplacement)
	eq(t, op.Target, store.Target{Type: h.Type, RootGB: h.RootGB, DataGB: 30})
	eq(t, op.ResumeState, store.HostRunning)
	eq(t, replacing.State, store.HostReplacing)
	eq(t, replacing.Closed, h.Closed)
	eq(t, replacing.Version, h.Version)
	eq(t, replacing.Replace, store.ReplaceBackup)
	eq(t, value(t, result(s.Host(ctx, h.Name))), replacing)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), before)
	observe3(s.ReplaceHost(ctx, h.Name, store.ReplaceBackup, store.Target{}, caller)).assertZero(t, store.ErrBusy)
	checkErr(t, s.Advance(ctx, op.ID, store.Update{HostState: store.HostRunning}), store.ErrUnreachable)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{HostState: store.HostRunning}), store.ErrUnreachable)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)

	watch, release := s.Watch(h.Name)
	defer release()
	checkErr(t, s.Retire(ctx, op.ID), nil)
	closed(t, watch, true)
	wantReplacement.Retired = true
	eq(t, value(t, result(s.Replacement(ctx, op.ID))), wantReplacement)
	withheld := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, withheld.Version, before.Version+1)
	eq(t, withheld.Accounts, []protocol.Account{})
	eq(t, withheld.Users, []protocol.User{})
	restore := &protocol.HostRestore{ID: op.ID, Accounts: []protocol.RestoreAccount{
		{Slug: "alpha", GID: alpha.GID, Databases: []string{"data/one", "data/two"}},
		{Slug: "beta", GID: beta.GID, Databases: []string{"data/three"}},
	}}
	eq(t, withheld.Backup.Restore, restore)
	checkErr(t, s.Retire(ctx, op.ID), nil)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), withheld)
	*now = now.Add(time.Minute)
	checkErr(t, s.Advance(ctx, op.ID, store.Update{Step: "restore", Generation: 2, HostState: store.HostRestoring,
		InstanceID: "replacement-instance", DataVolume: "replacement-volume", DataGB: 30}), nil)
	checkpoint := value(t, result(s.Operation(ctx, op.ID)))
	restoring := value(t, result(s.Host(ctx, h.Name)))
	eq(t, checkpoint.Generation, 2)
	eq(t, checkpoint.Step, "restore")
	eq(t, checkpoint.StepStarted, now.UTC().Truncate(time.Second))
	eq(t, restoring.State, store.HostRestoring)
	eq(t, restoring.Closed, h.Closed)
	eq(t, restoring.Version, withheld.Version+1)
	eq(t, restoring.Replace, store.ReplaceBackup)
	kept := store.KeptVolume{Volume: h.DataVolume, Host: h.Name, SizeGB: h.DataGB, Deleted: now.UTC().Truncate(time.Second),
		State: store.VolumeKept, Cause: store.CauseReplaced}
	eq(t, value(t, result(s.KeptVolume(ctx, h.DataVolume))), kept)
	eq(t, value(t, result(s.LastKeptVolumes(ctx, 1))), []store.KeptVolume{kept})
	for _, id := range []string{h.InstanceID, h.DataVolume, restoring.InstanceID, restoring.DataVolume} {
		eq(t, value(t, result(s.Explains(ctx, id))), true)
	}
	eq(t, value(t, result(s.Explains(ctx, "unrecorded"))), false)
	eq(t, value(t, result(s.NoteStray(ctx, "unrecorded", "instance", h.Name, *now))), true)
	eq(t, value(t, result(s.NoteStray(ctx, "unrecorded", "instance", h.Name, *now))), false)

	checkErr(t, s.Fail(ctx, op.ID, "restore failed"), nil)
	failed := value(t, result(s.Host(ctx, h.Name)))
	eq(t, failed.State, store.HostFailed)
	eq(t, failed.Reason, "restore failed")
	eq(t, failed.Replace, store.ReplaceBackup)
	eq(t, value(t, result(s.Desired(ctx, h.Name))).Backup.Restore, restore)
	checkErr(t, s.Admit(ctx, op.ID), store.ErrConflict)
	checkErr(t, s.Retire(ctx, op.ID), store.ErrConflict)
	resumed, retry, err := s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, nil)
	eq(t, retry.Step, checkpoint.Step)
	eq(t, retry.Generation, checkpoint.Generation)
	eq(t, resumed.State, store.HostReplacing)
	checkErr(t, s.Admit(ctx, op.ID), nil)
	wantReplacement.Admitted = true
	eq(t, value(t, result(s.Replacement(ctx, op.ID))), wantReplacement)
	admitted := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, admitted.Version, restoring.Version+1)
	eq(t, admitted.Backup.Restore, (*protocol.HostRestore)(nil))
	eq(t, admitted.Accounts, before.Accounts)
	eq(t, admitted.Users, before.Users)
	checkErr(t, s.Admit(ctx, op.ID), nil)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), admitted)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	running := value(t, result(s.Host(ctx, h.Name)))
	eq(t, running.State, store.HostRunning)
	eq(t, running.Replace, "")
	eq(t, running.Closed, h.Closed)
	eq(t, running.RunningSince, now.UTC().Truncate(time.Second))

	_, volumeOp, _ := replaceHost(t, s, h.Name, store.ReplaceVolume, store.Target{})
	checkErr(t, s.Retire(ctx, volumeOp.ID), nil)
	volumeDesired := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, volumeDesired.Accounts, []protocol.Account{})
	eq(t, volumeDesired.Users, []protocol.User{})
	eq(t, volumeDesired.Backup.Restore, (*protocol.HostRestore)(nil))
	checkErr(t, s.Admit(ctx, volumeOp.ID), nil)
	checkErr(t, s.Finish(ctx, volumeOp.ID, store.Update{DataVolume: running.DataVolume}), nil)
	eq(t, value(t, result(s.LastKeptVolumes(ctx, 2))), []store.KeptVolume{kept})
}

// R-4S6F-JIO1
func TestBackupHostGuardsForResourcesAndDestructiveDelete(t *testing.T) {
	_, s, _ := fixture(t)
	unallocated, createOp := create(t, s)
	checkErr(t, s.Fail(ctx, createOp.ID, "allocation failed"), nil)
	observe3(s.ReplaceHost(ctx, unallocated.Name, store.ReplaceVolume, store.Target{}, caller)).assertZero(t, store.ErrNoDataVolume)
	observe3(s.ReplaceHost(ctx, unallocated.Name, store.ReplaceBackup, store.Target{}, caller)).assertZero(t, store.ErrNoAddress)
	observe2(s.DeleteHost(ctx, unallocated.Name, false, false, caller)).assertZero(t, store.ErrNotClosed)
	deleting, deletion, err := s.DeleteHost(ctx, unallocated.Name, false, true, caller)
	checkErr(t, err, nil)
	eq(t, deleting.State, store.HostDeleting)
	eq(t, deleting.OnDelete, store.OnDeleteDelete)
	eq(t, deletion.ResumeState, store.HostFailed)
	checkErr(t, s.Fail(ctx, deletion.ID, "delete failed"), nil)
	observe3(s.ReplaceHost(ctx, unallocated.Name, store.ReplaceBackup, store.Target{}, caller)).assertZero(t, store.ErrHostFailed)

	h := readyHost(t, s)
	observe2(s.DeleteHost(ctx, h.Name, false, false, caller)).assertZero(t, store.ErrNotClosed)
	a, _ := place(t, s, "acme", h.Name)
	observe2(s.DeleteHost(ctx, h.Name, false, false, caller)).assertZero(t, store.ErrHasAccounts)
	remove(t, s, a.Slug)
	_, closeOp := closeHost(t, s, h.Name)
	checkErr(t, s.Finish(ctx, closeOp.ID, store.Update{}), nil)
	deleting, deletion, err = s.DeleteHost(ctx, h.Name, false, false, caller)
	checkErr(t, err, nil)
	eq(t, deleting.State, store.HostDeleting)
	eq(t, deleting.OnDelete, store.OnDeleteDelete)
	eq(t, deleting.Version, value(t, result(s.Host(ctx, h.Name))).Version)
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	observe3(s.ReplaceHost(ctx, h.Name, store.ReplaceBackup, store.Target{}, caller)).assertZero(t, store.ErrHostGone)
	observe2(s.OpenHost(ctx, h.Name, caller)).assertZero(t, store.ErrHostGone)
	observe2(s.CloseHost(ctx, h.Name, caller)).assertZero(t, store.ErrHostGone)
}

func TestRetiredBackupReplacementMustBeAdmittedBeforeItsRestoreDisappears(t *testing.T) {
	_, s, _ := fixture(t)
	h := readyHost(t, s)
	_, op, _ := replaceHost(t, s, h.Name, store.ReplaceBackup, store.Target{})
	checkErr(t, s.Retire(ctx, op.ID), nil)
	withheld := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, withheld.Backup.Restore, &protocol.HostRestore{ID: op.ID, Accounts: []protocol.RestoreAccount{}})
	before := value(t, result(s.Host(ctx, h.Name)))
	watch, release := s.Watch(h.Name)
	defer release()
	checkErr(t, s.Finish(ctx, op.ID, store.Update{HostState: store.HostRunning}), store.ErrUnreachable)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{DataVolume: "uncommitted-volume"}), store.ErrConflict)
	eq(t, value(t, result(s.Host(ctx, h.Name))), before)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	observe(s.KeptVolume(ctx, h.DataVolume)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), withheld)
	closed(t, watch, false)

	checkErr(t, s.Fail(ctx, op.ID, "restore failed"), nil)
	failedHost := value(t, result(s.Host(ctx, h.Name)))
	failedOperations := value(t, result(s.OperationsOf(ctx, h.Name, "", 10)))
	for _, mode := range []string{store.ReplaceBackup, store.ReplaceVolume} {
		observe3(s.ReplaceHost(ctx, h.Name, mode, store.Target{}, caller)).assertZero(t, store.ErrBusy)
	}
	observe2(s.DeleteHost(ctx, h.Name, true, false, caller)).assertZero(t, store.ErrBusy)
	eq(t, value(t, result(s.Host(ctx, h.Name))), failedHost)
	eq(t, value(t, result(s.OperationsOf(ctx, h.Name, "", 10))), failedOperations)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), withheld)
	closed(t, watch, false)

	_, retried, err := s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, nil)
	eq(t, retried.ID, op.ID)
	checkErr(t, s.Admit(ctx, retried.ID), nil)
	closed(t, watch, true)
	admitted := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, admitted.Version, withheld.Version+1)
	eq(t, admitted.Backup.Restore, (*protocol.HostRestore)(nil))
	checkErr(t, s.Finish(ctx, retried.ID, store.Update{}), nil)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), admitted)
}
