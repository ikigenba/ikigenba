package ops_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

// R-MNTG-22XB R-UFK7-TLG8 R-NAZJ-BQ0I R-NC7F-PHR7 R-NDFC-39HW R-N63X-SN1Q R-UI00-L4XM
func TestResizeGrowsThenChangesType(t *testing.T) {
	f := setup(t)
	h := f.ready()
	op := f.start(h, store.OpResize, store.Target{Type: "t4g.large", RootGB: 25, DataGB: 60})
	cl := &observedCloud{Cloud: f.memory, f: f, id: op.ID}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	for _, next := range []string{
		ops.StepWaitRoot, ops.StepGrowData, ops.StepWaitData, ops.StepStop,
		ops.StepWaitStopped, ops.StepSetType, ops.StepStart, ops.StepWaitRunning,
		ops.StepAssociate, ops.StepAssociate,
	} {
		equal(t, f.step(e, op.ID).Step, next)
	}
	equal(t, f.operation(op.ID).Status, store.OpDone)
	got := f.host(h.Name)
	equal(t, got.State, store.HostRunning)
	equal(t, got.Type, "t4g.large")
	equal(t, got.RootGB, 25)
	equal(t, got.DataGB, 60)
	equal(t, got.Version, h.Version+2)
	equal(t, got.AllocationID, h.AllocationID)
	wantSizes := []cloud.VolumeSpec{{AZ: h.RootVolume, SizeGB: 25}, {AZ: h.DataVolume, SizeGB: 60}}
	if !reflect.DeepEqual(cl.modify, wantSizes) {
		t.Fatalf("modification calls %v, want %v", cl.modify, wantSizes)
	}
	equal(t, len(cl.stop), 1)
	equal(t, cl.stop[0], h.InstanceID)
	equal(t, len(cl.start), 1)
	equal(t, cl.start[0], h.InstanceID)
	equal(t, len(cl.setType), 1)
	equal(t, cl.setType[0], [2]string{h.InstanceID, "t4g.large"})
	equal(t, len(cl.associate), 1)
	equal(t, cl.associate[0], [2]string{h.AllocationID, h.InstanceID})

	stop := f.start(got, store.OpStop, store.Target{})
	cl.id = stop.ID
	f.complete(e, stop.ID)
	op = f.start(got, store.OpResize, store.Target{Type: "t4g.xlarge"})
	cl.id = op.ID
	f.complete(e, op.ID)
	equal(t, f.operation(op.ID).Step, ops.StepSetType)
	got = f.host(h.Name)
	equal(t, got.State, store.HostStopped)
	equal(t, got.Type, "t4g.xlarge")
	equal(t, got.Version, h.Version+2)
	equal(t, len(cl.stop), 2)
	equal(t, len(cl.start), 1)
}

type resizeCloud struct {
	cloud.Cloud
	v       cloud.Volume
	m       cloud.Modification
	err     error
	readErr error
	calls   []int32
	before  func()
}

func (c *resizeCloud) DescribeVolume(_ context.Context, id string) (cloud.Volume, error) {
	v := c.v
	v.ID = id
	return v, nil
}

func (c *resizeCloud) VolumeModification(context.Context, string) (cloud.Modification, error) {
	return c.m, c.readErr
}

func (c *resizeCloud) ModifyVolume(_ context.Context, _ string, target int32) error {
	if c.before != nil {
		c.before()
	}
	c.calls = append(c.calls, target)
	return c.err
}

// R-MNTG-22XB R-UFK7-TLG8 R-NAZJ-BQ0I
func TestGrowResumesAndNotesOnlyFirstAttempt(t *testing.T) {
	f := setup(t)
	h := f.ready()
	op := f.start(h, store.OpResize, store.Target{DataGB: 60})
	cl := &resizeCloud{Cloud: f.memory, v: cloud.Volume{SizeGB: 50}, err: &cloud.Error{
		Service: "ec2", Operation: "ModifyVolume", Code: "VolumeModificationRateExceeded", Class: cloud.ClassTransient,
	}, before: func() {
		equal(t, f.operation(op.ID).Step, ops.StepGrowData)
	}}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	first := f.step(e, op.ID)
	equal(t, first.Step, ops.StepGrowData)
	equal(t, first.NextTry, f.now.Add(ops.MaxBackoff))
	equal(t, first.Attempts, 1)
	equal(t, len(cl.calls), 1)

	f.now = f.now.Add(ops.StepTimeout + time.Second)
	cl.err = nil
	next := f.step(e, op.ID)
	equal(t, next.Step, ops.StepWaitData)
	equal(t, next.Attempts, 0)
	equal(t, len(cl.calls), 2)
	equal(t, cl.calls[0], int32(60))
	equal(t, cl.calls[1], int32(60))

	f.now = f.now.Add(ops.StepTimeout + time.Second)
	cl.m = cloud.Modification{TargetGB: 60, State: cloud.ModificationModifying}
	next = f.step(e, op.ID)
	equal(t, next.Status, store.OpRunning)
	equal(t, next.Step, ops.StepWaitData)
	equal(t, next.NextTry, f.now.Add(ops.PollInterval))

	// A retry whose modification became effective is recorded without another request.
	// Use a fresh step start to exercise optimization inside the step deadline.
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{}))
	cl.m.State = cloud.ModificationOptimizing
	next = f.step(e, op.ID)
	equal(t, next.Status, store.OpDone)
	equal(t, f.host(h.Name).DataGB, 60)
	equal(t, f.host(h.Name).Version, h.Version+1)
	grows, err := f.st.PruneGrows(f.ctx, f.now.Add(store.GrowWindow), store.BatchSize)
	ok(t, err)
	equal(t, grows, 1)
}

