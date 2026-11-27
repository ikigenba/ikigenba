package ops_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type replacementCloud struct {
	cloud.Cloud
	f            *fixture
	op           store.Operation
	h            store.Host
	calls        []string
	keepRetiring bool
	onDescribe   func(id string) (cloud.Instance, error)
	onData       func(id string) (cloud.Volume, error)
	instancePage []cloud.Instance
	volumePage   []cloud.Volume
	onLaunch     func(cloud.Launch) (cloud.Instance, error)
	onVolume     func(cloud.VolumeSpec) (cloud.Volume, error)
	onTag        func() error
	association  string
}

func (c *replacementCloud) check(step string, retired bool) {
	c.f.t.Helper()
	equal(c.f.t, c.f.operation(c.op.ID).Step, step)
	r, err := c.f.st.Replacement(c.f.ctx, c.op.ID)
	ok(c.f.t, err)
	equal(c.f.t, r.Retired, retired)
	c.calls = append(c.calls, step)
}

func (c *replacementCloud) DescribeInstance(ctx context.Context, id string) (cloud.Instance, error) {
	if c.onDescribe != nil {
		return c.onDescribe(id)
	}
	i, err := c.Cloud.DescribeInstance(ctx, id)
	if c.keepRetiring && id == c.h.InstanceID && c.f.operation(c.op.ID).Step == ops.StepWaitRetired {
		i.State = cloud.InstanceShuttingDown
		return i, nil
	}
	return i, err
}

func (c *replacementCloud) DescribeVolume(ctx context.Context, id string) (cloud.Volume, error) {
	if c.onData != nil {
		return c.onData(id)
	}
	return c.Cloud.DescribeVolume(ctx, id)
}

func (c *replacementCloud) FindInstances(ctx context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	if c.instancePage != nil && tags[cloud.TagOperation] != "" {
		checkTags(c.f.t, tags, cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagOperation: c.op.ID})
		each(c.instancePage)
		return nil
	}
	return c.Cloud.FindInstances(ctx, tags, each)
}

func (c *replacementCloud) FindVolumes(ctx context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	if c.volumePage != nil && tags[cloud.TagOperation] != "" {
		checkTags(c.f.t, tags, cloud.Tags{cloud.TagKind: cloud.KindHostData, cloud.TagOperation: c.op.ID})
		each(c.volumePage)
		return nil
	}
	return c.Cloud.FindVolumes(ctx, tags, each)
}

func (c *replacementCloud) TerminateInstance(ctx context.Context, id string) error {
	c.check(ops.StepRetire, false)
	equal(c.f.t, id, c.h.InstanceID)
	return c.Cloud.TerminateInstance(ctx, id)
}

func (c *replacementCloud) RunInstance(ctx context.Context, launch cloud.Launch) (cloud.Instance, error) {
	c.check(ops.StepRelaunch, true)
	o := c.f.operation(c.op.ID)
	equal(c.f.t, launch.Type, o.Target.Type)
	equal(c.f.t, int(launch.RootGB), o.Target.RootGB)
	equal(c.f.t, launch.Template, c.f.cfg.Hosts.LaunchTemplate)
	equal(c.f.t, launch.InstanceProfile, c.f.cfg.Hosts.InstanceProfile)
	equal(c.f.t, launch.ClientToken, fmt.Sprintf("%s-%s-%d", o.ID, o.Step, o.Generation))
	checkTags(c.f.t, launch.Tags, cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagOperation: o.ID, cloud.TagHost: o.Host})
	checkTags(c.f.t, launch.InstanceTags, cloud.Tags{cloud.TagAccountsURL: accountURL})
	if c.onLaunch != nil {
		return c.onLaunch(launch)
	}
	return c.Cloud.RunInstance(ctx, launch)
}

func (c *replacementCloud) CreateVolume(ctx context.Context, volume cloud.VolumeSpec) (cloud.Volume, error) {
	c.check(ops.StepNewVolume, true)
	o := c.f.operation(c.op.ID)
	equal(c.f.t, int(volume.SizeGB), o.Target.DataGB)
	equal(c.f.t, volume.ClientToken, fmt.Sprintf("%s-%s-%d", o.ID, o.Step, o.Generation))
	equal(c.f.t, volume.AZ, c.f.host(o.Host).AZ)
	checkTags(c.f.t, volume.Tags, cloud.Tags{cloud.TagKind: cloud.KindHostData, cloud.TagOperation: o.ID, cloud.TagHost: o.Host})
	if c.onVolume != nil {
		return c.onVolume(volume)
	}
	return c.Cloud.CreateVolume(ctx, volume)
}

