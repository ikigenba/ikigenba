package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

// R-759B-PGTT
func TestHostNumberCanBeReusedOnlyAfterDeleteFinishes(t *testing.T) {
	_, s, _ := fixture(t)
	provisioning, _ := create(t, s)
	failed, failedCreate := create(t, s)
	checkErr(t, s.Fail(ctx, failedCreate.ID, "create failed"), nil)
	stopped, stoppedCreate := create(t, s)
	checkErr(t, s.Finish(ctx, stoppedCreate.ID, store.Update{}), nil)
	_, stop := start(t, s, stopped.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stop.ID, store.Update{}), nil)
	deleting, deletingCreate := create(t, s)
	checkErr(t, s.Finish(ctx, deletingCreate.ID, store.Update{}), nil)
	_, deletion := deleteHost(t, s, deleting.Name, false)
	fifth, _ := create(t, s)
	eq(t, []string{provisioning.Name, failed.Name, stopped.Name, deleting.Name, fifth.Name},
		[]string{"host01", "host02", "host03", "host04", "host05"})
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	reused, _ := create(t, s)
	eq(t, reused.Name, deleting.Name)
	eq(t, reused.Seq, fifth.Seq+1)
}

// R-YWEO-C76I R-7CKQ-039Z R-77P4-H0B7
func TestHostDeleteGuardsAndChoice(t *testing.T) {
	_, s, now := fixture(t)
	running, creation := create(t, s)
	busy, busyCreate := create(t, s)
	for _, keepData := range []bool{true, false} {
		for _, uid := range []string{"", strings.Repeat("u", valid.MaxUserIDBytes+1)} {
			observe2(s.DeleteHost(ctx, busy.Name, keepData, identity.Caller{UserID: uid})).assertZero(t, store.ErrUnreachable)
		}
		observe2(s.DeleteHost(ctx, "missing", keepData, caller)).assertZero(t, store.ErrNotFound)
		observe2(s.DeleteHost(ctx, busy.Name, keepData, caller)).assertZero(t, store.ErrBusy)
	}
	eq(t, value(t, result(s.Operation(ctx, busyCreate.ID))), busyCreate)
	eq(t, value(t, result(s.Host(ctx, busy.Name))), busy)
	checkErr(t, s.Finish(ctx, creation.ID, store.Update{}), nil)
	eq(t, value(t, result(s.Host(ctx, running.Name))).OnDelete, "")
	account, placing := place(t, s, "customer", running.Name)
	checkErr(t, s.Finish(ctx, placing.ID, store.Update{}), nil)
	active := value(t, result(s.Host(ctx, running.Name)))
	held := value(t, result(s.Account(ctx, account.Slug)))
	ops := value(t, result(s.OperationsOf(ctx, running.Name, "", 10)))
	for _, keepData := range []bool{true, false} {
		observe2(s.DeleteHost(ctx, running.Name, keepData, caller)).assertZero(t, store.ErrHasAccounts)
		eq(t, value(t, result(s.Account(ctx, account.Slug))), held)
		eq(t, value(t, result(s.OperationsOf(ctx, running.Name, "", 10))), ops)
		eq(t, value(t, result(s.Host(ctx, running.Name))), active)
	}
	remove(t, s, account.Slug)
	_, removal := deleteHost(t, s, running.Name, false)
	checkErr(t, s.Finish(ctx, removal.ID, store.Update{}), nil)
	for _, keepData := range []bool{true, false} {
		observe2(s.DeleteHost(ctx, running.Name, keepData, caller)).assertZero(t, store.ErrHostGone)
	}

	checkErr(t, s.Fail(ctx, busyCreate.ID, "create failed before data allocation"), nil)
	failed := value(t, result(s.Host(ctx, busy.Name)))
	eq(t, failed.OnDelete, "")
	eq(t, failed.DataVolume, "")
	resultHost, deletion := deleteHost(t, s, failed.Name, false)
	stamp := now.UTC().Truncate(time.Second)
	eq(t, deletion, store.Operation{ID: deletion.ID, Host: failed.Name, Kind: store.OpDelete, Status: store.OpRunning,
		Generation: 1, ResumeState: store.HostFailed, Started: stamp, StepStarted: stamp, NextTry: stamp, Caller: caller})
	eq(t, value(t, result(s.Operation(ctx, deletion.ID))), deletion)
	want := failed
	want.State, want.Reason, want.OnDelete, want.Operation = store.HostDeleting, "", store.OnDeleteDelete, deletion.ID
	eq(t, resultHost, want)
	eq(t, value(t, result(s.Host(ctx, failed.Name))), want)

	checkErr(t, s.Fail(ctx, deletion.ID, "delete failed"), nil)
	failedAgain := value(t, result(s.Host(ctx, failed.Name)))
	eq(t, failedAgain.State, store.HostFailed)
	eq(t, failedAgain.OnDelete, store.OnDeleteDelete)
	resumed, same, err := s.RetryHost(ctx, failed.Name, caller)
	checkErr(t, err, nil)
	eq(t, same.ID, deletion.ID)
	eq(t, resumed, want)
	checkErr(t, s.Fail(ctx, deletion.ID, "still failed"), nil)
	kept, newer := deleteHost(t, s, failed.Name, true)
	eq(t, newer.ResumeState, store.HostFailed)
	eq(t, kept.OnDelete, store.OnDeleteKeep)
	eq(t, value(t, result(s.Host(ctx, failed.Name))), kept)
}

