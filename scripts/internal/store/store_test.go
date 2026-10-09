package store_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

var ctx = context.Background()
var stamp = time.Date(2026, 10, 5, 12, 30, 20, 987654321, time.FixedZone("east", 3600))

type sequence struct{ n byte }

func (r *sequence) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

var handles sync.Map

func open(t *testing.T, path string) *store.Store {
	t.Helper()
	return configured(t, path, store.Config{Now: func() time.Time { return stamp }, Rand: &sequence{}})
}
func configured(t *testing.T, path string, cfg store.Config) *store.Store {
	t.Helper()
	if path == "" {
		path = filepath.Join(t.TempDir(), "scripts.db")
	}
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: scripts.Migrations(), Now: func() time.Time { return stamp }})
	must(t, err)
	s := store.New(d, cfg)
	handles.Store(s, d)
	t.Cleanup(func() { must(t, d.Close()); handles.Delete(s) })
	return s
}
func handle(s *store.Store) *db.DB    { d, _ := handles.Load(s); return d.(*db.DB) }
func closeStore(s *store.Store) error { return handle(s).Close() }
func create(t *testing.T, s *store.Store, owner, name string) store.Script {
	t.Helper()
	sc, err := s.Create(ctx, store.Draft{Owner: owner, Name: name, Repo: "repository", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	return sc
}
func run(sc store.Script, n int) store.Run {
	return store.Run{ID: fmt.Sprintf("run_%016x", n), Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: sc.Owner, RequestID: "request", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: stamp}
}
func add(t *testing.T, s *store.Store, r store.Run) store.Run {
	t.Helper()
	out, err := s.AddRun(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func equal(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v; want %#v", a, b)
	}
}
func catalog(t *testing.T, err error) {
	t.Helper()
	if err == nil || errors.Is(err, store.ErrNameTaken) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrEnded) || errors.Is(err, store.ErrNotSubscribed) || errors.Is(err, store.ErrDelivered) {
		t.Fatalf("not a catalog error: %v", err)
	}
}
func content(t *testing.T, s *store.Store) any {
	t.Helper()
	all := []any{}
	for _, owner := range []string{"alice", "bob"} {
		scripts, err := s.List(ctx, owner)
		must(t, err)
		all = append(all, scripts)
		for _, sc := range scripts {
			rr, err := s.Runs(ctx, sc.ID)
			must(t, err)
			all = append(all, rr)
		}
	}
	return all
}

func TestIDs(t *testing.T) {
	// R-JEHG-T7L7 R-JFPD-6ZBW R-JGX9-KR2L R-JI55-YITA
	equal(t, store.ScriptPrefix, "scr_")
	equal(t, store.RunPrefix, "run_")
	for _, f := range []struct {
		prefix string
		new    func(io.Reader) (string, error)
		valid  func(string) bool
	}{{store.ScriptPrefix, store.NewScriptID, store.ValidScriptID}, {store.RunPrefix, store.NewRunID, store.ValidRunID}} {
		b := bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8, 171})
		id, err := f.new(b)
		must(t, err)
		equal(t, id, f.prefix+"0102030405060708")
		equal(t, b.Len(), 1)
		equal(t, f.valid(id), true)
		for _, bad := range []string{"", id + " ", id[:len(id)-1], f.prefix + "010203040506070A", f.prefix + "010203040506070g", strings.Replace(id, f.prefix, "other_", 1)} {
			equal(t, f.valid(bad), false)
		}
		for n := 0; n < 8; n++ {
			id, err = f.new(bytes.NewReader(make([]byte, n)))
			equal(t, id, "")
			if err == nil {
				t.Fatal("short random read accepted")
			}
		}
		id, err = f.new(fullErrorReader{})
		equal(t, id, "")
		if err == nil {
			t.Fatal("full-byte random failure accepted")
		}
		id, err = f.new(errorReader{})
		equal(t, id, "")
		if err == nil {
			t.Fatal("random failure accepted")
		}
	}
	equal(t, store.ValidRunID("scr_0102030405060708"), false)
	equal(t, store.ValidScriptID("run_0102030405060708"), false)
}

