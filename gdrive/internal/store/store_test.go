package store_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
)

var background = context.Background()

type fixture struct {
	t    *testing.T
	d    *db.DB
	st   *store.Store
	path string
	now  time.Time
	seq  byte
}
type sequence struct{ n byte }

func (r *sequence) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}
func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, path: filepath.Join(t.TempDir(), "transfers.db"), now: time.Date(2026, 5, 3, 4, 5, 6, 123, time.FixedZone("offset", 3600))}
	f.open()
	t.Cleanup(func() {
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *fixture) open() {
	f.t.Helper()
	d, err := db.Open(background, db.Config{Path: f.path, Migrations: gdrive.Migrations(), Now: func() time.Time { return f.now }})
	if err != nil {
		f.t.Fatal(err)
	}
	f.d = d
	f.st = store.New(d, store.Config{Now: func() time.Time { return f.now }, Rand: &sequence{n: f.seq}})
	f.seq += 64
}
func (f *fixture) reopen() {
	f.t.Helper()
	if err := f.d.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.open()
}
func (f *fixture) add(status, user string) store.Transfer {
	f.t.Helper()
	v, err := f.st.Add(background, store.Transfer{Direction: store.DirectionPull, Status: status, Path: "reports/output", UserID: user, File: "drive-file", Name: "report", Kind: "file", ContentType: "text/plain", Format: "format", UserEmail: "person@example.test", RequestID: "request"})
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}
func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func requireError(t *testing.T, got error, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}
func (f *fixture) check(v store.Transfer) {
	f.t.Helper()
	got, err := f.st.Get(background, v.UserID, v.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	equal(f.t, got, v)
}
func reasons() []string {
	return []string{store.ReasonNotFound, store.ReasonForbidden, store.ReasonTooLarge, store.ReasonReconnect, store.ReasonProvider, store.ReasonArtifacts, store.ReasonInterrupted}
}
func invariant(t *testing.T, v store.Transfer) {
	t.Helper()
	if v.Direction != store.DirectionPull && v.Direction != store.DirectionPush {
		t.Fatal(v)
	}
	if v.Started.Location() != time.UTC || v.Started.Nanosecond() != 0 {
		t.Fatal(v)
	}
	switch v.Status {
	case store.StatusQueued, store.StatusRunning:
		if !v.Finished.IsZero() {
			t.Fatal(v)
		}
	case store.StatusDone, store.StatusFailed:
		if v.Finished.IsZero() || v.Finished.Location() != time.UTC || v.Finished.Nanosecond() != 0 {
			t.Fatal(v)
		}
	default:
		t.Fatal(v)
	}
	if v.Status == store.StatusFailed {
		found := false
		for _, r := range reasons() {
			found = found || r == v.Reason
		}
		if !found {
			t.Fatal(v)
		}
	} else if v.Reason != "" {
		t.Fatal(v)
	}
	if v.Status != store.StatusDone && (v.URL != "" || v.Link != "" || v.ArtifactID != "") {
		t.Fatal(v)
	}
}

// R-RXC7-L5N2 R-6MY4-PTMR R-S3FP-I0CJ R-SWPA-OI57
func TestEmptyAndAPI(t *testing.T) {
	f := setup(t)
	_ = store.New(f.d, store.Config{})
	for _, u := range []string{"", "alice", "bob"} {
		l, err := f.st.List(background, u)
		if err != nil || l == nil || len(l) != 0 {
			t.Fatalf("list %v %v", l, err)
		}
		v, err := f.st.Get(background, u, "missing")
		equal(t, v, store.Transfer{})
		requireError(t, err, store.ErrNotFound)
	}
	l, err := f.st.Unfinished(background)
	if err != nil || l == nil || len(l) != 0 {
		t.Fatalf("unfinished %v %v", l, err)
	}
	l, err = f.st.Prune(background, f.now, 1, 1)
	if err != nil || l == nil || len(l) != 0 {
		t.Fatal(l, err)
	}
}

// R-RZS0-CP4G R-S0ZW-QGV5 R-S27T-48LU R-S4NL-VS38
func TestConstantsAndIDs(t *testing.T) {
	equal(t, store.IDPrefix, "xfr_")
	equal(t, []string{store.DirectionPull, store.DirectionPush, store.StatusQueued, store.StatusRunning, store.StatusDone, store.StatusFailed}, []string{"pull", "push", "queued", "running", "done", "failed"})
	equal(t, reasons(), []string{"not_found", "forbidden", "too_large", "reconnect", "provider", "artifacts", "interrupted"})
	for i, a := range []error{store.ErrNotFound, store.ErrEnded, store.ErrUnreachable} {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range []error{store.ErrNotFound, store.ErrEnded, store.ErrUnreachable} {
			if i != j && errors.Is(a, b) {
				t.Fatal("wrapped sentinel")
			}
		}
	}
	if store.Unreachable == "" || strings.ContainsAny(store.Unreachable, "\r\n") {
		t.Fatal("invalid copy constant")
	}
	for _, v := range []string{"xfr_0123456789abcdef", "xfr_0000000000000000", "xfr_ffffffffffffffff"} {
		if !store.ValidID(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", "xfr_0123456789ABCDEF", "xfr_0123456789abcde", "xfr_0123456789abcdef0", "run_0123456789abcdef", "xfr_0123456789abcdeg", "xfr_0123456789abcdé"} {
		if store.ValidID(v) {
			t.Fatal(v)
		}
	}
}

// R-S8BB-13BB R-S73E-NBKM R-SU9H-WYNT
func TestAddNormalizationAndPersistence(t *testing.T) {
	f := setup(t)
	for _, direction := range []string{store.DirectionPull, store.DirectionPush} {
		for _, status := range []string{store.StatusQueued, store.StatusRunning} {
			draft := store.Transfer{ID: "ignored", Direction: direction, Status: status, Reason: "ignored", File: "file", Name: "name", Kind: "kind", Path: "path", ContentType: "ct", Format: "fmt", Bytes: -7, Started: f.now.Add(-time.Hour), Finished: f.now, URL: "url", Link: "link", ArtifactID: "artifact", UserID: "alice", UserEmail: "email", RequestID: "request"}
			v, err := f.st.Add(background, draft)
			if err != nil {
				t.Fatal(err)
			}
			if !store.ValidID(v.ID) {
				t.Fatal(v)
			}
			want := draft
			want.ID = v.ID
			want.Reason = ""
			want.Bytes = 0
			want.Started = f.now.UTC().Truncate(time.Second)
			want.Finished = time.Time{}
			want.URL = ""
			want.Link = ""
			want.ArtifactID = ""
			equal(t, v, want)
			invariant(t, v)
			f.check(v)
			f.reopen()
			f.check(v)
		}
	}
}

// R-S9J7-EV20
func TestRandomCandidates(t *testing.T) {
	f := setup(t)
	draft := store.Transfer{Direction: store.DirectionPull, Status: store.StatusQueued, Path: "path", UserID: "alice"}
	r := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))
	s := store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
	a, err := s.Add(background, draft)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, a.ID, "xfr_5a5a5a5a5a5a5a5a")
	equal(t, r.Len(), 0)
	r = bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x6b}, 8)...))
	s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
	b, err := s.Add(background, draft)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, b.ID, "xfr_6b6b6b6b6b6b6b6b")
	equal(t, r.Len(), 0)
	for _, data := range [][]byte{bytes.Repeat([]byte{0x5a}, 65), {1, 2, 3}} {
		r = bytes.NewReader(data)
		s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
		v, err := s.Add(background, draft)
		equal(t, v, store.Transfer{})
		requireError(t, err, store.ErrUnreachable)
		if len(data) == 65 {
			equal(t, r.Len(), 1)
		}
		f.check(a)
		f.check(b)
	}
	l, err := f.st.List(background, "alice")
	if err != nil || len(l) != 2 {
		t.Fatal(l, err)
	}
}

