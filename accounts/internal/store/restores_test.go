package store_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func requestRestore(t *testing.T, s *store.Store, r store.RestoreRequest) (store.Restore, store.Operation) {
	t.Helper()
	restore, operation, err := s.RequestRestore(ctx, r, caller)
	checkErr(t, err, nil)
	return restore, operation
}

// R-6OGS-YO6X R-FNP2-UKNY R-5NVT-HJY3
func TestRestoreChecksArgumentsAndTheLatestReportBeforeRecording(t *testing.T) {
	_, s, now := fixture(t)
	h, other := readyHost(t, s), readyHost(t, s)
	a := activeAccount(t, s, "acme", h.Name)
	place(t, s, "pending", h.Name)
	activeAccount(t, s, "other", other.Name)
	value(t, result(s.AddUser(ctx, "other", "outsider", firstKey, caller)))
	value(t, result(s.AddUser(ctx, a.Slug, "staff", firstKey, caller)))
	at := now.Add(-time.Minute).UTC().Truncate(time.Second)
	database := store.RestoreRequest{Account: a.Slug, Kind: protocol.RestoreDatabase, Path: "data/main.db", AsOf: at}
	files := store.RestoreRequest{Account: a.Slug, Kind: protocol.RestoreFiles, Path: "work/report", AsOf: at, User: "staff"}
	for _, invalid := range []store.RestoreRequest{
		{Account: a.Slug, Kind: "unknown", AsOf: at},
		{Account: a.Slug, Kind: protocol.RestoreFiles, Path: "../work", AsOf: at},
		{Account: a.Slug, Kind: protocol.RestoreDatabase, Path: "other/main.db", AsOf: at},
		{Account: a.Slug, Kind: protocol.RestoreDatabase, Path: "data/main.db", AsOf: at, User: "staff"},
		{Account: a.Slug, Kind: protocol.RestoreDatabase, Path: "data/main.db"},
	} {
		observe2(s.RequestRestore(ctx, invalid, caller)).assertZero(t, store.ErrUnreachable)
	}
	for _, badCaller := range []identity.Caller{{}, {UserID: strings.Repeat("x", valid.MaxUserIDBytes+1)}} {
		observe2(s.RequestRestore(ctx, database, badCaller)).assertZero(t, store.ErrUnreachable)
	}
	in := files
	in.Account, in.User = "missing", "unknown"
	observe2(s.RequestRestore(ctx, in, caller)).assertZero(t, store.ErrNotFound)
	in.Account = a.Slug
	observe2(s.RequestRestore(ctx, in, caller)).assertZero(t, store.ErrNoUser)
	in.User = "outsider"
	observe2(s.RequestRestore(ctx, in, caller)).assertZero(t, store.ErrNotInAccount)
	in.Account, in.User = "pending", ""
	observe2(s.RequestRestore(ctx, in, caller)).assertZero(t, store.ErrAccountNotActive)
	_, stopping := start(t, s, h.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stopping.ID, store.Update{}), nil)
	observe2(s.RequestRestore(ctx, files, caller)).assertZero(t, store.ErrNotRunning)
	_, starting := start(t, s, h.Name, store.OpStart, store.Target{})
	checkErr(t, s.Finish(ctx, starting.ID, store.Update{}), nil)
	observe2(s.RequestRestore(ctx, database, caller)).assertZero(t, store.ErrNoDatabase)
	observe2(s.RequestRestore(ctx, files, caller)).assertZero(t, store.ErrNoSnapshot)
	info := protocol.AccountBackup{Account: a.Slug, Databases: []protocol.Database{{Path: database.Path}}}
	recordBackup(t, s, h, *now, info)
	observe2(s.RequestRestore(ctx, database, caller)).assertZero(t, store.ErrNotRestorable)
	info.Databases[0].Oldest = at.Add(time.Second)
	recordBackup(t, s, h, *now, info)
	observe2(s.RequestRestore(ctx, database, caller)).assertZero(t, store.ErrNotRestorable)
	info.Databases[0].Oldest = at
	info.Snapshots = []int64{at.Unix()}
	recordBackup(t, s, h, *now, info, protocol.AccountBackup{Account: a.Slug})
	in = database
	in.AsOf = now.Add(time.Second)
	observe2(s.RequestRestore(ctx, in, caller)).assertZero(t, store.ErrNotRestorable)
	before := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, before.Backup.Restores, []protocol.RestoreRequest{})
	_, closeOp := closeHost(t, s, h.Name)
	checkErr(t, s.Finish(ctx, closeOp.ID, store.Update{}), nil)
	requestRestore(t, s, database)
	for i := 1; i < valid.MaxRestoresPerHost; i++ {
		requestRestore(t, s, files)
	}
	atCap := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, len(atCap.Backup.Restores), valid.MaxRestoresPerHost)
	observe2(s.RequestRestore(ctx, files, caller)).assertZero(t, store.ErrTooManyRestores)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), atCap)
	_, stopping = start(t, s, h.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stopping.ID, store.Update{}), nil)
}

