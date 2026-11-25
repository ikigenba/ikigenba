package ops_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
)

const scanIP = "192.0.2.41"

func (f *fixture) runningWithAddress(ip string) store.Host {
	f.t.Helper()
	h, op := f.create()
	ok(f.t, f.st.Finish(f.ctx, op.ID, store.Update{EIP: ip}))
	return f.host(h.Name)
}

// R-J1NR-RJTP R-J2VO-5BKE
func TestRecordCheckCoversOnlyLiveHostsWithAnAddress(t *testing.T) {
	f := setup(t)
	running := f.runningWithAddress(scanIP)
	stopping := f.runningWithAddress(scanIP)
	f.start(stopping, store.OpStop, store.Target{})
	stopped := f.runningWithAddress(scanIP)
	ok(t, f.st.Finish(f.ctx, f.start(stopped, store.OpStop, store.Target{}).ID, store.Update{}))
	starting := f.runningWithAddress(scanIP)
	ok(t, f.st.Finish(f.ctx, f.start(starting, store.OpStop, store.Target{}).ID, store.Update{}))
	f.start(starting, store.OpStart, store.Target{})
	resizing := f.runningWithAddress(scanIP)
	f.start(resizing, store.OpResize, store.Target{Type: "t4g.large"})
	deleting := f.runningWithAddress(scanIP)
	f.start(deleting, store.OpDelete, store.Target{})
	failed, failOp := f.create()
	ok(t, f.st.Advance(f.ctx, failOp.ID, store.Update{EIP: scanIP}))
	ok(t, f.st.Fail(f.ctx, failOp.ID, "fixture failure"))
	provisioning, create := f.create()
	ok(t, f.st.Advance(f.ctx, create.ID, store.Update{EIP: scanIP}))
	noAddress := f.runningWithAddress("")

	cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID}
	f.cfg.Cloud = cl
	covered := []store.Host{running, stopping, stopped, starting, resizing}
	outside := []store.Host{deleting, failed, provisioning, noAddress}
	before := make(map[string]store.Host)
	history := make(map[string][]store.Operation)
	for _, rangeOfHosts := range [][]store.Host{covered, outside} {
		for _, h := range rangeOfHosts {
			before[h.Name] = f.host(h.Name)
			operations, err := f.st.OperationsOf(f.ctx, h.Name, "", store.BatchSize)
			ok(t, err)
			history[h.Name] = operations
		}
	}

	ok(t, ops.New(f.cfg).Scan(f.ctx))
	want := make([]string, 0, len(covered))
	for _, h := range covered {
		want = append(want, h.Name+"."+f.cfg.Hosts.Domain)
	}
	checkIDs(t, cl.lookups, want)
	equal(t, len(cl.changes), len(covered))
	for i, h := range covered {
		equal(t, cl.changes[i], cloud.RecordChange{Action: cloud.RecordCreate, Name: want[i], IP: scanIP, TTL: ops.RecordTTL})
		got := f.host(h.Name)
		if got.Change == "" {
			t.Fatalf("%s has no change", h.Name)
		}
		got.Change = ""
		if !reflect.DeepEqual(got, before[h.Name]) {
			t.Fatalf("%s changed outside DNS state: got %+v, want %+v", h.Name, got, before[h.Name])
		}
	}
	for _, h := range outside {
		if got := f.host(h.Name); !reflect.DeepEqual(got, before[h.Name]) {
			t.Fatalf("uncovered host changed: got %+v, want %+v", got, before[h.Name])
		}
	}
	for host, old := range history {
		got, err := f.st.OperationsOf(f.ctx, host, "", store.BatchSize)
		ok(t, err)
		if !reflect.DeepEqual(got, old) {
			t.Fatalf("%s operations changed: got %+v, want %+v", host, got, old)
		}
	}
	equal(t, len(f.events()), 0)
}

