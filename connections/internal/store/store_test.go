package store_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/store"
)

var ctx = context.Background()

func open(t *testing.T, path string, now func() time.Time, r io.Reader) (*store.Store, *db.DB) {
	t.Helper()
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: connections.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	return store.New(d, store.Config{Now: now, Rand: r}), d
}
func fixture(now time.Time) store.Connection {
	return store.Connection{ID: "ignored", UserID: "user", UserEmail: "caller@example.test", Provider: "alpha", AccountEmail: "account@example.test", AccountSubject: "subject", RefreshToken: "refresh", AccessToken: "access", AccessExpires: now.Add(time.Hour), Scopes: []string{"two", "one"}, Status: store.StatusBroken, Created: now.Add(-time.Hour), Modified: now.Add(-time.Hour)}
}
func equal(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v want %#v", a, b)
	}
}

// R-8OJF-RAMO R-8PRC-52DD R-8S74-WLUR R-8TF1-ADLG R-8UMX-O5C5 R-8VUU-1X2U
func TestDeclarations(t *testing.T) {
	if store.IDPrefix != "con_" || store.StatusOK != "ok" || store.StatusBroken != "broken" || store.Unreachable == "" {
		t.Fatal("constants")
	}
	errs := []error{store.ErrNotFound, store.ErrExpired, store.ErrForeign, store.ErrUnreachable}
	for i, a := range errs {
		if a == nil {
			t.Fatal("nil")
		}
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Fatal("errors overlap")
			}
		}
	}
}

// R-8X2Q-FOTJ R-8ZIJ-78AX R-91YB-YRSB R-9368-CJJ0 R-94E4-QB9P R-95M1-430E R-Y1HL-ZSNZ R-981T-VMHS R-9J0X-BK61
func TestConnectionLifecycle(t *testing.T) {
	now := time.Date(2031, 2, 3, 4, 5, 6, 789, time.FixedZone("test", 3600))
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "state", "connections.db")
	s, d := open(t, path, clock, bytes.NewReader(bytes.Repeat([]byte{0x5a, 0x6b}, 32)))
	draft := fixture(now)
	a, created, e := s.Upsert(ctx, draft)
	if e != nil || !created {
		t.Fatal(created, e)
	}
	want := draft
	want.ID = "con_5a6b5a6b5a6b5a6b"
	want.Created = now.UTC().Truncate(time.Second)
	want.Modified = want.Created
	want.AccessExpires = draft.AccessExpires.UTC().Truncate(time.Second)
	want.Status = store.StatusOK
	equal(t, a, want)
	p := store.Pending{State: "persisted", Verifier: "verifier", UserID: "user", UserEmail: "email", Provider: "alpha", Expires: now.Add(time.Hour)}
	if e = s.PutPending(ctx, p); e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, clock, bytes.NewReader(append(bytes.Repeat([]byte{0x6b}, 8), bytes.Repeat([]byte{0x7c}, 8)...)))
	got, e := s.Get(ctx, "user", "alpha")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, a)
	pd, e := s.TakePending(ctx, p.State, p.UserID, p.Provider)
	if e != nil {
		t.Fatal(e)
	}
	p.Expires = p.Expires.UTC().Truncate(time.Second)
	equal(t, pd, p)
	now = now.Add(time.Minute)
	broken, e := s.MarkBroken(ctx, "user", "alpha")
	if e != nil {
		t.Fatal(e)
	}
	want.Status = store.StatusBroken
	want.Modified = now.UTC().Truncate(time.Second)
	equal(t, broken, want)
	check, err := s.Get(ctx, "user", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, check, broken)
	access := store.Access{Token: "new", Expires: now.Add(time.Hour)}
	updated, e := s.SetAccess(ctx, "user", "alpha", access)
	if e != nil {
		t.Fatal(e)
	}
	want.AccessToken = access.Token
	want.AccessExpires = access.Expires.UTC().Truncate(time.Second)
	equal(t, updated, want)
	check, err = s.Get(ctx, "user", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, check, updated)
	access.Refresh = "rotated"
	access.Scopes = []string{}
	updated, e = s.SetAccess(ctx, "user", "alpha", access)
	if e != nil {
		t.Fatal(e)
	}
	want.RefreshToken = access.Refresh
	want.Scopes = []string{}
	equal(t, updated, want)
	check, err = s.Get(ctx, "user", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, check, updated)
	draft.UserEmail = "replacement"
	replacement, made, e := s.Upsert(ctx, draft)
	if e != nil || made {
		t.Fatal(made, e)
	}
	want = draft
	want.ID = a.ID
	want.Created = a.Created
	want.Modified = now.UTC().Truncate(time.Second)
	want.Status = store.StatusOK
	want.AccessExpires = draft.AccessExpires.UTC().Truncate(time.Second)
	equal(t, replacement, want)
	for _, user := range []string{"other", "missing"} {
		if _, e = s.Get(ctx, user, "alpha"); !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	other := draft
	other.UserID = "other"
	other.Provider = "beta"
	b, _, e := s.Upsert(ctx, other)
	if e != nil {
		t.Fatal(e)
	}
	list, e := s.List(ctx, "user")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, list, []store.Connection{replacement})
	empty, e := s.List(ctx, "absent")
	if e != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, e)
	}
	for _, fn := range []func() (store.Connection, error){func() (store.Connection, error) { return s.SetAccess(ctx, "user", "beta", access) }, func() (store.Connection, error) { return s.MarkBroken(ctx, "user", "beta") }, func() (store.Connection, error) { return s.Delete(ctx, "user", "beta") }} {
		if _, e := fn(); !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	deleted, e := s.Delete(ctx, "user", "alpha")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, deleted, replacement)
	if _, e = s.Get(ctx, "user", "alpha"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	got, e = s.Get(ctx, "other", "beta")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, b)
	fresh, made, e := s.Upsert(ctx, draft)
	if e != nil || !made || fresh.ID == a.ID {
		t.Fatal(fresh, made, e)
	}
}