// R-6QWL-Q7OB R-6BLI-POCE R-W9TP-MEN4 R-ZEU8-XFM2 R-PMQ6-B5JS R-OYDR-9HO5
// R-4VU4-OTW4 R-535I-ZGCA
func TestRestoreResultsStayWithTheirHostAndLeavePendingDesired(t *testing.T) {
	_, s, now := fixture(t)
	h, other := readyHost(t, s), readyHost(t, s)
	a := activeAccount(t, s, "acme", h.Name)
	b := activeAccount(t, s, "elsewhere", other.Name)
	when := now.Add(-time.Minute)
	recordBackup(t, s, h, *now, protocol.AccountBackup{Account: a.Slug, Snapshots: []int64{when.Unix()}})
	recordBackup(t, s, other, *now, protocol.AccountBackup{Account: b.Slug, Snapshots: []int64{when.Unix()}})
	previous := value(t, result(s.Host(ctx, h.Name)))
	aRequest := store.RestoreRequest{Account: a.Slug, Kind: protocol.RestoreFiles, Path: "work/file", AsOf: when}
	watch, release := s.Watch(h.Name)
	defer release()
	aRestore, aOp := requestRestore(t, s, aRequest)
	closed(t, watch, true)
	want := store.Restore{ID: aOp.ID, Host: h.Name, Account: a.Slug, Kind: aRequest.Kind, Path: aRequest.Path,
		AsOf: when.UTC().Truncate(time.Second), State: protocol.StepPending, Asked: now.UTC().Truncate(time.Second), CreatedBy: caller.UserID}
	eq(t, aRestore, want)
	eq(t, aOp.Kind, store.OpRestore)
	eq(t, aOp.Host, h.Name)
	eq(t, aOp.Account, a.Slug)
	previous.Version++
	eq(t, value(t, result(s.Host(ctx, h.Name))), previous)
	eq(t, value(t, result(s.NextTry(ctx))), aOp.NextTry)
	d := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, d.Backup.Restores, []protocol.RestoreRequest{{ID: aRestore.ID, Account: a.Slug, Kind: aRequest.Kind,
		Path: aRequest.Path, User: aRequest.User, AsOf: want.AsOf}})
	eq(t, d.Version, previous.Version)
	foreign, _ := requestRestore(t, s, store.RestoreRequest{Account: b.Slug, Kind: protocol.RestoreFiles, AsOf: when})
	checkErr(t, s.Advance(ctx, aOp.ID, store.Update{Step: "observe", DataGB: previous.DataGB + 1, InstanceID: "ignore"}), nil)
	eq(t, value(t, result(s.Host(ctx, h.Name))), previous)
	observe(s.Restore(ctx, previous.Operation)).assertZero(t, store.ErrNotFound)
	_, err := s.RecordReport(ctx, h.Name, protocol.Report{Restores: []protocol.RestoreResult{
		{ID: aRestore.ID, State: protocol.StepPending, Copy: "ignore"},
		{ID: foreign.ID, State: protocol.StepDone, Copy: "wrong-host"},
	}}, *now)
	checkErr(t, err, nil)
	eq(t, value(t, result(s.Restore(ctx, aRestore.ID))), want)
	eq(t, value(t, result(s.Restore(ctx, foreign.ID))), foreign)

	settleWatch, settleRelease := s.Watch(h.Name)
	defer settleRelease()
	reason := strings.Repeat("<🙂", protocol.MaxBackupError)
	want.Reason = strings.Repeat("<🙂", (protocol.MaxBackupError-2)/10)
	want.State, want.Copy = protocol.StepDone, "work/copy"
	_, err = s.RecordReport(ctx, h.Name, protocol.Report{Restores: []protocol.RestoreResult{
		{ID: aRestore.ID, State: protocol.StepDone, Copy: want.Copy, Error: reason},
	}}, *now)
	checkErr(t, err, nil)
	closed(t, settleWatch, false)
	eq(t, value(t, result(s.Restore(ctx, aRestore.ID))), want)
	after := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, after.Version, d.Version)
	eq(t, after.Backup.Restores, []protocol.RestoreRequest{})
	_, err = s.RecordReport(ctx, h.Name, protocol.Report{Restores: []protocol.RestoreResult{
		{ID: aRestore.ID, State: protocol.StepFailed, Error: "too late"},
	}}, *now)
	checkErr(t, err, nil)
	eq(t, value(t, result(s.Restore(ctx, aRestore.ID))), want)
	end := value(t, result(s.Host(ctx, h.Name)))
	checkErr(t, s.Finish(ctx, aOp.ID, store.Update{DataGB: 1}), nil)
	eq(t, value(t, result(s.Operation(ctx, aOp.ID))).Status, store.OpDone)
	eq(t, value(t, result(s.Host(ctx, h.Name))), end)
	recordBackup(t, s, h, *now, protocol.AccountBackup{Account: a.Slug, Snapshots: []int64{when.Unix()}})
	direct, directOp := requestRestore(t, s, aRequest)
	end = value(t, result(s.Host(ctx, h.Name)))
	checkErr(t, s.Fail(ctx, directOp.ID, "engine timeout"), nil)
	eq(t, value(t, result(s.Host(ctx, h.Name))), end)
	direct.State = protocol.StepFailed
	eq(t, value(t, result(s.Restore(ctx, direct.ID))), direct)
	eq(t, value(t, result(s.Operation(ctx, directOp.ID))).Status, store.OpFailed)
}

