package ops_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

type observedCloud struct {
	cloud.Cloud
	f            *fixture
	id           string
	allocate     []cloud.Tags
	launch       []cloud.Launch
	createVolume []cloud.VolumeSpec
	attach       [][3]string
	associate    [][2]string
	stop         []string
	start        []string
	setType      [][2]string
	modify       []cloud.VolumeSpec
}

func (c *observedCloud) recorded(step string) store.Operation {
	c.f.t.Helper()
	o := c.f.operation(c.id)
	equal(c.f.t, o.Step, step)
	if o.Generation < 1 || o.StepStarted.IsZero() {
		c.f.t.Fatalf("step not recorded: %+v", o)
	}
	return o
}

func (c *observedCloud) AllocateAddress(ctx context.Context, tags cloud.Tags) (cloud.Address, error) {
	c.recorded(ops.StepAddress)
	c.allocate = append(c.allocate, tags)
	return c.Cloud.AllocateAddress(ctx, tags)
}

func (c *observedCloud) RunInstance(ctx context.Context, launch cloud.Launch) (cloud.Instance, error) {
	o := c.recorded(ops.StepLaunch)
	equal(c.f.t, launch.ClientToken, fmt.Sprintf("%s-%s-%d", o.ID, o.Step, o.Generation))
	c.launch = append(c.launch, launch)
	return c.Cloud.RunInstance(ctx, launch)
}

func (c *observedCloud) CreateVolume(ctx context.Context, spec cloud.VolumeSpec) (cloud.Volume, error) {
	o := c.recorded(ops.StepVolume)
	equal(c.f.t, spec.ClientToken, fmt.Sprintf("%s-%s-%d", o.ID, o.Step, o.Generation))
	c.createVolume = append(c.createVolume, spec)
	return c.Cloud.CreateVolume(ctx, spec)
}

func (c *observedCloud) AttachVolume(ctx context.Context, vol, instance, device string) error {
	c.recorded(ops.StepAttach)
	c.attach = append(c.attach, [3]string{vol, instance, device})
	return c.Cloud.AttachVolume(ctx, vol, instance, device)
}

func (c *observedCloud) AssociateAddress(ctx context.Context, allocation, instance string) (string, error) {
	c.recorded(ops.StepAssociate)
	c.associate = append(c.associate, [2]string{allocation, instance})
	return c.Cloud.AssociateAddress(ctx, allocation, instance)
}

func (c *observedCloud) StopInstance(ctx context.Context, id string) error {
	c.recorded(ops.StepStop)
	c.stop = append(c.stop, id)
	return c.Cloud.StopInstance(ctx, id)
}

func (c *observedCloud) StartInstance(ctx context.Context, id string) error {
	c.recorded(ops.StepStart)
	c.start = append(c.start, id)
	return c.Cloud.StartInstance(ctx, id)
}

func (c *observedCloud) SetInstanceType(ctx context.Context, id, typ string) error {
	c.recorded(ops.StepSetType)
	c.setType = append(c.setType, [2]string{id, typ})
	return c.Cloud.SetInstanceType(ctx, id, typ)
}

func (c *observedCloud) ModifyVolume(ctx context.Context, id string, size int32) error {
	step := ops.StepGrowRoot
	if c.f.operation(c.id).Step == ops.StepGrowData {
		step = ops.StepGrowData
	}
	c.recorded(step)
	c.modify = append(c.modify, cloud.VolumeSpec{AZ: id, SizeGB: size})
	return c.Cloud.ModifyVolume(ctx, id, size)
}

func tagset(h store.Host, op store.Operation, kind string) cloud.Tags {
	return cloud.Tags{cloud.TagKind: kind, cloud.TagHost: h.Name, cloud.TagOperation: op.ID}
}

func checkTags(t *testing.T, got, want cloud.Tags) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags %v, want %v", got, want)
	}
}