type fullErrorReader struct{}

func (fullErrorReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 1
	}
	return len(p), errors.New("random failure")
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }
func TestWordsAndNames(t *testing.T) {
	// R-8X3J-K44N R-8YBF-XVVC R-Z0SU-2CSG R-XX43-QPZC R-RDHU-ATN8
	equal(t, []string{store.StatusRunning, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed}, []string{"running", "exited", "killed", "timed_out", "failed"})
	equal(t, []string{store.ReasonRepositoryMissing, store.ReasonCommitMissing, store.ReasonTooLarge, store.ReasonGitFailed, store.ReasonTimedOut, store.ReasonStartFailed}, []string{"repository_missing", "commit_missing", "too_large", "git_failed", "timed_out", "start_failed"})
	equal(t, store.TriggerManual, "manual")
	const unreachable string = store.Unreachable
	if unreachable == "" {
		t.Fatal("empty catalog refusal")
	}
	equal(t, store.TriggerEvent, "event")
	sentinels := []error{store.ErrNotFound, store.ErrNameTaken, store.ErrEnded}
	for i, a := range sentinels {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatal("sentinels overlap")
			}
		}
	}
	for _, good := range []string{"a", "0", "a-", "about-us", "mcp-audit", "events-digest", "declarations2", strings.Repeat("a", 64)} {
		equal(t, store.ValidName(good), true)
	}
	for _, bad := range []string{"", "about", "mcp", "events", "declarations", "CRM Weekly", " report", "-report", "a_b", strings.Repeat("a", 65), "é", "a\x00"} {
		equal(t, store.ValidName(bad), false)
	}
	for b := 0; b < 256; b++ {
		name := "a" + string(byte(b))
		want := b >= int('a') && b <= int('z') || b >= int('0') && b <= int('9') || b == int('-')
		equal(t, store.ValidName(name), want)
	}
}