// R-UFK7-TLG8 R-NAZJ-BQ0I R-NDFC-39HW
func TestVolumeStepDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, step, state string
		target, volume    int32
		later             time.Duration
		calls             int
		wait              bool
		next, reason      string
	}{
		{name: "same target sent already", step: ops.StepGrowRoot, state: cloud.ModificationModifying, target: 30, volume: 20, next: ops.StepWaitRoot},
		{name: "volume already grew", step: ops.StepGrowRoot, volume: 30, next: ops.StepWaitRoot},
		{name: "other size still modifying", step: ops.StepGrowRoot, state: cloud.ModificationModifying, target: 25, volume: 20, later: 2 * ops.StepTimeout, wait: true},
		{name: "other size still optimizing", step: ops.StepGrowRoot, state: cloud.ModificationOptimizing, target: 25, volume: 25, later: 2 * ops.StepTimeout, wait: true},
		{name: "same size failed can try again", step: ops.StepGrowRoot, state: cloud.ModificationFailed, target: 30, volume: 20, calls: 1, next: ops.StepWaitRoot},
		{name: "same size failed while waiting", step: ops.StepWaitRoot, state: cloud.ModificationFailed, target: 30, volume: 20, reason: ops.ReasonModifyFailed},
		{name: "same size completed", step: ops.StepWaitRoot, state: cloud.ModificationCompleted, target: 30, volume: 20},
		{name: "size effective without modification", step: ops.StepWaitRoot, volume: 31},
		{name: "grow timeout without modification", step: ops.StepGrowRoot, volume: 20, later: 2 * ops.StepTimeout, reason: ops.ReasonTimedOut},
		{name: "expired wait still modifying", step: ops.StepWaitRoot, state: cloud.ModificationModifying, target: 30, volume: 20, later: 2 * ops.StepTimeout, wait: true},
		{name: "expired wait already optimizing", step: ops.StepWaitRoot, state: cloud.ModificationOptimizing, target: 30, volume: 30, later: 2 * ops.StepTimeout, reason: ops.ReasonTimedOut},
		{name: "wait timeout without modification", step: ops.StepWaitRoot, volume: 20, later: 2 * ops.StepTimeout, reason: ops.ReasonTimedOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			h := f.ready()
			op := f.start(h, store.OpResize, store.Target{RootGB: 30})
			ok(t, f.st.RecordStep(f.ctx, op.ID, tc.step, op.Generation, f.now))
			f.now = f.now.Add(tc.later)
			cl := &resizeCloud{Cloud: f.memory, v: cloud.Volume{SizeGB: tc.volume}, m: cloud.Modification{TargetGB: tc.target, State: tc.state}}
			f.cfg.Cloud = cl
			next := f.step(ops.New(f.cfg), op.ID)
			equal(t, len(cl.calls), tc.calls)
			switch {
			case tc.wait:
				equal(t, next.Status, store.OpRunning)
				equal(t, next.Step, tc.step)
				equal(t, next.NextTry, f.now.Add(ops.PollInterval))
			case tc.reason != "":
				equal(t, next.Status, store.OpFailed)
				equal(t, next.Error, tc.reason)
			case tc.next != "":
				equal(t, next.Step, tc.next)
			default:
				equal(t, next.Status, store.OpDone)
				got := f.host(h.Name)
				equal(t, got.RootGB, 30)
				equal(t, got.Version, h.Version+1)
			}
		})
	}
}

// R-RN68-5GGT
func TestResizeFailsWhenVolumeDisappearsBeforeModificationRead(t *testing.T) {
	for _, step := range []string{ops.StepGrowRoot, ops.StepWaitRoot, ops.StepGrowData, ops.StepWaitData} {
		t.Run(step, func(t *testing.T) {
			f := setup(t)
			h := f.ready()
			op := f.start(h, store.OpResize, store.Target{RootGB: 30, DataGB: 60})
			ok(t, f.st.RecordStep(f.ctx, op.ID, step, op.Generation, f.now))
			cl := &resizeCloud{
				Cloud: f.memory, v: cloud.Volume{SizeGB: 20},
				readErr: cloudErr(cloud.ClassNotFound),
			}
			f.cfg.Cloud = cl
			failed := f.step(ops.New(f.cfg), op.ID)
			equal(t, failed.Status, store.OpFailed)
			equal(t, failed.Error, ops.ReasonMissing)
			equal(t, f.host(h.Name).State, store.HostFailed)
			equal(t, len(cl.calls), 0)
		})
	}
}
