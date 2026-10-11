package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
)

var ctx = context.Background()
var epoch = time.Date(2032, 2, 3, 4, 5, 6, 987654321, time.FixedZone("test", 3600))

type random struct{ n byte }

func (r *random) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.n
		r.n++
	}
	return len(p), nil
}

type fixture struct {
	s           *store.Store
	d           *db.DB
	path, blobs string
	now         time.Time
}

func setup(t *testing.T, r io.Reader) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{path: filepath.Join(dir, "artifacts.db"), blobs: filepath.Join(dir, "blobs"), now: epoch}
	if e := os.Mkdir(f.blobs, 0700); e != nil {
		t.Fatal(e)
	}
	var e error
	f.d, e = db.Open(ctx, db.Config{Path: f.path, Migrations: artifacts.Migrations(), Now: func() time.Time { return f.now }})
	if e != nil {
		t.Fatal(e)
	}
	if r == nil {
		r = &random{}
	}
	f.s = store.New(f.d, f.blobs, store.Config{Now: func() time.Time { return f.now }, Rand: r})
	t.Cleanup(func() { _ = f.d.Close() })
	return f
}
func draft(p string) store.Draft {
	return store.Draft{Path: p, OwnerID: "alice", OwnerEmail: "alice@example.test", Max: 10000}
}
func put(t *testing.T, f *fixture, d store.Draft, b string) store.Artifact {
	t.Helper()
	a, _, e := f.s.Put(ctx, d, strings.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func hash(b string) string      { h := sha256.Sum256([]byte(b)); return hex.EncodeToString(h[:]) }
func utc(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
func equal(t *testing.T, a, b store.Artifact) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %+v want %+v", a, b)
	}
}
func snapshot(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	entries, e := os.ReadDir(f.blobs)
	if e != nil {
		t.Fatal(e)
	}
	out := map[string]string{}
	for _, entry := range entries {
		b, e := os.ReadFile(filepath.Join(f.blobs, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		out[entry.Name()] = string(b)
	}
	return out
}
func invariant(t *testing.T, f *fixture) {
	t.Helper()
	aa, e := f.s.List(ctx)
	if e != nil {
		t.Fatal(e)
	}
	files := snapshot(t, f)
	if len(files) != len(aa) {
		t.Fatalf("files %d records %d", len(files), len(aa))
	}
	counts := map[string]int{}
	for _, b := range files {
		counts[hash(b)]++
	}
	for _, a := range aa {
		if counts[a.SHA256] == 0 {
			t.Fatal(a)
		}
		counts[a.SHA256]--
	}
}
func read(t *testing.T, s *store.Store, p string) string {
	t.Helper()
	a, file, e := s.Open(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(file)
	_ = file.Close()
	if e != nil || int64(len(b)) != a.Bytes || hash(string(b)) != a.SHA256 {
		t.Fatal(a, e)
	}
	return string(b)
}

type failure struct {
	err   error
	calls int
}

func (r *failure) Read([]byte) (int, error) { r.calls++; return 0, r.err }

type hooked struct {
	r    io.Reader
	hook func()
	once sync.Once
	n    int
}

func (r *hooked) Read(p []byte) (int, error) {
	r.once.Do(r.hook)
	n, e := r.r.Read(p)
	r.n += n
	return n, e
}

// R-ZZ5Y-684F R-00DU-JZV4 R-01LQ-XRLT R-02TN-BJCI R-041J-PB37 R-059G-32TW
func TestSurface(t *testing.T) {
	f := setup(t, nil)
	if store.IDPrefix != "art_" || store.DefaultContentType != "application/octet-stream" || store.MaxSegment != 128 || store.MaxPath != 1024 || store.Unreachable == "" {
		t.Fatal("constants")
	}
	ee := []error{store.ErrNotFound, store.ErrInvalid, store.ErrPastExpiry, store.ErrForbidden, store.ErrTooLarge, store.ErrBody, store.ErrUnreachable}
	for i, a := range ee {
		if a == nil {
			t.Fatal("nil")
		}
		for j, b := range ee {
			if i != j && errors.Is(a, b) {
				t.Fatal("overlap")
			}
		}
	}
	absent := filepath.Join(t.TempDir(), "absent")
	_ = store.New(f.d, absent, store.Config{Now: func() time.Time { return epoch }, Rand: &random{}})
	if _, e := os.Stat(absent); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	s := store.New(f.d, f.blobs, store.Config{})
	aa, e := s.List(ctx)
	if e != nil || aa == nil || len(aa) != 0 {
		t.Fatal(aa, e)
	}
	a := put(t, f, draft("one"), "one")
	if !store.ValidPath(a.Path) || !store.ValidID(a.ID) {
		t.Fatal(a)
	}
	a2, e := f.s.Get(ctx, a.Path)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a2, a)
	a2, e = f.s.GetID(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a2, a)
	a2, file, e := f.s.Open(ctx, a.Path)
	if e != nil {
		t.Fatal(e)
	}
	_ = file.Close()
	equal(t, a2, a)
	if _, e = f.s.Sweep(ctx, f.now); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Reconcile(ctx); e != nil {
		t.Fatal(e)
	}
	a2, e = f.s.Delete(ctx, a.Path, a.OwnerID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a2, a)
}

// R-06HC-GUKL R-08X5-8E1Z
func TestGrammar(t *testing.T) {
	for _, id := range []string{"art_0123456789abcdef", "art_ffffffffffffffff"} {
		if !store.ValidID(id) {
			t.Fatal(id)
		}
	}
	for _, id := range []string{"", "art_0123456789abcde", "art_0123456789abcdef0", "art_0123456789abcdeF", "Art_0123456789abcdef", "art_0123456789abcdeg"} {
		if store.ValidID(id) {
			t.Fatal(id)
		}
	}
	maxPath := strings.Repeat(strings.Repeat("a", 127)+"/", 7) + strings.Repeat("a", 128)
	for _, p := range []string{"a", "scratch/blob.bin", "runs/result.bin", "A/b/C.tar.gz", ".hidden", "a..b", "x_y-z", strings.Repeat("a", 128), maxPath} {
		if !store.ValidPath(p) {
			t.Fatal(p)
		}
	}
	for _, p := range []string{"", "/a", "a/", "a//b", "../x", "./x", "a/./b", "a/../b", ".", "..", "a b", "a%2Fb", "a\\b", "a:b", "é", strings.Repeat("a", 129), maxPath + "b"} {
		if store.ValidPath(p) {
			t.Fatal(p)
		}
	}
	for b := range 256 {
		want := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '.' || b == '-' || b == '_' || b == '/'
		if store.ValidPath("a"+string([]byte{byte(b)})+"b") != want {
			t.Fatal(b)
		}
	}
}

// R-0A51-M5SO R-0BCX-ZXJD R-0DSQ-RH0R R-0YJ1-9KMK
func TestPersistenceAndReplacement(t *testing.T) {
	f := setup(t, nil)
	d := draft("Case/data")
	d.ContentType = "Some/TYPE ; strange=value"
	d.Expires = epoch.Add(time.Hour)
	a, c, e := f.s.Put(ctx, d, strings.NewReader("original"))
	if e != nil || !c {
		t.Fatal(c, e)
	}
	want := store.Artifact{ID: a.ID, Path: d.Path, Bytes: 8, ContentType: d.ContentType, SHA256: hash("original"), OwnerID: d.OwnerID, OwnerEmail: d.OwnerEmail, Created: utc(epoch), Modified: utc(epoch), Expires: utc(d.Expires)}
	equal(t, a, want)
	other := put(t, f, draft("other"), "second")
	if a.ID == other.ID {
		t.Fatal("duplicate id")
	}
	if e = f.d.Close(); e != nil {
		t.Fatal(e)
	}
	f.d, e = db.Open(ctx, db.Config{Path: f.path, Migrations: artifacts.Migrations(), Now: func() time.Time { return f.now }})
	if e != nil {
		t.Fatal(e)
	}
	f.s = store.New(f.d, f.blobs, store.Config{Now: func() time.Time { return f.now }, Rand: &random{n: 90}})
	got, e := f.s.Get(ctx, d.Path)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, a)
	if read(t, f.s, d.Path) != "original" {
		t.Fatal("bytes")
	}
	f.now = epoch.Add(2 * time.Hour)
	d.ContentType = ""
	d.Expires = time.Time{}
	d.OwnerEmail = "changed"
	b, c, e := f.s.Put(ctx, d, strings.NewReader("replacement"))
	if e != nil || c {
		t.Fatal(c, e)
	}
	want.Bytes = 11
	want.SHA256 = hash("replacement")
	want.ContentType = store.DefaultContentType
	want.Modified = utc(f.now)
	want.Expires = time.Time{}.UTC()
	equal(t, b, want)
	got, e = f.s.Get(ctx, d.Path)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, b)
	for _, x := range []store.Artifact{a, b, other} {
		for _, tm := range []time.Time{x.Created, x.Modified, x.Expires} {
			if tm.Location() != time.UTC || tm.Nanosecond() != 0 {
				t.Fatal(tm)
			}
		}
	}
	invariant(t, f)
}

// R-0CKU-DPA2
func TestRandomDraws(t *testing.T) {
	r := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))
	f := setup(t, r)
	a := put(t, f, draft("one"), "one")
	if a.ID != store.IDPrefix+strings.Repeat("5a", 8) || r.Len() != 0 {
		t.Fatal(a)
	}
	for range 3 {
		b, c, e := f.s.Put(ctx, draft("one"), strings.NewReader("new"))
		if e != nil || c || b.ID != a.ID {
			t.Fatal(b, c, e)
		}
	}
	r = bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 16), bytes.Repeat([]byte{0x6b}, 8)...))
	g := setup(t, r)
	a = put(t, g, draft("one"), "")
	b := put(t, g, draft("two"), "")
	if a.ID != store.IDPrefix+strings.Repeat("5a", 8) || b.ID != store.IDPrefix+strings.Repeat("6b", 8) || r.Len() != 0 {
		t.Fatal(a, b, r.Len())
	}
}

