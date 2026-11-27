package ops_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type deletionCloud struct {
	*recordCloud
	f           *fixture
	deleteID    string
	createID    string
	gone        bool
	terminated  []string
	disassoc    []string
	released    []string
	orphaned    []string
	orphanedTag func()
	storedData  cloud.Volume
	otherData   []cloud.Volume
	deleteData  []string
}

func (c *deletionCloud) lookup(tags cloud.Tags, kind string) bool {
	c.f.t.Helper()
	if tags[cloud.TagOperation] == "" {
		return false
	}
	checkTags(c.f.t, tags, cloud.Tags{cloud.TagOperation: c.createID, cloud.TagKind: kind})
	c.checkChoice()
	return true
}

func (c *deletionCloud) checkChoice() {
	c.f.t.Helper()
	op := c.f.operation(c.deleteID)
	h := c.f.host(op.Host)
	switch op.Step {
	case ops.StepDeleteData, ops.StepWaitDataDeleted:
		equal(c.f.t, h.OnDelete, store.OnDeleteDelete)
	case ops.StepOrphan:
		equal(c.f.t, h.OnDelete, store.OnDeleteKeep)
	}
}

func (c *deletionCloud) FindRecords(ctx context.Context, zone, name string) ([]cloud.Record, error) {
	c.checkChoice()
	return c.recordCloud.FindRecords(ctx, zone, name)
}

func (c *deletionCloud) ListKeys(_ context.Context, _, _ string, _ func([]string) bool) error {
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepDeleteBackups)
	return nil
}

func (c *deletionCloud) ChangeRecord(ctx context.Context, zone string, change cloud.RecordChange) (string, error) {
	c.checkChoice()
	return c.recordCloud.ChangeRecord(ctx, zone, change)
}

func (c *deletionCloud) ChangeInsync(ctx context.Context, change string) (bool, error) {
	c.checkChoice()
	return c.recordCloud.ChangeInsync(ctx, change)
}

func (c *deletionCloud) FindInstances(_ context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	if c.lookup(tags, cloud.KindHost) {
		state := cloud.InstanceShuttingDown
		if c.gone {
			state = cloud.InstanceTerminated
		}
		if each([]cloud.Instance{{ID: "i-stored", State: state}}) {
			each([]cloud.Instance{{ID: "i-tagged", State: state}})
		}
	}
	return nil
}

func (c *deletionCloud) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	c.checkChoice()
	equal(c.f.t, id, "i-stored")
	return cloud.Instance{}, cloudErr(cloud.ClassNotFound)
}

func (c *deletionCloud) TerminateInstance(_ context.Context, id string) error {
	c.checkChoice()
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepTerminate)
	c.terminated = append(c.terminated, id)
	if id == "i-stored" {
		return cloudErr(cloud.ClassNotFound)
	}
	return nil
}

func (c *deletionCloud) FindAddresses(_ context.Context, tags cloud.Tags, each func([]cloud.Address) bool) error {
	if c.lookup(tags, cloud.KindHost) {
		if each([]cloud.Address{{AllocationID: "ip-stored"}}) {
			each([]cloud.Address{{AllocationID: "ip-tagged", AssociationID: "assoc-tagged"}})
		}
	}
	return nil
}

func (c *deletionCloud) DescribeAddress(_ context.Context, id string) (cloud.Address, error) {
	c.checkChoice()
	equal(c.f.t, id, "ip-stored")
	return cloud.Address{AllocationID: id, AssociationID: "assoc-stored"}, nil
}

func (c *deletionCloud) DisassociateAddress(_ context.Context, association string) error {
	c.checkChoice()
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepRelease)
	c.disassoc = append(c.disassoc, association)
	return nil
}

func (c *deletionCloud) ReleaseAddress(_ context.Context, allocation string) error {
	c.checkChoice()
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepRelease)
	c.released = append(c.released, allocation)
	if allocation == "ip-tagged" {
		return cloudErr(cloud.ClassNotFound)
	}
	return nil
}

func (c *deletionCloud) FindVolumes(_ context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	if c.lookup(tags, cloud.KindHostData) {
		if c.otherData != nil {
			for _, volume := range c.otherData {
				if !each([]cloud.Volume{volume}) {
					break
				}
			}
			return nil
		}
		if each([]cloud.Volume{{ID: "v-stored"}}) {
			each([]cloud.Volume{{ID: "v-tagged"}})
		}
	}
	return nil
}