func (c *replacementCloud) AttachVolume(ctx context.Context, volume, instance, device string) error {
	c.check(ops.StepReattach, true)
	h := c.f.host(c.h.Name)
	equal(c.f.t, volume, h.DataVolume)
	equal(c.f.t, instance, h.InstanceID)
	equal(c.f.t, device, ops.DataDevice)
	s, err := c.DescribeVolume(ctx, volume)
	ok(c.f.t, err)
	equal(c.f.t, s.State, cloud.VolumeAvailable)
	return c.Cloud.AttachVolume(ctx, volume, instance, device)
}

func (c *replacementCloud) AssociateAddress(ctx context.Context, allocation, id string) (string, error) {
	c.check(ops.StepReassociate, true)
	equal(c.f.t, allocation, c.h.AllocationID)
	equal(c.f.t, id, c.f.host(c.h.Name).InstanceID)
	association, err := c.Cloud.AssociateAddress(ctx, allocation, id)
	c.association = association
	return association, err
}

func (c *replacementCloud) TagResource(ctx context.Context, id string, tags cloud.Tags) error {
	c.check(ops.StepTagOld, true)
	equal(c.f.t, id, c.h.DataVolume)
	checkTags(c.f.t, tags, cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue})
	if c.onTag != nil {
		return c.onTag()
	}
	return c.Cloud.TagResource(ctx, id, tags)
}

func (c *replacementCloud) StopInstance(context.Context, string) error {
	c.f.t.Fatal("replacement stopped an instance")
	return nil
}

func (c *replacementCloud) StartInstance(context.Context, string) error {
	c.f.t.Fatal("replacement started an instance")
	return nil
}

func (c *replacementCloud) DeleteVolume(context.Context, string) error {
	c.f.t.Fatal("replacement deleted a volume")
	return nil
}

func (c *replacementCloud) ModifyVolume(context.Context, string, int32) error {
	c.f.t.Fatal("replacement modified a volume")
	return nil
}

func (c *replacementCloud) DisassociateAddress(context.Context, string) error {
	c.f.t.Fatal("replacement disassociated an address")
	return nil
}

func (c *replacementCloud) ReleaseAddress(context.Context, string) error {
	c.f.t.Fatal("replacement released an address")
	return nil
}

func startReplacement(f *fixture, h store.Host, mode string, target store.Target) *replacementCloud {
	f.t.Helper()
	_, op, _, err := f.st.ReplaceHost(f.ctx, h.Name, mode, target, author)
	ok(f.t, err)
	c := &replacementCloud{Cloud: f.memory, f: f, h: h, op: op}
	f.cfg.Cloud = c
	return c
}

func stepsTo(f *fixture, e *ops.Engine, opID, step string) {
	f.t.Helper()
	for range 25 {
		op := f.step(e, opID)
		equal(f.t, op.Status, store.OpRunning)
		if op.Step == step {
			return
		}
	}
	f.t.Fatalf("operation %s never reached %s", opID, step)
}

