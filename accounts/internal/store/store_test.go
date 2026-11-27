package store_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

var ctx = context.Background()
var baseTime = time.Date(2026, 10, 29, 9, 3, 7, 900000000, time.FixedZone("chosen", -5*3600))
var caller = identity.Caller{UserID: "operator", Email: "operator@example.test", RequestID: "request"}
var spec = store.HostSpec{Type: "t4g.medium", RootGB: 20, DataGB: 50}
var firstKey = store.Key{Type: "ssh-ed25519", Body: "base-key", Line: "ssh-ed25519 base-key initial"}

type counterRandom uint64

func (r *counterRandom) Read(p []byte) (int, error) {
	*r++
	binary.BigEndian.PutUint64(p, uint64(*r))
	return 8, nil
}

func open(t *testing.T, path string, migrations fs.FS, cfg store.Config) (*db.DB, *store.Store) {
	t.Helper()
	d := value(t, result(db.Open(ctx, db.Config{Path: path, Migrations: migrations})))
	t.Cleanup(func() { checkErr(t, d.Close(), nil) })
	return d, store.New(d, cfg)
}

func fixture(t *testing.T) (*db.DB, *store.Store, *time.Time) {
	t.Helper()
	now := baseTime
	d, s := open(t, filepath.Join(t.TempDir(), "record.db"), accounts.Migrations(),
		store.Config{Now: func() time.Time { return now }, Rand: new(counterRandom)})
	return d, s, &now
}

type checked[T any] struct {
	value T
	err   error
}

func result[T any](v T, err error) checked[T] { return checked[T]{v, err} }

func value[T any](t *testing.T, v checked[T]) T {
	t.Helper()
	checkErr(t, v.err, nil)
	return v.value
}

func checkErr(t *testing.T, got, want error) {
	t.Helper()
	if want == nil && got != nil || want != nil && !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func create(t *testing.T, s *store.Store) (store.Host, store.Operation) {
	t.Helper()
	h, op, err := s.CreateHost(ctx, spec, caller)
	checkErr(t, err, nil)
	return h, op
}

func start(t *testing.T, s *store.Store, name, kind string, target store.Target) (store.Host, store.Operation) {
	t.Helper()
	h, op, err := s.StartHostOp(ctx, name, kind, target, caller)
	checkErr(t, err, nil)
	return h, op
}

func deleteHost(t *testing.T, s *store.Store, name string, keepData bool) (store.Host, store.Operation) {
	t.Helper()
	h, op, err := s.DeleteHost(ctx, name, keepData, true, caller)
	checkErr(t, err, nil)
	return h, op
}

func place(t *testing.T, s *store.Store, slug, host string) (store.Account, store.Operation) {
	t.Helper()
	a, op, err := s.PlaceAccount(ctx, slug, host, caller)
	checkErr(t, err, nil)
	return a, op
}

func remove(t *testing.T, s *store.Store, slug string) (store.Account, store.Operation) {
	t.Helper()
	a, op, _, err := s.RemoveAccount(ctx, slug, caller)
	checkErr(t, err, nil)
	return a, op
}

// R-GMXI-9FH1 R-GPDB-0YYF R-GQL7-EQP4 R-H6FW-DRC5
// R-OTU8-67AN R-759B-PGTT R-HG73-FX9P R-HHEZ-TP0E R-HJUS-L8HS
func TestHostsReopenAndPage(t *testing.T) {
	calls := 0
	var random io.Reader = new(counterRandom)
	cfg := store.Config{Now: func() time.Time { calls++; return baseTime }, Rand: random}
	path := filepath.Join(t.TempDir(), "record.db")
	d, s := open(t, path, accounts.Migrations(), cfg)
	h, op := create(t, s)
	eq(t, calls, 1)
	stamp := baseTime.UTC().Truncate(time.Second)
	wantHost := store.Host{Name: "host01", Seq: 1, State: store.HostProvisioning, Type: spec.Type, RootGB: spec.RootGB, DataGB: spec.DataGB,
		Version: 1, Operation: "op_0000000000000001", Created: stamp, CreatedBy: caller.UserID, Allowlist: []string{}}
	eq(t, h, wantHost)
	eq(t, op, store.Operation{ID: h.Operation, Host: h.Name, Kind: store.OpCreate, Status: store.OpRunning,
		Generation: 1, Started: stamp, StepStarted: stamp, NextTry: stamp, Caller: caller})
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	checkErr(t, d.Close(), nil)
	_, s = open(t, path, accounts.Migrations(), cfg)
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)

	checkErr(t, s.Finish(ctx, op.ID, store.Update{InstanceID: "instance-one"}), nil)
	h = value(t, result(s.HostByInstance(ctx, "instance-one")))
	eq(t, h.InstanceID, "instance-one")
	eq(t, h.Name, "host01")
	_, err := s.HostByInstance(ctx, "")
	checkErr(t, err, store.ErrNotFound)
	for i := 2; i <= 101; i++ {
		next, op := create(t, s)
		eq(t, next.Seq, int64(i))
		if i == 3 {
			checkErr(t, s.Finish(ctx, op.ID, store.Update{InstanceID: "deleted-instance"}), nil)
			_, deletion := deleteHost(t, s, next.Name, true)
			checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
			_, err := s.Host(ctx, next.Name)
			checkErr(t, err, store.ErrNotFound)
			_, err = s.HostByInstance(ctx, "deleted-instance")
			checkErr(t, err, store.ErrNotFound)
		}
		if i == 4 {
			eq(t, next.Name, "host03")
		}
		if i == 101 {
			eq(t, next.Name, "host100")
		}
	}
	asSeq := func(hs []store.Host) []int64 {
		out := []int64{}
		for _, h := range hs {
			out = append(out, h.Seq)
		}
		return out
	}
	eq(t, asSeq(value(t, result(s.ListHosts(ctx, 1, 2)))), []int64{2, 4})
	eq(t, asSeq(value(t, result(s.ListHostsBefore(ctx, 5, 2)))), []int64{2, 4})
	eq(t, asSeq(value(t, result(s.LastHosts(ctx, 2)))), []int64{100, 101})
}