// R-MNTG-22XB R-MP1C-FUO0 R-MQ98-TMEP R-MWCQ-QH46 R-MXKN-48UV R-RWXF-7MED R-N18C-9K2Y R-N2G8-NBTN R-N4W1-EVB1 R-N63X-SN1Q R-KFNV-I3W9
func TestCreateThroughRestarts(t *testing.T) {
	f := setup(t)
	h, op := f.create()
	cl := &observedCloud{Cloud: f.memory, f: f, id: op.ID}
	f.cfg.Cloud = cl
	want := []string{
		ops.StepLaunch, ops.StepWaitRunning, ops.StepVolume, ops.StepWaitVolume, ops.StepAttach,
		ops.StepWaitAttached, ops.StepAssociate, ops.StepAssociate,
	}
	for _, next := range want {
		// Every pass is a startup pass, with a fresh engine and the old store and cloud.
		f.now = f.now.Add(ReconcileAdvance)
		ctx, cancel := context.WithCancel(f.ctx)
		f.cfg.After = func(time.Duration) <-chan time.Time {
			cancel()
			return make(chan time.Time)
		}
		ok(t, ops.New(f.cfg).Run(ctx))
		cancel()
		equal(t, f.operation(op.ID).Step, next)
	}
	equal(t, f.operation(op.ID).Status, store.OpDone)
	final := f.host(h.Name)
	equal(t, final.State, store.HostRunning)
	equal(t, final.Version, h.Version+1)
	equal(t, len(cl.allocate), 1)
	equal(t, len(cl.launch), 1)
	equal(t, len(cl.createVolume), 1)
	equal(t, len(cl.attach), 1)
	equal(t, len(cl.associate), 1)
	checkTags(t, cl.allocate[0], tagset(h, op, cloud.KindHost))
	l := cl.launch[0]
	checkTags(t, l.Tags, tagset(h, op, cloud.KindHost))
	checkTags(t, l.InstanceTags, cloud.Tags{cloud.TagAccountsURL: accountURL})
	equal(t, l.Template, f.cfg.Hosts.LaunchTemplate)
	equal(t, l.InstanceProfile, f.cfg.Hosts.InstanceProfile)
	equal(t, l.Type, h.Type)
	equal(t, int(l.RootGB), h.RootGB)
	v := cl.createVolume[0]
	checkTags(t, v.Tags, tagset(h, op, cloud.KindHostData))
	equal(t, v.AZ, final.AZ)
	equal(t, int(v.SizeGB), h.DataGB)
	equal(t, cl.attach[0], [3]string{final.DataVolume, final.InstanceID, ops.DataDevice})
	equal(t, cl.associate[0], [2]string{final.AllocationID, final.InstanceID})
	i, err := f.memory.DescribeInstance(f.ctx, final.InstanceID)
	ok(t, err)
	equal(t, final.AZ, i.AZ)
	equal(t, final.RootVolume, i.RootVolume)
	a, err := f.memory.DescribeAddress(f.ctx, final.AllocationID)
	ok(t, err)
	equal(t, a.InstanceID, final.InstanceID)
	equal(t, a.AssociationID, final.AssociationID)
}

type replayCloud struct {
	cloud.Cloud
	f        *fixture
	id       string
	launches []string
	volumes  []string
}

func (c *replayCloud) FindInstances(_ context.Context, _ cloud.Tags, _ func([]cloud.Instance) bool) error {
	return nil
}

func (c *replayCloud) FindVolumes(_ context.Context, _ cloud.Tags, _ func([]cloud.Volume) bool) error {
	return nil
}

func (c *replayCloud) RunInstance(_ context.Context, l cloud.Launch) (cloud.Instance, error) {
	o := c.f.operation(c.id)
	equal(c.f.t, o.Step, ops.StepLaunch)
	c.launches = append(c.launches, l.ClientToken)
	if len(c.launches) == 1 {
		return cloud.Instance{}, cloudErr(cloud.ClassTransient)
	}
	return cloud.Instance{ID: "terminated", State: cloud.InstanceTerminated}, nil
}

