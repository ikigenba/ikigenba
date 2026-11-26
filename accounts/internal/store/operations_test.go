package store_test

import (
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

// R-GRT3-SIFT R-H7NS-RJ2U R-H214-5FQN R-71LM-K5LQ R-HCJE-AM1M
// R-HSE3-9MON R-HTLZ-NEFC R-HUTW-1661 R-HW1S-EXWQ R-HX9O-SPNF R-HYHL-6HE4
// R-RAJ3-7PFM R-I0XD-Y0VI R-HIMW-7GR3 R-I4L3-3C3L R-0LHN-KXWI
func TestOperationCheckpoints(t *testing.T) {
	_, s, now := fixture(t)
	h, op := create(t, s)
	other, otherOp := create(t, s)
	stamp := now.UTC().Truncate(time.Second)
	eq(t, value(t, result(s.NextTry(ctx))), stamp)
	eq(t, value(t, result(s.Runnable(ctx, *now, "", 1))), []store.Operation{op})
	eq(t, value(t, result(s.Runnable(ctx, *now, op.ID, 1))), []store.Operation{otherOp})
	*now = now.Add(time.Hour)
	checkErr(t, s.RecordStep(ctx, op.ID, "launch", 2, *now), nil)
	stepTime := now.UTC().Truncate(time.Second)
	checkErr(t, s.RecordStep(ctx, op.ID, "launch", 2, now.Add(time.Hour)), nil)
	checkErr(t, s.Defer(ctx, op.ID, now.Add(time.Minute), 3), nil)
	op.Step, op.Generation, op.StepStarted, op.Attempts, op.NextTry = "launch", 2, stepTime, 3, stepTime.Add(time.Minute)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	eq(t, value(t, result(s.OperationsOf(ctx, h.Name, "", 1))), []store.Operation{op})
	eq(t, value(t, result(s.OperationsOf(ctx, h.Name, op.ID, 1))), []store.Operation{})
	eq(t, value(t, result(s.Runnable(ctx, *now, "", 5))), []store.Operation{otherOp})

	update := store.Update{Step: "wait", Generation: 3, InstanceID: "instance", RootVolume: "root", DataVolume: "data",
		AllocationID: "allocation", AssociationID: "association", EIP: "address", AZ: "zone", Type: "sized", RootGB: 25, DataGB: 55}
	checkErr(t, s.Advance(ctx, op.ID, update), nil)
	op.Step, op.Generation, op.Attempts, op.StepStarted, op.NextTry = "wait", 3, 0, stepTime, stepTime
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	h.InstanceID, h.RootVolume, h.DataVolume, h.AllocationID, h.AssociationID, h.EIP, h.AZ =
		update.InstanceID, update.RootVolume, update.DataVolume, update.AllocationID, update.AssociationID, update.EIP, update.AZ
	h.Type, h.RootGB, h.DataGB, h.Version = update.Type, update.RootGB, update.DataGB, 2
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	checkErr(t, s.Advance(ctx, op.ID, store.Update{DataVolume: h.DataVolume}), nil)
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	checkErr(t, s.Advance(ctx, otherOp.ID, store.Update{InstanceID: h.InstanceID}), store.ErrConflict)
	checkErr(t, s.Finish(ctx, otherOp.ID, store.Update{InstanceID: h.InstanceID}), store.ErrConflict)
	eq(t, value(t, result(s.Host(ctx, other.Name))), other)
	eq(t, value(t, result(s.Operation(ctx, otherOp.ID))), otherOp)

	checkErr(t, s.Fail(ctx, op.ID, "launch failed"), nil)
	h.State, h.Reason = store.HostFailed, "launch failed"
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	failed := op
	failed.Status, failed.Error, failed.Finished = store.OpFailed, h.Reason, stepTime
	eq(t, value(t, result(s.Operation(ctx, op.ID))), failed)
	*now = now.Add(time.Hour)
	retrier := caller
	retrier.UserID = "second-operator"
	got, retried, err := s.RetryHost(ctx, h.Name, retrier)
	checkErr(t, err, nil)
	h.State, h.Reason = store.HostProvisioning, ""
	eq(t, got, h)
	finishTime := now.UTC().Truncate(time.Second)
	op.Caller, op.StepStarted, op.NextTry = retrier, finishTime, finishTime
	eq(t, retried, op)

	checkErr(t, s.Finish(ctx, op.ID, store.Update{RootGB: 30}), nil)
	op.Status, op.Finished = store.OpDone, finishTime
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	h.RootGB, h.Version, h.State, h.RunningSince = 30, 3, store.HostRunning, finishTime
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	for _, tc := range []struct {
		id   string
		want error
	}{{"missing", store.ErrNotFound}, {op.ID, store.ErrConflict}} {
		checkErr(t, s.RecordStep(ctx, tc.id, "step", 1, *now), tc.want)
		checkErr(t, s.Advance(ctx, tc.id, store.Update{}), tc.want)
		checkErr(t, s.Defer(ctx, tc.id, *now, 1), tc.want)
		checkErr(t, s.Fail(ctx, tc.id, "failure"), tc.want)
		checkErr(t, s.Finish(ctx, tc.id, store.Update{}), tc.want)
		checkErr(t, s.RecordChange(ctx, tc.id, "account", "change"), tc.want)
	}
	checkErr(t, s.Fail(ctx, otherOp.ID, ""), nil)
	failed = value(t, result(s.Operation(ctx, otherOp.ID)))
	eq(t, failed.Status, store.OpFailed)
	eq(t, failed.Error, "")
	_, _, err = s.RetryHost(ctx, other.Name, caller)
	checkErr(t, err, nil)
	checkErr(t, s.Finish(ctx, otherOp.ID, store.Update{}), nil)
	eq(t, value(t, result(s.NextTry(ctx))), time.Time{})
}

// R-78X0-US1W R-HOQE-4BGK R-7A4X-8JSL R-RAJ3-7PFM R-J7LV-BZTT R-J8TR-PRKI
func TestHostGuardsAndVolumeWindow(t *testing.T) {
	_, s, now := fixture(t)
	h, c := create(t, s)
	guard := func(name, kind string, target store.Target, want error) {
		t.Helper()
		got, op, err := s.StartHostOp(ctx, name, kind, target, caller)
		checkErr(t, err, want)
		eq(t, got, store.Host{})
		eq(t, op, store.Operation{})
	}
	begin := func(kind, state string, target, wantTarget store.Target) store.Operation {
		t.Helper()
		before := value(t, result(s.Host(ctx, h.Name)))
		after, op := start(t, s, h.Name, kind, target)
		stamp := now.UTC().Truncate(time.Second)
		eq(t, op, store.Operation{ID: op.ID, Host: h.Name, Kind: kind, Target: wantTarget, ResumeState: before.State,
			Status: store.OpRunning, Generation: 1, Started: stamp, StepStarted: stamp, NextTry: stamp, Caller: caller})
		before.State, before.Reason, before.Operation = state, "", op.ID
		eq(t, after, before)
		eq(t, value(t, result(s.Host(ctx, h.Name))), after)
		eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
		return op
	}
	guard("missing", "invalid", store.Target{}, store.ErrUnreachable)
	guard("missing", store.OpStop, store.Target{}, store.ErrNotFound)
	guard(h.Name, store.OpStop, store.Target{}, store.ErrBusy)
	_, _, err := s.RetryHost(ctx, "missing", caller)
	checkErr(t, err, store.ErrNotFound)
	_, _, err = s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, store.ErrNotFailed)
	checkErr(t, s.Fail(ctx, c.ID, "initial failure"), nil)
	guard(h.Name, store.OpStart, store.Target{}, store.ErrHostFailed)
	_, _, err = s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, nil)
	checkErr(t, s.Finish(ctx, c.ID, store.Update{RootVolume: "root", DataVolume: "data", AllocationID: "allocation", InstanceID: "instance"}), nil)
	beforeDelete := value(t, result(s.Host(ctx, h.Name)))
	beforeDeleteOps := value(t, result(s.OperationsOf(ctx, h.Name, "", 2)))
	guard(h.Name, store.OpDelete, store.Target{}, store.ErrUnreachable)
	eq(t, value(t, result(s.Host(ctx, h.Name))), beforeDelete)
	eq(t, value(t, result(s.OperationsOf(ctx, h.Name, "", 2))), beforeDeleteOps)
	guard(h.Name, store.OpStart, store.Target{}, store.ErrNotStopped)
	guard(h.Name, store.OpResize, store.Target{RootGB: spec.RootGB - 1}, store.ErrShrinks)
	guard(h.Name, store.OpResize, store.Target{RootGB: spec.RootGB, Type: spec.Type}, store.ErrNothingToChange)

	for i := range store.MaxGrows {
		checkErr(t, s.NoteGrow(ctx, "root", now.Add(time.Duration(i)*time.Second)), nil)
		checkErr(t, s.NoteGrow(ctx, "other", now.Add(time.Duration(i)*time.Second)), nil)
	}
	guard(h.Name, store.OpResize, store.Target{RootGB: spec.RootGB + 1}, store.ErrGrowLimit)
	same := *now
	*now = now.Add(store.GrowWindow + time.Second)
	checkErr(t, s.NoteGrow(ctx, "root", *now), nil)
	// NoteGrow pruned two root notes and added one, so another root grow can be started.
	resize := begin(store.OpResize, store.HostResizing, store.Target{Type: spec.Type, RootGB: spec.RootGB + 1, DataGB: spec.DataGB},
		store.Target{RootGB: spec.RootGB + 1})
	checkErr(t, s.Finish(ctx, resize.ID, store.Update{RootGB: resize.Target.RootGB}), nil)
	eq(t, value(t, result(s.PruneGrows(ctx, *now, 1))), 1)
	eq(t, value(t, result(s.PruneGrows(ctx, *now, 5))), 1)
	eq(t, value(t, result(s.PruneGrows(ctx, *now, 5))), 0)

	stop := begin(store.OpStop, store.HostStopping, store.Target{Type: "ignored"}, store.Target{})
	checkErr(t, s.Finish(ctx, stop.ID, store.Update{}), nil)
	earliest := value(t, result(s.Operation(ctx, c.ID)))
	eq(t, value(t, result(s.OperationsOf(ctx, h.Name, "", 1))), []store.Operation{earliest})
	guard(h.Name, store.OpStop, store.Target{}, store.ErrNotRunning)
	startOp := begin(store.OpStart, store.HostStarting, store.Target{DataGB: 75}, store.Target{})
	checkErr(t, s.Finish(ctx, startOp.ID, store.Update{}), nil)

	_, deletion := deleteHost(t, s, h.Name, true)
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	guard(h.Name, store.OpStop, store.Target{}, store.ErrHostGone)
	_, _, err = s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, store.ErrHostGone)
	for _, resource := range []string{"root", "data", "instance", "allocation"} {
		eq(t, value(t, result(s.Explains(ctx, resource))), true)
	}
	eq(t, value(t, result(s.Explains(ctx, "unknown"))), false)
	eq(t, value(t, result(s.NoteStray(ctx, "unknown", "volume", h.Name, same))), true)
	eq(t, value(t, result(s.NoteStray(ctx, "unknown", "instance", "", *now))), false)
}