func (c *deletionCloud) DescribeVolume(_ context.Context, id string) (cloud.Volume, error) {
	c.checkChoice()
	equal(c.f.t, id, "v-stored")
	if c.storedData.ID == "" {
		return cloud.Volume{}, cloudErr(cloud.ClassNotFound)
	}
	return c.storedData, nil
}

func (c *deletionCloud) DeleteVolume(_ context.Context, id string) error {
	c.checkChoice()
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepDeleteData)
	if id == c.storedData.ID {
		equal(c.f.t, c.storedData.State, cloud.VolumeAvailable)
		c.storedData.State = cloud.VolumeDeleting
	} else {
		for i, v := range c.otherData {
			if v.ID == id {
				equal(c.f.t, v.State, cloud.VolumeAvailable)
				c.otherData[i].State = cloud.VolumeDeleting
			}
		}
	}
	c.deleteData = append(c.deleteData, id)
	if id == "v-gone" {
		return cloudErr(cloud.ClassNotFound)
	}
	return nil
}

func (c *deletionCloud) TagResource(_ context.Context, id string, tags cloud.Tags) error {
	c.checkChoice()
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepOrphan)
	checkTags(c.f.t, tags, cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue})
	c.orphaned = append(c.orphaned, id)
	if id == "v-stored" {
		return cloudErr(cloud.ClassNotFound)
	}
	if c.orphanedTag != nil {
		c.orphanedTag()
	}
	return nil
}

func checkIDs(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got ids %v, want %v", got, want)
	}
}