func (c *replayCloud) CreateVolume(_ context.Context, s cloud.VolumeSpec) (cloud.Volume, error) {
	o := c.f.operation(c.id)
	equal(c.f.t, o.Step, ops.StepVolume)
	c.volumes = append(c.volumes, s.ClientToken)
	if len(c.volumes) == 1 {
		return cloud.Volume{}, cloudErr(cloud.ClassTransient)
	}
	return cloud.Volume{ID: "data"}, nil
}

// R-MQ98-TMEP R-MYSJ-I0LK
func TestTokensPersistPerGeneration(t *testing.T) {
	f := setup(t)
	_, op := f.create()
	cl := &replayCloud{Cloud: f.memory, f: f, id: op.ID}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	f.step(e, op.ID)
	for range ops.MaxGenerations + 1 {
		op = f.step(ops.New(f.cfg), op.ID)
	}
	equal(t, op.Status, store.OpFailed)
	equal(t, op.Error, ops.ReasonTerminated)
	want := []string{
		op.ID + "-launch-1", op.ID + "-launch-1", op.ID + "-launch-2", op.ID + "-launch-3",
	}
	if !reflect.DeepEqual(cl.launches, want) {
		t.Fatalf("launch tokens %v, want %v", cl.launches, want)
	}
	h, op := f.create()
	cl.id = op.ID
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepVolume, AZ: "example-zone"}))
	equal(t, f.step(e, op.ID).Step, ops.StepVolume)
	equal(t, f.step(ops.New(f.cfg), op.ID).Step, ops.StepWaitVolume)
	equal(t, f.host(h.Name).DataVolume, "data")
	want = []string{op.ID + "-volume-1", op.ID + "-volume-1"}
	if !reflect.DeepEqual(cl.volumes, want) {
		t.Fatalf("volume tokens %v, want %v", cl.volumes, want)
	}
}

type adoptCloud struct {
	cloud.Cloud
	f     *fixture
	op    store.Operation
	state string
}

func (c *adoptCloud) FindAddresses(_ context.Context, tags cloud.Tags, each func([]cloud.Address) bool) error {
	if tags[cloud.TagOperation] != "" {
		checkTags(c.f.t, tags, cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagOperation: c.op.ID})
		each([]cloud.Address{{AllocationID: "eip-z", IP: "192.0.2.10"}})
		each([]cloud.Address{{AllocationID: "eip-a", IP: "192.0.2.15"}})
	}
	return nil
}

func (c *adoptCloud) FindInstances(_ context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	if tags[cloud.TagOperation] != "" {
		checkTags(c.f.t, tags, cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagOperation: c.op.ID})
		each([]cloud.Instance{{ID: "i-a", State: cloud.InstanceTerminated}, {ID: "i-z", State: cloud.InstanceRunning}})
		each([]cloud.Instance{{ID: "i-b", State: c.state}})
	}
	return nil
}

func (c *adoptCloud) FindVolumes(_ context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	if tags[cloud.TagOperation] != "" {
		checkTags(c.f.t, tags, cloud.Tags{cloud.TagKind: cloud.KindHostData, cloud.TagOperation: c.op.ID})
		each([]cloud.Volume{{ID: "vol-z"}})
		each([]cloud.Volume{{ID: "vol-a"}})
	}
	return nil
}

func (*adoptCloud) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	return cloud.Instance{ID: id, State: cloud.InstanceRunning, AZ: "zone", RootVolume: "root"}, nil
}

