package ops_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
)

type leftoverCloud struct {
	cloud.Cloud
	lookups []string
	changes []cloud.RecordChange
	find    func(string) error
	change  func(cloud.RecordChange) error
	recover func()
}

func (c *leftoverCloud) FindRecords(ctx context.Context, zone, name string) ([]cloud.Record, error) {
	c.lookups = append(c.lookups, name)
	if c.find != nil {
		if err := c.find(name); err != nil {
			return nil, err
		}
	}
	return c.Cloud.FindRecords(ctx, zone, name)
}

func (c *leftoverCloud) ChangeRecord(ctx context.Context, zone string, change cloud.RecordChange) (string, error) {
	c.changes = append(c.changes, change)
	if c.change != nil {
		if err := c.change(change); err != nil {
			return "", err
		}
	}
	return c.Cloud.ChangeRecord(ctx, zone, change)
}

func (c *leftoverCloud) FindInstances(ctx context.Context, tags cloud.Tags, each func([]cloud.Instance) bool) error {
	if c.recover != nil {
		c.recover()
	}
	return c.Cloud.FindInstances(ctx, tags, each)
}

func (f *fixture) deletedWithAddress(ip string) (string, store.Operation) {
	f.t.Helper()
	h := f.runningWithAddress(ip)
	// Simulate the previous release, which finished the delete without
	// touching a host's DNS record.
	op := f.start(h, store.OpDelete, store.Target{})
	ok(f.t, f.st.Finish(f.ctx, op.ID, store.Update{}))
	return h.Name + "." + f.cfg.Hosts.Domain, f.operation(op.ID)
}

func (f *fixture) seedRecord(name, ip string, ttl int64) {
	f.t.Helper()
	_, err := f.memory.ChangeRecord(f.ctx, f.cfg.Hosts.ZoneID, cloud.RecordChange{
		Action: cloud.RecordCreate, Name: name, IP: ip, TTL: ttl,
	})
	ok(f.t, err)
}

func (f *fixture) recordsAt(name string) []cloud.Record {
	f.t.Helper()
	records, err := f.memory.FindRecords(f.ctx, f.cfg.Hosts.ZoneID, name)
	ok(f.t, err)
	return records
}

// R-9SZQ-ZHX0
func TestLeftoverCheckRemovesOnlyTheDeletedHostsOwnRecord(t *testing.T) {
	f := setup(t)
	const earlierTTL = 73
	h := f.runningWithAddress(scanIP)
	ok(t, f.st.NoteHostRecord(f.ctx, h.Name, "earlier-change", "earlier conflict"))
	op := f.start(h, store.OpDelete, store.Target{})
	ok(t, f.st.Finish(f.ctx, op.ID, store.Update{}))
	ok(t, f.db.Write(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, "UPDATE hosts SET reason = ? WHERE name = ?", "earlier reason", h.Name)
		return err
	}))
	owned := h.Name + "." + f.cfg.Hosts.Domain
	hosts, err := f.st.DeletedHosts(f.ctx, 0, store.BatchSize)
	ok(t, err)
	equal(t, len(hosts), 1)
	equal(t, hosts[0].Reason, "earlier reason")
	equal(t, hosts[0].Change, "earlier-change")
	equal(t, hosts[0].RecordReason, "earlier conflict")
	beforeOps, err := f.st.OperationsOf(f.ctx, h.Name, "", store.BatchSize)
	ok(t, err)
	other, _ := f.deletedWithAddress(scanIP)
	absent, _ := f.deletedWithAddress(scanIP)
	noAddress, _ := f.deletedWithAddress("")
	settled, settledOp := f.deletedWithAddress(scanIP)
	ok(t, f.st.SettleDeleted(f.ctx, settledOp.Host))
	for _, name := range []string{owned, noAddress, settled} {
		f.seedRecord(name, scanIP, earlierTTL)
	}
	f.seedRecord(other, "192.0.2.42", 17)
	before := f.recordsAt(other)
	cl := &leftoverCloud{Cloud: f.memory}
	f.cfg.Cloud = cl
	ok(t, ops.New(f.cfg).Scan(f.ctx))
	checkIDs(t, cl.lookups, []string{owned, other, absent})
	equal(t, len(cl.changes), 1)
	equal(t, cl.changes[0], cloud.RecordChange{Action: cloud.RecordDelete, Name: owned, IP: scanIP, TTL: earlierTTL})
	equal(t, len(f.recordsAt(owned)), 0)
	if got := f.recordsAt(other); !reflect.DeepEqual(got, before) {
		t.Fatalf("record at %s = %v, want %v", other, got, before)
	}
	for _, name := range []string{noAddress, settled} {
		equal(t, len(f.recordsAt(name)), 1)
	}
	remaining, err := f.st.DeletedHosts(f.ctx, 0, store.BatchSize)
	ok(t, err)
	equal(t, len(remaining), 0)
	_, err = f.st.Host(f.ctx, op.Host)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reading settled host = %v, want %v", err, store.ErrNotFound)
	}
	afterOps, err := f.st.OperationsOf(f.ctx, h.Name, "", store.BatchSize)
	ok(t, err)
	if !reflect.DeepEqual(afterOps, beforeOps) {
		t.Fatalf("operations for %s = %v, want %v", h.Name, afterOps, beforeOps)
	}
	equal(t, len(f.events()), 0)
	cl.lookups, cl.changes = nil, nil
	ok(t, ops.New(f.cfg).Scan(f.ctx))
	equal(t, len(cl.lookups), 0)
	equal(t, len(cl.changes), 0)

	// Expose the host again through DeletedHosts without touching its other fields.
	ok(t, f.db.Write(f.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, "UPDATE host_records SET settled = 0 WHERE host_name = ?", h.Name)
		return err
	}))
	remaining, err = f.st.DeletedHosts(f.ctx, 0, store.BatchSize)
	ok(t, err)
	if !reflect.DeepEqual(remaining, hosts) {
		t.Fatalf("deleted host after scan = %v, want %v", remaining, hosts)
	}
}