func TestScriptsAndPersistence(t *testing.T) {
	// R-R2IQ-UVYZ R-RMLU-90BA R-KOTN-CHRL R-R9U5-5IF5 R-XZJW-I9GQ R-Y0RS-W17F R-LD7M-ZWLH R-LFNF-RG2V R-LGVC-57TK R-LI38-IZK9 R-LJB4-WRAY R-LKJ1-AJ1N R-Y1ZP-9SY4 R-LMYU-22J1
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := open(t, path)
	z := create(t, s, "alice", "zulu")
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	equal(t, a.Name, "alpha")
	equal(t, a.Owner, "alice")
	equal(t, a.Repo, "repository")
	equal(t, a.Ref, "main")
	equal(t, a.Created, stamp.UTC().Truncate(time.Second))
	equal(t, a.Last, (*store.Run)(nil))
	equal(t, store.ValidScriptID(a.ID), true)
	if a.ID == z.ID || a.ID == b.ID {
		t.Fatal("duplicate ids")
	}
	list, err := s.List(ctx, "alice")
	must(t, err)
	equal(t, list, []store.Script{a, z})
	list, err = s.List(ctx, "absent")
	must(t, err)
	equal(t, list, []store.Script{})
	for _, lookup := range [][2]string{{"bob", "alpha"}, {"alice", a.ID}, {"", "alpha"}, {"alice", ""}} {
		got, err := s.Find(ctx, lookup[0], lookup[1])
		equal(t, got, store.Script{})
		equal(t, errors.Is(err, store.ErrNotFound), true)
	}
	taken, err := s.Taken(ctx, "beta")
	must(t, err)
	equal(t, taken, true)
	taken, err = s.Taken(ctx, b.ID)
	must(t, err)
	equal(t, taken, false)
	r := add(t, s, run(a, 1))
	got, err := s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, *got.Last, r)
	updated, changed, err := s.SetRef(ctx, a.ID, "release")
	must(t, err)
	equal(t, changed, true)
	a.Ref = "release"
	a.Last = &r
	equal(t, updated, a)
	updated, changed, err = s.SetRef(ctx, a.ID, "release")
	must(t, err)
	equal(t, changed, false)
	equal(t, updated, a)
	before := content(t, s)
	got, changed, err = s.SetRef(ctx, a.ID, "")
	equal(t, got, store.Script{})
	equal(t, changed, false)
	catalog(t, err)
	_, changed, err = s.SetRef(ctx, "missing", "ref")
	equal(t, changed, false)
	equal(t, errors.Is(err, store.ErrNotFound), true)
	equal(t, content(t, s), before)
	equal(t, errors.Is(s.Delete(ctx, "missing"), store.ErrNotFound), true)
	must(t, closeStore(s))
	s = open(t, path)
	equal(t, content(t, s), before)
	got, err = s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, got, a)
	must(t, s.Delete(ctx, a.ID))
	_, err = s.Find(ctx, "alice", "alpha")
	equal(t, errors.Is(err, store.ErrNotFound), true)
	_, err = s.RunByID(ctx, r.ID)
	equal(t, errors.Is(err, store.ErrNotFound), true)
	taken, err = s.Taken(ctx, "alpha")
	must(t, err)
	equal(t, taken, false)
	got, err = s.Find(ctx, "bob", "beta")
	must(t, err)
	equal(t, got, b)
	got, err = s.Find(ctx, "alice", "zulu")
	must(t, err)
	equal(t, got, z)
	must(t, closeStore(s))
	s = open(t, path)
	_, err = s.RunByID(ctx, r.ID)
	equal(t, errors.Is(err, store.ErrNotFound), true)
	_, err = s.Find(ctx, "alice", "alpha")
	equal(t, errors.Is(err, store.ErrNotFound), true)
	create(t, s, "bob", "alpha")
}
func TestCreateFailuresAndCollisions(t *testing.T) {
	// R-8QLS-QL4D R-L8C1-GTMP R-L9JX-ULDE R-LARU-8D43 R-G9YJ-7JM1
	random := bytes.NewReader(bytes.Repeat([]byte{1, 2, 3, 4, 5, 6, 7, 8}, 9))
	s := configured(t, "", store.Config{Rand: random, Now: func() time.Time { return stamp }})
	defer func() { must(t, closeStore(s)) }()
	a := create(t, s, "alice", "alpha")
	equal(t, a.ID, "scr_0102030405060708")
	before := content(t, s)
	d := store.Draft{Owner: "bob", Name: "alpha", Repo: "repository", Ref: "main"}
	n := random.Len()
	sc, err := s.Create(ctx, d)
	equal(t, sc, store.Script{})
	equal(t, errors.Is(err, store.ErrNameTaken), true)
	equal(t, random.Len(), n)
	for _, bad := range []store.Draft{{Owner: "alice", Name: "about", Repo: "r", Ref: "main"}, {Name: "alpha", Repo: "r", Ref: "main"}, {Owner: "alice", Name: "alpha", Ref: "main"}, {Owner: "alice", Name: "alpha", Repo: "r"}} {
		sc, err = s.Create(ctx, bad)
		equal(t, sc, store.Script{})
		catalog(t, err)
		equal(t, random.Len(), n)
		equal(t, content(t, s), before)
	}
	d.Name = "other"
	sc, err = s.Create(ctx, d)
	equal(t, sc, store.Script{})
	catalog(t, err)
	equal(t, n-random.Len(), 64)
	equal(t, content(t, s), before)
	sc, err = s.Create(ctx, d)
	equal(t, sc, store.Script{})
	catalog(t, err)
	equal(t, content(t, s), before)
	retry := bytes.NewReader(append(bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 8)...))
	s2 := configured(t, "", store.Config{Rand: retry, Now: func() time.Time { return stamp }})
	defer func() { must(t, closeStore(s2)) }()
	create(t, s2, "alice", "first")
	second := create(t, s2, "alice", "second")
	equal(t, second.ID, "scr_0202020202020202")
	equal(t, retry.Len(), 0)
}
func TestRunTransitionsAndReads(t *testing.T) {
	// R-R4YJ-MFGD R-KQ1J-Q9IA R-LUA8-COZ7 R-LVI4-QGPW R-LWQ1-48GL R-LZ5T-VRXZ R-Y5NE-F467
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := open(t, path)
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	r1 := add(t, s, run(a, 1))
	r2 := add(t, s, run(a, 2))
	r3 := run(b, 3)
	r3.Started = stamp.Add(time.Hour)
	r3 = add(t, s, r3)
	rr, err := s.Runs(ctx, a.ID)
	must(t, err)
	equal(t, rr, []store.Run{r2, r1})
	rr, err = s.Runs(ctx, "missing")
	must(t, err)
	equal(t, rr, []store.Run{})
	rr, err = s.Running(ctx)
	must(t, err)
	equal(t, rr, []store.Run{r1, r2, r3})
	for _, pair := range [][2]string{{"bob", r1.ID}, {"", r1.ID}, {"alice", ""}, {"alice", "missing"}} {
		got, err := s.FindRun(ctx, pair[0], pair[1])
		equal(t, got, store.Run{})
		equal(t, errors.Is(err, store.ErrNotFound), true)
	}
	got, err := s.RunByID(ctx, r3.ID)
	must(t, err)
	equal(t, got, r3)
	got, err = s.RunByID(ctx, "missing")
	equal(t, got, store.Run{})
	equal(t, errors.Is(err, store.ErrNotFound), true)
	for i, status := range []string{store.StatusExited, store.StatusKilled, store.StatusTimedOut} {
		r := []store.Run{r1, r2, r3}[i]
		e := store.Ending{Status: status, Finished: stamp.Add(2 * time.Hour), StdoutBytes: 12, StderrBytes: 7, Truncated: true}
		if status == store.StatusExited {
			e.ExitCode = 255
		}
		ended, err := s.FinishRun(ctx, r.ID, e)
		must(t, err)
		r.Status = e.Status
		r.ExitCode = e.ExitCode
		r.Finished = e.Finished.UTC().Truncate(time.Second)
		r.StdoutBytes = e.StdoutBytes
		r.StderrBytes = e.StderrBytes
		r.Truncated = e.Truncated
		equal(t, ended, r)
		got, err = s.FindRun(ctx, r.User, r.ID)
		must(t, err)
		equal(t, got, r)
		got, err = s.RunByID(ctx, r.ID)
		must(t, err)
		equal(t, got, r)
		equal(t, r.Started.Location(), time.UTC)
		equal(t, r.Finished.Location(), time.UTC)
		equal(t, r.Started.Nanosecond(), 0)
		equal(t, r.Finished.Nanosecond(), 0)
	}
	rr, err = s.Running(ctx)
	must(t, err)
	equal(t, rr, []store.Run{})
	before := content(t, s)
	must(t, closeStore(s))
	s = open(t, path)
	equal(t, content(t, s), before)
	must(t, s.DeleteRun(ctx, r2.ID))
	got, err = s.RunByID(ctx, r2.ID)
	equal(t, got, store.Run{})
	equal(t, errors.Is(err, store.ErrNotFound), true)
	sc, err := s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, sc.Last.ID, r1.ID)
	before = content(t, s)
	equal(t, errors.Is(s.DeleteRun(ctx, "missing"), store.ErrNotFound), true)
	equal(t, content(t, s), before)
	must(t, closeStore(s))
	s = open(t, path)
	equal(t, content(t, s), before)
}

