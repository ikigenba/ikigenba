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

// R-6JL7-FL85 R-6KT3-TCYU R-6BLI-POCE R-AOH7-FU0Q R-6X03-N2DS
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

// R-AOH7-FU0Q R-6X03-N2DS R-NL69-RFEK R-PU1K-LRZY
func TestAccountBackupFinalFollowsPlacementOnTheSameHost(t *testing.T) {
	_, s, now := fixture(t)
	h := readyHost(t, s)
	a := activeAccount(t, s, "acme", h.Name)
	*now = now.Add(time.Minute)
	_, closed := closeHost(t, s, h.Name)
	finalAt := now.UTC().Truncate(time.Second)
	recordBackup(t, s, h, *now, protocol.AccountBackup{Account: a.Slug, Final: protocol.Step{
		ID: closed.ID, State: protocol.StepDone, At: finalAt,
	}})
	checkErr(t, s.Finish(ctx, closed.ID, store.Update{}), nil)
	got := value(t, result(s.BackupOf(ctx, a.Slug)))
	eq(t, got.Final, protocol.StepDone)
	eq(t, got.FinalAt, finalAt)

	*now = now.Add(time.Minute)
	_, removed := remove(t, s, a.Slug)
	checkErr(t, s.Finish(ctx, removed.ID, store.Update{}), nil)
	fresh := activeAccount(t, s, a.Slug, h.Name)
	want := store.AccountBackup{Account: fresh.Slug, Host: fresh.Host, State: fresh.State}
	eq(t, value(t, result(s.BackupOf(ctx, fresh.Slug))), want)
	eq(t, value(t, result(s.BackupsOn(ctx, h.Name))), []store.AccountBackup{want})
	for _, host := range []string{"", h.Name} {
		eq(t, value(t, result(s.ListBackups(ctx, host, "", 1))), []store.AccountBackup{want})
	}
	eq(t, value(t, result(s.BackupSteps(ctx, closed.ID))), []store.BackupStep{
		{State: protocol.StepPending, At: closed.Started},
		{Account: fresh.Slug, State: protocol.StepDone, At: finalAt},
	})

	_, opening := openHost(t, s, h.Name)
	checkErr(t, s.Finish(ctx, opening.ID, store.Update{}), nil)
	_, later := closeHost(t, s, h.Name)
	want.Final, want.FinalAt = protocol.StepPending, later.Started
	eq(t, value(t, result(s.BackupOf(ctx, fresh.Slug))), want)
}

// R-AOH7-FU0Q R-PU1K-LRZY
func TestAccountBackupStepsUseReportTimeWhenTheOperationStartedBeforePlacement(t *testing.T) {
	for _, kind := range []string{store.OpClose, store.OpReplace} {
		t.Run(kind, func(t *testing.T) {
			_, s, now := fixture(t)
			h := readyHost(t, s)
			a := activeAccount(t, s, "acme", h.Name)
			var op store.Operation
			if kind == store.OpClose {
				_, op = closeHost(t, s, h.Name)
			} else {
				_, op, _ = replaceHost(t, s, h.Name, store.ReplaceBackup, store.Target{})
			}
			reportStep := func(a store.Account, at time.Time) {
				t.Helper()
				entry := protocol.AccountBackup{Account: a.Slug}
				step := protocol.Step{ID: op.ID, State: protocol.StepDone, At: at}
				if kind == store.OpClose {
					entry.Final = step
				} else {
					entry.Restored = step
				}
				recordBackup(t, s, h, *now, entry)
			}
			checkStep := func(slug, state string, at time.Time) {
				t.Helper()
				got := value(t, result(s.BackupOf(ctx, slug)))
				if kind == store.OpClose {
					eq(t, got.Final, state)
					eq(t, got.FinalAt, at)
				} else {
					eq(t, got.Restore, state)
				}
			}
			eq(t, op.Started, a.Created)
			reportStep(a, a.Created.Add(-time.Second))
			checkStep(a.Slug, protocol.StepDone, a.Created.Add(-time.Second))

			*now = now.Add(time.Minute)
			_, removed := remove(t, s, a.Slug)
			checkErr(t, s.Finish(ctx, removed.ID, store.Update{}), nil)
			a = activeAccount(t, s, a.Slug, h.Name)
			checkStep(a.Slug, "", time.Time{})
			eq(t, value(t, result(s.BackupSteps(ctx, op.ID))), []store.BackupStep{
				{State: protocol.StepPending, At: op.Started},
				{Account: a.Slug, State: protocol.StepDone, At: op.Started.Add(-time.Second)},
			})

			for _, tc := range []struct {
				slug   string
				offset time.Duration
				state  string
			}{
				{"early", -time.Second, ""},
				{"equal", 0, protocol.StepDone},
				{"later", time.Second, protocol.StepDone},
			} {
				placed := activeAccount(t, s, tc.slug, h.Name)
				checkStep(placed.Slug, "", time.Time{})
				at := placed.Created.Add(tc.offset)
				reportStep(placed, at)
				if tc.state == "" {
					at = time.Time{}
				}
				checkStep(placed.Slug, tc.state, at)
			}
			if kind == store.OpReplace {
				checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
				checkStep("equal", "", time.Time{})
			}
		})
	}
}

// R-6ZFW-ELV6 R-NL69-RFEK R-70NS-SDLV R-71VP-65CK R-733L-JX39 R-GS0T-HQVC
// R-AOH7-FU0Q R-74BH-XOTY R-535I-ZGCA
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
