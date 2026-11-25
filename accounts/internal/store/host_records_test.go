package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

// R-A56Q-T7BY
func TestDeletedHostsPagesRetainedHostRecords(t *testing.T) {
	_, s, _ := fixture(t)
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 1))), []store.Host{})

	first, firstCreate := create(t, s)
	live, liveCreate := create(t, s)
	last, lastCreate := create(t, s)
	const retainedEIP = "192.0.2.9"
	checkErr(t, s.Advance(ctx, firstCreate.ID, store.Update{EIP: retainedEIP}), nil)
	checkErr(t, s.Finish(ctx, firstCreate.ID, store.Update{}), nil)
	checkErr(t, s.NoteHostRecord(ctx, first.Name, "first change", "first reason"), nil)
	checkErr(t, s.Finish(ctx, liveCreate.ID, store.Update{EIP: "192.0.2.10"}), nil)
	checkErr(t, s.Finish(ctx, lastCreate.ID, store.Update{}), nil)

	// Delete in reverse sequence order.
	deletingLast, lastDelete := start(t, s, last.Name, store.OpDelete, store.Target{})
	checkErr(t, s.Finish(ctx, lastDelete.ID, store.Update{}), nil)
	deletingLast.State = store.HostDeleted
	deletingLast.EIP = ""
	deletingFirst, firstDelete := start(t, s, first.Name, store.OpDelete, store.Target{})
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 3))), []store.Host{deletingLast})
	checkErr(t, s.Finish(ctx, firstDelete.ID, store.Update{}), nil)
	deletingFirst.State = store.HostDeleted
	deletingFirst.EIP = retainedEIP

	want := []store.Host{deletingFirst, deletingLast}
	wantLive := value(t, result(s.Host(ctx, live.Name)))
	for _, limit := range []int{-1, 0, paging.MaxLimit + 2} {
		_, err := s.DeletedHosts(ctx, 0, limit)
		checkErr(t, err, store.ErrUnreachable)
		eq(t, value(t, result(s.DeletedHosts(ctx, 0, paging.MaxLimit+1))), want)
		eq(t, value(t, result(s.Host(ctx, live.Name))), wantLive)
	}
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 1))), want[:1])
	eq(t, value(t, result(s.DeletedHosts(ctx, first.Seq, 1))), want[1:])
	eq(t, value(t, result(s.DeletedHosts(ctx, live.Seq, 1))), want[1:])
	eq(t, value(t, result(s.DeletedHosts(ctx, last.Seq, 1))), []store.Host{})

	checkErr(t, s.SettleDeleted(ctx, first.Name), nil)
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 1))), want[1:])
}

// R-A7MJ-KQTC
func TestSettleDeletedGuardsAndPreservesHostRecord(t *testing.T) {
	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: new(counterRandom)}
	path := filepath.Join(t.TempDir(), "settlement.db")
	migrations := accounts.Migrations()
	d, s := open(t, path, migrations, cfg)
	live := runningHost(t, s, "live instance")
	h, creation := create(t, s)
	checkErr(t, s.Advance(ctx, creation.ID, store.Update{EIP: "192.0.2.1"}), nil)
	checkErr(t, s.Finish(ctx, creation.ID, store.Update{EIP: "192.0.2.2"}), nil)
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "host change", "host conflict"), nil)
	_, deletion := start(t, s, h.Name, store.OpDelete, store.Target{})
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE hosts SET reason = ? WHERE name = ?", "host reason", h.Name)
		return err
	}), nil)
	deleted, otherCreation := create(t, s)
	checkErr(t, s.Finish(ctx, otherCreation.ID, store.Update{}), nil)
	_, otherDeletion := start(t, s, deleted.Name, store.OpDelete, store.Target{})
	checkErr(t, s.Finish(ctx, otherDeletion.ID, store.Update{}), nil)
	before := value(t, result(s.DeletedHosts(ctx, 0, 2)))
	eq(t, len(before), 2)
	eq(t, before[0].Name, h.Name)
	eq(t, before[0].EIP, "192.0.2.2")
	eq(t, before[1].Name, deleted.Name)
	ops := value(t, result(s.OperationsOf(ctx, h.Name, "", 2)))

	checkErr(t, s.SettleDeleted(ctx, "unknown"), store.ErrNotFound)
	checkErr(t, s.SettleDeleted(ctx, live.Name), store.ErrConflict)
	eq(t, value(t, result(s.Host(ctx, live.Name))), live)
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 2))), before)
	checkErr(t, s.SettleDeleted(ctx, h.Name), nil)
	checkErr(t, s.SettleDeleted(ctx, h.Name), nil)
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 2))), before[1:])
	_, err := s.Host(ctx, h.Name)
	checkErr(t, err, store.ErrNotFound)
	checkErr(t, d.Close(), nil)

	d, s = open(t, path, migrations, cfg)
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 2))), before[1:])
	checkErr(t, s.SettleDeleted(ctx, h.Name), nil)
	_, _, err = s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, store.ErrHostGone)
	eq(t, value(t, result(s.OperationsOf(ctx, h.Name, "", 2))), ops)
	eq(t, value(t, result(s.Host(ctx, live.Name))), live)

	// Expose the settled host's fields through Host, which only reads live hosts.
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE hosts SET state = ? WHERE name = ?", store.HostRunning, h.Name)
		return err
	}), nil)
	want := before[0]
	want.State = store.HostRunning
	eq(t, value(t, result(s.Host(ctx, h.Name))), want)

	checkErr(t, s.SettleDeleted(ctx, deleted.Name), nil)
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 2))), []store.Host{})
}