// R-CK3G-GRCI R-COZ1-ZUBA R-CQ6Y-DM1Z R-CXIC-O8I5 R-CYQ9-208U R-CZY5-FRZJ R-D161-TJQ8 R-D3LU-L37M R-D4TQ-YUYB R-D61N-CMP0 R-D8HG-466E R-D9PC-HXX3 R-DAX8-VPNS R-HIMJ-PXDT R-DFSU-ESMK R-DH0Q-SKD9
func TestReplaceCheckpointsKeepAddressAndData(t *testing.T) {
	type stepName string
	for _, pair := range []struct{ got, want stepName }{
		{ops.StepWaitFinal, "wait_final"}, {ops.StepWaitOpened, "wait_opened"},
		{ops.StepRetire, "retire"}, {ops.StepWaitRetired, "wait_retired"},
		{ops.StepRelaunch, "relaunch"}, {ops.StepWaitRelaunched, "wait_relaunched"},
		{ops.StepNewVolume, "new_volume"}, {ops.StepWaitNewVolume, "wait_new_volume"},
		{ops.StepReattach, "reattach"}, {ops.StepWaitReattached, "wait_reattached"},
		{ops.StepTagOld, "tag_old"}, {ops.StepReassociate, "reassociate"},
		{ops.StepRestore, "restore"}, {ops.StepAdmit, "admit"},
		{ops.StepWaitAdmitted, "wait_admitted"}, {ops.StepWaitRestored, "wait_restored"},
	} {
		equal(t, pair.got, pair.want)
	}
	for _, mode := range []string{store.ReplaceVolume, store.ReplaceBackup} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			h := f.ready()
			f.place("tenant", h.Name)
			target := store.Target{Type: "t4g.large", RootGB: 25}
			if mode == store.ReplaceBackup {
				target.DataGB = 60
			}
			c := startReplacement(f, h, mode, target)
			e := ops.New(f.cfg)
			wantSteps := []string{ops.StepRetire, ops.StepRelaunch}
			c.keepRetiring = true
			equal(t, f.step(e, c.op.ID).Step, ops.StepWaitRetired)
			for range 2 {
				equal(t, f.step(e, c.op.ID).Step, ops.StepWaitRetired)
				r, err := f.st.Replacement(f.ctx, c.op.ID)
				ok(t, err)
				equal(t, r.Retired, false)
				d, err := f.st.Desired(f.ctx, h.Name)
				ok(t, err)
				equal(t, len(d.Accounts), 1)
			}
			c.keepRetiring = false
			stepsTo(f, e, c.op.ID, ops.StepRelaunch)
			d, err := f.st.Desired(f.ctx, h.Name)
			ok(t, err)
			equal(t, len(d.Accounts), 0)
			equal(t, f.step(e, c.op.ID).Step, ops.StepWaitRelaunched)
			onNew := f.host(h.Name)
			equal(t, onNew.InstanceID != h.InstanceID, true)
			equal(t, onNew.AllocationID, h.AllocationID)
			if mode == store.ReplaceBackup {
				stepsTo(f, e, c.op.ID, ops.StepNewVolume)
				equal(t, f.step(e, c.op.ID).Step, ops.StepWaitNewVolume)
				onNew = f.host(h.Name)
				equal(t, onNew.DataVolume != h.DataVolume, true)
				equal(t, onNew.DataGB, target.DataGB)
				v, err := f.st.KeptVolume(f.ctx, h.DataVolume)
				ok(t, err)
				equal(t, v.State, store.VolumeKept)
				equal(t, v.Cause, store.CauseReplaced)
				wantSteps = append(wantSteps, ops.StepNewVolume)
			}
			wantSteps = append(wantSteps, ops.StepReattach)
			if mode == store.ReplaceBackup {
				wantSteps = append(wantSteps, ops.StepTagOld)
			}
			wantSteps = append(wantSteps, ops.StepReassociate)
			if mode == store.ReplaceBackup {
				stepsTo(f, e, c.op.ID, ops.StepRestore)
				equal(t, f.host(h.Name).State, store.HostRestoring)
				reportOperation(f, h.Name, protocol.Step{ID: c.op.ID, State: protocol.StepDone}, []protocol.AccountBackup{
					{Account: "tenant", Restored: protocol.Step{ID: c.op.ID, State: protocol.StepDone, At: f.now}},
				}, false)
			}
			stepsTo(f, e, c.op.ID, ops.StepWaitAdmitted)
			equal(t, c.association != "", true)
			equal(t, f.host(h.Name).AssociationID, c.association)
			d, err = f.st.Desired(f.ctx, h.Name)
			ok(t, err)
			equal(t, len(d.Accounts), 1)
			equal(t, f.step(e, c.op.ID).Step, ops.StepWaitAdmitted)
			before := f.host(h.Name)
			f.report(h.Name, before.Version, before.Version)
			equal(t, f.step(e, c.op.ID).Status, store.OpDone)
			after := f.host(h.Name)
			equal(t, after.State, store.HostRunning)
			equal(t, after.Name, h.Name)
			equal(t, after.AllocationID, h.AllocationID)
			equal(t, after.EIP, h.EIP)
			equal(t, after.InstanceID, onNew.InstanceID)
			equal(t, after.Type, target.Type)
			equal(t, after.RootGB, target.RootGB)
			equal(t, after.DataGB, onNew.DataGB)
			equal(t, after.DataVolume, onNew.DataVolume)
			equal(t, slices.Equal(c.calls, wantSteps), true)
			v, err := f.memory.DescribeVolume(f.ctx, h.DataVolume)
			ok(t, err)
			if mode == store.ReplaceBackup {
				equal(t, v.State, cloud.VolumeAvailable)
				equal(t, v.Tags[cloud.TagOrphaned], cloud.OrphanedTrue)
			} else {
				equal(t, v.Attachment.InstanceID, after.InstanceID)
			}
		})
	}
}