// R-HG73-FX9P
func TestOperationIDCollisionsRollBack(t *testing.T) {
	stamp := func() time.Time { return baseTime }
	path := filepath.Join(t.TempDir(), "record.db")
	buf := append(make([]byte, 8), bytes.Repeat([]byte{1}, 8)...)
	buf = append(buf, bytes.Repeat([]byte{1}, 64)...)
	_, s := open(t, path, accounts.Migrations(), store.Config{Now: stamp, Rand: bytes.NewReader(buf)})
	_, first := create(t, s)
	// The next store draws first's identifier, then a fresh identifier.
	d, s := open(t, filepath.Join(t.TempDir(), "collision.db"), accounts.Migrations(),
		store.Config{Now: stamp, Rand: bytes.NewReader(append(make([]byte, 8), buf...))})
	_, op := create(t, s)
	eq(t, op.ID, first.ID)
	h, op := create(t, s)
	eq(t, h.Seq, int64(2))
	eq(t, op.ID, "op_0101010101010101")
	for range 2 {
		got, operation, err := s.CreateHost(ctx, spec, caller)
		checkErr(t, err, store.ErrUnreachable)
		eq(t, got, store.Host{})
		eq(t, operation, store.Operation{})
	}
	s = store.New(d, store.Config{Now: stamp, Rand: new(counterRandom)})
	h, _ = create(t, s)
	eq(t, h.Seq, int64(3))
}