// R-9SZQ-ZHX0
func TestLeftoverCheckDeletesOneExactMatchAmongOtherRecords(t *testing.T) {
	f := setup(t)
	name, _ := f.deletedWithAddress(scanIP)
	cl := &recordCloud{Cloud: f.memory, zone: f.cfg.Hosts.ZoneID, records: map[string][]cloud.Record{
		name: {
			{Type: "TXT", Values: []string{scanIP}},
			{Type: "A", Values: []string{scanIP, "192.0.2.42"}},
			{Type: "A", Values: []string{"192.0.2.42"}},
			{Type: "A", Values: []string{scanIP}, TTL: 63},
		},
	}}
	f.cfg.Cloud = cl
	ok(t, ops.New(f.cfg).Scan(f.ctx))
	equal(t, len(cl.changes), 1)
	equal(t, cl.changes[0], cloud.RecordChange{Action: cloud.RecordDelete, Name: name, IP: scanIP, TTL: 63})
}

// R-0EU3-XGXR
func TestLeftoverCheckStopsAtAnErrorAndTheScanContinues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stage string
		err   error
	}{
		{name: "throttled find", stage: "find", err: cloudErr(cloud.ClassTransient)},
		{name: "missing find", stage: "find", err: cloudErr(cloud.ClassNotFound)},
		{name: "refused delete", stage: "delete", err: cloudErr(cloud.ClassRefused)},
		{name: "mismatched delete", stage: "delete", err: cloudErr(cloud.ClassMismatch)},
		{name: "failed settle", stage: "settle", err: store.ErrUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			first, firstOp := f.deletedWithAddress(scanIP)
			second, secondOp := f.deletedWithAddress(scanIP)
			for _, name := range []string{first, second} {
				f.seedRecord(name, scanIP, 45)
			}
			ok(t, f.st.NoteGrow(f.ctx, "expired-volume", f.now.Add(-store.GrowWindow)))
			stray, err := f.memory.AllocateAddress(f.ctx, cloud.Tags{cloud.TagKind: cloud.KindHost})
			ok(t, err)
			cl := &leftoverCloud{Cloud: f.memory}
			switch tc.stage {
			case "find":
				cl.find = func(string) error { return tc.err }
			case "delete":
				cl.change = func(cloud.RecordChange) error { return tc.err }
			case "settle":
				cl.change = func(cloud.RecordChange) error { f.db.SetFailing(true); return nil }
				cl.recover = func() { f.db.SetFailing(false) }
			}
			f.cfg.Cloud = cl
			e := ops.New(f.cfg)
			err = e.Scan(f.ctx)
			f.db.SetFailing(false)
			if !errors.Is(err, tc.err) {
				t.Fatalf("scan failure = %v, want %v", err, tc.err)
			}
			checkIDs(t, cl.lookups, []string{first})
			if tc.stage != "settle" {
				equal(t, len(f.recordsAt(first)), 1)
			}
			remaining, err := f.st.DeletedHosts(f.ctx, 0, store.BatchSize)
			ok(t, err)
			equal(t, len(remaining), 2)
			equal(t, remaining[0].Name, firstOp.Host)
			equal(t, remaining[1].Name, secondOp.Host)
			events := f.events()
			equal(t, len(events), 1)
			equal(t, events[0].Name, trail.EventResourceUnexplained)
			equal(t, events[0].Attrs["resource"], any(stray.AllocationID))
			n, err := f.st.PruneGrows(f.ctx, f.now, store.BatchSize)
			ok(t, err)
			equal(t, n, 0)
			cl.find, cl.change, cl.lookups = nil, nil, nil
			ok(t, e.Scan(f.ctx))
			checkIDs(t, cl.lookups, []string{first, second})
			equal(t, len(f.recordsAt(first)), 0)
			equal(t, len(f.recordsAt(second)), 0)
			equal(t, len(f.events()), 1)
		})
	}
}