// R-MNTG-22XB R-MP1C-FUO0 R-NFV4-USZA R-IZ7Z-00CB R-J0FV-DS30 R-NIAX-MCGO R-97G8-3Z45 R-9NAX-2ZR6 R-9CBT-N22X
func TestDeleteSettlesDNSBeforeReleasingAndWalksCreateResources(t *testing.T) {
	f := setup(t)
	// Give the create the highest ID so its resources only appear after
	// stepping through a full page of stop/start history.
	f.st = store.New(f.db, store.Config{Now: f.clock, Rand: io.MultiReader(strings.NewReader(strings.Repeat("\xff", 8)), testRandom(1))})
	f.cfg.Store = f.st
	h, create := f.create()
	h.EIP = "192.0.2.9"
	ok(t, f.st.Finish(f.ctx, create.ID, store.Update{
		InstanceID: "i-stored", AllocationID: "ip-stored", EIP: h.EIP, DataVolume: "v-stored",
	}))
	for range store.BatchSize/2 + 1 {
		stopping := f.start(h, store.OpStop, store.Target{})
		ok(t, f.st.Finish(f.ctx, stopping.ID, store.Update{}))
		starting := f.start(h, store.OpStart, store.Target{})
		ok(t, f.st.Finish(f.ctx, starting.ID, store.Update{}))
	}
	z, _ := f.place("zulu", h.Name)
	a, _ := f.place("alpha", h.Name)
	_, zRemove := f.remove(z.Slug)
	_, aRemove := f.remove(a.Slug)
	// The host has removing accounts whose unplacements stopped.
	for _, op := range []store.Operation{zRemove, aRemove} {
		ok(t, f.st.Fail(f.ctx, op.ID, "host never applied removal"))
	}
	deleter := identity.Caller{UserID: "deleter", RequestID: "delete-request"}
	del := f.deleteHost(h, true, deleter)
	suffix := "." + f.cfg.Hosts.Domain
	records := &recordCloud{
		zone: f.cfg.Hosts.ZoneID,
		records: map[string][]cloud.Record{
			a.Slug + suffix: {{Type: "A", Values: []string{h.EIP}, TTL: 61}},
			z.Slug + suffix: {{Type: "A", Values: []string{"192.0.2.19"}, TTL: 32}},
			h.Name + suffix: {
				{Type: "A", Values: []string{"192.0.2.21"}, TTL: 47},
				{Type: "A", Values: []string{h.EIP}, TTL: 48},
				{Type: "TXT", Values: []string{h.EIP}, TTL: 49},
				{Type: "A", Values: []string{h.EIP, "192.0.2.23"}, TTL: 50},
			},
		},
		before: func() { equal(t, f.operation(del.ID).Step, ops.StepUnrecord) },
	}
	retryCaller := identity.Caller{UserID: "retrying-operator", RequestID: "retry-request"}
	var retried store.Operation
	cl := &deletionCloud{
		recordCloud: records, f: f, createID: create.ID, deleteID: del.ID,
		orphanedTag: func() {
			var err error
			_, retried, _, err = f.st.RemoveAccount(f.ctx, a.Slug, retryCaller)
			ok(t, err)
			ok(t, f.st.Defer(f.ctx, retried.ID, f.now.Add(time.Hour), 0))
		},
	}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	equal(t, f.step(e, del.ID).Step, ops.StepWaitUnrecord)
	equal(t, len(records.lookups), 3)
	var accountLookups []string
	for _, name := range records.lookups {
		if name != h.Name+suffix {
			accountLookups = append(accountLookups, name)
		}
	}
	checkIDs(t, accountLookups, []string{a.Slug + suffix, z.Slug + suffix})
	equal(t, len(records.changes), 2)
	changes := make(map[cloud.RecordChange]string)
	for i, change := range records.changes {
		changes[change] = fmt.Sprintf("change-%d", i+1)
	}
	accountChange := changes[cloud.RecordChange{Action: cloud.RecordDelete, Name: a.Slug + suffix, IP: h.EIP, TTL: 61}]
	hostChange := changes[cloud.RecordChange{Action: cloud.RecordDelete, Name: h.Name + suffix, IP: h.EIP, TTL: 48}]
	if accountChange == "" || hostChange == "" {
		t.Fatalf("missing matching account or host deletion: %v", records.changes)
	}
	equal(t, f.account(a.Slug).Change, accountChange)
	equal(t, f.account(z.Slug).Change, "")
	equal(t, f.host(h.Name).Change, hostChange)
	records.sync = func(id string) (bool, error) { return id == accountChange, nil }
	equal(t, f.step(e, del.ID).Step, ops.StepWaitUnrecord)
	equal(t, len(cl.terminated), 0)
	equal(t, len(cl.released), 0)
	records.queried = nil
	f.now = f.now.Add(ops.RecordSettle)
	equal(t, f.step(e, del.ID).Step, ops.StepWaitUnrecord)
	equal(t, slices.Contains(records.queried, hostChange), true)
	records.sync = func(id string) (bool, error) {
		if id == hostChange {
			return false, cloudErr(cloud.ClassNotFound)
		}
		return true, nil
	}
	equal(t, f.step(e, del.ID).Step, ops.StepTerminate)
	equal(t, slices.Contains(records.queried, accountChange), true)
	equal(t, len(cl.released), 0)
	equal(t, f.step(e, del.ID).Step, ops.StepWaitTerminated)
	checkIDs(t, cl.terminated, []string{"i-stored", "i-tagged"})
	equal(t, f.step(e, del.ID).Step, ops.StepWaitTerminated)
	equal(t, len(cl.released), 0)
	cl.gone = true
	equal(t, f.step(e, del.ID).Step, ops.StepRelease)
	equal(t, f.step(e, del.ID).Step, ops.StepOrphan)
	checkIDs(t, cl.disassoc, []string{"assoc-stored", "assoc-tagged"})
	checkIDs(t, cl.released, []string{"ip-stored", "ip-tagged"})
	equal(t, f.step(e, del.ID).Status, store.OpDone)
	checkIDs(t, cl.orphaned, []string{"v-stored", "v-tagged"})
	if _, err := f.st.Host(f.ctx, h.Name); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("host remains, got %v", err)
	}
	events := f.events()
	for _, ev := range events {
		if ev.Name == trail.EventOperationStarted {
			t.Fatalf("engine emitted operation.started: %+v", ev)
		}
	}
	finishedWithData(t, events, del, store.OpDone, "", store.OnDeleteKeep, deleter)
	eventWith(t, events, trail.EventHostChanged,
		telemetry.Attrs{"host": h.Name, "from": store.HostDeleting, "to": store.HostDeleted, "operation": del.ID}, deleter)
	for _, removed := range []store.Operation{retried, zRemove} {
		if _, err := f.st.Account(f.ctx, removed.Account); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("account %s remains, got %v", removed.Account, err)
		}
		equal(t, f.operation(removed.ID).Status, store.OpDone)
		finishedEvent(t, events, removed, store.OpDone, "", removed.Caller)
		accountEvent(t, events, store.Account{Host: h.Name, Slug: removed.Account}, store.AccountRemoving, trail.AccountRemoved, deleter)
	}
}

