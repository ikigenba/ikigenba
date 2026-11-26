package ops_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type recordCloud struct {
	cloud.Cloud
	records   map[string][]cloud.Record
	lookups   []string
	changes   []cloud.RecordChange
	queried   []string
	launches  int
	synced    bool
	syncErr   error
	changeErr error
	zone      string
	before    func()
	find      func(string) error
	sync      func(string) (bool, error)
}

func (c *recordCloud) FindRecords(_ context.Context, zone, name string) ([]cloud.Record, error) {
	if zone != c.zone {
		return nil, fmt.Errorf("unexpected zone %q", zone)
	}
	c.lookups = append(c.lookups, name)
	if c.find != nil {
		if err := c.find(name); err != nil {
			return nil, err
		}
	}
	return c.records[name], nil
}

func (c *recordCloud) ChangeRecord(_ context.Context, zone string, change cloud.RecordChange) (string, error) {
	if zone != c.zone {
		return "", fmt.Errorf("unexpected zone %q", zone)
	}
	c.changes = append(c.changes, change)
	if c.before != nil {
		c.before()
	}
	return fmt.Sprintf("change-%d", len(c.changes)), c.changeErr
}

func (c *recordCloud) ChangeInsync(_ context.Context, id string) (bool, error) {
	c.queried = append(c.queried, id)
	if c.sync != nil {
		return c.sync(id)
	}
	return c.synced, c.syncErr
}

func (c *recordCloud) RunInstance(ctx context.Context, launch cloud.Launch) (cloud.Instance, error) {
	c.launches++
	return c.Cloud.RunInstance(ctx, launch)
}

// R-9J8J-XBZG R-9LOC-OVGU
func TestCreateHostRecordsItsNameBeforeLaunch(t *testing.T) {
	for _, state := range []struct {
		name  string
		ready bool
		err   error
	}{
		{name: "in sync", ready: true},
		{name: "missing", err: cloudErr(cloud.ClassNotFound)},
	} {
		t.Run(state.name, func(t *testing.T) {
			f := setup(t)
			h, op := f.create()
			cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, before: func() {
				equal(t, f.operation(op.ID).Step, ops.StepHostRecord)
			}}
			f.cfg.Cloud = cl
			e := ops.New(f.cfg)
			equal(t, f.step(e, op.ID).Step, ops.StepHostRecord)
			equal(t, f.step(e, op.ID).Step, ops.StepWaitHostRecord)
			h = f.host(h.Name)
			name := h.Name + "." + f.cfg.Hosts.Domain
			checkIDs(t, cl.lookups, []string{name})
			equal(t, len(cl.changes), 1)
			equal(t, cl.changes[0], cloud.RecordChange{Action: cloud.RecordCreate, Name: name, IP: h.EIP, TTL: ops.RecordTTL})
			equal(t, h.Change, "change-1")
			next := f.step(e, op.ID)
			equal(t, next.Step, ops.StepWaitHostRecord)
			equal(t, next.NextTry, f.now.Add(ops.PollInterval))
			equal(t, f.host(h.Name).InstanceID, "")
			cl.synced, cl.syncErr = state.ready, state.err
			equal(t, f.step(e, op.ID).Step, ops.StepLaunch)
			checkIDs(t, cl.queried, []string{h.Change, h.Change})
		})
	}
}

