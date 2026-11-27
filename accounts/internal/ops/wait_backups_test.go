package ops_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
)

// R-COZ1-ZUBA R-CQ6Y-DM1Z R-CTUN-IXA2 R-CV2J-WP0R R-HG6Q-YDWF R-3T6L-SNWX
func TestCloseWaitsForCurrentAccountsAndOpenWaitsForApplied(t *testing.T) {
	f := setup(t)
	h := f.ready()
	removed, place := f.place("to-remove", h.Name)
	ok(t, f.st.Finish(f.ctx, place.ID, store.Update{}))
	c := &struct{ cloud.Cloud }{Cloud: f.memory}
	f.cfg.Cloud = c
	e := ops.New(f.cfg)
	ok(t, e.Scan(f.ctx))
	_, closing, err := f.st.CloseHost(f.ctx, h.Name, author)
	ok(t, err)
	reportOperation(f, h.Name, protocol.Step{ID: closing.ID, State: protocol.StepDone}, []protocol.AccountBackup{
		{Account: removed.Slug, Error: "private diagnostic", Final: protocol.Step{ID: closing.ID, State: protocol.StepFailed, At: f.now}},
	}, true)
	_, unplace := f.remove(removed.Slug)
	ok(t, f.st.Finish(f.ctx, unplace.ID, store.Update{}))
	newAccount, placement := f.place("later", h.Name)
	ok(t, f.st.Defer(f.ctx, placement.ID, f.now.Add(time.Hour), 0))

	// No cloud calls are allowed while close and open wait on the daemon.
	c.Cloud = nil
	o := f.step(e, closing.ID)
	equal(t, o.Step, ops.StepWaitFinal)
	f.now = f.now.Add(ops.StepTimeout + time.Second)
	o = f.step(e, closing.ID)
	equal(t, o.Status, store.OpRunning)
	equal(t, o.NextTry, f.now.Add(ops.PollInterval))
	equal(t, f.host(h.Name).Closed.IsZero(), true)
	reportOperation(f, h.Name, protocol.Step{}, []protocol.AccountBackup{
		{Account: newAccount.Slug, Final: protocol.Step{ID: closing.ID, State: protocol.StepDone, At: f.now}},
	}, true)
	equal(t, f.step(e, closing.ID).Status, store.OpDone)
	equal(t, f.host(h.Name).State, store.HostRunning)
	equal(t, f.host(h.Name).Closed, f.now)

	opening, op, err := f.st.OpenHost(f.ctx, h.Name, author)
	ok(t, err)
	o = f.step(e, op.ID)
	equal(t, o.Step, ops.StepWaitOpened)
	equal(t, o.NextTry, f.now.Add(ops.PollInterval))
	equal(t, o.Status, store.OpRunning)
	f.report(h.Name, opening.Version, opening.Version)
	equal(t, f.step(e, op.ID).Status, store.OpDone)
	equal(t, f.host(h.Name).State, store.HostRunning)
}

// R-CNR5-M2KL R-CV2J-WP0R R-HG6Q-YDWF R-3T6L-SNWX
func TestCloseFailureAndOpenTimeout(t *testing.T) {
	reasons := []string{ops.ReasonTerminated, ops.ReasonTimedOut, ops.ReasonRecordConflict,
		ops.ReasonMissing, ops.ReasonNoAccountsURL, ops.ReasonModifyFailed, ops.ReasonVolumeFailed,
		ops.ReasonBadSize, ops.ReasonFinalFailed, ops.ReasonRestoreFailed, ops.ReasonNotEmpty}
	seen := make(map[string]bool, len(reasons))
	for _, reason := range reasons {
		equal(t, reason != "" && !strings.Contains(reason, "\n") && !seen[reason], true)
		seen[reason] = true
	}
	f := setup(t)
	h := f.ready()
	_, placement := f.place("tenant", h.Name)
	ok(t, f.st.Finish(f.ctx, placement.ID, store.Update{}))
	_, closing, err := f.st.CloseHost(f.ctx, h.Name, author)
	ok(t, err)
	reportOperation(f, h.Name, protocol.Step{ID: closing.ID, State: protocol.StepDone}, []protocol.AccountBackup{
		{Account: placement.Account, Error: "private diagnostic", Final: protocol.Step{ID: closing.ID, State: protocol.StepFailed, At: f.now}},
	}, true)
	c := &struct{ cloud.Cloud }{Cloud: f.memory}
	f.cfg.Cloud = c
	e := ops.New(f.cfg)
	ok(t, e.Scan(f.ctx))
	c.Cloud = nil
	o := f.step(e, closing.ID)
	equal(t, o.Status, store.OpFailed)
	equal(t, o.Error, ops.ReasonFinalFailed)
	equal(t, f.host(h.Name).State, store.HostFailed)
	_, opening, err := f.st.OpenHost(f.ctx, h.Name, author)
	ok(t, err)
	equal(t, f.step(e, opening.ID).Step, ops.StepWaitOpened)
	f.now = f.now.Add(ops.StepTimeout + time.Second)
	o = f.step(e, opening.ID)
	equal(t, o.Status, store.OpFailed)
	equal(t, o.Error, ops.ReasonTimedOut)
	equal(t, f.host(h.Name).State, store.HostFailed)
	_, _, _, err = f.st.ReplaceHost(f.ctx, h.Name, store.ReplaceVolume, store.Target{}, author)
	ok(t, err)
}