// R-97G8-3Z45 R-98O4-HQUU R-DKOF-XVLC R-9NAX-2ZR6 R-9CBT-N22X
func TestDeleteDataResumesAndWaitsForEveryCreateVolume(t *testing.T) {
	f := setup(t)
	h, create := f.create()
	ok(t, f.st.Finish(f.ctx, create.ID, store.Update{InstanceID: "i-stored", AllocationID: "ip-stored", DataVolume: "v-stored"}))
	caller := identity.Caller{UserID: "data-deleter", RequestID: "delete-data-request"}
	op := f.deleteHost(h, false, caller)
	cl := &deletionCloud{
		f: f, createID: create.ID, deleteID: op.ID,
		recordCloud: &recordCloud{zone: f.cfg.Hosts.ZoneID},
		storedData:  cloud.Volume{ID: "v-stored", State: cloud.VolumeAvailable},
		otherData: []cloud.Volume{
			{ID: "v-stored", State: cloud.VolumeAvailable},
			{ID: "v-tagged", State: cloud.VolumeAvailable},
			{ID: "v-gone", State: cloud.VolumeAvailable},
			{ID: "v-deleting", State: cloud.VolumeDeleting},
			{ID: "v-deleted", State: cloud.VolumeDeleted},
		},
	}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitUnrecord)
	f.now = f.now.Add(ops.RecordSettle)
	equal(t, f.step(e, op.ID).Step, ops.StepTerminate)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitTerminated)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitTerminated)
	equal(t, len(cl.deleteData), 0)
	equal(t, len(cl.released), 0)
	cl.gone = true
	equal(t, f.step(e, op.ID).Step, ops.StepRelease)
	equal(t, f.step(e, op.ID).Step, ops.StepDeleteData)
	checkIDs(t, cl.released, []string{"ip-stored", "ip-tagged"})
	equal(t, f.step(e, op.ID).Step, ops.StepWaitDataDeleted)
	checkIDs(t, cl.deleteData, []string{"v-stored", "v-tagged", "v-gone"})
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepDeleteData}))
	e = ops.New(f.cfg)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitDataDeleted)
	checkIDs(t, cl.deleteData, []string{"v-stored", "v-tagged", "v-gone"})
	for i := range cl.otherData {
		cl.otherData[i].State = cloud.VolumeDeleted
	}
	// Once all tagged results are gone, the stored ID alone still holds
	// the operation open.
	equal(t, f.step(e, op.ID).Status, store.OpRunning)
	cl.storedData = cloud.Volume{}
	cl.otherData = []cloud.Volume{{ID: "v-tagged", State: cloud.VolumeDeleting}}
	equal(t, f.step(e, op.ID).Status, store.OpRunning)
	cl.otherData[0].State = cloud.VolumeDeleted
	equal(t, f.step(e, op.ID).Step, ops.StepDeleteBackups)
	equal(t, f.step(e, op.ID).Status, store.OpDone)
	equal(t, len(cl.orphaned), 0)
	events := f.events()
	finishedWithData(t, events, op, store.OpDone, "", store.OnDeleteDelete, caller)
	eventWith(t, events, trail.EventHostChanged,
		telemetry.Attrs{"host": h.Name, "from": store.HostDeleting, "to": store.HostDeleted, "operation": op.ID}, caller)
}