// R-9J8J-XBZG
func TestCreateHostAdoptsOnlyAnExactRecord(t *testing.T) {
	const address = "192.0.2.14"
	match := cloud.Record{Type: "A", Values: []string{address}, TTL: 42}
	for _, tc := range []struct {
		name    string
		records []cloud.Record
		err     error
		adopt   bool
		calls   int
	}{
		{name: "matching", records: []cloud.Record{match}, adopt: true},
		{name: "multiple matching records", records: []cloud.Record{match, match}},
		{name: "different type", records: []cloud.Record{{Type: "TXT", Values: []string{address}}}},
		{name: "different address", records: []cloud.Record{{Type: "A", Values: []string{"192.0.2.15"}}}},
		{name: "multiple addresses", records: []cloud.Record{{Type: "A", Values: []string{address, "192.0.2.15"}}}},
		{name: "create refused", err: cloudErr(cloud.ClassRefused), calls: 1},
		{name: "create mismatch", err: cloudErr(cloud.ClassMismatch), calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			h, op := f.create()
			ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepHostRecord, EIP: address}))
			name := h.Name + "." + f.cfg.Hosts.Domain
			cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, records: map[string][]cloud.Record{name: tc.records}, changeErr: tc.err}
			f.cfg.Cloud = cl
			next := f.step(ops.New(f.cfg), op.ID)
			checkIDs(t, cl.lookups, []string{name})
			equal(t, len(cl.changes), tc.calls)
			equal(t, len(cl.queried), 0)
			h = f.host(h.Name)
			equal(t, h.Change, "")
			equal(t, h.InstanceID, "")
			equal(t, cl.launches, 0)
			if tc.adopt {
				equal(t, next.Step, ops.StepLaunch)
				equal(t, next.Status, store.OpRunning)
			} else {
				equal(t, next.Status, store.OpFailed)
				equal(t, next.Error, ops.ReasonRecordConflict)
				equal(t, h.State, store.HostFailed)
				equal(t, h.Reason, ops.ReasonRecordConflict)
			}
		})
	}
}

// R-IY02-M8LM
func TestRecordCreatedAsProvisioningEndsDoesNotAdvanceOrTell(t *testing.T) {
	f := setup(t)
	h, op := f.create()
	ok(t, f.st.Advance(f.ctx, op.ID, store.Update{Step: ops.StepHostRecord, EIP: "192.0.2.14"}))
	cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, before: func() {
		ok(t, f.st.Fail(f.ctx, op.ID, "ended independently"))
	}}
	f.cfg.Cloud = cl
	next := f.step(ops.New(f.cfg), op.ID)
	equal(t, next.Step, ops.StepHostRecord)
	equal(t, next.Status, store.OpFailed)
	equal(t, next.Error, "ended independently")
	equal(t, f.host(h.Name).Change, "")
	equal(t, len(f.events()), 0)
}