// R-J1NR-RJTP R-J2VO-5BKE
func TestRecordCheckNotesAndClearsConflicts(t *testing.T) {
	matching := cloud.Record{Type: "A", Values: []string{scanIP}}
	for _, tc := range []struct {
		name    string
		records []cloud.Record
		err     error
		writes  int
	}{
		{name: "wrong IP", records: []cloud.Record{{Type: "A", Values: []string{"192.0.2.42"}}}},
		{name: "wrong type", records: []cloud.Record{{Type: "TXT", Values: []string{scanIP}}}},
		{name: "multiple", records: []cloud.Record{matching, {Type: "CNAME", Values: []string{"elsewhere.example"}}}},
		{name: "create refused", err: cloudErr(cloud.ClassRefused), writes: 1},
		{name: "create mismatch", err: cloudErr(cloud.ClassMismatch), writes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			h := f.runningWithAddress(scanIP)
			ok(t, f.st.NoteHostRecord(f.ctx, h.Name, "earlier-change", ""))
			h = f.host(h.Name)
			name := h.Name + "." + f.cfg.Hosts.Domain
			cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, records: map[string][]cloud.Record{name: tc.records}, changeErr: tc.err}
			f.cfg.Cloud = cl
			e := ops.New(f.cfg)
			ok(t, e.Scan(f.ctx))
			equal(t, len(cl.changes), tc.writes)
			got := f.host(h.Name)
			equal(t, got.RecordReason, ops.ReasonRecordConflict)
			got.RecordReason = ""
			if !reflect.DeepEqual(got, h) {
				t.Fatalf("conflict changed host outside its record reason: got %+v, want %+v", got, h)
			}
			cl.records[name] = []cloud.Record{matching}
			ok(t, e.Scan(f.ctx))
			equal(t, len(cl.changes), tc.writes)
			if got := f.host(h.Name); !reflect.DeepEqual(got, h) {
				t.Fatalf("correct record did not clear conflict alone: got %+v, want %+v", got, h)
			}
			equal(t, len(f.events()), 0)
		})
	}
}

// R-J5BG-WV1S
func TestRecordCheckHealsEveryPage(t *testing.T) {
	f := setup(t)
	names := make([]string, 120)
	for i := range names {
		h := f.runningWithAddress(scanIP)
		names[i] = h.Name
	}
	ok(t, ops.New(f.cfg).Scan(f.ctx))
	for _, host := range names {
		name := host + "." + f.cfg.Hosts.Domain
		records, err := f.memory.FindRecords(f.ctx, f.cfg.Hosts.ZoneID, name)
		ok(t, err)
		equal(t, len(records), 1)
		equal(t, records[0].Name, name)
		equal(t, records[0].Type, "A")
		equal(t, records[0].TTL, int64(ops.RecordTTL))
		checkIDs(t, records[0].Values, []string{scanIP})
		if f.host(host).Change == "" {
			t.Fatalf("%s has no recorded change", host)
		}
	}
}

// R-J5BG-WV1S
func TestRecordCheckReadsAtMostOnePageAhead(t *testing.T) {
	f := setup(t)
	records := make(map[string][]cloud.Record)
	for range store.BatchSize + 1 {
		h := f.runningWithAddress(scanIP)
		records[h.Name+"."+f.cfg.Hosts.Domain] = []cloud.Record{{Type: "A", Values: []string{scanIP}}}
	}
	cl := &recordCloud{
		Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, records: records,
		find: func(string) error {
			// Exact matches require no store write, so only loading a later
			// page interrupts the DNS checks after the store fails.
			f.db.SetFailing(true)
			return nil
		},
	}
	f.cfg.Cloud = cl
	err := ops.New(f.cfg).Scan(f.ctx)
	f.db.SetFailing(false)
	if !errors.Is(err, store.ErrUnreachable) {
		t.Fatalf("scan failure = %v, want %v", err, store.ErrUnreachable)
	}
	if n := len(cl.lookups); n == 0 || n > store.BatchSize {
		t.Fatalf("scan checked %d records after reading its first page, limit %d", n, store.BatchSize)
	}
	equal(t, len(cl.changes), 0)
}