// R-0F0N-58RG R-0G8J-J0I5 R-0R7M-YY6E R-4O1P-M05N
func TestEarlyRefusal(t *testing.T) {
	f := setup(t, nil)
	a := put(t, f, draft("owned"), "old")
	before := snapshot(t, f)
	body := &failure{err: errors.New("body")}
	rnd := &failure{err: errors.New("random")}
	s := store.New(f.d, f.blobs, store.Config{Now: func() time.Time { return f.now }, Rand: rnd})
	for _, tc := range []struct {
		d    store.Draft
		want error
	}{{store.Draft{Path: "a//b", OwnerID: "alice", Max: 1}, store.ErrInvalid}, {store.Draft{Path: "new", Max: 1}, store.ErrInvalid}, {store.Draft{Path: "new", OwnerID: "alice", Max: -1}, store.ErrInvalid}, {store.Draft{Path: "new", OwnerID: "alice", Max: 1, Expires: f.now}, store.ErrPastExpiry}, {store.Draft{Path: "new", OwnerID: "alice", Max: 1, Expires: utc(f.now).Add(999 * time.Millisecond)}, store.ErrPastExpiry}, {store.Draft{Path: "owned", OwnerID: "bob", Max: 1}, store.ErrForbidden}} {
		_, _, e := s.Put(ctx, tc.d, body)
		if !errors.Is(e, tc.want) {
			t.Fatal(e, tc.want)
		}
		if !reflect.DeepEqual(snapshot(t, f), before) {
			t.Fatal("changed")
		}
	}
	if body.calls != 0 || rnd.calls != 0 {
		t.Fatal("read")
	}
	for _, tc := range []struct {
		p, u string
		want error
	}{{"../bad", "alice", store.ErrInvalid}, {"missing", "alice", store.ErrNotFound}, {"owned", "bob", store.ErrForbidden}} {
		_, e := s.Delete(ctx, tc.p, tc.u)
		if !errors.Is(e, tc.want) {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(snapshot(t, f), before) {
			t.Fatal("delete changed")
		}
	}
	got, e := s.Get(ctx, "owned")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, a)
	invariant(t, f)
}