// R-P2DG-ME5W
func TestDeleteReconcilesPersistedStepWithChoiceAfterCheckingTimeout(t *testing.T) {
	for _, tc := range []struct {
		persisted, want string
		keep            bool
	}{
		{ops.StepOrphan, ops.StepDeleteData, false},
		{ops.StepDeleteData, ops.StepOrphan, true},
		{ops.StepWaitDataDeleted, ops.StepOrphan, true},
		{ops.StepDeleteBackups, ops.StepOrphan, true},
	} {
		for _, age := range []time.Duration{0, ops.StepTimeout, ops.StepTimeout + time.Second} {
			t.Run(tc.persisted+"/"+age.String(), func(t *testing.T) {
				f := setup(t)
				h, create := f.create()
				ok(t, f.st.Finish(f.ctx, create.ID, store.Update{DataVolume: "v-stored"}))
				op := f.deleteHost(h, tc.keep, author)
				ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: tc.persisted}))

				cl := &struct{ cloud.Cloud }{Cloud: f.memory}
				f.cfg.Cloud = cl
				e := ops.New(f.cfg)
				ok(t, e.Scan(f.ctx))
				// Any cloud call after the initial scan fails the test.
				cl.Cloud = nil

				f.now = f.now.Add(age)
				ok(t, e.Pass(f.ctx))
				if age > ops.StepTimeout {
					failed := f.operation(op.ID)
					equal(t, failed.Status, store.OpFailed)
					equal(t, failed.Error, ops.ReasonTimedOut)
					equal(t, failed.Step, tc.persisted)
					equal(t, f.host(h.Name).Reason, ops.ReasonTimedOut)
					_, _, err := f.st.RetryHost(f.ctx, h.Name, author)
					ok(t, err)
					ok(t, e.Pass(f.ctx))
				}
				next := f.operation(op.ID)
				equal(t, next.Status, store.OpRunning)
				equal(t, next.Step, tc.want)
			})
		}
	}
}

// R-98O4-HQUU R-DKOF-XVLC
func TestDeleteWithoutDataVolumesMovesThroughBothDataSteps(t *testing.T) {
	f := setup(t)
	h, create := f.create()
	ok(t, f.st.Finish(f.ctx, create.ID, store.Update{}))
	op := f.deleteHost(h, false, author)
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepDeleteData}))
	cl := &deletionCloud{f: f, createID: create.ID, deleteID: op.ID,
		recordCloud: &recordCloud{zone: f.cfg.Hosts.ZoneID}, otherData: []cloud.Volume{}}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitDataDeleted)
	equal(t, f.step(e, op.ID).Step, ops.StepDeleteBackups)
	equal(t, f.step(e, op.ID).Status, store.OpDone)
	equal(t, len(cl.deleteData), 0)
}

// R-9CBT-N22X
func TestDeleteFinishEmitsOnlyAfterSuccessfulStoreWrite(t *testing.T) {
	f := setup(t)
	h, create := f.create()
	ok(t, f.st.Finish(f.ctx, create.ID, store.Update{}))
	op := f.deleteHost(h, true, author)
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepOrphan}))
	stopObserving := f.observeEventFormation(func() {
		equal(t, f.operation(op.ID).Status, store.OpDone)
		_, err := f.st.Host(f.ctx, h.Name)
		equal(t, errors.Is(err, store.ErrNotFound), true)
	})
	defer stopObserving()
	cl := &deletionCloud{f: f, createID: create.ID, deleteID: op.ID,
		recordCloud: &recordCloud{zone: f.cfg.Hosts.ZoneID},
		orphanedTag: func() { f.db.SetFailing(true) }}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	ok(t, e.Scan(f.ctx))
	err := e.Pass(f.ctx)
	f.db.SetFailing(false)
	equal(t, errors.Is(err, store.ErrUnreachable), true)
	equal(t, f.operation(op.ID).Status, store.OpRunning)
	equal(t, f.host(h.Name).State, store.HostDeleting)
	equal(t, len(f.events()), 0)

	cl.orphanedTag = nil
	equal(t, f.step(e, op.ID).Status, store.OpDone)
	events := f.events()
	equal(t, len(events), 2)
	finishedWithData(t, events, op, store.OpDone, "", store.OnDeleteKeep, author)
	eventWith(t, events, trail.EventHostChanged,
		telemetry.Attrs{"host": h.Name, "from": store.HostDeleting, "to": store.HostDeleted, "operation": op.ID}, author)
}

type oldHostCloud struct {
	cloud.Cloud
	t     *testing.T
	old   store.Host
	extra []string
}

func (c oldHostCloud) rejectOld(id string) {
	c.t.Helper()
	if slices.Contains(c.extra, id) || slices.Contains([]string{c.old.InstanceID, c.old.RootVolume, c.old.DataVolume, c.old.AllocationID, c.old.AssociationID}, id) {
		c.t.Fatalf("operation touched earlier host's resource %q", id)
	}
}

func (c oldHostCloud) TerminateInstance(ctx context.Context, id string) error {
	c.rejectOld(id)
	return c.Cloud.TerminateInstance(ctx, id)
}

