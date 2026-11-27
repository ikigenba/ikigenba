package store_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

// R-741F-BP34 R-7JW4-APQ5
func TestKeptVolumesOnEmptyDatabaseAndLimitBounds(t *testing.T) {
	_, s, _ := fixture(t)
	zero := time.Time{}
	for _, limit := range []int{1, paging.MaxLimit + 1} {
		eq(t, value(t, result(s.ListKeptVolumes(ctx, zero, "", limit))), []store.KeptVolume{})
		eq(t, value(t, result(s.ListKeptVolumesBefore(ctx, zero, "", limit))), []store.KeptVolume{})
		eq(t, value(t, result(s.LastKeptVolumes(ctx, limit))), []store.KeptVolume{})
		eq(t, value(t, result(s.DueVolumeDeletes(ctx, baseTime, "", limit))), []store.KeptVolume{})
		eq(t, value(t, result(s.AdoptKept(ctx, limit))), 0)
	}
	for _, limit := range []int{-1, 0, paging.MaxLimit + 2} {
		observe(s.ListKeptVolumes(ctx, zero, "", limit)).assertZero(t, store.ErrUnreachable)
		observe(s.ListKeptVolumesBefore(ctx, zero, "", limit)).assertZero(t, store.ErrUnreachable)
		observe(s.LastKeptVolumes(ctx, limit)).assertZero(t, store.ErrUnreachable)
		observe(s.DueVolumeDeletes(ctx, baseTime, "", limit)).assertZero(t, store.ErrUnreachable)
		observe(s.AdoptKept(ctx, limit)).assertZero(t, store.ErrUnreachable)
	}
	const unknown = "missing"
	observe(s.KeptVolume(ctx, unknown)).assertZero(t, store.ErrNotFound)
	observe(s.DeleteKeptVolume(ctx, unknown, caller)).assertZero(t, store.ErrNotFound)
	checkErr(t, s.DeferVolumeDelete(ctx, unknown, baseTime, 1), store.ErrNotFound)
	checkErr(t, s.FinishVolumeDelete(ctx, unknown), store.ErrNotFound)
	checkErr(t, s.FailVolumeDelete(ctx, unknown, "refusal"), store.ErrNotFound)
	observe2(s.DeleteHost(ctx, unknown, true, true, caller)).assertZero(t, store.ErrNotFound)
	observe2(s.DeleteHost(ctx, unknown, false, true, caller)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.NextTry(ctx))), zero)
}

// R-7DSM-DV0O R-72TI-XXCF R-7NJT-G0Y8
func TestHostFinishRecordsOnlyItsDataVolume(t *testing.T) {
	_, s, now := fixture(t)
	a, aCreate := create(t, s)
	b, bCreate := create(t, s)
	c, cCreate := create(t, s)
	checkErr(t, s.Finish(ctx, aCreate.ID, store.Update{DataVolume: "old-data", RootVolume: "root"}), nil)
	checkErr(t, s.Finish(ctx, bCreate.ID, store.Update{DataVolume: "delete-data"}), nil)
	checkErr(t, s.Finish(ctx, cCreate.ID, store.Update{}), nil)
	observe(s.KeptVolume(ctx, "old-data")).assertZero(t, store.ErrNotFound)
	for _, volume := range []string{"root", "delete-data", "unknown"} {
		observe(s.DeleteKeptVolume(ctx, volume, caller)).assertZero(t, store.ErrNotFound)
	}

	_, keep := deleteHost(t, s, a.Name, true)
	_, discard := deleteHost(t, s, b.Name, false)
	_, noData := deleteHost(t, s, c.Name, true)
	*now = time.Date(2026, 11, 25, 9, 3, 7, 900_000_000, time.FixedZone("local", -5*3600))
	const data = "vol-a"
	checkErr(t, s.Finish(ctx, keep.ID, store.Update{DataVolume: data, DataGB: 60}), nil)
	checkErr(t, s.Finish(ctx, discard.ID, store.Update{}), nil)
	checkErr(t, s.Finish(ctx, noData.ID, store.Update{}), nil)
	want := store.KeptVolume{Volume: data, Host: a.Name, SizeGB: 60, Deleted: time.Date(2026, 11, 25, 14, 3, 7, 0, time.UTC), State: store.VolumeKept, Cause: store.CauseDeleted}
	eq(t, value(t, result(s.KeptVolume(ctx, data))), want)
	eq(t, value(t, result(s.ListKeptVolumes(ctx, time.Time{}, "", 2))), []store.KeptVolume{want})
	for _, volume := range []string{"root", "delete-data", "old-data", "unknown"} {
		observe(s.KeptVolume(ctx, volume)).assertZero(t, store.ErrNotFound)
		observe(s.DeleteKeptVolume(ctx, volume, caller)).assertZero(t, store.ErrNotFound)
	}
	eq(t, value(t, result(s.AdoptKept(ctx, 2))), 0)
	checkErr(t, s.DeferVolumeDelete(ctx, "delete-data", *now, 1), store.ErrConflict)
	checkErr(t, s.FinishVolumeDelete(ctx, "delete-data"), store.ErrConflict)

	// A later deleted host bearing the same recorded volume does not change
	// the kept record originally written for it.
	reuse, creation := create(t, s)
	checkErr(t, s.Finish(ctx, creation.ID, store.Update{DataVolume: data}), nil)
	_, laterDelete := deleteHost(t, s, reuse.Name, false)
	checkErr(t, s.Finish(ctx, laterDelete.ID, store.Update{}), nil)
	eq(t, value(t, result(s.KeptVolume(ctx, data))), want)

	value(t, result(s.DeleteKeptVolume(ctx, data, caller)))
	checkErr(t, s.DeferVolumeDelete(ctx, data, *now, 1), nil)
	got := value(t, result(s.KeptVolume(ctx, data)))
	eq(t, got.NextTry, want.Deleted)
	checkErr(t, s.FinishVolumeDelete(ctx, data), nil)
	observe(s.KeptVolume(ctx, data)).assertZero(t, store.ErrNotFound)
}