// R-0HGF-WS8U R-G0WA-SR5L R-G247-6IWA R-4O1P-M05N
func TestStreamLimits(t *testing.T) {
	f := setup(t, nil)
	a := put(t, f, draft("owned"), "old")
	before := snapshot(t, f)
	for _, capBytes := range []int64{0, 1, 3} {
		d := draft("owned")
		d.Max = capBytes
		r := &hooked{r: strings.NewReader("0123456789"), hook: func() {}}
		_, _, e := f.s.Put(ctx, d, r)
		if !errors.Is(e, store.ErrTooLarge) || int64(r.n) > capBytes+1 {
			t.Fatal(e, r.n)
		}
		if !reflect.DeepEqual(before, snapshot(t, f)) {
			t.Fatal("files")
		}
		got, _ := f.s.Get(ctx, "owned")
		equal(t, got, a)
	}
	d := draft("exact")
	d.Max = 3
	if b := put(t, f, d, "abc"); b.Bytes != 3 {
		t.Fatal(b)
	}
	d.Path = "unbounded"
	d.Max = math.MaxInt64
	if b := put(t, f, d, "abcde"); b.Bytes != 5 || b.SHA256 != hash("abcde") {
		t.Fatal(b)
	}
	bodyErr := errors.New("input broke")
	for _, cancelled := range []bool{false, true} {
		c, cancel := context.WithCancel(ctx)
		r := &hooked{r: &failure{err: bodyErr}, hook: func() {
			if cancelled {
				cancel()
			}
		}}
		_, _, e := f.s.Put(c, draft("owned"), r)
		cancel()
		if !errors.Is(e, store.ErrBody) || !errors.Is(e, bodyErr) || errors.Is(e, store.ErrUnreachable) {
			t.Fatal(e)
		}
		got, _ := f.s.Get(ctx, "owned")
		equal(t, got, a)
	}
	invariant(t, f)
}