// R-46VA-T1Q2
func TestRecordCheckCloudFailureDoesNotInterruptStrayScan(t *testing.T) {
	for _, tc := range []struct {
		name, action string
		class        cloud.Class
	}{
		{"find refused", "find", cloud.ClassRefused},
		{"find transient", "find", cloud.ClassTransient},
		{"create transient", "create", cloud.ClassTransient},
		{"create missing", "create", cloud.ClassNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			first := f.runningWithAddress(scanIP)
			second := f.runningWithAddress(scanIP)
			ok(t, f.st.NoteHostRecord(f.ctx, first.Name, "existing-change", "existing conflict"))
			ok(t, f.st.NoteGrow(f.ctx, "expired-volume", f.now.Add(-store.GrowWindow)))
			stray, err := f.memory.AllocateAddress(f.ctx, cloud.Tags{cloud.TagKind: cloud.KindHost})
			ok(t, err)
			firstName, secondName := first.Name+"."+f.cfg.Hosts.Domain, second.Name+"."+f.cfg.Hosts.Domain
			failure := cloudErr(tc.class)
			cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID}
			if tc.action == "find" {
				cl.find = func(string) error { return failure }
			} else {
				cl.changeErr = failure
			}
			f.cfg.Cloud = cl
			e := ops.New(f.cfg)
			if err := e.Scan(f.ctx); !errors.Is(err, failure) {
				t.Fatalf("scan failure = %v, want %v", err, failure)
			}
			checkIDs(t, cl.lookups, []string{firstName})
			if tc.action == "find" {
				equal(t, len(cl.changes), 0)
			} else {
				equal(t, len(cl.changes), 1)
			}
			equal(t, f.host(first.Name).Change, "existing-change")
			equal(t, f.host(first.Name).RecordReason, "existing conflict")
			events := f.events()
			equal(t, len(events), 1)
			equal(t, events[0].Name, trail.EventResourceUnexplained)
			equal(t, events[0].Attrs["resource"], any(stray.AllocationID))
			n, err := f.st.PruneGrows(f.ctx, f.now, store.BatchSize)
			ok(t, err)
			equal(t, n, 0)
			cl.find, cl.changeErr, cl.lookups = nil, nil, nil
			ok(t, e.Scan(f.ctx))
			checkIDs(t, cl.lookups, []string{firstName, secondName})
			equal(t, f.host(first.Name).RecordReason, "")
			equal(t, len(f.events()), 1)
		})
	}
}

// R-4837-6TGR
func TestRecordCheckSkipsHostDeletedAfterPageRead(t *testing.T) {
	f := setup(t)
	first := f.runningWithAddress(scanIP)
	second := f.runningWithAddress(scanIP)
	firstName, secondName := first.Name+"."+f.cfg.Hosts.Domain, second.Name+"."+f.cfg.Hosts.Domain
	deleted := false
	cl := &recordCloud{
		Cloud: f.memory, zone: f.cfg.Hosts.ZoneID,
		records: map[string][]cloud.Record{firstName: {{Type: "TXT", Values: []string{"unrelated"}}}},
		find: func(name string) error {
			if name == firstName && !deleted {
				op := f.start(first, store.OpDelete, store.Target{})
				ok(t, f.st.Finish(f.ctx, op.ID, store.Update{}))
				deleted = true
			}
			return nil
		},
	}
	f.cfg.Cloud = cl
	ok(t, ops.New(f.cfg).Scan(f.ctx))
	checkIDs(t, cl.lookups, []string{firstName, secondName, firstName})
	equal(t, len(cl.changes), 1)
	equal(t, cl.changes[0].Name, secondName)
	equal(t, f.host(second.Name).Change, "change-1")
}

type recordCheckStoreFailureCloud struct {
	*recordCloud
	recoverStore func()
}

func (c *recordCheckStoreFailureCloud) FindInstances(ctx context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	c.recoverStore()
	return c.Cloud.FindInstances(ctx, tags, each)
}

// R-4837-6TGR
func TestRecordCheckStoreFailureDoesNotInterruptStrayScan(t *testing.T) {
	f := setup(t)
	first := f.runningWithAddress(scanIP)
	second := f.runningWithAddress(scanIP)
	ok(t, f.st.NoteGrow(f.ctx, "expired-volume", f.now.Add(-store.GrowWindow)))
	stray, err := f.memory.AllocateAddress(f.ctx, cloud.Tags{cloud.TagKind: cloud.KindHost})
	ok(t, err)
	cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, before: func() { f.db.SetFailing(true) }}
	f.cfg.Cloud = &recordCheckStoreFailureCloud{recordCloud: cl, recoverStore: func() { f.db.SetFailing(false) }}
	e := ops.New(f.cfg)
	err = e.Scan(f.ctx)
	f.db.SetFailing(false)
	if !errors.Is(err, store.ErrUnreachable) {
		t.Fatalf("scan failure = %v, want %v", err, store.ErrUnreachable)
	}
	checkIDs(t, cl.lookups, []string{first.Name + "." + f.cfg.Hosts.Domain})
	equal(t, len(cl.changes), 1)
	equal(t, f.host(first.Name).Change, "")
	equal(t, f.host(second.Name).Change, "")
	events := f.events()
	equal(t, len(events), 1)
	equal(t, events[0].Name, trail.EventResourceUnexplained)
	equal(t, events[0].Attrs["resource"], any(stray.AllocationID))
	n, err := f.st.PruneGrows(f.ctx, f.now, store.BatchSize)
	ok(t, err)
	equal(t, n, 0)
}