func eventWith(t *testing.T, events []telemetry.Event, name string, attrs telemetry.Attrs, who identity.Caller) {
	t.Helper()
	matches := 0
	for _, e := range events {
		if e.Name == name && reflect.DeepEqual(e.Attrs, attrs) {
			equal(t, e.User, who.UserID)
			equal(t, e.RequestID, who.RequestID)
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("found %d %s events with attrs %v in %v", matches, name, attrs, events)
	}
}

func finishedEvent(t *testing.T, events []telemetry.Event, op store.Operation, status, reason string, who identity.Caller) {
	t.Helper()
	finishedWithData(t, events, op, status, reason, "", who)
}

func finishedWithData(t *testing.T, events []telemetry.Event, op store.Operation, status, reason, data string, who identity.Caller) {
	t.Helper()
	eventWith(t, events, trail.EventOperationFinished, telemetry.Attrs{
		"operation": op.ID, "kind": op.Kind, "host": op.Host, "account": op.Account, "status": status, "reason": reason, "data": data,
	}, who)
}

func accountEvent(t *testing.T, events []telemetry.Event, a store.Account, from, to string, who identity.Caller) {
	t.Helper()
	eventWith(t, events, trail.EventAccountChanged, telemetry.Attrs{
		"account": a.Slug, "host": a.Host, "from": from, "to": to,
	}, who)
}

// R-MNTG-22XB R-MP1C-FUO0 R-NLYM-RNOR R-12K8-XQA8 R-NOEF-J765 R-NPMB-WYWU R-9CBT-N22X R-L0E6-07I2
func TestPlaceWaitsForAddressDNSAndAppliedVersion(t *testing.T) {
	f := setup(t)
	h, provision := f.create()
	// Provision is kept deferred while the placement waits for an address.
	ok(t, f.st.Defer(f.ctx, provision.ID, f.now.Add(3*ops.StepTimeout), 0))
	a, op := f.place("acme", h.Name)
	h.EIP = "192.0.2.1"
	cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, records: map[string][]cloud.Record{
		h.Name + "." + f.cfg.Hosts.Domain: {{Type: "A", Values: []string{h.EIP}}},
	}, before: func() {
		equal(t, f.operation(op.ID).Step, ops.StepRecord)
	}}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitAddress)
	f.now = f.now.Add(ops.StepTimeout + time.Second)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitAddress)
	equal(t, f.operation(op.ID).Status, store.OpRunning)
	equal(t, len(cl.changes), 0)
	ok(t, f.st.Finish(f.ctx, provision.ID, store.Update{EIP: h.EIP}))
	equal(t, f.step(e, op.ID).Step, ops.StepRecord)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitRecord)
	equal(t, f.account(a.Slug).Change, "change-1")
	equal(t, len(cl.changes), 1)
	equal(t, cl.changes[0], cloud.RecordChange{Action: cloud.RecordCreate, Name: a.Slug + "." + f.cfg.Hosts.Domain, IP: h.EIP, TTL: ops.RecordTTL})
	equal(t, f.step(e, op.ID).Step, ops.StepWaitRecord)
	equal(t, len(cl.queried), 1)
	equal(t, cl.queried[0], f.account(a.Slug).Change)
	cl.syncErr = cloudErr(cloud.ClassNotFound)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitApplied)
	f.now = f.now.Add(ops.StepTimeout + time.Second)
	equal(t, f.step(e, op.ID).Step, ops.StepWaitApplied)
	equal(t, f.operation(op.ID).Status, store.OpRunning)
	f.report(h.Name, a.Version, a.Version)
	f.complete(e, op.ID)
	equal(t, f.account(a.Slug).State, store.AccountActive)
	events := f.events()
	finishedEvent(t, events, op, store.OpDone, "", author)
	accountEvent(t, events, a, store.AccountPending, store.AccountActive, author)
	for _, event := range events {
		if event.Name == trail.EventOperationStarted {
			t.Fatal("engine reported an operation start")
		}
	}
}

// R-12K8-XQA8 R-9CBT-N22X
func TestPlaceRecordAdoptionAndConflicts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		records []cloud.Record
		err     error
		adopt   bool
		calls   int
	}{
		{name: "adopt", records: []cloud.Record{{Type: "A", Values: []string{"192.0.2.1"}}}, adopt: true},
		{name: "another IP", records: []cloud.Record{{Type: "A", Values: []string{"192.0.2.2"}}}},
		{name: "another type", records: []cloud.Record{{Type: "TXT", Values: []string{"192.0.2.1"}}}},
		{name: "two values", records: []cloud.Record{{Type: "A", Values: []string{"192.0.2.1", "192.0.2.2"}}}},
		{name: "refused", err: cloudErr(cloud.ClassRefused), calls: 1},
		{name: "mismatch", err: cloudErr(cloud.ClassMismatch), calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			h, provisioning := f.create()
			ok(t, f.st.Finish(f.ctx, provisioning.ID, store.Update{EIP: "192.0.2.1"}))
			a, op := f.place("acme", h.Name)
			name := a.Slug + "." + f.cfg.Hosts.Domain
			cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, records: map[string][]cloud.Record{
				name: tc.records, h.Name + "." + f.cfg.Hosts.Domain: {{Type: "A", Values: []string{"192.0.2.1"}}},
			}, changeErr: tc.err}
			f.cfg.Cloud = cl
			e := ops.New(f.cfg)
			equal(t, f.step(e, op.ID).Step, ops.StepRecord)
			next := f.step(e, op.ID)
			equal(t, len(cl.changes), tc.calls)
			if tc.adopt {
				equal(t, next.Step, ops.StepWaitApplied)
				equal(t, f.account(a.Slug).Change, "")
				return
			}
			got := f.account(a.Slug)
			equal(t, next.Status, store.OpFailed)
			equal(t, next.Error, ops.ReasonRecordConflict)
			equal(t, got.State, store.AccountFailed)
			equal(t, got.Reason, ops.ReasonRecordConflict)
			events := f.events()
			finishedEvent(t, events, op, store.OpFailed, ops.ReasonRecordConflict, author)
			accountEvent(t, events, a, store.AccountPending, store.AccountFailed, author)
		})
	}
}