// R-0JW8-OBQ8 R-0G8J-J0I5 R-0YJ1-9KMK
func TestRaces(t *testing.T) {
	f := setup(t, nil)
	const n = 8
	start := make(chan struct{})
	type result struct {
		a store.Artifact
		c bool
		e error
	}
	out := make(chan result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			a, c, e := f.s.Put(ctx, draft("shared"), strings.NewReader(strings.Repeat("x", i+1)))
			out <- result{a, c, e}
		})
	}
	close(start)
	wg.Wait()
	close(out)
	id := ""
	creates := 0
	for r := range out {
		if r.e != nil {
			t.Fatal(r.e)
		}
		if r.c {
			creates++
		}
		if id != "" && id != r.a.ID {
			t.Fatal("ids")
		}
		id = r.a.ID
	}
	if creates != 1 {
		t.Fatal(creates)
	}
	a, e := f.s.Get(ctx, "shared")
	if e != nil || a.Bytes < 1 || a.Bytes > n || a.SHA256 != hash(strings.Repeat("x", int(a.Bytes))) {
		t.Fatal(a, e)
	}
	r := &hooked{r: strings.NewReader("loser"), hook: func() { d := draft("race"); d.OwnerID = "bob"; put(t, f, d, "winner") }}
	_, _, e = f.s.Put(ctx, draft("race"), r)
	if !errors.Is(e, store.ErrForbidden) || read(t, f.s, "race") != "winner" {
		t.Fatal(e)
	}
	invariant(t, f)
}