func reportOperation(f *fixture, host string, step protocol.Step, facts []protocol.AccountBackup, final bool) {
	f.t.Helper()
	h := f.host(host)
	step.At = f.now
	r := protocol.Report{Protocol: protocol.Version, Attempted: h.Version, Applied: h.Version, Backups: facts}
	if final {
		r.HostBackup.Final = step
	} else {
		r.HostBackup.Restored = step
	}
	_, err := f.st.RecordReport(f.ctx, host, r, f.now)
	ok(f.t, err)
}

// R-D2DY-7BGX R-D3LU-L37M
func TestReplacementGenerationStopsAfterTerminations(t *testing.T) {
	f := setup(t)
	h := f.ready()
	c := startReplacement(f, h, store.ReplaceVolume, store.Target{})
	e := ops.New(f.cfg)
	stepsTo(f, e, c.op.ID, ops.StepRelaunch)
	for generation := 1; generation <= ops.MaxGenerations; generation++ {
		o := f.step(e, c.op.ID)
		equal(t, o.Step, ops.StepWaitRelaunched)
		equal(t, o.Generation, generation)
		c.onDescribe = func(id string) (cloud.Instance, error) {
			equal(t, id, f.host(h.Name).InstanceID)
			return cloud.Instance{}, cloudErr(cloud.ClassNotFound)
		}
		equal(t, f.step(e, c.op.ID).Step, ops.StepWaitRelaunched)
		c.onDescribe = func(id string) (cloud.Instance, error) {
			equal(t, id, f.host(h.Name).InstanceID)
			return cloud.Instance{ID: id, State: cloud.InstanceTerminated}, nil
		}
		ok(t, f.memory.TerminateInstance(f.ctx, f.host(h.Name).InstanceID))
		o = f.step(e, c.op.ID)
		c.onDescribe = nil
		if generation < ops.MaxGenerations {
			equal(t, o.Generation, generation+1)
			equal(t, o.Step, ops.StepRelaunch)
		} else {
			equal(t, o.Status, store.OpFailed)
			equal(t, o.Error, ops.ReasonTerminated)
		}
	}
	equal(t, len(c.calls), ops.MaxGenerations+1)
}

// R-CXIC-O8I5 R-CYQ9-208U R-CSMR-55JD
func TestReplacementAbsentOldInstanceAndConcurrentOperationEnd(t *testing.T) {
	for _, ended := range []bool{false, true} {
		f := setup(t)
		h := f.ready()
		c := startReplacement(f, h, store.ReplaceVolume, store.Target{})
		e := ops.New(f.cfg)
		c.onDescribe = func(id string) (cloud.Instance, error) {
			equal(t, id, h.InstanceID)
			if ended && f.operation(c.op.ID).Step == ops.StepWaitRetired {
				ok(t, f.st.Fail(f.ctx, c.op.ID, "another writer's result"))
			}
			return cloud.Instance{}, cloudErr(cloud.ClassNotFound)
		}
		equal(t, f.step(e, c.op.ID).Step, ops.StepWaitRetired)
		op := f.step(e, c.op.ID)
		r, err := f.st.Replacement(f.ctx, c.op.ID)
		ok(t, err)
		equal(t, r.Retired, !ended)
		equal(t, len(c.calls), 0)
		if ended {
			equal(t, op.Status, store.OpFailed)
			equal(t, op.Error, "another writer's result")
			for _, event := range f.events() {
				equal(t, event.Attrs["operation"] == op.ID, false)
			}
		} else {
			equal(t, op.Step, ops.StepRelaunch)
		}
	}
}