func earlierMigrations(t *testing.T) (fstest.MapFS, string) {
	t.Helper()
	all := accounts.Migrations()
	earlier := fstest.MapFS{}
	var schema strings.Builder
	for _, name := range []string{"0001_accounts.sql", "0002_hosts.sql", "0003_bans.sql", "0004_invites.sql", "0005_host_records.sql"} {
		body := value(t, result(fs.ReadFile(all, name)))
		earlier[name] = &fstest.MapFile{Data: body}
		schema.Write(body)
	}
	return earlier, schema.String()
}

// R-7R7I-LC6B R-7PZM-7KFM R-76H8-38KI R-77P4-H0B7
func TestKeptVolumeMigrationAdoptsEarlierDeletes(t *testing.T) {
	all := accounts.Migrations()
	earlier, schema := earlierMigrations(t)
	migration := value(t, result(fs.ReadFile(all, "0006_kept_volumes.sql")))
	previousTable := regexp.MustCompile(`(?i)\b(` + strings.Join(tableNames(schema), "|") + `)\b`)
	createStatement := regexp.MustCompile(`^CREATE (TABLE|INDEX|UNIQUE INDEX)\b`)
	for statement := range strings.SplitSeq(strings.TrimSuffix(strings.TrimSpace(string(migration)), ";"), ";") {
		if !createStatement.MatchString(strings.TrimSpace(statement)) || previousTable.MatchString(statement) {
			t.Fatalf("migration statement is not additive: %q", statement)
		}
	}

	rand := new(counterRandom)
	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: rand}
	path := filepath.Join(t.TempDir(), "prior.db")
	d, s := open(t, path, earlier, cfg)
	const volume = "vol-vacated"
	vacated, vacatedDelete := create(t, s)
	adopted, adoptedDelete := create(t, s)
	another, anotherDelete := create(t, s)
	deleting, inProgress := create(t, s)
	finished := baseTime.UTC().Truncate(time.Second)
	// Simulate an earlier release writing operations without a delete choice
	// or a kept-volume row. Keep a release in flight across the migration.
	seedEarlierDelete(t, d.Write, vacated, vacatedDelete, volume, finished)
	seedEarlierDelete(t, d.Write, adopted, adoptedDelete, "vol-adopted", finished.Add(time.Second))
	seedEarlierDelete(t, d.Write, another, anotherDelete, "vol-another", finished.Add(2*time.Second))
	seedEarlierDelete(t, d.Write, deleting, inProgress, "vol-in-progress", time.Time{})
	checkErr(t, d.Close(), nil)

	d, s = open(t, path, all, cfg)
	oldOps := value(t, result(s.OperationsOf(ctx, vacated.Name, "", 2)))
	eq(t, len(oldOps), 1)
	eq(t, oldOps[0].ID, vacatedDelete.ID)
	original := value(t, result(s.Operation(ctx, adoptedDelete.ID)))
	eq(t, original.Finished, finished.Add(time.Second))
	reused, reusedCreate := create(t, s)
	eq(t, reused.Name, vacated.Name)
	eq(t, reused.Seq, deleting.Seq+1)
	eq(t, value(t, result(s.KeptVolume(ctx, volume))), store.KeptVolume{Volume: volume, Host: vacated.Name,
		SizeGB: spec.DataGB, Deleted: finished, State: store.VolumeKept})
	eq(t, value(t, result(s.OperationsOf(ctx, reused.Name, "", 2))), []store.Operation{reusedCreate})
	moved := value(t, result(s.Operation(ctx, vacatedDelete.ID)))
	if moved.Host == vacated.Name {
		t.Fatal("vacated operation still belongs to the reused name")
	}
	oldOps[0].Host = moved.Host
	eq(t, moved, oldOps[0])
	observe(s.Host(ctx, moved.Host)).assertZero(t, store.ErrNotFound)

	observe(s.KeptVolume(ctx, "vol-adopted")).assertZero(t, store.ErrNotFound)
	before := value(t, result(s.DeletedHosts(ctx, 0, 10)))
	eq(t, len(before), 2)
	observe(s.AdoptKept(ctx, 0)).assertZero(t, store.ErrUnreachable)
	eq(t, value(t, result(s.LastKeptVolumes(ctx, 2))), []store.KeptVolume{{
		Volume: volume, Host: vacated.Name, SizeGB: spec.DataGB, Deleted: finished, State: store.VolumeKept,
	}})
	eq(t, value(t, result(s.AdoptKept(ctx, 1))), 1)
	eq(t, value(t, result(s.AdoptKept(ctx, 1))), 1)
	eq(t, value(t, result(s.AdoptKept(ctx, 1))), 0)
	eq(t, value(t, result(s.Operation(ctx, adoptedDelete.ID))), original)
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 10))), before)
	eq(t, value(t, result(s.KeptVolume(ctx, "vol-adopted"))), store.KeptVolume{Volume: "vol-adopted", Host: adopted.Name,
		SizeGB: spec.DataGB, Deleted: finished.Add(time.Second), State: store.VolumeKept})
	eq(t, value(t, result(s.KeptVolume(ctx, "vol-another"))), store.KeptVolume{Volume: "vol-another", Host: another.Name,
		SizeGB: spec.DataGB, Deleted: finished.Add(2 * time.Second), State: store.VolumeKept})
	eq(t, value(t, result(s.Host(ctx, deleting.Name))).OnDelete, store.OnDeleteKeep)
	checkErr(t, s.Finish(ctx, inProgress.ID, store.Update{}), nil)
	eq(t, value(t, result(s.KeptVolume(ctx, "vol-in-progress"))).State, store.VolumeKept)
	checkErr(t, d.Close(), nil)

	d, _ = open(t, path, earlier, cfg)
	// The earlier release can update its own tables after this migration.
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE hosts SET reason = ? WHERE name = ?", "legacy write", reused.Name)
		return err
	}), nil)
	checkErr(t, d.Close(), nil)
	_, s = open(t, path, all, cfg)
	eq(t, value(t, result(s.Host(ctx, reused.Name))).Reason, "legacy write")
	eq(t, value(t, result(s.KeptVolume(ctx, volume))).Host, vacated.Name)
}