// R-Y09P-M0XA R-X7K6-H7VR
func TestRandomBounds(t *testing.T) {
	now := time.Unix(1800000000, 0)
	clock := func() time.Time { return now }
	r := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 72))
	s, _ := open(t, filepath.Join(t.TempDir(), "db"), clock, r)
	a, _, e := s.Upsert(ctx, fixture(now))
	if e != nil || a.ID != "con_5a5a5a5a5a5a5a5a" {
		t.Fatal(a, e)
	}
	draft := fixture(now)
	draft.UserID = "second"
	if _, _, e = s.Upsert(ctx, draft); !errors.Is(e, store.ErrUnreachable) || r.Len() != 0 {
		t.Fatal(e, r.Len())
	}
	list, e := s.List(ctx, "second")
	if e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
	r2 := bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 16), bytes.Repeat([]byte{0x6b}, 8)...))
	s2, _ := open(t, filepath.Join(t.TempDir(), "db"), clock, r2)
	if _, _, e = s2.Upsert(ctx, fixture(now)); e != nil {
		t.Fatal(e)
	}
	b, _, e := s2.Upsert(ctx, draft)
	if e != nil || b.ID != "con_6b6b6b6b6b6b6b6b" {
		t.Fatal(b, e)
	}
	s3, _ := open(t, filepath.Join(t.TempDir(), "db"), clock, bytes.NewReader(make([]byte, 8)))
	if _, _, e = s3.Upsert(ctx, fixture(now)); e != nil {
		t.Fatal(e)
	}
	for range 3 {
		if _, _, e = s3.Upsert(ctx, fixture(now)); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e = s3.Upsert(ctx, draft); !errors.Is(e, store.ErrUnreachable) {
		t.Fatal(e)
	}
}