// R-0L45-23GX R-0MC1-FV7M
func TestListAndLookup(t *testing.T) {
	f := setup(t, nil)
	empty, e := f.s.List(ctx)
	if e != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, e)
	}
	var want []store.Artifact
	for _, p := range []string{"z", "a", "A"} {
		d := draft(p)
		d.OwnerID = p
		want = append(want, put(t, f, d, p))
	}
	aa, e := f.s.List(ctx)
	if e != nil || len(aa) != 3 {
		t.Fatal(aa, e)
	}
	equal(t, aa[0], want[2])
	equal(t, aa[1], want[1])
	equal(t, aa[2], want[0])
	for _, a := range aa {
		b, e := f.s.Get(ctx, a.Path)
		if e != nil {
			t.Fatal(e)
		}
		equal(t, b, a)
		b, e = f.s.GetID(ctx, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		equal(t, b, a)
	}
	for _, p := range []string{".", "bad//path"} {
		_, e = f.s.Get(ctx, p)
		if !errors.Is(e, store.ErrInvalid) {
			t.Fatal(e)
		}
	}
	_, e = f.s.Get(ctx, "absent")
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	for _, id := range []string{"", "garbage", store.IDPrefix + strings.Repeat("f", 16)} {
		_, e = f.s.GetID(ctx, id)
		if !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
}

// R-G5RW-BU4D R-FZOE-EZEW R-4O1P-M05N
func TestOpenAndDelete(t *testing.T) {
	f := setup(t, nil)
	a := put(t, f, draft("one"), "original")
	other := put(t, f, draft("other"), "other")
	b, file, e := f.s.Open(ctx, "one")
	if e != nil || file == nil {
		t.Fatal(e)
	}
	equal(t, b, a)
	if off, e := file.Seek(0, io.SeekCurrent); e != nil || off != 0 {
		t.Fatal(off, e)
	}
	put(t, f, draft("one"), "replacement")
	content, e := io.ReadAll(file)
	_ = file.Close()
	if e != nil || string(content) != "original" {
		t.Fatal(e)
	}
	b, file, e = f.s.Open(ctx, "one")
	if e != nil {
		t.Fatal(e)
	}
	gone, e := f.s.Delete(ctx, "one", "alice")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, gone, b)
	content, e = io.ReadAll(file)
	_ = file.Close()
	if e != nil || string(content) != "replacement" {
		t.Fatal(e)
	}
	_, e = f.s.Get(ctx, "one")
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	_, e = f.s.GetID(ctx, a.ID)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	b, e = f.s.Get(ctx, "other")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, b, other)
	fresh, c, e := f.s.Put(ctx, draft("one"), strings.NewReader("again"))
	if e != nil || !c || fresh.ID == a.ID {
		t.Fatal(fresh, c, e)
	}
	for _, tc := range []struct {
		path string
		want error
	}{
		{"bad//path", store.ErrInvalid},
		{"absent", store.ErrNotFound},
	} {
		_, file, e := f.s.Open(ctx, tc.path)
		if !errors.Is(e, tc.want) || file != nil {
			t.Fatalf("Open(%q): file %v error %v; want nil file and %v", tc.path, file, e, tc.want)
		}
	}
	invariant(t, f)
}

// R-0SFJ-CPX3 R-4O1P-M05N
func TestSweep(t *testing.T) {
	f := setup(t, nil)
	var expected []store.Artifact
	for _, p := range []string{"z", "A", "later", "forever"} {
		d := draft(p)
		if p != "forever" {
			d.Expires = f.now.Add(time.Hour)
			if p == "later" {
				d.Expires = d.Expires.Add(time.Second)
			}
		}
		a := put(t, f, d, p)
		switch p {
		case "A":
			expected = append([]store.Artifact{a}, expected...)
		case "z":
			expected = append(expected, a)
		}
	}
	f.now = f.now.Add(time.Hour)
	if read(t, f.s, "A") != "A" {
		t.Fatal("hidden expired")
	}
	gone, e := f.s.Sweep(ctx, f.now)
	if e != nil || !reflect.DeepEqual(gone, expected) {
		t.Fatal(gone, e)
	}
	aa, e := f.s.List(ctx)
	if e != nil || len(aa) != 2 || aa[0].Path != "forever" || aa[1].Path != "later" {
		t.Fatal(aa, e)
	}
	gone, e = f.s.Sweep(ctx, f.now)
	if e != nil || gone == nil || len(gone) != 0 {
		t.Fatal(gone, e)
	}
	invariant(t, f)
}