// R-IY02-M8LM
func TestCancelledPlaceDropsWithoutEvent(t *testing.T) {
	f := setup(t)
	h, provisioning := f.create()
	ok(t, f.st.Finish(f.ctx, provisioning.ID, store.Update{EIP: "192.0.2.1"}))
	a, op := f.place("acme", h.Name)
	cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, before: func() {
		f.remove(a.Slug)
	}}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	equal(t, f.step(e, op.ID).Step, ops.StepRecord)
	next := f.step(e, op.ID)
	equal(t, next.Status, store.OpCancelled)
	equal(t, next.Step, ops.StepRecord)
	equal(t, f.account(a.Slug).Change, "")
	equal(t, len(f.events()), 0)
}

// R-MNTG-22XB R-NFV4-USZA R-IZ7Z-00CB R-J0FV-DS30 R-NQU8-AQNJ R-9CBT-N22X
func TestUnplaceWaitsForDNSAndApplicationWhenAttempted(t *testing.T) {
	for _, attempted := range []bool{false, true} {
		t.Run(fmt.Sprint(attempted), func(t *testing.T) {
			f := setup(t)
			h, provisioning := f.create()
			h.EIP = "192.0.2.9"
			ok(t, f.st.Finish(f.ctx, provisioning.ID, store.Update{EIP: h.EIP}))
			a, place := f.place("acme", h.Name)
			ok(t, f.st.Finish(f.ctx, place.ID, store.Update{}))
			if attempted {
				f.report(h.Name, a.Version, a.Version-1)
			}
			a, op := f.remove(a.Slug)
			name := a.Slug + "." + f.cfg.Hosts.Domain
			cl := &recordCloud{
				Cloud: f.memory, zone: f.cfg.Hosts.ZoneID,
				records: map[string][]cloud.Record{name: {
					{Type: "CNAME", Values: []string{h.EIP}, TTL: 15},
					{Type: "A", Values: []string{"192.0.2.42"}, TTL: 12},
					{Type: "A", Values: []string{h.EIP}, TTL: 45},
				}, h.Name + "." + f.cfg.Hosts.Domain: {
					{Type: "A", Values: []string{h.EIP}, TTL: 55},
				}},
				before: func() { equal(t, f.operation(op.ID).Step, ops.StepUnrecord) },
			}
			f.cfg.Cloud = cl
			e := ops.New(f.cfg)
			ok(t, e.Scan(f.ctx))
			cl.lookups = nil
			equal(t, f.step(e, op.ID).Step, ops.StepWaitUnrecord)
			equal(t, len(cl.lookups), 1)
			equal(t, len(cl.changes), 1)
			equal(t, cl.changes[0], cloud.RecordChange{Action: cloud.RecordDelete, Name: name, IP: h.EIP, TTL: 45})
			equal(t, f.account(a.Slug).Change, "change-1")
			cl.synced = true
			equal(t, f.step(e, op.ID).Step, ops.StepWaitUnrecord)
			cl.synced = false
			f.now = f.now.Add(ops.RecordSettle)
			equal(t, f.step(e, op.ID).Step, ops.StepWaitUnrecord)
			cl.synced = true
			equal(t, f.step(e, op.ID).Step, ops.StepWaitApplied)
			f.now = f.now.Add(ops.StepTimeout + time.Second)
			if attempted {
				equal(t, f.step(e, op.ID).Status, store.OpRunning)
				f.report(h.Name, a.RemovedVersion, a.RemovedVersion)
			}
			f.complete(e, op.ID)
			if _, err := f.st.Account(f.ctx, a.Slug); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("account still present, got %v", err)
			}
			events := f.events()
			finishedEvent(t, events, op, store.OpDone, "", author)
			accountEvent(t, events, a, store.AccountRemoving, trail.AccountRemoved, author)
		})
	}
}