// R-HHEN-C5N4 R-DEKY-10VV R-3T6L-SNWX
func TestReplacementRestoreChecksLivenessAndReportsStateAfterCommit(t *testing.T) {
	f := setup(t)
	h := f.ready()
	c := startReplacement(f, h, store.ReplaceBackup, store.Target{})
	var formed []string
	stopObserving := f.observeEventFormation(func() {
		formed = append(formed, f.host(h.Name).State)
	})
	e := ops.New(f.cfg)
	stepsTo(f, e, c.op.ID, ops.StepRestore)
	equal(t, slices.Equal(formed, []string{store.HostRestoring}), true)
	restoring := f.host(h.Name)
	f.now = f.now.Add(2 * ops.StepTimeout)
	f.report(h.Name, restoring.Version, restoring.Version)
	equal(t, f.step(e, c.op.ID).Status, store.OpRunning)
	reportOperation(f, h.Name, protocol.Step{ID: c.op.ID, State: protocol.StepFailed, Code: protocol.CodeNotEmpty}, nil, false)
	o := f.step(e, c.op.ID)
	equal(t, o.Status, store.OpFailed)
	equal(t, o.Error, ops.ReasonNotEmpty)
	equal(t, f.host(h.Name).State, store.HostFailed)
	retrier := identity.Caller{UserID: "replacement-retrier", RequestID: "retry-restore"}
	_, _, err := f.st.RetryHost(f.ctx, h.Name, retrier)
	ok(t, err)
	equal(t, f.host(h.Name).State, store.HostReplacing)
	formed = nil
	o = f.step(e, o.ID)
	stopObserving()
	equal(t, len(formed) > 0, true)
	equal(t, formed[0], store.HostRestoring)
	equal(t, o.Step, ops.StepRestore)
	equal(t, o.Error, ops.ReasonNotEmpty)
	events := f.events()
	attrs := telemetry.Attrs{"host": h.Name, "from": store.HostReplacing, "to": store.HostRestoring, "operation": o.ID}
	events = slices.DeleteFunc(events, func(event telemetry.Event) bool {
		return event.Name != trail.EventHostChanged || !reflect.DeepEqual(event.Attrs, attrs)
	})
	equal(t, len(events), 2)
	for _, who := range []identity.Caller{author, retrier} {
		var own []telemetry.Event
		for _, event := range events {
			if event.User == who.UserID {
				own = append(own, event)
			}
		}
		eventWith(t, own, trail.EventHostChanged, attrs, who)
	}
}

func TestReplacementHostLossAtBothDaemonWaits(t *testing.T) {
	for _, wait := range []string{ops.StepRestore, ops.StepWaitAdmitted} {
		for _, outcome := range []string{ops.ReasonMissing, ops.ReasonTerminated, ops.ReasonTimedOut} {
			t.Run(wait+"/"+outcome, func(t *testing.T) {
				f := setup(t)
				h := f.ready()
				mode := store.ReplaceBackup
				if wait == ops.StepWaitAdmitted {
					mode = store.ReplaceVolume
				}
				c := startReplacement(f, h, mode, store.Target{})
				e := ops.New(f.cfg)
				stepsTo(f, e, c.op.ID, wait)
				mutations := len(c.calls)
				switch outcome {
				case ops.ReasonMissing:
					c.onDescribe = func(string) (cloud.Instance, error) { return cloud.Instance{}, cloudErr(cloud.ClassNotFound) }
				case ops.ReasonTerminated:
					c.onDescribe = func(string) (cloud.Instance, error) { return cloud.Instance{State: cloud.InstanceStopped}, nil }
				case ops.ReasonTimedOut:
					f.now = f.now.Add(ops.StepTimeout + time.Second)
				}
				o := f.step(e, c.op.ID)
				equal(t, o.Status, store.OpFailed)
				equal(t, o.Error, outcome)
				equal(t, f.host(h.Name).State, store.HostFailed)
				equal(t, len(c.calls), mutations)
			})
		}
	}
}