// R-SAR3-SMSP
func TestInvalidDrafts(t *testing.T) {
	f := setup(t)
	base := store.Transfer{Direction: store.DirectionPull, Status: store.StatusQueued, UserID: "alice", Path: "path"}
	for _, mutate := range []func(*store.Transfer){func(v *store.Transfer) { v.Direction = "" }, func(v *store.Transfer) { v.Direction = "other" }, func(v *store.Transfer) { v.Status = store.StatusDone }, func(v *store.Transfer) { v.Status = store.StatusFailed }, func(v *store.Transfer) { v.Status = "" }, func(v *store.Transfer) { v.UserID = "" }, func(v *store.Transfer) { v.Path = "" }} {
		v := base
		mutate(&v)
		r := bytes.NewReader(bytes.Repeat([]byte{1}, 8))
		s := store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
		got, err := s.Add(background, v)
		equal(t, got, store.Transfer{})
		requireError(t, err, store.ErrUnreachable)
		equal(t, r.Len(), 8)
	}
	l, err := f.st.Unfinished(background)
	if err != nil || len(l) != 0 {
		t.Fatal(l, err)
	}
}

// R-SBZ0-6EJE R-SEES-XY0S R-6O61-3LDG R-SU9H-WYNT
func TestTransitionsPersist(t *testing.T) {
	f := setup(t)
	other := f.add(store.StatusRunning, "bob")
	v := f.add(store.StatusQueued, "alice")
	started := v.Started
	f.now = f.now.Add(time.Hour)
	got, err := f.st.Start(background, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	v.Status = store.StatusRunning
	equal(t, got, v)
	f.reopen()
	f.check(v)
	f.check(other)
	for _, n := range []int64{11, 0, math.MaxInt64} {
		got, err = f.st.SetBytes(background, v.ID, n)
		if err != nil {
			t.Fatal(err)
		}
		v.Bytes = n
		equal(t, got, v)
		f.reopen()
		f.check(v)
		f.check(other)
	}
	e := store.Ending{Status: store.StatusDone, Bytes: 14, File: "new-file", Name: "new-name", URL: "artifact-url", Link: "drive-link", ArtifactID: "artifact-id"}
	got, err = f.st.Finish(background, v.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	v.Status = e.Status
	v.Bytes = e.Bytes
	v.File = e.File
	v.Name = e.Name
	v.URL = e.URL
	v.Link = e.Link
	v.ArtifactID = e.ArtifactID
	v.Finished = f.now.UTC().Truncate(time.Second)
	equal(t, got, v)
	equal(t, v.Started, started)
	invariant(t, got)
	f.reopen()
	f.check(v)
	f.check(other)
	for _, status := range []string{store.StatusQueued, store.StatusRunning} {
		for _, r := range reasons() {
			x := f.add(status, "alice")
			e := store.Ending{Status: store.StatusFailed, Reason: r, Bytes: 3, Name: "failed-name"}
			got, err = f.st.Finish(background, x.ID, e)
			if err != nil {
				t.Fatal(err)
			}
			x.Status = e.Status
			x.Reason = r
			x.Bytes = 3
			x.Name = e.Name
			x.Finished = f.now.UTC().Truncate(time.Second)
			equal(t, got, x)
			invariant(t, got)
			f.reopen()
			f.check(x)
		}
	}
	x := f.add(store.StatusRunning, "alice")
	got, err = f.st.Finish(background, x.ID, store.Ending{Status: store.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	x.Status = store.StatusDone
	x.Finished = f.now.UTC().Truncate(time.Second)
	equal(t, got, x)
	f.reopen()
	f.check(x)
}

// R-SD6W-K6A3 R-SFMP-BPRH R-SI2I-398V R-ST1L-J6X4
func TestTransitionErrors(t *testing.T) {
	f := setup(t)
	q := f.add(store.StatusQueued, "alice")
	r := f.add(store.StatusRunning, "alice")
	done, err := f.st.Finish(background, f.add(store.StatusRunning, "alice").ID, store.Ending{Status: store.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := f.st.Finish(background, f.add(store.StatusRunning, "alice").ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonProvider})
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		call func() (store.Transfer, error)
		want error
	}{
		{func() (store.Transfer, error) { return f.st.Start(background, "missing") }, store.ErrNotFound},
		{func() (store.Transfer, error) { return f.st.Start(background, r.ID) }, store.ErrEnded},
		{func() (store.Transfer, error) { return f.st.Start(background, done.ID) }, store.ErrEnded},
		{func() (store.Transfer, error) { return f.st.Start(background, failed.ID) }, store.ErrEnded},
		{func() (store.Transfer, error) { return f.st.SetBytes(background, "missing", 0) }, store.ErrNotFound},
		{func() (store.Transfer, error) { return f.st.SetBytes(background, "missing", -1) }, store.ErrUnreachable},
		{func() (store.Transfer, error) { return f.st.SetBytes(background, q.ID, 1) }, store.ErrEnded},
		{func() (store.Transfer, error) { return f.st.SetBytes(background, done.ID, 1) }, store.ErrEnded},
		{func() (store.Transfer, error) { return f.st.SetBytes(background, failed.ID, 1) }, store.ErrEnded},
		{func() (store.Transfer, error) { return f.st.SetBytes(background, r.ID, -1) }, store.ErrUnreachable},
		{func() (store.Transfer, error) {
			return f.st.Finish(background, "missing", store.Ending{Status: store.StatusDone})
		}, store.ErrNotFound},
		{func() (store.Transfer, error) {
			return f.st.Finish(background, q.ID, store.Ending{Status: store.StatusDone})
		}, store.ErrUnreachable},
		{func() (store.Transfer, error) {
			return f.st.Finish(background, done.ID, store.Ending{Status: store.StatusDone})
		}, store.ErrEnded},
		{func() (store.Transfer, error) {
			return f.st.Finish(background, failed.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonProvider})
		}, store.ErrEnded},
	} {
		v, err := c.call()
		equal(t, v, store.Transfer{})
		requireError(t, err, c.want)
	}
	for _, e := range []store.Ending{{}, {Status: "unknown"}, {Status: store.StatusDone, Reason: store.ReasonProvider}, {Status: store.StatusDone, Bytes: -1}, {Status: store.StatusFailed}, {Status: store.StatusFailed, Reason: "unknown"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, Bytes: -1}, {Status: store.StatusFailed, Reason: store.ReasonProvider, File: "file"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, URL: "url"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, Link: "link"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, ArtifactID: "artifact"}} {
		for _, id := range []string{"missing", q.ID, r.ID, done.ID, failed.ID} {
			v, err := f.st.Finish(background, id, e)
			equal(t, v, store.Transfer{})
			requireError(t, err, store.ErrUnreachable)
		}
	}
	after, err := f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, after, before)
	f.reopen()
	after, err = f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, after, before)
}

// R-SKIA-USQ9 R-SLQ7-8KGY R-SMY3-MC7N R-SXX7-29VW R-SU9H-WYNT
func TestReadScopeOrderingRecovery(t *testing.T) {
	f := setup(t)
	old := f.add(store.StatusRunning, "alice")
	other := f.add(store.StatusQueued, "bob")
	f.now = f.now.Add(time.Second)
	a := f.add(store.StatusQueued, "alice")
	b := f.add(store.StatusRunning, "alice")
	l, err := f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, l, []store.Transfer{b, a, old})
	u, err := f.st.Unfinished(background)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, u, []store.Transfer{old, other, a, b})
	for _, v := range u {
		invariant(t, v)
	}
	for _, user := range []string{"bob", "", "stranger"} {
		v, err := f.st.Get(background, user, old.ID)
		equal(t, v, store.Transfer{})
		requireError(t, err, store.ErrNotFound)
	}
	f.reopen()
	u, err = f.st.Unfinished(background)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, u, []store.Transfer{old, other, a, b})
	ended, err := f.st.Finish(background, old.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted, Bytes: old.Bytes})
	if err != nil {
		t.Fatal(err)
	}
	invariant(t, ended)
	u, err = f.st.Unfinished(background)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, u, []store.Transfer{other, a, b})
	f.reopen()
	u, err = f.st.Unfinished(background)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, u, []store.Transfer{other, a, b})
	f.check(ended)
}