// R-COZ1-ZUBA R-CQ6Y-DM1Z R-DI8N-6C3Y R-3T6L-SNWX
func TestPartialRestoreWaitsForResultWithoutChangingHost(t *testing.T) {
	for _, state := range []string{protocol.StepDone, protocol.StepFailed} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			h := f.ready()
			a, placement := f.place("tenant", h.Name)
			ok(t, f.st.Finish(f.ctx, placement.ID, store.Update{}))
			h = f.host(h.Name)
			r := protocol.Report{Protocol: protocol.Version, Attempted: h.Version, Applied: h.Version, Backups: []protocol.AccountBackup{
				{Account: a.Slug, Snapshot: f.now, Snapshots: []int64{f.now.Unix()}},
			}}
			_, err := f.st.RecordReport(f.ctx, h.Name, r, f.now)
			ok(t, err)
			req := store.RestoreRequest{Account: a.Slug, Kind: protocol.RestoreFiles, AsOf: f.now}
			_, op, err := f.st.RequestRestore(f.ctx, req, author)
			ok(t, err)
			c := &struct{ cloud.Cloud }{Cloud: f.memory}
			f.cfg.Cloud = c
			e := ops.New(f.cfg)
			ok(t, e.Scan(f.ctx))
			c.Cloud = nil
			equal(t, f.step(e, op.ID).Step, ops.StepWaitRestored)
			f.now = f.now.Add(ops.StepTimeout + time.Second)
			o := f.step(e, op.ID)
			equal(t, o.Status, store.OpRunning)
			equal(t, o.NextTry, f.now.Add(ops.PollInterval))
			h = f.host(h.Name)
			r = protocol.Report{Protocol: protocol.Version, Attempted: h.Version, Applied: h.Version, Restores: []protocol.RestoreResult{
				{ID: op.ID, State: state, Copy: "restore/tenant", Error: "private diagnostic"},
			}}
			_, err = f.st.RecordReport(f.ctx, h.Name, r, f.now)
			ok(t, err)
			o = f.step(e, op.ID)
			equal(t, f.host(h.Name).State, store.HostRunning)
			if state == protocol.StepDone {
				equal(t, o.Status, store.OpDone)
				equal(t, o.Error, "")
			} else {
				equal(t, o.Status, store.OpFailed)
				equal(t, o.Error, ops.ReasonRestoreFailed)
			}
			for _, event := range f.events() {
				if event.Attrs["operation"] == o.ID {
					equal(t, event.Name, trail.EventOperationFinished)
				}
			}
		})
	}
}

// R-DJGJ-K3UN
func TestDeleteDoesNotActOnVolumeKeptByReplacement(t *testing.T) {
	f := setup(t)
	h := f.ready()
	c := startReplacement(f, h, store.ReplaceBackup, store.Target{})
	e := ops.New(f.cfg)
	stepsTo(f, e, c.op.ID, ops.StepRestore)
	reportOperation(f, h.Name, protocol.Step{ID: c.op.ID, State: protocol.StepDone}, nil, false)
	stepsTo(f, e, c.op.ID, ops.StepWaitAdmitted)
	after := f.host(h.Name)
	f.report(h.Name, after.Version, after.Version)
	equal(t, f.step(e, c.op.ID).Status, store.OpDone)
	f.cfg.Cloud = &keptReplacementCloud{Cloud: f.memory, t: t, kept: h.DataVolume}
	d := f.deleteHost(after, false, author)
	f.complete(ops.New(f.cfg), d.ID)
	v, err := f.memory.DescribeVolume(f.ctx, h.DataVolume)
	ok(t, err)
	equal(t, v.State, cloud.VolumeAvailable)
	kept, err := f.st.KeptVolume(f.ctx, h.DataVolume)
	ok(t, err)
	equal(t, kept.State, store.VolumeKept)
	_, err = f.st.KeptVolume(f.ctx, after.DataVolume)
	equal(t, errors.Is(err, store.ErrNotFound), true)
}

type keptReplacementCloud struct {
	cloud.Cloud
	t    *testing.T
	kept string
}

func (c *keptReplacementCloud) DeleteVolume(ctx context.Context, id string) error {
	equal(c.t, id != c.kept, true)
	return c.Cloud.DeleteVolume(ctx, id)
}

func (c *keptReplacementCloud) TagResource(ctx context.Context, id string, tags cloud.Tags) error {
	equal(c.t, id != c.kept, true)
	return c.Cloud.TagResource(ctx, id, tags)
}