// R-FNP2-UKNY R-PV9G-ZJQN
func TestUnfinishedRestoresBoundCancellation(t *testing.T) {
	for _, cancel := range []string{"remove account", "replace host"} {
		t.Run(cancel, func(t *testing.T) {
			_, s, now := fixture(t)
			h := readyHost(t, s)
			acme := activeAccount(t, s, "acme", h.Name)
			at := now.Add(-time.Minute)
			fact := protocol.AccountBackup{Account: acme.Slug, Snapshots: []int64{at.Unix()}}
			recordBackup(t, s, h, *now, fact)
			req := store.RestoreRequest{Account: acme.Slug, Kind: protocol.RestoreFiles, AsOf: at}
			ids := make([]string, 0, valid.MaxRestoresPerHost)
			for range valid.MaxRestoresPerHost {
				r, op := requestRestore(t, s, req)
				ids = append(ids, op.ID)
				_, err := s.RecordReport(ctx, h.Name, protocol.Report{
					Backups:  []protocol.AccountBackup{fact},
					Restores: []protocol.RestoreResult{{ID: r.ID, State: protocol.StepDone, Copy: "work/copy"}},
				}, *now)
				checkErr(t, err, nil)
				eq(t, value(t, result(s.Restore(ctx, r.ID))).State, protocol.StepDone)
			}
			desired := value(t, result(s.Desired(ctx, h.Name)))
			eq(t, desired.Backup.Restores, []protocol.RestoreRequest{})
			observe2(s.RequestRestore(ctx, req, caller)).assertZero(t, store.ErrTooManyRestores)
			eq(t, value(t, result(s.Desired(ctx, h.Name))), desired)
			var cancelled []string
			if cancel == "remove account" {
				var err error
				_, _, cancelled, err = s.RemoveAccount(ctx, acme.Slug, caller)
				checkErr(t, err, nil)
			} else {
				_, _, cancelled = replaceHost(t, s, h.Name, store.ReplaceBackup, store.Target{})
			}
			eq(t, cancelled, ids)
			for _, id := range cancelled {
				eq(t, value(t, result(s.Operation(ctx, id))).Status, store.OpCancelled)
				eq(t, value(t, result(s.Restore(ctx, id))).State, protocol.StepDone)
			}
		})
	}
}