// R-999Q-9E8H R-9AHM-N5Z6 R-9CXF-EPGK R-9FD8-68XY
func TestPendingLifecycle(t *testing.T) {
	now := time.Unix(1800000000, 999)
	s, _ := open(t, filepath.Join(t.TempDir(), "db"), func() time.Time { return now }, bytes.NewReader(make([]byte, 8)))
	p := store.Pending{State: "state", Verifier: "v", UserID: "user", UserEmail: "e", Provider: "alpha", Expires: now.Add(time.Minute)}
	if e := s.PutPending(ctx, p); e != nil {
		t.Fatal(e)
	}
	if _, e := s.TakePending(ctx, p.State, "user", "beta"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.TakePending(ctx, p.State, "other", "alpha"); !errors.Is(e, store.ErrForeign) {
		t.Fatal(e)
	}
	got, e := s.TakePending(ctx, p.State, "user", "alpha")
	if e != nil {
		t.Fatal(e)
	}
	p.Expires = p.Expires.UTC().Truncate(time.Second)
	equal(t, got, p)
	if _, e = s.TakePending(ctx, p.State, "user", "alpha"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if e = s.PutPending(ctx, p); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Minute)
	if _, e = s.TakePending(ctx, p.State, "other", "alpha"); !errors.Is(e, store.ErrExpired) {
		t.Fatal(e)
	}
	if _, e = s.TakePending(ctx, p.State, "user", "alpha"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	p.State = "future"
	p.Expires = now.Add(time.Minute)
	if e = s.PutPending(ctx, p); e != nil {
		t.Fatal(e)
	}
	expired := p
	expired.State = "already"
	expired.Expires = now
	if e = s.PutPending(ctx, expired); e != nil {
		t.Fatal(e)
	}
	if _, e = s.TakePending(ctx, expired.State, "user", "alpha"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if e = s.PurgeExpiredPending(ctx); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Minute)
	if e = s.PurgeExpiredPending(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.TakePending(ctx, p.State, "user", "alpha"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
}

// R-90QF-L01M R-9E5B-SH79 R-9K8T-PBWQ
func TestConcurrentCalls(t *testing.T) {
	now := time.Unix(1800000000, 0)
	s, _ := open(t, filepath.Join(t.TempDir(), "db"), func() time.Time { return now }, bytes.NewReader(make([]byte, 8)))
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	ids := map[string]bool{}
	for range 12 {
		wg.Go(func() {
			a, c, e := s.Upsert(ctx, fixture(now))
			if e != nil {
				t.Error(e)
			}
			mu.Lock()
			defer mu.Unlock()
			if c {
				created++
			}
			ids[a.ID] = true
		})
	}
	wg.Wait()
	if created != 1 || len(ids) != 1 {
		t.Fatal(created, ids)
	}
	p := store.Pending{State: "state", UserID: "user", Provider: "alpha", Expires: now.Add(time.Hour)}
	if e := s.PutPending(ctx, p); e != nil {
		t.Fatal(e)
	}
	success := 0
	for range 12 {
		wg.Go(func() {
			_, e := s.TakePending(ctx, p.State, p.UserID, p.Provider)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				success++
			} else if !errors.Is(e, store.ErrNotFound) {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if success != 1 {
		t.Fatal(success)
	}
}

// R-X8S2-UZMG
func TestFailuresFirst(t *testing.T) {
	now := time.Unix(1800000000, 0)
	s, d := open(t, filepath.Join(t.TempDir(), "db"), func() time.Time { return now }, bytes.NewReader(make([]byte, 16)))
	a, _, e := s.Upsert(ctx, fixture(now))
	if e != nil {
		t.Fatal(e)
	}
	p := store.Pending{State: "state", UserID: "user", Provider: "alpha", Expires: now.Add(time.Hour)}
	if e = s.PutPending(ctx, p); e != nil {
		t.Fatal(e)
	}
	for _, fail := range []bool{false, true} {
		c, cancel := context.WithCancel(ctx)
		if !fail {
			cancel()
		} else {
			d.SetFailing(true)
		}
		calls := []func() error{func() error { _, e := s.Get(c, "absent", "alpha"); return e }, func() error { _, e := s.List(c, "user"); return e }, func() error { _, _, e := s.Upsert(c, fixture(now)); return e }, func() error { _, e := s.SetAccess(c, "absent", "alpha", store.Access{}); return e }, func() error { _, e := s.MarkBroken(c, "absent", "alpha"); return e }, func() error { _, e := s.Delete(c, "user", "alpha"); return e }, func() error { return s.PutPending(c, p) }, func() error { _, e := s.TakePending(c, "state", "other", "alpha"); return e }, func() error { return s.PurgeExpiredPending(c) }}
		for _, fn := range calls {
			e := fn()
			if !errors.Is(e, store.ErrUnreachable) || errors.Is(e, store.ErrNotFound) || errors.Is(e, store.ErrForeign) || errors.Is(e, store.ErrExpired) {
				t.Fatal(e)
			}
		}
		cancel()
		d.SetFailing(false)
	}
	got, e := s.Get(ctx, "user", "alpha")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, a)
	if _, e = s.TakePending(ctx, "state", "user", "alpha"); e != nil {
		t.Fatal(e)
	}
}

// R-9368-CJJ0 R-9FD8-68XY R-9K8T-PBWQ
func TestOrderingPurgeAndConcurrentMethods(t *testing.T) {
	now := time.Unix(1800000000, 0)
	r := &guardReader{}
	s, _ := open(t, filepath.Join(t.TempDir(), "db"), func() time.Time { return now }, r)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, name := range []string{"zeta", "alpha", "middle"} {
		name := name
		wg.Go(func() {
			<-start
			draft := fixture(now)
			draft.Provider = name
			if _, _, e := s.Upsert(ctx, draft); e != nil {
				t.Error(e)
			}
		})
	}
	close(start)
	wg.Wait()
	foreign := fixture(now)
	foreign.UserID = "foreign"
	if _, _, e := s.Upsert(ctx, foreign); e != nil {
		t.Fatal(e)
	}
	list, e := s.List(ctx, "user")
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for _, c := range list {
		names = append(names, c.Provider)
	}
	equal(t, names, []string{"alpha", "middle", "zeta"})
	p := store.Pending{State: "future", UserID: "user", Provider: "alpha", Expires: now.Add(time.Hour)}
	if e = s.PutPending(ctx, p); e != nil {
		t.Fatal(e)
	}
	expired := p
	expired.State = "expiring"
	expired.Expires = now.Add(time.Minute)
	if e = s.PutPending(ctx, expired); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Minute)
	if e = s.PurgeExpiredPending(ctx); e != nil {
		t.Fatal(e)
	}
	unchanged, e := s.List(ctx, "user")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, unchanged, list)
	if _, e = s.TakePending(ctx, "future", "user", "alpha"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.TakePending(ctx, "expiring", "user", "alpha"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	start = make(chan struct{})
	for i := range 20 {
		i := i
		wg.Go(func() {
			<-start
			var callErr error
			switch i % 7 {
			case 0:
				_, callErr = s.Get(ctx, "user", "middle")
			case 1:
				_, callErr = s.List(ctx, "user")
			case 2:
				_, callErr = s.SetAccess(ctx, "user", "middle", store.Access{Token: "token", Expires: now})
			case 3:
				_, callErr = s.MarkBroken(ctx, "user", "middle")
			case 4:
				callErr = s.PutPending(ctx, store.Pending{State: fmt.Sprint(i), UserID: "user", Provider: "alpha", Expires: now.Add(time.Hour)})
			case 5:
				callErr = s.PurgeExpiredPending(ctx)
			case 6:
				_, callErr = s.Delete(ctx, "nobody", "alpha")
				if errors.Is(callErr, store.ErrNotFound) {
					callErr = nil
				}
			}
			if callErr != nil {
				t.Error(callErr)
			}
		})
	}
	close(start)
	wg.Wait()
	if r.overlap.Load() {
		t.Fatal("random source read concurrently")
	}
}

type guardReader struct {
	busy    atomic.Bool
	overlap atomic.Bool
	n       byte
}

func (r *guardReader) Read(p []byte) (int, error) {
	if !r.busy.CompareAndSwap(false, true) {
		r.overlap.Store(true)
	}
	defer r.busy.Store(false)
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}
