package ops_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

type faultCloud struct {
	cloud.Cloud
	allocate  func(context.Context, cloud.Tags) (cloud.Address, error)
	instance  func(context.Context, string) (cloud.Instance, error)
	volume    func(context.Context, string) (cloud.Volume, error)
	stop      func(context.Context, string) error
	start     func(context.Context, string) error
	associate func(context.Context, string, string) (string, error)
}

func (c *faultCloud) AllocateAddress(ctx context.Context, tags cloud.Tags) (cloud.Address, error) {
	return c.allocate(ctx, tags)
}

func (c *faultCloud) DescribeInstance(ctx context.Context, id string) (cloud.Instance, error) {
	return c.instance(ctx, id)
}

func (c *faultCloud) DescribeVolume(ctx context.Context, id string) (cloud.Volume, error) {
	return c.volume(ctx, id)
}

func (c *faultCloud) StopInstance(ctx context.Context, id string) error { return c.stop(ctx, id) }

func (c *faultCloud) StartInstance(ctx context.Context, id string) error { return c.start(ctx, id) }

func (c *faultCloud) AssociateAddress(ctx context.Context, allocation, instance string) (string, error) {
	return c.associate(ctx, allocation, instance)
}

// R-RJIJ-058Q R-RN68-5GGT R-L0E6-07I2
func TestCloudErrorsAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name     string
		attempts int
		age      time.Duration
		err      error
		delay    time.Duration
		reason   string
	}{
		{name: "first retry", err: cloudErr(cloud.ClassTransient), delay: ops.PollInterval},
		{name: "doubling", attempts: 3, err: cloudErr(cloud.ClassTransient), delay: 8 * ops.PollInterval},
		{name: "saturation", attempts: 64, err: cloudErr(cloud.ClassTransient), delay: ops.MaxBackoff},
		{name: "volume rate limit", err: &cloud.Error{Code: "VolumeModificationRateExceeded", Class: cloud.ClassTransient}, delay: ops.MaxBackoff},
		{name: "refused", err: cloudErr(cloud.ClassRefused), reason: cloudErr(cloud.ClassRefused).Error()},
		{name: "mismatch", err: cloudErr(cloud.ClassMismatch), reason: cloudErr(cloud.ClassMismatch).Error()},
		{name: "missing", err: cloudErr(cloud.ClassNotFound), reason: ops.ReasonMissing},
		{name: "timeout", age: ops.StepTimeout + time.Second, reason: ops.ReasonTimedOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			h, op := f.create()
			ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepAssociate, AllocationID: "ip", InstanceID: "id"}))
			ok(t, f.st.Defer(f.ctx, op.ID, f.now, tc.attempts))
			f.now = f.now.Add(tc.age)
			calls := 0
			f.cfg.Cloud = &faultCloud{Cloud: f.memory, associate: func(_ context.Context, aid, iid string) (string, error) {
				equal(t, aid, "ip")
				equal(t, iid, "id")
				calls++
				return "", tc.err
			}}
			ok(t, ops.New(f.cfg).Pass(f.ctx))
			next := f.operation(op.ID)
			if tc.age > ops.StepTimeout {
				equal(t, calls, 0)
			} else {
				equal(t, calls, 1)
			}
			if tc.delay > 0 {
				equal(t, next.Status, store.OpRunning)
				equal(t, next.Step, ops.StepAssociate)
				equal(t, next.Attempts, tc.attempts+1)
				equal(t, next.NextTry, f.now.Add(tc.delay))
				return
			}
			equal(t, next.Status, store.OpFailed)
			equal(t, next.Error, tc.reason)
			equal(t, f.host(h.Name).State, store.HostFailed)
		})
	}
}