// R-FNP2-UKNY
func TestPendingRestoresCountAfterOperationsFinish(t *testing.T) {
	_, s, now := fixture(t)
	h := readyHost(t, s)
	acme := activeAccount(t, s, "acme", h.Name)
	at := now.Add(-time.Minute)
	recordBackup(t, s, h, *now, protocol.AccountBackup{Account: acme.Slug, Snapshots: []int64{at.Unix()}})
	req := store.RestoreRequest{Account: acme.Slug, Kind: protocol.RestoreFiles, AsOf: at}
	for range valid.MaxRestoresPerHost {
		r, op := requestRestore(t, s, req)
		checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
		eq(t, value(t, result(s.Restore(ctx, r.ID))), r)
	}
	desired := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, len(desired.Backup.Restores), valid.MaxRestoresPerHost)
	observe2(s.RequestRestore(ctx, req, caller)).assertZero(t, store.ErrTooManyRestores)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), desired)
}

// R-FNP2-UKNY
func TestRestoreCapCountsPendingAndRunningTogether(t *testing.T) {
	_, s, now := fixture(t)
	h := readyHost(t, s)
	acme := activeAccount(t, s, "acme", h.Name)
	at := now.Add(-time.Minute)
	fact := protocol.AccountBackup{Account: acme.Slug, Snapshots: []int64{at.Unix()}}
	recordBackup(t, s, h, *now, fact)
	req := store.RestoreRequest{Account: acme.Slug, Kind: protocol.RestoreFiles, AsOf: at}
	pending := valid.MaxRestoresPerHost / 2
	var completed []protocol.RestoreResult
	for i := range valid.MaxRestoresPerHost {
		r, op := requestRestore(t, s, req)
		if i < pending {
			checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
			continue
		}
		completed = append(completed, protocol.RestoreResult{ID: r.ID, State: protocol.StepDone, Copy: "work/copy"})
	}
	_, err := s.RecordReport(ctx, h.Name, protocol.Report{Backups: []protocol.AccountBackup{fact}, Restores: completed}, *now)
	checkErr(t, err, nil)
	desired := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, len(desired.Backup.Restores), pending)
	observe2(s.RequestRestore(ctx, req, caller)).assertZero(t, store.ErrTooManyRestores)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), desired)

	checkErr(t, s.Finish(ctx, completed[0].ID, store.Update{}), nil)
	r, _ := requestRestore(t, s, req)
	eq(t, value(t, result(s.Restore(ctx, r.ID))), r)
	observe2(s.RequestRestore(ctx, req, caller)).assertZero(t, store.ErrTooManyRestores)
}