func addKept(t *testing.T, s *store.Store, volume string) store.KeptVolume {
	t.Helper()
	h, creation := create(t, s)
	checkErr(t, s.Finish(ctx, creation.ID, store.Update{DataVolume: volume}), nil)
	_, deletion := deleteHost(t, s, h.Name, true)
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	return value(t, result(s.KeptVolume(ctx, volume)))
}

// R-7IO7-WXZG R-7JW4-APQ5
func TestKeptVolumeListUsesTimeAndVolumeCursor(t *testing.T) {
	_, s, now := fixture(t)
	first := addKept(t, s, "vol-1")
	*now = now.Add(time.Hour)
	second := addKept(t, s, "vol-2")
	third := addKept(t, s, "vol-3")
	third = value(t, result(s.DeleteKeptVolume(ctx, third.Volume, caller)))
	*now = now.Add(time.Hour)
	fourth := addKept(t, s, "vol-4")
	gone := addKept(t, s, "vol-deleted")
	value(t, result(s.DeleteKeptVolume(ctx, gone.Volume, caller)))
	checkErr(t, s.FinishVolumeDelete(ctx, gone.Volume), nil)

	eq(t, value(t, result(s.ListKeptVolumes(ctx, time.Time{}, "", 2))), []store.KeptVolume{fourth, third})
	eq(t, value(t, result(s.ListKeptVolumes(ctx, third.Deleted, third.Volume, 2))), []store.KeptVolume{second, first})
	eq(t, value(t, result(s.ListKeptVolumes(ctx, third.Deleted.Add(time.Nanosecond), "vol-0", 3))), []store.KeptVolume{third, second, first})
	eq(t, value(t, result(s.ListKeptVolumesBefore(ctx, second.Deleted, second.Volume, 1))), []store.KeptVolume{third})
	eq(t, value(t, result(s.ListKeptVolumesBefore(ctx, second.Deleted, second.Volume, 2))), []store.KeptVolume{fourth, third})
	eq(t, value(t, result(s.ListKeptVolumesBefore(ctx, second.Deleted.Add(time.Nanosecond), "vol-0", 3))), []store.KeptVolume{fourth})
	eq(t, value(t, result(s.ListKeptVolumesBefore(ctx, fourth.Deleted, fourth.Volume, 1))), []store.KeptVolume{})
	eq(t, value(t, result(s.LastKeptVolumes(ctx, 3))), []store.KeptVolume{third, second, first})
	eq(t, value(t, result(s.LastKeptVolumes(ctx, 1))), []store.KeptVolume{first})
}

