package store_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func activeAccount(t *testing.T, s *store.Store, slug, host string) store.Account {
	t.Helper()
	a, placing := place(t, s, slug, host)
	checkErr(t, s.Finish(ctx, placing.ID, store.Update{}), nil)
	a.State = store.AccountActive
	return a
}

func recordBackup(t *testing.T, s *store.Store, h store.Host, received time.Time, backups ...protocol.AccountBackup) {
	t.Helper()
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Backups: backups}, received)))
}

// R-6JL7-FL85 R-6KT3-TCYU R-6BLI-POCE R-NJYD-DNNV R-6X03-N2DS
func TestBackupFactsTrackTheLatestEntryForEachAccount(t *testing.T) {
	d, s, now := fixture(t)
	host, other := readyHost(t, s), readyHost(t, s)
	activeAccount(t, s, "alpha", host.Name)
	activeAccount(t, s, "beta", host.Name)
	activeAccount(t, s, "foreign", other.Name)
	eq(t, value(t, result(s.BackupOf(ctx, "alpha"))), store.AccountBackup{Account: "alpha", Host: host.Name, State: store.AccountActive})
	eq(t, value(t, result(s.BackupOf(ctx, "foreign"))), store.AccountBackup{Account: "foreign", Host: other.Name, State: store.AccountActive})
	eq(t, value(t, result(s.BackupsOn(ctx, other.Name)))[0].Account, "foreign")
	observe(s.BackupOf(ctx, "unknown")).assertZero(t, store.ErrNotFound)

	snapshot := time.Date(2026, 11, 25, 9, 3, 7, 900_000_000, time.FixedZone("report", -5*3600))
	checked, synced, written := snapshot.Add(time.Minute), snapshot.Add(2*time.Minute), snapshot.Add(3*time.Minute)
	entries := []protocol.AccountBackup{
		{Account: "beta", Snapshot: snapshot.Add(-time.Hour)},
		{Account: "foreign", Snapshot: snapshot.Add(time.Hour)},
		{Account: "alpha", Snapshot: snapshot.Add(-time.Hour)},
		{Account: "alpha", Snapshot: snapshot, Checked: checked, Error: "backup error", DatabaseTotal: 12,
			DatabaseErrors: 3, Synced: synced, Written: written},
	}
	recordBackup(t, s, host, *now, entries...)
	report := value(t, result(s.Report(ctx, host.Name)))
	eq(t, len(report.Body.Backups), 3)
	for i, originalIndex := range []int{0, 2, 3} {
		got, want := report.Body.Backups[i], entries[originalIndex]
		eq(t, got.Account, want.Account)
		eq(t, got.Snapshot.Equal(want.Snapshot), true)
		eq(t, got.Error, want.Error)
	}
	utc := func(value time.Time) time.Time { return value.UTC().Truncate(time.Second) }
	want := store.AccountBackup{Account: "alpha", Host: host.Name, State: store.AccountActive, Snapshot: utc(snapshot),
		Checked: utc(checked), Error: entries[3].Error, Databases: 12, DatabaseErrors: 3,
		Synced: utc(synced), Written: utc(written), Reported: utc(*now)}
	eq(t, value(t, result(s.BackupOf(ctx, "alpha"))), want)
	eq(t, value(t, result(s.BackupOf(ctx, "foreign"))).Snapshot, time.Time{})
	*now = now.Add(10 * time.Minute)
	recordBackup(t, s, host, *now, protocol.AccountBackup{Account: "beta", Snapshot: *now})
	eq(t, value(t, result(s.BackupOf(ctx, "alpha"))), want)

	_, closeOp := closeHost(t, s, host.Name)
	recordBackup(t, s, host, *now, protocol.AccountBackup{Account: "alpha", Final: protocol.Step{
		ID: closeOp.ID, State: protocol.StepDone, At: *now,
	}})
	checkErr(t, s.Finish(ctx, closeOp.ID, store.Update{}), nil)
	final := value(t, result(s.BackupOf(ctx, "alpha")))
	eq(t, final.Final, protocol.StepDone)
	eq(t, final.FinalAt, utc(*now))
	_, replacement, _ := replaceHost(t, s, host.Name, store.ReplaceBackup, store.Target{})
	eq(t, value(t, result(s.BackupOf(ctx, "alpha"))).Restore, protocol.StepPending)
	recordBackup(t, s, host, *now, protocol.AccountBackup{Account: "alpha", Restored: protocol.Step{
		ID: replacement.ID, State: protocol.StepDone, At: *now,
	}})
	alpha := value(t, result(s.BackupOf(ctx, "alpha")))
	eq(t, alpha.Restore, protocol.StepDone)
	eq(t, alpha.Final, protocol.StepDone)
	checkErr(t, s.Fail(ctx, replacement.ID, "another restore failed"), nil)
	eq(t, value(t, result(s.BackupOf(ctx, "alpha"))).Restore, protocol.StepDone)
	observed := value(t, result(s.BackupsOn(ctx, host.Name)))
	eq(t, observed, []store.AccountBackup{alpha, value(t, result(s.BackupOf(ctx, "beta")))})
	d.SetFailing(true)
	observe(s.BackupsOn(ctx, host.Name)).assertZero(t, store.ErrUnreachable)
	d.SetFailing(false)
}

