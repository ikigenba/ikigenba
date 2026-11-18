package ops_test

import (
	"context"
	"errors"
	"io"
	"reflect"
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
}

func (c *deletionCloud) lookup(tags cloud.Tags, kind string) bool {
	c.f.t.Helper()
	if tags[cloud.TagOperation] == "" {
		return false
	}
	checkTags(c.f.t, tags, cloud.Tags{cloud.TagOperation: c.createID, cloud.TagKind: kind})
	return true
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
	equal(c.f.t, id, "i-stored")
	return cloud.Instance{}, cloudErr(cloud.ClassNotFound)
}

func (c *deletionCloud) TerminateInstance(_ context.Context, id string) error {
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
	equal(c.f.t, id, "ip-stored")
	return cloud.Address{AllocationID: id, AssociationID: "assoc-stored"}, nil
}

func (c *deletionCloud) DisassociateAddress(_ context.Context, association string) error {
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepRelease)
	c.disassoc = append(c.disassoc, association)
	return nil
}

func (c *deletionCloud) ReleaseAddress(_ context.Context, allocation string) error {
	equal(c.f.t, c.f.operation(c.deleteID).Step, ops.StepRelease)
	c.released = append(c.released, allocation)
	if allocation == "ip-tagged" {
		return cloudErr(cloud.ClassNotFound)
	}
	return nil
}

func (c *deletionCloud) FindVolumes(_ context.Context, tags cloud.Tags, each func([]cloud.Volume) bool) error {
	if c.lookup(tags, cloud.KindHostData) {
		if each([]cloud.Volume{{ID: "v-stored"}}) {
			each([]cloud.Volume{{ID: "v-tagged"}})
		}
	}
	return nil
}

func (c *deletionCloud) TagResource(_ context.Context, id string, tags cloud.Tags) error {
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

// R-MNTG-22XB R-MP1C-FUO0 R-S48T-I8UJ R-NFV4-USZA R-S6OM-9SBX R-NIAX-MCGO R-SACB-F3K0 R-13S5-BI0X
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
	_, del, err := f.st.StartHostOp(f.ctx, h.Name, store.OpDelete, store.Target{}, deleter)
	ok(t, err)
	suffix := "." + f.cfg.Hosts.Domain
	records := &recordCloud{
		zone: f.cfg.Hosts.ZoneID,
		records: map[string][]cloud.Record{
			a.Slug + suffix: {{Type: "A", Values: []string{h.EIP}, TTL: 61}},
			z.Slug + suffix: {{Type: "A", Values: []string{"192.0.2.19"}, TTL: 32}},
		},
		before: func() { equal(t, f.operation(del.ID).Step, ops.StepUnrecord) },
	}
	retryCaller := identity.Caller{UserID: "retrying-operator", RequestID: "retry-request"}
	var retried store.Operation
	cl := &deletionCloud{
		recordCloud: records, f: f, createID: create.ID, deleteID: del.ID,
		orphanedTag: func() {
			var err error
			_, retried, err = f.st.RemoveAccount(f.ctx, a.Slug, retryCaller)
			ok(t, err)
			ok(t, f.st.Defer(f.ctx, retried.ID, f.now.Add(time.Hour), 0))
		},
	}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	equal(t, f.step(e, del.ID).Step, ops.StepWaitUnrecord)
	checkIDs(t, records.lookups, []string{a.Slug + suffix, z.Slug + suffix})
	equal(t, len(records.changes), 1)
	equal(t, records.changes[0], cloud.RecordChange{Action: cloud.RecordDelete, Name: a.Slug + suffix, IP: h.EIP, TTL: 61})
	equal(t, f.account(a.Slug).Change, "change-1")
	equal(t, f.account(z.Slug).Change, "")
	records.syncErr = cloudErr(cloud.ClassNotFound)
	equal(t, f.step(e, del.ID).Step, ops.StepWaitUnrecord)
	equal(t, len(cl.terminated), 0)
	equal(t, len(cl.released), 0)
	f.now = f.now.Add(ops.RecordSettle)
	equal(t, f.step(e, del.ID).Step, ops.StepTerminate)
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
	finishedEvent(t, events, del, store.OpDone, "", deleter)
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