// R-SO60-03YC R-SPDW-DVP1
func TestPrune(t *testing.T) {
	f := setup(t)
	start := f.now
	finish := func(status, user string) store.Transfer {
		v := f.add(status, user)
		ended, err := f.st.Finish(background, v.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
		if err != nil {
			t.Fatal(err)
		}
		return ended
	}
	aOld := finish(store.StatusRunning, "alice")
	aUnfinished := f.add(store.StatusQueued, "alice")
	bOld := finish(store.StatusRunning, "bob")
	bNew := finish(store.StatusRunning, "bob")
	f.now = start.Add(24 * time.Hour)
	boundary := finish(store.StatusRunning, "alice")
	f.now = start.Add(24*time.Hour + time.Second)
	newest := finish(store.StatusRunning, "alice")
	now := start.UTC().Truncate(time.Second).Add(48 * time.Hour)
	beforeA, err := f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	beforeB, err := f.st.List(background, "bob")
	if err != nil {
		t.Fatal(err)
	}
	for _, limits := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		l, err := f.st.Prune(background, now, limits[0], limits[1])
		if l != nil {
			t.Fatal(l)
		}
		requireError(t, err, store.ErrUnreachable)
	}
	for _, limits := range [][2]int64{{math.MaxInt64, 1}, {1, math.MaxInt64}} {
		l, err := f.st.Prune(background, now, limits[0], limits[1])
		if err != nil || l == nil || len(l) != 0 {
			t.Fatal(l, err)
		}
	}
	l, err := f.st.Prune(background, now, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, l, []store.Transfer{aOld, bOld})
	for _, v := range l {
		_, err := f.st.Get(background, v.UserID, v.ID)
		requireError(t, err, store.ErrNotFound)
	}
	for _, v := range []store.Transfer{aUnfinished, bNew, boundary, newest} {
		f.check(v)
	}
	f.reopen()
	for _, v := range l {
		_, err := f.st.Get(background, v.UserID, v.ID)
		requireError(t, err, store.ErrNotFound)
	}
	got, err := f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, beforeA[:3])
	got, err = f.st.List(background, "bob")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, beforeB[:1])
	l, err = f.st.Prune(background, now, 1, 1)
	if err != nil || l == nil || len(l) != 0 {
		t.Fatal(l, err)
	}
}