func (c oldHostCloud) DisassociateAddress(ctx context.Context, id string) error {
	c.rejectOld(id)
	return c.Cloud.DisassociateAddress(ctx, id)
}

func (c oldHostCloud) ReleaseAddress(ctx context.Context, id string) error {
	c.rejectOld(id)
	return c.Cloud.ReleaseAddress(ctx, id)
}

func (c oldHostCloud) TagResource(ctx context.Context, id string, tags cloud.Tags) error {
	c.rejectOld(id)
	return c.Cloud.TagResource(ctx, id, tags)
}

func (c oldHostCloud) DeleteVolume(ctx context.Context, id string) error {
	c.rejectOld(id)
	return c.Cloud.DeleteVolume(ctx, id)
}

// R-8VIM-KBOF R-9JN7-XOJ3
func TestDeleteOfReusedNameLeavesEarlierResourcesAndStrays(t *testing.T) {
	f := setup(t)
	h, create := f.create()
	e := ops.New(f.cfg)
	f.complete(e, create.ID)
	earlier := f.host(h.Name)
	f.complete(e, f.deleteHost(earlier, true, author).ID)
	keptBefore, err := f.memory.DescribeVolume(f.ctx, earlier.DataVolume)
	ok(t, err)
	equal(t, keptBefore.Tags[cloud.TagOrphaned], cloud.OrphanedTrue)
	tags := cloud.Tags{cloud.TagKind: cloud.KindHostData, cloud.TagHost: earlier.Name, cloud.TagOperation: create.ID}
	extra, err := f.memory.CreateVolume(f.ctx, cloud.VolumeSpec{AZ: earlier.AZ, SizeGB: 20, ClientToken: "late-earlier-volume", Tags: tags})
	ok(t, err)
	strayTags := cloud.Tags{cloud.TagKind: cloud.KindHostData, cloud.TagHost: earlier.Name, cloud.TagOperation: "no-such-create"}
	stray, err := f.memory.CreateVolume(f.ctx, cloud.VolumeSpec{AZ: earlier.AZ, SizeGB: 20, ClientToken: "unowned-volume", Tags: strayTags})
	ok(t, err)
	f.cfg.Cloud = oldHostCloud{Cloud: f.memory, t: t, old: earlier, extra: []string{extra.ID, stray.ID}}
	e = ops.New(f.cfg)
	current, currentCreate := f.create()
	equal(t, current.Name, earlier.Name)
	f.complete(e, currentCreate.ID)
	current = f.host(current.Name)
	equal(t, current.State, store.HostRunning)
	f.complete(e, f.deleteHost(current, false, author).ID)
	ok(t, e.Scan(f.ctx))
	keptAfter, err := f.memory.DescribeVolume(f.ctx, earlier.DataVolume)
	ok(t, err)
	checkTags(t, keptAfter.Tags, keptBefore.Tags)
	kept, err := f.st.KeptVolume(f.ctx, earlier.DataVolume)
	ok(t, err)
	equal(t, kept.State, store.VolumeKept)
	late, err := f.memory.DescribeVolume(f.ctx, extra.ID)
	ok(t, err)
	checkTags(t, late.Tags, tags)
	unowned, err := f.memory.DescribeVolume(f.ctx, stray.ID)
	ok(t, err)
	checkTags(t, unowned.Tags, strayTags)
	_, err = f.memory.DescribeVolume(f.ctx, current.DataVolume)
	equal(t, cloud.ClassOf(err), cloud.ClassNotFound)
}

// R-9CBT-N22X
func TestDeleteFailureRetainsChoiceInOperationEvent(t *testing.T) {
	f := setup(t)
	h := f.ready()
	caller := identity.Caller{UserID: "deleting-user", RequestID: "delete-failure"}
	op := f.deleteHost(h, false, caller)
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepDeleteData}))
	f.now = f.now.Add(ops.StepTimeout + time.Second)
	equal(t, f.step(ops.New(f.cfg), op.ID).Status, store.OpFailed)
	equal(t, f.host(h.Name).State, store.HostFailed)
	events := f.events()
	finishedWithData(t, events, op, store.OpFailed, ops.ReasonTimedOut, store.OnDeleteDelete, caller)
	eventWith(t, events, trail.EventHostChanged,
		telemetry.Attrs{"host": h.Name, "from": store.HostDeleting, "to": store.HostFailed, "operation": op.ID}, caller)
}