// R-6ZFW-ELV6 R-NL69-RFEK R-70NS-SDLV R-71VP-65CK R-733L-JX39 R-GS0T-HQVC
// R-NJYD-DNNV R-74BH-XOTY R-535I-ZGCA
func TestRemovedBackupsPageAndDeleteLifecycle(t *testing.T) {
	rand := new(counterRandom)
	now := baseTime
	cfg := store.Config{Now: func() time.Time { return now }, Rand: rand}
	path := filepath.Join(t.TempDir(), "record.db")
	d, s := open(t, path, accounts.Migrations(), cfg)
	h, other := readyHost(t, s), readyHost(t, s)
	activeAccount(t, s, "alpha", h.Name)
	activeAccount(t, s, "beta", h.Name)
	activeAccount(t, s, "gamma", h.Name)
	activeAccount(t, s, "elsewhere", other.Name)
	snapshot := now.UTC().Truncate(time.Second)
	recordBackup(t, s, h, now,
		protocol.AccountBackup{Account: "alpha", Snapshot: snapshot},
		protocol.AccountBackup{Account: "beta", Snapshot: snapshot.Add(-time.Hour)})
	observe(s.DeleteAccountBackups(ctx, "alpha", identity.Caller{UserID: strings.Repeat("x", valid.MaxUserIDBytes+1)})).
		assertZero(t, store.ErrUnreachable)
	observe(s.DeleteAccountBackups(ctx, "alpha", identity.Caller{})).assertZero(t, store.ErrUnreachable)
	observe(s.DeleteAccountBackups(ctx, "alpha", caller)).assertZero(t, store.ErrAccountExists)
	_, removeAlpha := remove(t, s, "alpha")
	checkErr(t, s.Finish(ctx, removeAlpha.ID, store.Update{}), nil)
	remove(t, s, "beta")
	remove(t, s, "gamma") // No backup facts, so deletion leaves no backup entry.
	_, deletion := deleteHost(t, s, h.Name, true)
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	observe(s.ListBackups(ctx, h.Name, "", 3)).assertZero(t, store.ErrNotFound)
	expected := store.AccountBackup{Account: "alpha", Host: h.Name, State: store.BackupsRemoved,
		Snapshot: snapshot, Reported: snapshot}
	eq(t, value(t, result(s.ListBackups(ctx, "", "", 1))), []store.AccountBackup{expected})
	beta := expected
	beta.Account, beta.Snapshot = "beta", snapshot.Add(-time.Hour)
	eq(t, value(t, result(s.ListBackups(ctx, "", "alpha", 1))), []store.AccountBackup{beta})
	eq(t, value(t, result(s.ListBackups(ctx, "", "beta", 10))),
		[]store.AccountBackup{{Account: "elsewhere", Host: other.Name, State: store.AccountActive}})
	eq(t, value(t, result(s.ListBackups(ctx, other.Name, "", 10))),
		[]store.AccountBackup{{Account: "elsewhere", Host: other.Name, State: store.AccountActive}})
	eq(t, value(t, result(s.NextTry(ctx))), time.Time{})
	b := value(t, result(s.DeleteAccountBackups(ctx, "alpha", caller)))
	eq(t, b, store.BackupsDelete{Account: "alpha", State: store.DeleteBackupsDeleting, NextTry: snapshot, Caller: caller})
	now = now.Add(time.Minute)
	eq(t, value(t, result(s.DeleteAccountBackups(ctx, "alpha", identity.Caller{UserID: "second"}))), b)
	eq(t, value(t, result(s.BackupsDeleteOf(ctx, "alpha"))), b)
	eq(t, value(t, result(s.NextTry(ctx))), b.NextTry)
	observe2(s.PlaceAccount(ctx, "alpha", other.Name, caller)).assertZero(t, store.ErrBackupsDeleting)
	eq(t, value(t, result(s.NameHeld(ctx, "alpha"))), false)
	expected.Delete = store.DeleteBackupsDeleting
	eq(t, value(t, result(s.ListBackups(ctx, "", "", 1))), []store.AccountBackup{expected})
	next := now.Add(time.Minute)
	checkErr(t, s.DeferBackupDelete(ctx, "alpha", next, 2), nil)
	b.Attempts, b.NextTry = 2, next.UTC().Truncate(time.Second)
	second := value(t, result(s.DeleteAccountBackups(ctx, "unused-slug", caller)))
	eq(t, value(t, result(s.DueBackupDeletes(ctx, now, "", 10))), []store.BackupsDelete{second})
	eq(t, value(t, result(s.NextTry(ctx))), second.NextTry)
	// A fresh handle sees the same pending deletion and its complete caller.
	checkErr(t, d.Close(), nil)
	_, s = open(t, path, accounts.Migrations(), cfg)
	eq(t, value(t, result(s.DueBackupDeletes(ctx, next, "", 1))), []store.BackupsDelete{b})
	eq(t, value(t, result(s.DueBackupDeletes(ctx, next, "alpha", 2))), []store.BackupsDelete{second})
	checkErr(t, s.FailBackupDelete(ctx, "alpha", "not available"), nil)
	b.State, b.Reason = store.DeleteBackupsFailed, "not available"
	eq(t, value(t, result(s.BackupsDeleteOf(ctx, "alpha"))), b)
	expected.Delete, expected.DeleteReason = store.DeleteBackupsFailed, b.Reason
	eq(t, value(t, result(s.ListBackups(ctx, "", "", 1))), []store.AccountBackup{expected})
	eq(t, value(t, result(s.DueBackupDeletes(ctx, next, "", 10))), []store.BackupsDelete{second})
	checkErr(t, s.DeferBackupDelete(ctx, "alpha", next, 3), store.ErrConflict)
	checkErr(t, s.FinishBackupDelete(ctx, "alpha"), store.ErrConflict)
	checkErr(t, s.FailBackupDelete(ctx, "alpha", "different"), store.ErrConflict)

	a := activeAccount(t, s, "alpha", other.Name)
	fresh := store.AccountBackup{Account: a.Slug, Host: a.Host, State: a.State}
	eq(t, value(t, result(s.BackupOf(ctx, a.Slug))), fresh)
	eq(t, value(t, result(s.ListBackups(ctx, "", "", 2))), []store.AccountBackup{fresh, beta})
	eq(t, value(t, result(s.BackupsDeleteOf(ctx, a.Slug))), b)
	_, unplace := remove(t, s, a.Slug)
	checkErr(t, s.Finish(ctx, unplace.ID, store.Update{}), nil)
	retry := value(t, result(s.DeleteAccountBackups(ctx, a.Slug, caller)))
	eq(t, retry, store.BackupsDelete{Account: a.Slug, State: store.DeleteBackupsDeleting, NextTry: now.UTC().Truncate(time.Second), Caller: caller})
	checkErr(t, s.FinishBackupDelete(ctx, a.Slug), nil)
	checkErr(t, s.FinishBackupDelete(ctx, second.Account), nil)
	eq(t, value(t, result(s.NextTry(ctx))), time.Time{})
	eq(t, value(t, result(s.ListBackups(ctx, "", "", 1))), []store.AccountBackup{beta})
	observe(s.BackupsDeleteOf(ctx, a.Slug)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.DeferBackupDelete(ctx, a.Slug, next, 1), store.ErrNotFound)
	checkErr(t, s.FailBackupDelete(ctx, a.Slug, "different"), store.ErrNotFound)
	place(t, s, a.Slug, other.Name)
}