// R-YXMK-PYX7 R-7NJT-G0Y8 R-7ORP-TSOX
func TestDeleteKeptVolumeDefersFailsRetriesAndFinishes(t *testing.T) {
	_, s, now := fixture(t)
	v := addKept(t, s, "vol-delete")
	wanted := identity.Caller{UserID: strings.Repeat("u", valid.MaxUserIDBytes), Email: "second@example.test", RequestID: "delete request"}
	for _, uid := range []string{"", strings.Repeat("u", valid.MaxUserIDBytes+1)} {
		observe(s.DeleteKeptVolume(ctx, v.Volume, identity.Caller{UserID: uid})).assertZero(t, store.ErrUnreachable)
	}
	for id, want := range map[string]error{v.Volume: store.ErrConflict, "missing": store.ErrNotFound} {
		checkErr(t, s.DeferVolumeDelete(ctx, id, *now, 0), want)
		checkErr(t, s.FinishVolumeDelete(ctx, id), want)
		checkErr(t, s.FailVolumeDelete(ctx, id, "refusal"), want)
	}
	eq(t, value(t, result(s.KeptVolume(ctx, v.Volume))), v)
	*now = now.Add(time.Minute)
	want := v
	want.State, want.Caller, want.NextTry = store.VolumeDeleting, wanted, now.UTC().Truncate(time.Second)
	eq(t, value(t, result(s.DeleteKeptVolume(ctx, v.Volume, wanted))), want)
	observe(s.DeleteKeptVolume(ctx, v.Volume, caller)).assertZero(t, store.ErrVolumeDeleting)
	eq(t, value(t, result(s.ListKeptVolumes(ctx, time.Time{}, "", 1))), []store.KeptVolume{want})
	eq(t, value(t, result(s.DueVolumeDeletes(ctx, want.NextTry, "", 1))), []store.KeptVolume{want})

	deferred := now.Add(time.Minute)
	checkErr(t, s.DeferVolumeDelete(ctx, v.Volume, deferred, 2), nil)
	want.NextTry, want.Attempts = deferred.UTC().Truncate(time.Second), 2
	eq(t, value(t, result(s.KeptVolume(ctx, v.Volume))), want)
	checkErr(t, s.FailVolumeDelete(ctx, v.Volume, "provider refused deletion"), nil)
	want.State, want.Reason = store.VolumeKept, "provider refused deletion"
	eq(t, value(t, result(s.KeptVolume(ctx, v.Volume))), want)
	eq(t, value(t, result(s.ListKeptVolumes(ctx, time.Time{}, "", 1))), []store.KeptVolume{want})
	eq(t, value(t, result(s.NextTry(ctx))), time.Time{})
	checkErr(t, s.DeferVolumeDelete(ctx, v.Volume, deferred, 3), store.ErrConflict)
	eq(t, value(t, result(s.KeptVolume(ctx, v.Volume))), want)

	*now = now.Add(time.Minute)
	want.State, want.Reason, want.Attempts, want.NextTry, want.Caller = store.VolumeDeleting, "", 0, now.UTC().Truncate(time.Second), caller
	eq(t, value(t, result(s.DeleteKeptVolume(ctx, v.Volume, caller))), want)
	checkErr(t, s.FinishVolumeDelete(ctx, v.Volume), nil)
	checkErr(t, s.FinishVolumeDelete(ctx, v.Volume), store.ErrConflict)
	checkErr(t, s.FailVolumeDelete(ctx, v.Volume, "stale attempt"), store.ErrConflict)
	checkErr(t, s.DeferVolumeDelete(ctx, v.Volume, deferred, 3), store.ErrConflict)
	observe(s.KeptVolume(ctx, v.Volume)).assertZero(t, store.ErrNotFound)
	observe(s.DeleteKeptVolume(ctx, v.Volume, caller)).assertZero(t, store.ErrNotFound)
	eq(t, value(t, result(s.NextTry(ctx))), time.Time{})
	eq(t, value(t, result(s.AdoptKept(ctx, 1))), 0)
	eq(t, value(t, result(s.ListKeptVolumes(ctx, time.Time{}, "", 1))), []store.KeptVolume{})
}

// R-7MBX-297J
func TestDueVolumeDeletesPageAndSurviveRestart(t *testing.T) {
	now := baseTime
	cfg := store.Config{Now: func() time.Time { return now }, Rand: new(counterRandom)}
	path := filepath.Join(t.TempDir(), "record.db")
	migrations := accounts.Migrations()
	d, s := open(t, path, migrations, cfg)
	b := addKept(t, s, "vol-b")
	a := addKept(t, s, "vol-a")
	c := addKept(t, s, "vol-c")
	addKept(t, s, "vol-kept")
	eq(t, value(t, result(s.NextTry(ctx))), time.Time{})
	a = value(t, result(s.DeleteKeptVolume(ctx, a.Volume, caller)))
	eq(t, value(t, result(s.NextTry(ctx))), a.NextTry)
	other := identity.Caller{UserID: "different", Email: "different@example.test", RequestID: "different-request"}
	b = value(t, result(s.DeleteKeptVolume(ctx, b.Volume, other)))
	c = value(t, result(s.DeleteKeptVolume(ctx, c.Volume, caller)))
	later := now.Add(time.Minute).Truncate(time.Second).UTC()
	checkErr(t, s.DeferVolumeDelete(ctx, c.Volume, later, 1), nil)
	c.NextTry, c.Attempts = later, 1
	_, op := create(t, s)
	earlier := now.Add(-time.Second).Truncate(time.Second).UTC()
	checkErr(t, s.Defer(ctx, op.ID, earlier, 1), nil)
	eq(t, value(t, result(s.NextTry(ctx))), earlier)
	checkErr(t, s.Defer(ctx, op.ID, later.Add(time.Minute), 2), nil)
	eq(t, value(t, result(s.NextTry(ctx))), a.NextTry)
	checkErr(t, d.Close(), nil)

	_, s = open(t, path, migrations, cfg)
	eq(t, value(t, result(s.DueVolumeDeletes(ctx, a.NextTry, "", 1))), []store.KeptVolume{a})
	eq(t, value(t, result(s.DueVolumeDeletes(ctx, a.NextTry, a.Volume, 2))), []store.KeptVolume{b})
	eq(t, value(t, result(s.DueVolumeDeletes(ctx, a.NextTry, b.Volume, 2))), []store.KeptVolume{})
	eq(t, value(t, result(s.DueVolumeDeletes(ctx, later, b.Volume, 1))), []store.KeptVolume{c})
	eq(t, value(t, result(s.DueVolumeDeletes(ctx, later, "", 3))), []store.KeptVolume{a, b, c})
}