// R-SRTP-5F6F R-ST1L-J6X4
func TestUnavailablePrecedence(t *testing.T) {
	f := setup(t)
	v := f.add(store.StatusRunning, "alice")
	before, err := f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	calls := func(ctx context.Context) []error {
		errs := []error{}
		r, e := f.st.Add(ctx, store.Transfer{})
		equal(t, r, store.Transfer{})
		errs = append(errs, e)
		r, e = f.st.Start(ctx, "missing")
		equal(t, r, store.Transfer{})
		errs = append(errs, e)
		r, e = f.st.SetBytes(ctx, v.ID, -1)
		equal(t, r, store.Transfer{})
		errs = append(errs, e)
		r, e = f.st.Finish(ctx, "missing", store.Ending{})
		equal(t, r, store.Transfer{})
		errs = append(errs, e)
		r, e = f.st.Get(ctx, "alice", v.ID)
		equal(t, r, store.Transfer{})
		errs = append(errs, e)
		l, e := f.st.List(ctx, "alice")
		if l != nil {
			t.Fatal(l)
		}
		errs = append(errs, e)
		l, e = f.st.Unfinished(ctx)
		if l != nil {
			t.Fatal(l)
		}
		errs = append(errs, e)
		l, e = f.st.Prune(ctx, f.now, 0, 0)
		if l != nil {
			t.Fatal(l)
		}
		return append(errs, e)
	}
	ctx, cancel := context.WithCancel(background)
	cancel()
	for _, e := range calls(ctx) {
		requireError(t, e, store.ErrUnreachable)
		requireError(t, e, context.Canceled)
	}
	f.d.SetFailing(true)
	for _, e := range calls(ctx) {
		requireError(t, e, store.ErrUnreachable)
		requireError(t, e, context.Canceled)
	}
	for _, e := range calls(background) {
		requireError(t, e, store.ErrUnreachable)
		if errors.Is(e, store.ErrNotFound) || errors.Is(e, store.ErrEnded) {
			t.Fatal(e)
		}
	}
	f.d.SetFailing(false)
	after, err := f.st.List(background, "alice")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, after, before)
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, e := range calls(background) {
		requireError(t, e, store.ErrUnreachable)
	}
	f.open()
	f.check(v)
}