// R-0EU3-XGXR R-46VA-T1Q2
func TestScanReturnsFailuresFromBothRecordChecks(t *testing.T) {
	f := setup(t)
	live := f.runningWithAddress(scanIP)
	liveName := live.Name + "." + f.cfg.Hosts.Domain
	deletedName, _ := f.deletedWithAddress(scanIP)
	liveErr, leftoverErr := cloudErr(cloud.ClassTransient), cloudErr(cloud.ClassRefused)
	cl := &leftoverCloud{Cloud: f.memory, find: func(name string) error {
		if name == liveName {
			return liveErr
		}
		return leftoverErr
	}}
	f.cfg.Cloud = cl
	err := ops.New(f.cfg).Scan(f.ctx)
	if !errors.Is(err, liveErr) || !errors.Is(err, leftoverErr) {
		t.Fatalf("scan failure = %v, want both %v and %v", err, liveErr, leftoverErr)
	}
	checkIDs(t, cl.lookups, []string{liveName, deletedName})
}

// R-ABJK-WACE
func TestLeftoverCheckRemovesRecordsAcrossPages(t *testing.T) {
	f := setup(t)
	names := make([]string, 120)
	for i := range names {
		name, _ := f.deletedWithAddress(scanIP)
		names[i] = name
		f.seedRecord(name, scanIP, 45)
	}
	ok(t, ops.New(f.cfg).Scan(f.ctx))
	for _, name := range names {
		equal(t, len(f.recordsAt(name)), 0)
	}
}

// R-ABJK-WACE
func TestLeftoverCheckReadsNextPageAfterProcessingTheFirst(t *testing.T) {
	f := setup(t)
	firstPage := make([]string, 0, store.BatchSize)
	for range store.BatchSize - 1 {
		name, _ := f.deletedWithAddress(scanIP)
		firstPage = append(firstPage, name)
	}
	// Leave a pending delete on each side of the first page's final Seq.
	below := f.runningWithAddress(scanIP)
	deleteBelow := f.start(below, store.OpDelete, store.Target{})
	pageEnd, _ := f.deletedWithAddress(scanIP)
	above := f.runningWithAddress(scanIP)
	deleteAbove := f.start(above, store.OpDelete, store.Target{})
	trailing, _ := f.deletedWithAddress(scanIP)
	belowName := below.Name + "." + f.cfg.Hosts.Domain
	aboveName := above.Name + "." + f.cfg.Hosts.Domain
	firstPage = append(firstPage, pageEnd)
	secondPage := []string{aboveName, trailing}
	for _, name := range firstPage {
		f.seedRecord(name, scanIP, 45)
	}
	for _, name := range []string{belowName, aboveName, trailing} {
		f.seedRecord(name, scanIP, 45)
	}

	checked := make(map[string]bool, len(firstPage))
	cl := &leftoverCloud{Cloud: f.memory, change: func(change cloud.RecordChange) error {
		if !slices.Contains(firstPage, change.Name) || checked[change.Name] {
			return nil
		}
		checked[change.Name] = true
		switch len(checked) {
		case 1:
			return f.st.Finish(f.ctx, deleteBelow.ID, store.Update{})
		case len(firstPage):
			return f.st.Finish(f.ctx, deleteAbove.ID, store.Update{})
		default:
			return nil
		}
	}}
	f.cfg.Cloud = cl
	e := ops.New(f.cfg)
	ok(t, e.Scan(f.ctx))
	equal(t, len(cl.lookups), len(firstPage)+len(secondPage))
	for _, page := range []struct{ got, want []string }{
		{got: cl.lookups[:len(firstPage)], want: firstPage},
		{got: cl.lookups[len(firstPage):], want: secondPage},
	} {
		slices.Sort(page.got)
		slices.Sort(page.want)
		checkIDs(t, page.got, page.want)
	}
	equal(t, len(f.recordsAt(aboveName)), 0)
	equal(t, len(f.recordsAt(belowName)), 1)

	cl.lookups = nil
	ok(t, e.Scan(f.ctx))
	checkIDs(t, cl.lookups, []string{belowName})
	equal(t, len(f.recordsAt(belowName)), 0)
}