// R-0TNF-QHNS
func TestReconcile(t *testing.T) {
	f := setup(t, nil)
	keep := put(t, f, draft("keep"), "keep")
	missing := put(t, f, draft("A-missing"), "remove")
	directory := put(t, f, draft("z-dir"), "dirbytes")
	entries, e := os.ReadDir(f.blobs)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		p := filepath.Join(f.blobs, entry.Name())
		b, e := os.ReadFile(filepath.Clean(p))
		if e != nil {
			t.Fatal(e)
		}
		if string(b) == "remove" || string(b) == "dirbytes" {
			if e = os.Remove(p); e != nil {
				t.Fatal(e)
			}
		}
		if string(b) == "dirbytes" {
			if e = os.Mkdir(p, 0700); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = os.WriteFile(filepath.Join(f.blobs, "orphan"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(f.blobs, "orphan-dir", "nested"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink("absent", filepath.Join(f.blobs, "orphan-link")); e != nil {
		t.Fatal(e)
	}
	gone, e := f.s.Reconcile(ctx)
	if e != nil || !reflect.DeepEqual(gone, []store.Artifact{missing, directory}) {
		t.Fatal(gone, e)
	}
	a, e := f.s.Get(ctx, "keep")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a, keep)
	if read(t, f.s, "keep") != "keep" {
		t.Fatal("bytes")
	}
	gone, e = f.s.Reconcile(ctx)
	if e != nil || gone == nil || len(gone) != 0 {
		t.Fatal(gone, e)
	}
	invariant(t, f)
}

// R-FSO1-3ZKG R-FTVX-HRB5 R-4O1P-M05N
func TestUnavailableAndPrecedence(t *testing.T) {
	for _, condition := range []string{"done", "failing"} {
		t.Run(condition, func(t *testing.T) {
			f := setup(t, nil)
			a := put(t, f, draft("owned"), "old")
			before := snapshot(t, f)
			c := ctx
			if condition == "done" {
				done, cancel := context.WithCancel(ctx)
				cancel()
				c = done
			} else {
				f.d.SetFailing(true)
			}
			calls := []func() error{
				func() error { _, e := f.s.List(c); return e }, func() error { _, e := f.s.Get(c, "bad//path"); return e }, func() error { _, e := f.s.GetID(c, "absent"); return e }, func() error {
					_, file, e := f.s.Open(c, "bad//path")
					if file != nil {
						t.Fatal("non-nil file")
					}
					return e
				}, func() error {
					_, _, e := f.s.Put(c, store.Draft{Path: "bad//path", Max: -1}, &failure{err: errors.New("body")})
					return e
				}, func() error { _, e := f.s.Delete(c, "bad//path", "bob"); return e }, func() error { _, e := f.s.Sweep(c, f.now.Add(time.Hour)); return e }, func() error { _, e := f.s.Reconcile(c); return e },
			}
			for _, call := range calls {
				if e := call(); !errors.Is(e, store.ErrUnreachable) {
					t.Fatal(e)
				}
			}
			f.d.SetFailing(false)
			got, e := f.s.Get(ctx, "owned")
			if e != nil {
				t.Fatal(e)
			}
			equal(t, got, a)
			if !reflect.DeepEqual(before, snapshot(t, f)) {
				t.Fatal("files changed")
			}
			invariant(t, f)
		})
	}
	for _, condition := range []string{"cancel", "fail", "random"} {
		t.Run(condition, func(t *testing.T) {
			f := setup(t, nil)
			a := put(t, f, draft("owned"), "old")
			before := snapshot(t, f)
			c, cancel := context.WithCancel(ctx)
			defer cancel()
			s := f.s
			d := draft("owned")
			if condition == "random" {
				d.Path = "new"
				s = store.New(f.d, f.blobs, store.Config{Now: func() time.Time { return f.now }, Rand: &failure{err: errors.New("random broke")}})
			}
			r := &hooked{r: strings.NewReader("new"), hook: func() {
				switch condition {
				case "cancel":
					cancel()
				case "fail":
					f.d.SetFailing(true)
				}
			}}
			_, _, e := s.Put(c, d, r)
			if !errors.Is(e, store.ErrUnreachable) {
				t.Fatal(e)
			}
			f.d.SetFailing(false)
			got, e := f.s.Get(ctx, "owned")
			if e != nil {
				t.Fatal(e)
			}
			equal(t, got, a)
			if !reflect.DeepEqual(before, snapshot(t, f)) {
				t.Fatal("files changed")
			}
			invariant(t, f)
		})
	}
	t.Run("missing directory", func(t *testing.T) {
		f := setup(t, nil)
		if e := os.Remove(f.blobs); e != nil {
			t.Fatal(e)
		}
		r := &failure{err: errors.New("body")}
		_, _, e := f.s.Put(ctx, draft("new"), r)
		if !errors.Is(e, store.ErrUnreachable) || r.calls != 0 {
			t.Fatal(e, r.calls)
		}
		aa, e := f.s.List(ctx)
		if e != nil || len(aa) != 0 {
			t.Fatal(aa, e)
		}
		if _, e = os.Stat(f.blobs); !errors.Is(e, os.ErrNotExist) {
			t.Fatal(e)
		}
	})
	t.Run("argument expiry owner", func(t *testing.T) {
		f := setup(t, nil)
		put(t, f, draft("owned"), "old")
		d := draft("owned")
		d.OwnerID = "bob"
		d.Expires = f.now
		d.Max = -1
		_, _, e := f.s.Put(ctx, d, &failure{})
		if !errors.Is(e, store.ErrInvalid) {
			t.Fatal(e)
		}
		d.Max = 1
		_, _, e = f.s.Put(ctx, d, &failure{})
		if !errors.Is(e, store.ErrPastExpiry) {
			t.Fatal(e)
		}
		d.Expires = time.Time{}
		_, _, e = f.s.Put(ctx, d, &failure{})
		if !errors.Is(e, store.ErrForbidden) {
			t.Fatal(e)
		}
	})
	t.Run("body precedes cancel", func(t *testing.T) {
		f := setup(t, nil)
		c, cancel := context.WithCancel(ctx)
		d := draft("one")
		d.Max = 0
		r := &hooked{r: strings.NewReader("x"), hook: cancel}
		_, _, e := f.s.Put(c, d, r)
		if !errors.Is(e, store.ErrTooLarge) {
			t.Fatal(e)
		}
		invariant(t, f)
	})
}

type resultReader struct {
	data string
	err  error
}

func (r *resultReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, r.err
}

// R-FTVX-HRB5 R-G247-6IWA R-FSO1-3ZKG
func TestPrecedenceDuringStreaming(t *testing.T) {
	for _, tc := range []struct {
		name string
		max  int64
		data string
		want error
	}{{"past cap first", 0, "x", store.ErrTooLarge}, {"body error first", 3, "x", store.ErrBody}} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, nil)
			c, cancel := context.WithCancel(ctx)
			boom := errors.New("broken body")
			d := draft("one")
			d.Max = tc.max
			r := &hooked{r: &resultReader{data: tc.data, err: boom}, hook: cancel}
			_, _, e := f.s.Put(c, d, r)
			if !errors.Is(e, tc.want) || errors.Is(e, store.ErrUnreachable) {
				t.Fatal(e)
			}
			invariant(t, f)
		})
	}
	for _, mode := range []string{"cancel", "failing"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t, nil)
			rnd := &failure{err: errors.New("random")}
			s := store.New(f.d, f.blobs, store.Config{Now: func() time.Time { return f.now }, Rand: rnd})
			c, cancel := context.WithCancel(ctx)
			defer cancel()
			r := &hooked{r: strings.NewReader("body"), hook: func() {
				if mode == "cancel" {
					cancel()
				} else {
					f.d.SetFailing(true)
				}
			}}
			_, _, e := s.Put(c, draft("one"), r)
			if !errors.Is(e, store.ErrUnreachable) || rnd.calls != 0 {
				t.Fatal(e, rnd.calls)
			}
			f.d.SetFailing(false)
			invariant(t, f)
		})
	}
	f := setup(t, nil)
	f.d.SetFailing(true)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, e := f.s.Get(c, "bad//path")
	if !errors.Is(e, context.Canceled) || !errors.Is(e, store.ErrUnreachable) {
		t.Fatal(e)
	}
}