// R-LAM0-NY2N R-SVHE-AQEI
func TestConcurrentTransitions(t *testing.T) {
	f := setup(t)
	v := f.add(store.StatusQueued, "alice")
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	gate := make(chan struct{})
	for range 16 {
		wg.Go(func() { <-gate; _, err := f.st.Start(background, v.ID); errs <- err })
	}
	close(gate)
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			requireError(t, err, store.ErrEnded)
		}
	}
	equal(t, success, 1)
	ends := make(chan store.Transfer, 16)
	errs = make(chan error, 16)
	gate = make(chan struct{})
	for i := range 16 {
		wg.Go(func() {
			<-gate
			got, err := f.st.Finish(background, v.ID, store.Ending{Status: store.StatusDone, Bytes: int64(i), File: "file", Name: "name"})
			ends <- got
			errs <- err
		})
	}
	close(gate)
	wg.Wait()
	close(errs)
	close(ends)
	success = 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			requireError(t, err, store.ErrEnded)
		}
	}
	equal(t, success, 1)
	for got := range ends {
		if got.ID != "" {
			f.check(got)
		}
	}
	got, err := f.st.SetBytes(background, v.ID, 77)
	equal(t, got, store.Transfer{})
	requireError(t, err, store.ErrEnded)
	// Calls of every method overlap on the same store, including its random reader.
	gate = make(chan struct{})
	errs = make(chan error, 32)
	for i := range 32 {
		wg.Go(func() {
			<-gate
			switch i % 8 {
			case 0:
				_, e := f.st.Add(background, store.Transfer{Direction: store.DirectionPush, Status: store.StatusRunning, UserID: "bob", Path: "path"})
				errs <- e
			case 1:
				_, e := f.st.Get(background, "alice", v.ID)
				errs <- e
			case 2:
				_, e := f.st.List(background, "alice")
				errs <- e
			case 3:
				_, e := f.st.Unfinished(background)
				errs <- e
			case 4:
				_, e := f.st.Prune(background, f.now, 1, 1)
				errs <- e
			case 5:
				_, e := f.st.Start(background, v.ID)
				if !errors.Is(e, store.ErrEnded) {
					errs <- errors.New("expected ErrEnded")
				}
			case 6:
				_, e := f.st.SetBytes(background, v.ID, 1)
				if !errors.Is(e, store.ErrEnded) {
					errs <- errors.New("expected ErrEnded")
				}
			case 7:
				_, e := f.st.Finish(background, v.ID, store.Ending{Status: store.StatusDone})
				if !errors.Is(e, store.ErrEnded) {
					errs <- errors.New("expected ErrEnded")
				}
			}
		})
	}
	close(gate)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
}