// R-YYI8-J48A
func TestRecordHostChangeGuardsAndPreservesRecords(t *testing.T) {
	_, s, _ := fixture(t)
	h, op := create(t, s)
	other, otherOp := create(t, s)
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "", "conflict"), nil)
	h.RecordReason = "conflict"
	checkErr(t, s.RecordHostChange(ctx, "", h.Name, "unexpected"), store.ErrNotFound)
	checkErr(t, s.RecordHostChange(ctx, "missing", h.Name, "unexpected"), store.ErrNotFound)
	checkErr(t, s.RecordHostChange(ctx, op.ID, other.Name, "unexpected"), store.ErrNotFound)
	checkErr(t, s.RecordHostChange(ctx, op.ID, "missing", "unexpected"), store.ErrNotFound)
	eq(t, value(t, result(s.ListHosts(ctx, 0, 2))), []store.Host{h, other})
	watch, release := s.Watch(h.Name)
	defer release()
	checkErr(t, s.RecordHostChange(ctx, op.ID, h.Name, "first"), nil)
	h.Change = "first"
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	eq(t, value(t, result(s.Operation(ctx, otherOp.ID))), otherOp)
	closed(t, watch, false)

	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	finishedHost := value(t, result(s.Host(ctx, h.Name)))
	finishedOp := value(t, result(s.Operation(ctx, op.ID)))
	checkErr(t, s.RecordHostChange(ctx, op.ID, "missing", "unexpected"), store.ErrConflict)
	checkErr(t, s.RecordHostChange(ctx, op.ID, h.Name, "unexpected"), store.ErrConflict)
	eq(t, value(t, result(s.ListHosts(ctx, 0, 2))), []store.Host{finishedHost, other})
	eq(t, value(t, result(s.Operation(ctx, op.ID))), finishedOp)
}

// R-Z0Y1-ANPO
func TestNoteHostRecordChangesOnlyTheRecord(t *testing.T) {
	_, s, _ := fixture(t)
	h, op := create(t, s)
	checkErr(t, s.NoteHostRecord(ctx, "missing", "change", "reason"), store.ErrNotFound)
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "first", "first reason"), nil)
	h.Change, h.RecordReason = "first", "first reason"
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)

	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	h = value(t, result(s.Host(ctx, h.Name)))
	eq(t, h.State, store.HostRunning)
	finished := value(t, result(s.Operation(ctx, op.ID)))
	watch, release := s.Watch(h.Name)
	defer release()
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "", "conflict"), nil)
	h.RecordReason = "conflict"
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "second", ""), nil)
	h.Change, h.RecordReason = "second", ""
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), finished)
	closed(t, watch, false)

	_, deletion := start(t, s, h.Name, store.OpDelete, store.Target{})
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "unexpected", "unexpected"), store.ErrNotFound)
	eq(t, value(t, result(s.ListHosts(ctx, 0, 2))), []store.Host{})
}

// R-DBOZ-HX6S R-YBAC-4CV8
func TestHostRecordReadsAndDesiredAfterReopen(t *testing.T) {
	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: new(counterRandom)}
	path := filepath.Join(t.TempDir(), "record.db")
	d, s := open(t, path, accounts.Migrations(), cfg)
	h, op := create(t, s)
	eq(t, h.Change, "")
	eq(t, h.RecordReason, "")
	checkErr(t, s.Advance(ctx, op.ID, store.Update{InstanceID: "instance"}), nil)
	h = value(t, result(s.Host(ctx, h.Name)))
	wantDesired := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, wantDesired.Domain, "")

	checkErr(t, s.RecordHostChange(ctx, op.ID, h.Name, "operation-change"), nil)
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "scan-change", "conflict"), nil)
	h.Change, h.RecordReason = "scan-change", "conflict"
	eq(t, value(t, result(s.ListHosts(ctx, 0, 1))), []store.Host{h})
	eq(t, value(t, result(s.Desired(ctx, h.Name))), wantDesired)

	checkErr(t, s.RecordHostChange(ctx, op.ID, h.Name, "newest-change"), nil)
	h.Change = "newest-change"
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "", ""), nil)
	h.RecordReason = ""
	checkErr(t, d.Close(), nil)
	_, s = open(t, path, accounts.Migrations(), cfg)
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	eq(t, value(t, result(s.HostByInstance(ctx, h.InstanceID))), h)
	eq(t, value(t, result(s.ListHostsBefore(ctx, h.Seq+1, 1))), []store.Host{h})
	eq(t, value(t, result(s.LastHosts(ctx, 1))), []store.Host{h})
	eq(t, value(t, result(s.Desired(ctx, h.Name))), wantDesired)
}