// R-D161-TJQ8 R-D4TQ-YUYB
func TestReplacementAdoptsResourcesAfterLostAnswers(t *testing.T) {
	f := setup(t)
	h := f.ready()
	c := startReplacement(f, h, store.ReplaceBackup, store.Target{DataGB: 65})
	e := ops.New(f.cfg)
	stepsTo(f, e, c.op.ID, ops.StepRelaunch)
	var launched cloud.Instance
	c.onLaunch = func(spec cloud.Launch) (cloud.Instance, error) {
		var err error
		launched, err = f.memory.RunInstance(f.ctx, spec)
		ok(t, err)
		return cloud.Instance{}, cloudErr(cloud.ClassTransient)
	}
	equal(t, f.step(e, c.op.ID).Step, ops.StepRelaunch)
	equal(t, f.host(h.Name).InstanceID, h.InstanceID)
	c.instancePage = []cloud.Instance{
		{ID: "zz-live", State: cloud.InstanceStopped},
		{ID: "00-terminated", State: cloud.InstanceTerminated},
		launched,
	}
	e = ops.New(f.cfg)
	equal(t, f.step(e, c.op.ID).Step, ops.StepWaitRelaunched)
	equal(t, f.host(h.Name).InstanceID, launched.ID)
	equal(t, len(c.calls), 2)
	stepsTo(f, e, c.op.ID, ops.StepNewVolume)
	var created cloud.Volume
	c.onVolume = func(spec cloud.VolumeSpec) (cloud.Volume, error) {
		var err error
		created, err = f.memory.CreateVolume(f.ctx, spec)
		ok(t, err)
		return cloud.Volume{}, cloudErr(cloud.ClassTransient)
	}
	equal(t, f.step(e, c.op.ID).Step, ops.StepNewVolume)
	equal(t, f.host(h.Name).DataVolume, h.DataVolume)
	c.volumePage = []cloud.Volume{{ID: "zz-data"}, created, {ID: "zzz-data"}}
	equal(t, f.step(ops.New(f.cfg), c.op.ID).Step, ops.StepWaitNewVolume)
	equal(t, f.host(h.Name).DataVolume, created.ID)
	equal(t, len(c.calls), 3)
}

// R-D161-TJQ8
func TestReplacementWithNoAccountsURLSendsNoRequest(t *testing.T) {
	f := setup(t)
	h := f.ready()
	c := startReplacement(f, h, store.ReplaceVolume, store.Target{})
	e := ops.New(f.cfg)
	stepsTo(f, e, c.op.ID, ops.StepRelaunch)
	f.cfg.AccountsURL = func() (string, error) { return "", nil }
	e = ops.New(f.cfg)
	// Use a current scan stamp with the base cloud; the step must never
	// call the nil cloud while the URL is unavailable.
	ok(t, e.Scan(f.ctx))
	c.Cloud = nil
	o := f.step(e, c.op.ID)
	equal(t, o.Status, store.OpFailed)
	equal(t, o.Error, ops.ReasonNoAccountsURL)
	equal(t, len(c.calls), 1)
}

// R-D3LU-L37M
func TestReplacementRecordsNewInstancePlacement(t *testing.T) {
	f := setup(t)
	h := f.ready()
	c := startReplacement(f, h, store.ReplaceBackup, store.Target{Type: "t4g.large", RootGB: 30})
	e := ops.New(f.cfg)
	stepsTo(f, e, c.op.ID, ops.StepWaitRelaunched)
	previous := f.host(h.Name)
	newAZ, newRoot := h.AZ+"-replacement", h.RootVolume+"-replacement"
	c.onDescribe = func(id string) (cloud.Instance, error) {
		equal(t, id, previous.InstanceID)
		return cloud.Instance{State: cloud.InstanceRunning, AZ: newAZ, RootVolume: newRoot}, nil
	}
	equal(t, f.step(e, c.op.ID).Step, ops.StepNewVolume)
	after := f.host(h.Name)
	equal(t, after.AZ, newAZ)
	equal(t, after.RootVolume, newRoot)
	equal(t, after.Type, c.op.Target.Type)
	equal(t, after.RootGB, c.op.Target.RootGB)
}