// R-ZZ5Y-684F
func TestDefaultSourcesByUse(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cfg        store.Config
		fixedClock bool
	}{
		{name: "nil clock", cfg: store.Config{Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))}},
		{name: "nil random", cfg: store.Config{Now: func() time.Time { return epoch }}, fixedClock: true},
		{name: "both nil", cfg: store.Config{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t, nil)
			s := store.New(f.d, f.blobs, tc.cfg)
			a, created, e := s.Put(ctx, draft("defaults"), strings.NewReader("default sources"))
			if e != nil || !created || !store.ValidID(a.ID) {
				t.Fatalf("create: %+v %v %v", a, created, e)
			}
			if a.Created.IsZero() || a.Modified.IsZero() || !a.Created.Equal(a.Modified) || a.Created.Location() != time.UTC || a.Modified.Location() != time.UTC || a.Created.Nanosecond() != 0 || a.Modified.Nanosecond() != 0 {
				t.Fatalf("timestamps: %+v", a)
			}
			if tc.fixedClock && !a.Created.Equal(utc(epoch)) {
				t.Fatalf("injected clock: %v", a.Created)
			}
			if tc.name == "nil clock" && a.ID != store.IDPrefix+strings.Repeat("5a", 8) {
				t.Fatal("injected random", a.ID)
			}
			got, e := s.Get(ctx, a.Path)
			if e != nil {
				t.Fatal(e)
			}
			equal(t, got, a)
			if read(t, s, a.Path) != "default sources" {
				t.Fatal("stored body")
			}
			invariant(t, f)
		})
	}
}