// R-S9J7-EV20
func TestRandomReadFailure(t *testing.T) {
	f := setup(t)
	r := &failingReader{}
	s := store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
	v, err := s.Add(background, store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, UserID: "alice", Path: "path"})
	equal(t, v, store.Transfer{})
	requireError(t, err, store.ErrUnreachable)
	l, err := s.List(background, "alice")
	if err != nil || len(l) != 0 {
		t.Fatal(l, err)
	}
}

type failingReader struct{}

func (*failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// R-SO60-03YC
func TestPruneBeyondDurationHorizon(t *testing.T) {
	f := setup(t)
	f.now = time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)
	old := f.add(store.StatusRunning, "alice")
	old, err := f.st.Finish(background, old.ID, store.Ending{Status: store.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newest := f.add(store.StatusRunning, "alice")
	got, err := f.st.Prune(background, f.now, math.MaxInt64, 1)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	got, err = f.st.Prune(background, f.now, 200000, 1)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, []store.Transfer{old})
	f.check(newest)
	f.reopen()
	_, err = f.st.Get(background, old.UserID, old.ID)
	requireError(t, err, store.ErrNotFound)
	f.check(newest)
}

// R-LAM0-NY2N R-SVHE-AQEI
func TestMixedTransitions(t *testing.T) {
	f := setup(t)
	for range 12 {
		original := f.add(store.StatusQueued, "alice")
		var got [3]store.Transfer
		var errs [3]error
		gate := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-gate; got[0], errs[0] = f.st.Start(background, original.ID) })
		wg.Go(func() { <-gate; got[1], errs[1] = f.st.SetBytes(background, original.ID, 11) })
		wg.Go(func() {
			<-gate
			got[2], errs[2] = f.st.Finish(background, original.ID, store.Ending{Status: store.StatusDone, Bytes: 37})
		})
		close(gate)
		wg.Wait()
		running := original
		running.Status = store.StatusRunning
		if errs[0] != nil {
			t.Fatal(errs[0])
		}
		equal(t, got[0], running)
		final := running
		if errs[1] == nil {
			progress := running
			progress.Bytes = 11
			equal(t, got[1], progress)
			final = progress
		} else {
			requireError(t, errs[1], store.ErrEnded)
			equal(t, got[1], store.Transfer{})
		}
		if errs[2] == nil {
			ended := running
			ended.Status = store.StatusDone
			ended.Bytes = 37
			ended.Finished = f.now.UTC().Truncate(time.Second)
			equal(t, got[2], ended)
			final = ended
		} else {
			requireError(t, errs[2], store.ErrUnreachable)
			equal(t, got[2], store.Transfer{})
		}
		f.check(final)
	}
}

// R-SO60-03YC
func TestPruneRanksIncludeUnfinished(t *testing.T) {
	f := setup(t)
	old := f.add(store.StatusRunning, "alice")
	old, err := f.st.Finish(background, old.ID, store.Ending{Status: store.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	queued := f.add(store.StatusQueued, "alice")
	running := f.add(store.StatusRunning, "alice")
	got, err := f.st.Prune(background, f.now.Add(48*time.Hour), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, []store.Transfer{old})
	f.check(queued)
	f.check(running)
}