// R-D61N-CMP0 R-D8HG-466E
func TestReplacementVolumeWaitsCheckAttachmentTargetAndState(t *testing.T) {
	for _, state := range []string{cloud.VolumeError, cloud.VolumeInUse} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			h := f.ready()
			c := startReplacement(f, h, store.ReplaceBackup, store.Target{})
			e := ops.New(f.cfg)
			stepsTo(f, e, c.op.ID, ops.StepWaitNewVolume)
			c.onData = func(string) (cloud.Volume, error) {
				return cloud.Volume{}, cloudErr(cloud.ClassNotFound)
			}
			equal(t, f.step(e, c.op.ID).Step, ops.StepWaitNewVolume)
			newHost := f.host(h.Name)
			actual := cloud.Volume{ID: newHost.DataVolume, State: state}
			c.onData = func(id string) (cloud.Volume, error) {
				equal(t, id, actual.ID)
				return actual, nil
			}
			if state == cloud.VolumeError {
				o := f.step(e, c.op.ID)
				equal(t, o.Status, store.OpFailed)
				equal(t, o.Error, ops.ReasonVolumeFailed)
				return
			}
			actual.Attachment.InstanceID = newHost.InstanceID
			actual.Attachment.State = cloud.AttachmentAttaching
			equal(t, f.step(e, c.op.ID).Step, ops.StepReattach)
			mutations := len(c.calls)
			actual.Attachment.InstanceID = h.InstanceID
			equal(t, f.step(e, c.op.ID).Step, ops.StepReattach)
			actual.Attachment.InstanceID = newHost.InstanceID
			equal(t, f.step(e, c.op.ID).Step, ops.StepWaitReattached)
			equal(t, f.step(e, c.op.ID).Step, ops.StepWaitReattached)
			actual.Attachment.State = cloud.AttachmentAttached
			actual.Attachment.InstanceID = h.InstanceID
			equal(t, f.step(e, c.op.ID).Step, ops.StepWaitReattached)
			actual.Attachment.InstanceID = newHost.InstanceID
			equal(t, f.step(e, c.op.ID).Step, ops.StepTagOld)
			equal(t, len(c.calls), mutations)
		})
	}
}

// R-D9PC-HXX3
func TestReplacementTagOldHandlesMissingAndEmptyVolume(t *testing.T) {
	for _, oldID := range []string{"", "vol-old"} {
		t.Run(fmt.Sprintf("has-old-volume=%t", oldID != ""), func(t *testing.T) {
			f := setup(t)
			h, create := f.create()
			ok(t, f.st.Finish(f.ctx, create.ID, store.Update{DataVolume: oldID, AllocationID: "eip-existing"}))
			c := startReplacement(f, f.host(h.Name), store.ReplaceBackup, store.Target{})
			ok(t, f.st.Retire(f.ctx, c.op.ID))
			ok(t, f.st.Advance(f.ctx, c.op.ID, store.Update{Step: ops.StepTagOld}))
			c.onTag = func() error { return cloudErr(cloud.ClassNotFound) }
			equal(t, f.step(ops.New(f.cfg), c.op.ID).Step, ops.StepReassociate)
			if oldID == "" {
				equal(t, len(c.calls), 0)
			} else {
				equal(t, slices.Equal(c.calls, []string{ops.StepTagOld}), true)
			}
		})
	}
}

// R-HHEN-C5N4
func TestReplacementRestoreFailurePrefersNotEmptyAcrossStepSet(t *testing.T) {
	for _, code := range []string{protocol.CodeFailed, protocol.CodeNotEmpty} {
		t.Run(code, func(t *testing.T) {
			f := setup(t)
			h := f.ready()
			f.place("tenant", h.Name)
			c := startReplacement(f, h, store.ReplaceBackup, store.Target{})
			e := ops.New(f.cfg)
			stepsTo(f, e, c.op.ID, ops.StepRestore)
			reportOperation(f, h.Name, protocol.Step{ID: c.op.ID, State: protocol.StepFailed, Code: protocol.CodeFailed}, []protocol.AccountBackup{
				{Account: "tenant", Error: "private diagnostic", Restored: protocol.Step{ID: c.op.ID, State: protocol.StepFailed, At: f.now, Code: code}},
			}, false)
			result := f.step(e, c.op.ID)
			equal(t, result.Status, store.OpFailed)
			want := ops.ReasonRestoreFailed
			if code == protocol.CodeNotEmpty {
				want = ops.ReasonNotEmpty
			}
			equal(t, result.Error, want)
		})
	}
}