// R-NME6-5759
func TestRemoveAccountCancelsRunningRestoresInIDOrder(t *testing.T) {
	now := baseTime
	rand := counterRandom(0)
	_, s := open(t, filepath.Join(t.TempDir(), "record.db"), accounts.Migrations(),
		store.Config{Now: func() time.Time { return now }, Rand: &rand})
	h := readyHost(t, s)
	acme := activeAccount(t, s, "acme", h.Name)
	beta := activeAccount(t, s, "beta", h.Name)
	at := now.Add(-time.Minute)
	recordBackup(t, s, h, now, protocol.AccountBackup{Account: acme.Slug, Snapshots: []int64{at.Unix()}},
		protocol.AccountBackup{Account: beta.Slug, Snapshots: []int64{at.Unix()}})
	req := store.RestoreRequest{Account: beta.Slug, Kind: protocol.RestoreFiles, AsOf: at}
	other, otherOp := requestRestore(t, s, req)
	req.Account = acme.Slug
	settled, settledOp := requestRestore(t, s, req)
	checkErr(t, s.Finish(ctx, settledOp.ID, store.Update{}), nil)
	rand = 0xaf
	first, firstOp := requestRestore(t, s, req)
	rand = 0x9f
	second, secondOp := requestRestore(t, s, req)
	now = now.Add(time.Minute)
	_, _, ids, err := s.RemoveAccount(ctx, acme.Slug, caller)
	checkErr(t, err, nil)
	eq(t, ids, []string{secondOp.ID, firstOp.ID})
	for _, r := range []store.Restore{first, second, settled} {
		r.State = protocol.StepFailed
		eq(t, value(t, result(s.Restore(ctx, r.ID))), r)
	}
	eq(t, value(t, result(s.Operation(ctx, settledOp.ID))).Status, store.OpDone)
	for _, op := range []store.Operation{firstOp, secondOp} {
		op.Status, op.Finished = store.OpCancelled, now.UTC().Truncate(time.Second)
		eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	}
	d := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, d.Backup.Restores, []protocol.RestoreRequest{{
		ID: other.ID, Account: other.Account, Kind: other.Kind, AsOf: other.AsOf,
	}})
	eq(t, value(t, result(s.Operation(ctx, otherOp.ID))), otherOp)
	gamma := activeAccount(t, s, "gamma", h.Name)
	_, _, ids, err = s.RemoveAccount(ctx, gamma.Slug, caller)
	checkErr(t, err, nil)
	eq(t, ids, []string(nil))
}

// R-WB1M-06DT
func TestRestoresOnOrdersByTimeThenDescendingIDAndCountsBeyondLimit(t *testing.T) {
	_, s, now := fixture(t)
	h, other := readyHost(t, s), readyHost(t, s)
	a := activeAccount(t, s, "acme", h.Name)
	at := now.Add(-time.Minute)
	recordBackup(t, s, h, *now, protocol.AccountBackup{Account: a.Slug, Snapshots: []int64{at.Unix()}})
	req := store.RestoreRequest{Account: a.Slug, Kind: protocol.RestoreFiles, AsOf: at}
	var all []store.Restore
	for i := range 30 {
		if i%3 == 0 {
			*now = now.Add(time.Second)
		}
		req.Path = fmt.Sprintf("file-%d", i)
		r, op := requestRestore(t, s, req)
		checkErr(t, s.Fail(ctx, op.ID, "simulate completed work"), nil)
		r.State = protocol.StepFailed
		all = append(all, r)
	}
	slices.Reverse(all)
	got, total, err := s.RestoresOn(ctx, h.Name, 25)
	checkErr(t, err, nil)
	eq(t, total, len(all))
	eq(t, got, all[:25])
	got, total, err = s.RestoresOn(ctx, other.Name, 25)
	checkErr(t, err, nil)
	eq(t, total, 0)
	eq(t, got, []store.Restore{})
	observe2(s.RestoresOn(ctx, "unknown", 25)).assertZero(t, store.ErrNotFound)
}