func TestRunValidation(t *testing.T) {
	// R-LPEM-TM0F R-99AJ-DTJL
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	base := run(sc, 10)
	before := content(t, s)
	invalid := []func(*store.Run){func(r *store.Run) { r.ID = "bad" }, func(r *store.Run) { r.Ref = "" }, func(r *store.Run) { r.User = "" }, func(r *store.Run) { r.Script = "" }, func(r *store.Run) { r.Trigger = "invalid" }, func(r *store.Run) { r.Started = time.Time{} }, func(r *store.Run) { r.StdoutBytes = -1 }, func(r *store.Run) { r.StderrBytes = -1 }, func(r *store.Run) { r.SHA = "A" + strings.Repeat("a", 39) }, func(r *store.Run) { r.SHA = "" }, func(r *store.Run) { r.SHA = "a" }, func(r *store.Run) { r.ExitCode = 1 }, func(r *store.Run) { r.StdoutBytes = 1 }, func(r *store.Run) { r.StderrBytes = 1 }, func(r *store.Run) { r.Finished = stamp }, func(r *store.Run) { r.Truncated = true }, func(r *store.Run) { r.Reason = store.ReasonStartFailed }, func(r *store.Run) { r.Status = store.StatusExited }}
	for i, mutate := range invalid {
		r := base
		mutate(&r)
		out, err := s.AddRun(ctx, r)
		equal(t, out, store.Run{})
		if err == nil {
			t.Fatalf("invalid case %d", i)
		}
		catalog(t, err)
		equal(t, content(t, s), before)
	}
	missing := base
	missing.Script = "missing"
	_, err := s.AddRun(ctx, missing)
	equal(t, errors.Is(err, store.ErrNotFound), true)
	add(t, s, base)
	before = content(t, s)
	_, err = s.AddRun(ctx, base)
	catalog(t, err)
	equal(t, content(t, s), before)
	for i, reason := range []string{store.ReasonRepositoryMissing, store.ReasonCommitMissing, store.ReasonTooLarge, store.ReasonGitFailed, store.ReasonTimedOut, store.ReasonStartFailed} {
		r := run(sc, 20+i)
		r.Status = store.StatusFailed
		r.SHA = ""
		r.Reason = reason
		r.Finished = stamp
		r.StdoutBytes = 4
		r.StderrBytes = 3
		r.Truncated = true
		out := add(t, s, r)
		r.Started = r.Started.UTC().Truncate(time.Second)
		r.Finished = r.Finished.UTC().Truncate(time.Second)
		equal(t, out, r)
		equal(t, out.Reason, reason)
		equal(t, out.Status, store.StatusFailed)
		equal(t, out.Finished.Location(), time.UTC)
		equal(t, out.Finished.Nanosecond(), 0)
	}
	failed := run(sc, 40)
	failed.Status = store.StatusFailed
	failed.Reason = store.ReasonGitFailed
	failed.Finished = stamp
	for _, mutate := range []func(*store.Run){func(r *store.Run) { r.Reason = "unknown" }, func(r *store.Run) { r.ExitCode = 1 }, func(r *store.Run) { r.Finished = time.Time{} }, func(r *store.Run) { r.Finished = stamp.Add(-time.Second) }} {
		r := failed
		mutate(&r)
		_, err = s.AddRun(ctx, r)
		catalog(t, err)
	}
	good := store.Ending{Status: store.StatusExited, Finished: stamp, ExitCode: 0}
	for _, bad := range []store.Ending{{Status: store.StatusRunning, Finished: stamp}, {Status: store.StatusFailed, Finished: stamp}, {Status: store.StatusExited, Finished: stamp, ExitCode: -1}, {Status: store.StatusExited, Finished: stamp, ExitCode: 256}, {Status: store.StatusKilled, Finished: stamp, ExitCode: 1}, {Status: store.StatusTimedOut, Finished: stamp, ExitCode: 1}, {Status: store.StatusExited}, {Status: store.StatusExited, Finished: stamp, StdoutBytes: -1}, {Status: store.StatusExited, Finished: stamp, StderrBytes: -1}} {
		before = content(t, s)
		for _, id := range []string{base.ID, "missing"} {
			out, err := s.FinishRun(ctx, id, bad)
			equal(t, out, store.Run{})
			catalog(t, err)
		}
		equal(t, content(t, s), before)
	}
	out, err := s.FinishRun(ctx, "missing", good)
	equal(t, out, store.Run{})
	equal(t, errors.Is(err, store.ErrNotFound), true)
	early := good
	early.Finished = stamp.Add(-time.Second)
	before = content(t, s)
	_, err = s.FinishRun(ctx, base.ID, early)
	catalog(t, err)
	equal(t, content(t, s), before)
	_, err = s.FinishRun(ctx, base.ID, good)
	must(t, err)
	before = content(t, s)
	out, err = s.FinishRun(ctx, base.ID, early)
	equal(t, out, store.Run{})
	equal(t, errors.Is(err, store.ErrEnded), true)
	equal(t, content(t, s), before)
}
func TestRetention(t *testing.T) {
	// R-9AIF-RLAA R-M1LM-NBFD
	s := open(t, "")
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	now := stamp.UTC().Truncate(time.Second).Add(10 * 24 * time.Hour)
	for _, sc := range []store.Script{a, b} {
		for n := 1; n <= 5; n++ {
			r := run(sc, n)
			if sc.ID == b.ID {
				r.ID = fmt.Sprintf("run_%016x", n+10)
			}
			r.Started = now.Add(-time.Duration(n) * 24 * time.Hour)
			if n != 1 {
				r.Status = store.StatusFailed
				r.Reason = store.ReasonStartFailed
				r.Finished = r.Started
			}
			add(t, s, r)
		}
	}
	before := content(t, s)
	got, err := s.PastKeeping(ctx, now, 3, 2)
	must(t, err)
	want := []store.Run{}
	ids := []string{a.ID, b.ID}
	if ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	for _, id := range ids {
		rr, err := s.Runs(ctx, id)
		must(t, err)
		want = append(want, rr[3:]...)
	}
	equal(t, got, want)
	equal(t, content(t, s), before)
	got, err = s.PastKeeping(ctx, now, math.MaxInt64, 1)
	must(t, err)
	equal(t, got, []store.Run{})
	got, err = s.PastKeeping(ctx, now, 1, math.MaxInt64)
	must(t, err)
	equal(t, got, []store.Run{})
	for _, v := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		got, err = s.PastKeeping(ctx, now, v[0], v[1])
		equal(t, got, ([]store.Run)(nil))
		catalog(t, err)
	}
}
func TestConcurrentWrites(t *testing.T) {
	// R-LBZQ-M4US R-LT2B-YX8I R-M41F-EUWR R-Y9B3-KFEA
	s := open(t, "")
	const n = 12
	errorsOut := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Go(func() {
			_, err := s.Create(ctx, store.Draft{Owner: "alice", Name: "alpha", Repo: "r", Ref: "main"})
			errorsOut <- err
		})
	}
	wg.Wait()
	close(errorsOut)
	wins := 0
	for err := range errorsOut {
		if err == nil {
			wins++
		} else {
			equal(t, errors.Is(err, store.ErrNameTaken), true)
		}
	}
	equal(t, wins, 1)
	list, err := s.List(ctx, "alice")
	must(t, err)
	equal(t, len(list), 1)
	sc := list[0]
	r := add(t, s, run(sc, 1))
	winner := make(chan store.Run, 1)
	errorsOut = make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		wg.Go(func() {
			ended, err := s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, ExitCode: i, Finished: stamp})
			if err == nil {
				winner <- ended
			}
			errorsOut <- err
		})
	}
	wg.Wait()
	close(errorsOut)
	wins = 0
	for err := range errorsOut {
		if err == nil {
			wins++
		} else {
			equal(t, errors.Is(err, store.ErrEnded), true)
		}
	}
	equal(t, wins, 1)
	final, err := s.RunByID(ctx, r.ID)
	must(t, err)
	equal(t, final, <-winner)

	assertScript := func(got store.Script) {
		equal(t, got.ID, sc.ID)
		equal(t, got.Name, sc.Name)
		equal(t, got.Owner, sc.Owner)
		equal(t, got.Repo, sc.Repo)
		equal(t, got.Created, sc.Created)
		equal(t, *got.Last, final)
	}
	for i := 0; i < n; i++ {
		i := i
		wg.Go(func() {
			got, err := s.Find(ctx, "alice", "alpha")
			must(t, err)
			assertScript(got)
			list, err := s.List(ctx, "alice")
			must(t, err)
			equal(t, len(list), 1)
			assertScript(list[0])
			taken, err := s.Taken(ctx, "alpha")
			must(t, err)
			equal(t, taken, true)
			gotRun, err := s.RunByID(ctx, r.ID)
			must(t, err)
			equal(t, gotRun, final)
			gotRun, err = s.FindRun(ctx, "alice", r.ID)
			must(t, err)
			equal(t, gotRun, final)
			rr, err := s.Runs(ctx, sc.ID)
			must(t, err)
			equal(t, rr, []store.Run{final})
			rr, err = s.Running(ctx)
			must(t, err)
			equal(t, rr, []store.Run{})
			rr, err = s.PastKeeping(ctx, stamp, 1, 1)
			must(t, err)
			equal(t, rr, []store.Run{})
			ref := fmt.Sprintf("ref%d", i)
			got, changed, err := s.SetRef(ctx, sc.ID, ref)
			must(t, err)
			assertScript(got)
			equal(t, got.Ref, ref)
			equal(t, changed, true)
		})
	}
	wg.Wait()
	deletedIDs := make([]string, n)
	for i := range n {
		deletedIDs[i] = add(t, s, run(sc, 100+i)).ID
	}
	for _, id := range deletedIDs {
		wg.Go(func() { must(t, s.DeleteRun(ctx, id)) })
	}
	wg.Wait()
	rr, err := s.Runs(ctx, sc.ID)
	must(t, err)
	equal(t, rr, []store.Run{final})
	for _, id := range deletedIDs {
		_, err := s.RunByID(ctx, id)
		equal(t, errors.Is(err, store.ErrNotFound), true)
	}

	start := make(chan struct{})
	errorsOut = make(chan error, 2)
	wg.Go(func() { <-start; _, err := s.AddRun(ctx, run(sc, 2)); errorsOut <- err })
	wg.Go(func() { <-start; errorsOut <- s.Delete(ctx, sc.ID) })
	close(start)
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			equal(t, errors.Is(err, store.ErrNotFound), true)
		}
	}
	rr, err = s.Runs(ctx, sc.ID)
	must(t, err)
	equal(t, rr, []store.Run{})
}
func TestClosedAndCancelled(t *testing.T) {
	// R-Y6VA-SVWW R-Y837-6NNL R-G9YJ-7JM1
	path := filepath.Join(t.TempDir(), "scripts.db")
	s := open(t, path)
	sc := create(t, s, "alice", "alpha")
	r := add(t, s, run(sc, 1))
	before := content(t, s)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	calls := func(c context.Context) []error {
		_, a := s.Find(c, "alice", "alpha")
		_, b := s.List(c, "alice")
		_, d := s.Taken(c, "alpha")
		_, e := s.Create(c, store.Draft{})
		_, _, f := s.SetRef(c, sc.ID, "")
		g := s.Delete(c, sc.ID)
		_, h := s.AddRun(c, store.Run{})
		_, i := s.FinishRun(c, r.ID, store.Ending{})
		_, j := s.FindRun(c, "alice", r.ID)
		_, k := s.RunByID(c, r.ID)
		_, l := s.Runs(c, sc.ID)
		_, m := s.Running(c)
		_, n := s.PastKeeping(c, stamp, 0, 0)
		o := s.DeleteRun(c, r.ID)
		return []error{a, b, d, e, f, g, h, i, j, k, l, m, n, o}
	}
	for _, err := range calls(cancelled) {
		catalog(t, err)
		equal(t, errors.Is(err, context.Canceled), true)
	}
	equal(t, content(t, s), before)
	handle(s).SetFailing(true)
	_, err := s.Create(ctx, store.Draft{Owner: "alice", Name: "alpha", Repo: "r", Ref: "main"})
	catalog(t, err)
	_, err = s.RunByID(ctx, "absent")
	catalog(t, err)
	for _, err := range calls(ctx) {
		catalog(t, err)
	}
	for _, err := range calls(cancelled) {
		equal(t, errors.Is(err, context.Canceled), true)
	}
	handle(s).SetFailing(false)
	equal(t, content(t, s), before)
	must(t, closeStore(s))
	for _, err := range calls(ctx) {
		catalog(t, err)
	}
	for _, err := range calls(cancelled) {
		equal(t, errors.Is(err, context.Canceled), true)
	}
	equal(t, content(t, open(t, path)), before)
}