// R-MWCQ-QH46 R-MXKN-48UV R-N18C-9K2Y R-KFNV-I3W9
func TestCreateAdoptsLowestIDsAcrossPages(t *testing.T) {
	for _, state := range []string{cloud.InstancePending, cloud.InstanceRunning, cloud.InstanceStopping, cloud.InstanceStopped} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			h, op := f.create()
			f.cfg.Cloud = &adoptCloud{f: f, op: op, state: state}
			e := ops.New(f.cfg)
			equal(t, f.step(e, op.ID).Step, ops.StepLaunch)
			h = f.host(h.Name)
			equal(t, h.AllocationID, "eip-a")
			equal(t, h.EIP, "192.0.2.15")
			equal(t, f.step(e, op.ID).Step, ops.StepWaitRunning)
			equal(t, f.host(h.Name).InstanceID, "i-b")
			equal(t, f.step(e, op.ID).Step, ops.StepVolume)
			equal(t, f.step(e, op.ID).Step, ops.StepWaitVolume)
			equal(t, f.host(h.Name).DataVolume, "vol-a")
		})
	}
}

// R-MXKN-48UV R-UI00-L4XM
func TestLaunchRequiresURLAndCheckedSizes(t *testing.T) {
	for _, tc := range []struct {
		name, step, reason string
		size               int
		url                func() (string, error)
	}{
		{"missing URL", ops.StepLaunch, ops.ReasonNoAccountsURL, 20, func() (string, error) { return "", nil }},
		{"URL error", ops.StepLaunch, ops.ReasonNoAccountsURL, 20, func() (string, error) { return accountURL, errors.New("bad URL") }},
		{"large root", ops.StepLaunch, ops.ReasonBadSize, math.MaxInt32 + 1, func() (string, error) { return accountURL, nil }},
		{"large data", ops.StepVolume, ops.ReasonBadSize, math.MaxInt32 + 1, func() (string, error) { return accountURL, nil }},
		{"large modify", ops.StepGrowRoot, ops.ReasonBadSize, math.MaxInt32 + 1, func() (string, error) { return accountURL, nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			h, op, err := f.st.CreateHost(f.ctx, store.HostSpec{Type: "t4g.medium", RootGB: tc.size, DataGB: tc.size}, author)
			ok(t, err)
			if tc.step == ops.StepGrowRoot {
				ok(t, f.st.Finish(f.ctx, op.ID, store.Update{RootGB: 20, DataGB: 50}))
				op = f.start(f.host(h.Name), store.OpResize, store.Target{RootGB: tc.size})
			}
			ok(t, f.st.RecordStep(f.ctx, op.ID, tc.step, op.Generation, f.now))
			f.cfg.AccountsURL = tc.url
			// The scan can list, but calls for the operation would use nil methods.
			f.cfg.Cloud = &pagedStrays{st: f.st, t: t, now: f.clock}
			e := ops.New(f.cfg)
			ok(t, e.Scan(f.ctx))
			ended := f.step(e, op.ID)
			equal(t, ended.Status, store.OpFailed)
			equal(t, ended.Error, tc.reason)
		})
	}
}

// R-N7BU-6ESF R-N8JQ-K6J4 R-RWXF-7MED R-N63X-SN1Q R-MNTG-22XB
func TestStopAndStartKeepAddress(t *testing.T) {
	f := setup(t)
	h := f.ready()
	op := f.start(h, store.OpStop, store.Target{})
	cl := &observedCloud{Cloud: f.memory, f: f, id: op.ID}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitStopped)
	f.complete(e, op.ID)
	equal(t, f.host(h.Name).State, store.HostStopped)
	equal(t, len(cl.stop), 1)
	equal(t, cl.stop[0], h.InstanceID)

	op = f.start(h, store.OpStart, store.Target{})
	cl.id = op.ID
	equal(t, f.step(e, op.ID).Step, ops.StepWaitRunning)
	equal(t, f.step(e, op.ID).Step, ops.StepAssociate)
	f.complete(e, op.ID)
	restarted := f.host(h.Name)
	equal(t, restarted.State, store.HostRunning)
	equal(t, restarted.AllocationID, h.AllocationID)
	equal(t, restarted.EIP, h.EIP)
	equal(t, cl.associate[0], [2]string{h.AllocationID, h.InstanceID})
	equal(t, len(cl.start), 1)
	equal(t, cl.start[0], h.InstanceID)
}