func seedEarlierDelete(t *testing.T, write func(context.Context, func(*sql.Tx) error) error, h store.Host, op store.Operation, volume string, finished time.Time) {
	t.Helper()
	state, status := store.HostDeleting, store.OpRunning
	if !finished.IsZero() {
		state, status = store.HostDeleted, store.OpDone
	}
	checkErr(t, write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE hosts SET state = ?, data_volume = ? WHERE name = ?", state, volume, h.Name); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE operations SET kind = ?, status = ?, finished = ? WHERE id = ?",
			store.OpDelete, status, finished.Unix(), op.ID)
		return err
	}), nil)
}

// R-76H8-38KI
func TestReusedHostNameStartsWithFreshRecord(t *testing.T) {
	_, s, now := fixture(t)
	h, op := create(t, s)
	resources := store.Update{InstanceID: "instance-before", RootVolume: "root-before", DataVolume: "data-before", AllocationID: "allocation-before"}
	checkErr(t, s.Finish(ctx, op.ID, resources), nil)
	value(t, result(s.Allow(ctx, h.Name, "192.0.2.12")))
	value(t, result(s.Unban(ctx, h.Name, "192.0.2.13")))
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{}, *now)))
	checkErr(t, s.NoteHostRecord(ctx, h.Name, "change-before", "reason-before"), nil)
	_, deletion := deleteHost(t, s, h.Name, true)
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	kept := value(t, result(s.KeptVolume(ctx, resources.DataVolume)))
	replacement, newOp := create(t, s)
	eq(t, replacement.Name, h.Name)
	eq(t, replacement.Seq, h.Seq+1)
	eq(t, value(t, result(s.Host(ctx, replacement.Name))), replacement)
	eq(t, replacement.Allowlist, []string{})
	eq(t, replacement.Change, "")
	eq(t, replacement.RecordReason, "")
	eq(t, value(t, result(s.Report(ctx, replacement.Name))), nil)
	eq(t, value(t, result(s.Desired(ctx, replacement.Name))).Unban, []string{})
	eq(t, value(t, result(s.OperationsOf(ctx, replacement.Name, "", 10))), []store.Operation{newOp})
	eq(t, value(t, result(s.DeletedHosts(ctx, 0, 10))), []store.Host{})
	for _, id := range []string{resources.InstanceID, resources.RootVolume, resources.DataVolume, resources.AllocationID} {
		eq(t, value(t, result(s.Explains(ctx, id))), true)
	}
	eq(t, value(t, result(s.KeptVolume(ctx, resources.DataVolume))), kept)
}