func TestRetentionBeyondDurationRange(t *testing.T) {
	// R-9AIF-RLAA
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	old := run(sc, 1)
	old.Started = time.Date(2, 1, 1, 0, 0, 0, 0, time.UTC)
	old.Finished = old.Started
	old.Status = store.StatusFailed
	old.Reason = store.ReasonStartFailed
	old = add(t, s, old)
	recent := run(sc, 2)
	recent.Started = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	add(t, s, recent)
	got, err := s.PastKeeping(ctx, recent.Started, 200000, 1)
	must(t, err)
	equal(t, got, []store.Run{old})
	got, err = s.PastKeeping(ctx, recent.Started, math.MaxInt64, 1)
	must(t, err)
	equal(t, got, []store.Run{})
}
func TestDeletingEveryStatus(t *testing.T) {
	// R-Y1ZP-9SY4 R-LI38-IZK9
	path := filepath.Join(t.TempDir(), "db")
	s := open(t, path)
	sc := create(t, s, "alice", "alpha")
	for i, status := range []string{store.StatusRunning, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed} {
		r := run(sc, i+1)
		if status == store.StatusFailed {
			r.Status = status
			r.Reason = store.ReasonStartFailed
			r.Finished = stamp
			add(t, s, r)
		} else {
			add(t, s, r)
			if status != store.StatusRunning {
				_, err := s.FinishRun(ctx, r.ID, store.Ending{Status: status, Finished: stamp})
				must(t, err)
			}
		}
	}
	rr, err := s.Runs(ctx, sc.ID)
	must(t, err)
	got, err := s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, *got.Last, rr[0])
	list, err := s.List(ctx, "alice")
	must(t, err)
	equal(t, *list[0].Last, rr[0])
	got, _, err = s.SetRef(ctx, sc.ID, "ref")
	must(t, err)
	equal(t, *got.Last, rr[0])
	must(t, s.Delete(ctx, sc.ID))
	must(t, closeStore(s))
	s = open(t, path)
	for _, r := range rr {
		_, err = s.RunByID(ctx, r.ID)
		equal(t, errors.Is(err, store.ErrNotFound), true)
	}
}

type sentinelReader struct{ err error }

func (r sentinelReader) Read([]byte) (int, error) { return 0, r.err }
func TestCreateRandomContentSentinels(t *testing.T) {
	// R-L8C1-GTMP
	for _, sentinel := range []error{store.ErrNotFound, store.ErrNameTaken, store.ErrEnded} {
		s := configured(t, "", store.Config{Rand: sentinelReader{sentinel}, Now: func() time.Time { return stamp }})
		before := content(t, s)
		sc, err := s.Create(ctx, store.Draft{Owner: "alice", Name: "alpha", Repo: "repo", Ref: "main"})
		equal(t, sc, store.Script{})
		catalog(t, err)
		equal(t, err.Error(), sentinel.Error())
		equal(t, content(t, s), before)
		must(t, closeStore(s))
	}
}