// R-1WCG-1391 R-BYG6-N24Z R-RJ2D-W3MH R-6Z5T-SM4C R-5HSB-KP8M R-GWWF-0TU4 R-PJ2H-5UBP
func TestErrorClasses(t *testing.T) {
	base := []error{store.ErrUnreachable, store.ErrNotFound, store.ErrTaken, store.ErrConflict}
	groups := [][]error{base, {store.ErrNotInAccount, store.ErrKeyNotHeld, store.ErrNoDatabase, store.ErrNoSnapshot, store.ErrNoUser}, {store.ErrBusy, store.ErrHostFailed,
		store.ErrNotFailed, store.ErrNotRunning, store.ErrNotStopped, store.ErrHasAccounts, store.ErrHostFull,
		store.ErrHostGone, store.ErrNothingToChange, store.ErrShrinks, store.ErrGrowLimit, store.ErrHasUsers,
		store.ErrAccountRemoving, store.ErrAccountFull, store.ErrTooManyKeys, store.ErrNotLocked, store.ErrUserRemoving,
		store.ErrTooManyUnbans, store.ErrAllowlistFull, store.ErrTooManyInvites, store.ErrLastKey, store.ErrNotActive, store.ErrNotAdmin, store.ErrSelf, store.ErrVolumeDeleting,
		store.ErrNotClosed, store.ErrNoDataVolume, store.ErrAccountNotActive, store.ErrTooManyRestores,
		store.ErrAccountExists, store.ErrBackupsDeleting, store.ErrNoAddress, store.ErrNotRestorable, store.ErrWithheld}}
	seen := map[error]bool{}
	for i, group := range groups {
		for pos, e := range group {
			if e == nil || seen[e] {
				t.Fatalf("duplicate or empty error %v", e)
			}
			seen[e] = true
			for classIndex, class := range base {
				want := i == 0 && pos == classIndex || i == 1 && classIndex == 1 || i == 2 && classIndex == 3
				eq(t, errors.Is(e, class), want)
			}
		}
	}
}

// R-0K9R-765T R-GZ4I-34VZ R-H2S7-8G42 R-6VI4-NAW9 R-4NAU-0FP9 R-4M2X-MNYK R-5814-IJB2 R-5990-WB1R
func TestPublishedValues(t *testing.T) {
	eq(t, []string{store.OnDeleteKeep, store.OnDeleteDelete, store.VolumeKept, store.VolumeDeleting, store.VolumeDeleted},
		[]string{"keep", "delete", "kept", "deleting", "deleted"})
	eq(t, []string{store.HostProvisioning, store.HostRunning, store.HostStopping, store.HostStopped, store.HostStarting,
		store.HostResizing, store.HostDeleting, store.HostFailed, store.HostDeleted,
		store.HostClosing, store.HostOpening, store.HostReplacing, store.HostRestoring},
		[]string{"provisioning", "running", "stopping", "stopped", "starting", "resizing", "deleting", "failed", "deleted", "closing", "opening", "replacing", "restoring"})
	eq(t, []string{store.AccountPending, store.AccountActive, store.AccountFailed, store.AccountRemoving, store.UserActive,
		store.UserLocked, store.UserRemoving}, []string{"pending", "active", "failed", "removing", "active", "locked", "removing"})
	eq(t, []string{store.HealthSync, store.HealthPending, store.HealthError, store.HealthSilent, store.HealthNone, store.AlarmNone,
		store.AlarmSilent, store.AlarmError}, []string{"sync", "pending", "error", "silent", "none", "", "silent", "error"})
	eq(t, []string{store.OpRunning, store.OpFailed, store.OpDone, store.OpCancelled, store.OpCreate, store.OpStop, store.OpStart,
		store.OpResize, store.OpDelete, store.OpPlace, store.OpUnplace, store.OpClose, store.OpOpen, store.OpReplace, store.OpRestore},
		[]string{"running", "failed", "done", "cancelled", "create", "stop", "start", "resize", "delete", "place", "unplace", "close", "open", "replace", "restore"})
	eq(t, []string{store.ReplaceVolume, store.ReplaceBackup, store.CauseDeleted, store.CauseReplaced},
		[]string{"volume", "backup", "deleted", "replaced"})
	eq(t, []string{store.BackupHealthy, store.BackupStale, store.BackupError, store.BackupNone, store.BackupsRemoved,
		store.DeleteBackupsDeleting, store.DeleteBackupsFailed}, []string{"healthy", "stale", "error", "none", "removed", "deleting", "failed"})
	eq(t, []time.Duration{store.SilentAfter, store.GrowWindow}, []time.Duration{5 * time.Minute, 24 * time.Hour})
	eq(t, []int{store.FirstID, store.BatchSize, store.MaxGrows}, []int{20000, 50, 4})
}