// R-MV4U-CPDH
func TestRunLeavesStepRecordedWhenCloudCallCancelled(t *testing.T) {
	f := setup(t)
	_, op := f.create()
	inFlight := make(chan context.Context, 1)
	f.cfg.Cloud = &faultCloud{Cloud: f.memory, allocate: func(ctx context.Context, _ cloud.Tags) (cloud.Address, error) {
		inFlight <- ctx
		<-ctx.Done()
		return cloud.Address{}, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- ops.New(f.cfg).Run(ctx) }()
	var call context.Context
	select {
	case call = <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("cloud call never started")
	}
	equal(t, f.operation(op.ID).Step, ops.StepAddress)
	cancel()
	select {
	case err := <-stopped:
		ok(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("engine remained in flight")
	}
	if !errors.Is(call.Err(), context.Canceled) {
		t.Fatalf("cloud context %v", call.Err())
	}
	pending := f.operation(op.ID)
	equal(t, pending.Status, store.OpRunning)
	equal(t, pending.Step, ops.StepAddress)
	equal(t, pending.Generation, op.Generation)
	equal(t, pending.Error, "")
	f.cfg.Cloud = f.memory
	equal(t, f.step(ops.New(f.cfg), op.ID).Step, ops.StepLaunch)
}

// R-MSP1-L5W3 R-RWXF-7MED R-N2G8-NBTN R-N4W1-EVB1 R-N7BU-6ESF R-N8JQ-K6J4
func TestWaitsAndUnexpectedNotFound(t *testing.T) {
	for _, tc := range []struct {
		name, kind, step string
		instance         cloud.Instance
		volume           cloud.Volume
		err              error
		wait             bool
		next, reason     string
	}{
		{name: "create pending", kind: store.OpCreate, step: ops.StepWaitRunning, instance: cloud.Instance{State: cloud.InstancePending}, wait: true},
		{name: "create terminating", kind: store.OpCreate, step: ops.StepWaitRunning, instance: cloud.Instance{State: cloud.InstanceShuttingDown}, next: ops.StepLaunch},
		{name: "create not found", kind: store.OpCreate, step: ops.StepWaitRunning, err: cloudErr(cloud.ClassNotFound), wait: true},
		{name: "start terminated", kind: store.OpStart, step: ops.StepWaitRunning, instance: cloud.Instance{State: cloud.InstanceTerminated}, reason: ops.ReasonTerminated},
		{name: "start not found", kind: store.OpStart, step: ops.StepWaitRunning, err: cloudErr(cloud.ClassNotFound), reason: ops.ReasonMissing},
		{name: "stop not found", kind: store.OpStop, step: ops.StepStop, err: cloudErr(cloud.ClassNotFound), reason: ops.ReasonMissing},
		{name: "send start not found", kind: store.OpStart, step: ops.StepStart, err: cloudErr(cloud.ClassNotFound), reason: ops.ReasonMissing},
		{name: "wait stop not found", kind: store.OpStop, step: ops.StepWaitStopped, err: cloudErr(cloud.ClassNotFound), reason: ops.ReasonMissing},
		{name: "stop already terminating", kind: store.OpStop, step: ops.StepWaitStopped, instance: cloud.Instance{State: cloud.InstanceShuttingDown}, reason: ops.ReasonTerminated},
		{name: "stop waiting", kind: store.OpStop, step: ops.StepWaitStopped, instance: cloud.Instance{State: cloud.InstanceStopping}, wait: true},
		{name: "data volume absent", kind: store.OpCreate, step: ops.StepWaitVolume, err: cloudErr(cloud.ClassNotFound), wait: true},
		{name: "data volume error", kind: store.OpCreate, step: ops.StepWaitVolume, volume: cloud.Volume{State: cloud.VolumeError}, reason: ops.ReasonVolumeFailed},
		{name: "data volume attached", kind: store.OpCreate, step: ops.StepWaitVolume, volume: cloud.Volume{State: cloud.VolumeInUse, Attachment: cloud.Attachment{InstanceID: "i-1"}}, next: ops.StepAssociate},
		{name: "data volume attached elsewhere", kind: store.OpCreate, step: ops.StepWaitVolume, volume: cloud.Volume{State: cloud.VolumeInUse, Attachment: cloud.Attachment{InstanceID: "i-other"}}, wait: true},
		{name: "skip redundant attach", kind: store.OpCreate, step: ops.StepAttach, volume: cloud.Volume{State: cloud.VolumeInUse, Attachment: cloud.Attachment{InstanceID: "i-1"}}, next: ops.StepWaitAttached},
		{name: "attachment pending", kind: store.OpCreate, step: ops.StepWaitAttached, volume: cloud.Volume{Attachment: cloud.Attachment{InstanceID: "i-1", State: cloud.AttachmentAttaching}}, wait: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			h, op := f.create()
			ok(t, f.st.Advance(f.ctx, op.ID, store.Update{InstanceID: "i-1", DataVolume: "v-1"}))
			if tc.kind != store.OpCreate {
				ok(t, f.st.Finish(f.ctx, op.ID, store.Update{}))
				if tc.kind == store.OpStart {
					stop := f.start(h, store.OpStop, store.Target{})
					ok(t, f.st.Finish(f.ctx, stop.ID, store.Update{}))
				}
				op = f.start(h, tc.kind, store.Target{})
			}
			ok(t, f.st.RecordStep(f.ctx, op.ID, tc.step, op.Generation, f.now))
			ok(t, f.st.Defer(f.ctx, op.ID, f.now, 2))
			f.cfg.Cloud = &faultCloud{
				Cloud: f.memory,
				instance: func(_ context.Context, id string) (cloud.Instance, error) {
					equal(t, id, "i-1")
					return tc.instance, tc.err
				},
				volume: func(_ context.Context, id string) (cloud.Volume, error) {
					equal(t, id, "v-1")
					return tc.volume, tc.err
				},
				stop:  func(context.Context, string) error { return tc.err },
				start: func(context.Context, string) error { return tc.err },
			}
			next := f.step(ops.New(f.cfg), op.ID)
			switch {
			case tc.wait:
				equal(t, next.Status, store.OpRunning)
				equal(t, next.Step, tc.step)
				equal(t, next.Attempts, 2)
				equal(t, next.NextTry, f.now.Add(ops.PollInterval))
			case tc.reason != "":
				equal(t, next.Status, store.OpFailed)
				equal(t, next.Error, tc.reason)
			default:
				equal(t, next.Step, tc.next)
				if tc.next == ops.StepLaunch {
					equal(t, next.Generation, op.Generation+1)
				}
			}
		})
	}
}
