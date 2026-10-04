package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/store"
)

var ctx = context.Background()
var stamp = time.Date(2026, 10, 1, 12, 34, 56, 987654321, time.FixedZone("test", 3600))

type counter struct{ n byte }

type failedReader struct{ err error }

func (r failedReader) Read(_ []byte) (int, error) { return 0, r.err }

type trackedReader struct {
	reader *bytes.Reader
	calls  int
}

func (r *trackedReader) Read(p []byte) (int, error) { r.calls++; return r.reader.Read(p) }

func (r *counter) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}
func open(t *testing.T, c store.Config) *store.Store {
	t.Helper()
	if c.Now == nil {
		c.Now = func() time.Time { return stamp }
	}
	if c.Rand == nil {
		c.Rand = &counter{}
	}
	s, e := store.Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func draft(name string) store.Draft {
	return store.Draft{Owner: "alice", Name: name, Repo: "repository", Ref: "main", Visibility: store.Public, Listed: true}
}
func create(t *testing.T, s *store.Store, d store.Draft) store.Site {
	t.Helper()
	x, e := s.Create(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func equal(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v; want %#v", a, b)
	}
}
func checkErr(t *testing.T, e, w error) {
	t.Helper()
	if !errors.Is(e, w) {
		t.Fatalf("got %v; want %v", e, w)
	}
}

// R-19Q1-XPV3
func catalog(t *testing.T, e error) {
	t.Helper()
	if e == nil || errors.Is(e, store.ErrNotFound) || errors.Is(e, store.ErrNameTaken) || errors.Is(e, store.ErrNotPublic) {
		t.Fatalf("not a catalog error: %v", e)
	}
}

type content struct {
	Alice, Bob []store.Site
	Apex       store.Site
	Set        bool
}

func snapshot(t *testing.T, s *store.Store) content {
	t.Helper()
	a, e := s.List(ctx, "alice")
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.List(ctx, "bob")
	if e != nil {
		t.Fatal(e)
	}
	x, set, e := s.Apex(ctx)
	if e != nil {
		t.Fatal(e)
	}
	return content{a, b, x, set}
}

func TestDeclarationsAndValidation(t *testing.T) {
	// R-0V39-CGYR R-0WB5-Q8PG R-0XJ2-40G5 R-0YQY-HS6U R-0ZYU-VJXJ R-116R-9BO8 R-12EN-N3EX R-AN8M-G38O
	c := store.Config{Source: ":memory:", Now: func() time.Time { return stamp }, Rand: &counter{}}
	s := open(t, c)
	if s == nil {
		t.Fatal("nil store")
	}
	v, l, r := store.Private, false, "next"
	_ = store.Change{Visibility: &v, Listed: &l, Ref: &r}
	x := store.Site{ID: "id", Name: "name", Slug: "slug", Owner: "owner", Repo: "repo", Ref: "ref", Visibility: "visibility", Listed: true, Commit: "commit", Created: stamp, Published: stamp}
	equal(t, x.Name, "name")
	equal(t, store.IDPrefix, "sit_")
	equal(t, store.Public, "public")
	equal(t, store.Private, "private")
	equal(t, store.Unreachable, "cannot reach the catalog; try again later")
	sentinels := []error{store.ErrDatabase, store.ErrNotFound, store.ErrNameTaken, store.ErrNotPublic}
	for i, e := range sentinels {
		if e == nil {
			t.Fatal("nil sentinel")
		}
		for j, f := range sentinels {
			if i != j && errors.Is(e, f) {
				t.Fatal("sentinels overlap")
			}
		}
	}
	// R-1AXY-BHLS
	for _, v := range []string{"sit_0123456789abcdef", "sit_0000000000000000"} {
		if !store.ValidID(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", "sit_0123456789abcde", "sit_0123456789abcdef0", "SIT_0123456789abcdef", "sit_0123456789abcdeF", "sit_0123456789abcdeg", "sit_0123456789abcdé"} {
		if store.ValidID(v) {
			t.Fatal(v)
		}
	}
	// R-1C5U-P9CH
	for _, v := range []string{"a", "0", "a-", "about-us", "mcp-notes", strings.Repeat("a", 64)} {
		if !store.ValidName(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", "about", "mcp", "Docs", " docs", "-docs", strings.Repeat("a", 65), "é", "a/b", "a_b", "a\x00"} {
		if store.ValidName(v) {
			t.Fatal(v)
		}
	}
	for b := 0; b < 256; b++ {
		name := "x" + string([]byte{byte(b)})
		want := b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-'
		equal(t, store.ValidName(name), want)
		id := "sit_000000000000000" + string([]byte{byte(b)})
		equal(t, store.ValidID(id), b >= '0' && b <= '9' || b >= 'a' && b <= 'f')
	}
}

func TestMemoryAndEmptyCatalog(t *testing.T) {
	// R-1DDR-3136 R-1ELN-GSTV
	dir := t.TempDir()
	t.Chdir(dir)
	for _, source := range []string{"", ":memory:", filepath.Join("nested", "state", "sites.db"), "zero.db"} {
		t.Run(fmt.Sprintf("source%q", source), func(t *testing.T) {
			if source == "zero.db" {
				if e := os.WriteFile(source, nil, 0600); e != nil {
					t.Fatal(e)
				}
			}
			s := open(t, store.Config{Source: source})
			a, e := s.List(ctx, "alice")
			equal(t, e, nil)
			equal(t, a, []store.Site{})
			a, e = s.Visible(ctx, "bob")
			equal(t, e, nil)
			equal(t, a, []store.Site{})
			taken, e := s.Taken(ctx, "orphan")
			equal(t, e, nil)
			equal(t, taken, false)
			x, set, e := s.Apex(ctx)
			equal(t, e, nil)
			equal(t, set, false)
			equal(t, x, store.Site{})
			x = create(t, s, draft("docs"))
			y, e := s.BySlug(ctx, x.Slug)
			equal(t, e, nil)
			equal(t, y, x)
			if source == "" || source == ":memory:" {
				if _, e = os.Stat(":memory:"); !os.IsNotExist(e) {
					t.Fatal("memory path exists")
				}
			}
		})
	}
	info, e := os.Stat(filepath.Join(dir, "nested"))
	if e != nil {
		t.Fatal(e)
	}
	equal(t, info.Mode().Perm(), os.FileMode(0700))
	info, e = os.Stat(filepath.Join(dir, "nested", "state"))
	if e != nil {
		t.Fatal(e)
	}
	equal(t, info.Mode().Perm(), os.FileMode(0700))
	existing := filepath.Join(dir, "existing")
	if e = os.Mkdir(existing, 0750); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(existing, "orphan"), []byte("unrelated content"), 0600); e != nil {
		t.Fatal(e)
	}
	existingStore := open(t, store.Config{Source: filepath.Join(existing, "db")})
	assertEmpty(t, existingStore)
	info, e = os.Stat(existing)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, info.Mode().Perm(), os.FileMode(0750))
}
func assertEmpty(t *testing.T, s *store.Store) {
	t.Helper()
	for _, owner := range []string{"alice", "bob", ""} {
		xs, e := s.List(ctx, owner)
		equal(t, e, nil)
		equal(t, xs, []store.Site{})
		xs, e = s.Visible(ctx, owner)
		equal(t, e, nil)
		equal(t, xs, []store.Site{})
	}
	for _, name := range []string{"orphan", "docs", ""} {
		taken, e := s.Taken(ctx, name)
		equal(t, e, nil)
		equal(t, taken, false)
	}
	a, set, e := s.Apex(ctx)
	equal(t, e, nil)
	equal(t, set, false)
	equal(t, a, store.Site{})
}
func wal(t *testing.T, path string) {
	t.Helper()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Close() }()
	var mode string
	if e = db.QueryRow("PRAGMA journal_mode").Scan(&mode); e != nil {
		t.Fatal(e)
	}
	equal(t, mode, "wal")
}
func TestPersistenceAndWAL(t *testing.T) {
	// R-1FTJ-UKKK R-I173-6PUZ R-1KP5-DNJC R-2K28-H01H
	path := filepath.Join(t.TempDir(), "state", "db")
	s := open(t, store.Config{Source: path})
	x := create(t, s, draft("docs"))
	equal(t, x.Name, "docs")
	equal(t, x.Owner, "alice")
	equal(t, x.Repo, "repository")
	equal(t, x.Ref, "main")
	equal(t, x.Visibility, store.Public)
	equal(t, x.Listed, true)
	equal(t, x.Slug, "docs")
	equal(t, x.Commit, "")
	equal(t, x.Published, time.Time{})
	equal(t, x.Created, stamp.UTC().Truncate(time.Second))
	if !store.ValidID(x.ID) {
		t.Fatal(x.ID)
	}
	y, e := s.Publish(ctx, x.ID, strings.Repeat("a", 40))
	equal(t, e, nil)
	_, e = s.SetApex(ctx, x.ID)
	equal(t, e, nil)
	d := draft("draft")
	d.Listed = false
	d.Owner = "bob"
	d.Visibility = store.Private
	z := create(t, s, d)
	if z.ID == x.ID {
		t.Fatal("duplicate id")
	}
	before := snapshot(t, s)
	wal(t, path)
	equal(t, s.Close(), nil)
	wal(t, path)
	s = open(t, store.Config{Source: path})
	equal(t, snapshot(t, s), before)
	for _, expected := range []store.Site{y, z} {
		a, e := s.Find(ctx, expected.Owner, expected.Name)
		equal(t, e, nil)
		equal(t, a, expected)
		a, e = s.BySlug(ctx, expected.Slug)
		equal(t, e, nil)
		equal(t, a, expected)
		equal(t, a.Created.Location(), time.UTC)
		equal(t, a.Created.Nanosecond(), 0)
		if !a.Published.IsZero() {
			equal(t, a.Published.Location(), time.UTC)
			equal(t, a.Published.Nanosecond(), 0)
		}
	}
}
func TestOpenFailures(t *testing.T) {
	// R-1H1G-8CB9 R-1JH8-ZVSN R-I2EZ-KHLO
	root := t.TempDir()
	block := filepath.Join(root, "block")
	bad := filepath.Join(root, "bad")
	for _, p := range []string{block, bad} {
		if e := os.WriteFile(p, []byte("not sqlite"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, path := range []string{filepath.Join(block, "child", "db"), bad} {
		s, e := store.Open(ctx, store.Config{Source: path})
		equal(t, s, (*store.Store)(nil))
		checkErr(t, e, store.ErrDatabase)
		if e.Error() == "" || strings.Contains(e.Error(), "\n") {
			t.Fatal(e)
		}
		if path != bad {
			expected := os.MkdirAll(filepath.Dir(path), 0700)
			equal(t, e.Error(), expected.Error())
		}
		for _, p := range []string{block, bad} {
			b, err := os.ReadFile(filepath.Clean(p))
			equal(t, err, nil)
			equal(t, string(b), "not sqlite")
			info, err := os.Stat(p)
			equal(t, err, nil)
			equal(t, info.Mode().Perm(), os.FileMode(0600))
		}
	}
	path := filepath.Join(root, "readonly", "db")
	s := open(t, store.Config{Source: path})
	equal(t, s.Close(), nil)
	original, e := os.ReadFile(filepath.Clean(path))
	equal(t, e, nil)
	for _, target := range []string{path, filepath.Dir(path)} {
		info, err := os.Stat(target)
		equal(t, err, nil)
		mode := info.Mode().Perm()
		if err = os.Chmod(target, mode&^0222); err != nil {
			t.Fatal(err)
		}
		s, err = store.Open(ctx, store.Config{Source: path})
		fileInfo, statErr := os.Stat(path)
		equal(t, statErr, nil)
		expectedMode := os.FileMode(0600)
		if target == path {
			expectedMode = mode &^ 0222
		}
		equal(t, fileInfo.Mode().Perm(), expectedMode)
		restore := os.Chmod(target, mode)
		equal(t, restore, nil)
		equal(t, s, (*store.Store)(nil))
		checkErr(t, err, store.ErrDatabase)
		b, err := os.ReadFile(filepath.Clean(path))
		equal(t, err, nil)
		equal(t, b, original)
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	for _, source := range []string{bad, filepath.Join(root, "absent", "db")} {
		s, e = store.Open(done, store.Config{Source: source})
		equal(t, s, (*store.Store)(nil))
		checkErr(t, e, context.Canceled)
	}
	if _, e = os.Stat(filepath.Join(root, "absent", "db")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

func TestRandomCandidates(t *testing.T) {
	// R-1LX1-RFA1 R-1N4Y-570Q R-UEZD-QS32 R-1QSN-AI8T
	stream := &trackedReader{reader: bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8, 14, 91, 124, 41})}
	s := open(t, store.Config{Rand: stream})
	d := draft("draft")
	d.Listed = false
	x := create(t, s, d)
	equal(t, x.ID, "sit_0102030405060708")
	equal(t, x.Slug, "draft-0e5b7c29")
	equal(t, stream.reader.Len(), 0)
	equal(t, stream.calls, 2)
	before := snapshot(t, s)
	for _, name := range []string{x.Name, x.Slug} {
		d.Name = name
		d.Owner = "bob"
		got, e := s.Create(ctx, d)
		equal(t, got, store.Site{})
		checkErr(t, e, store.ErrNameTaken)
		equal(t, snapshot(t, s), before)
		equal(t, stream.calls, 2)
	}
	invalid := []store.Draft{draft("about"), draft("docs"), draft("docs"), draft("draft")}
	invalid[1].Owner = ""
	invalid[2].Ref = ""
	invalid[3].Visibility = "other"
	for _, d := range invalid {
		got, e := s.Create(ctx, d)
		equal(t, got, store.Site{})
		catalog(t, e)
		equal(t, snapshot(t, s), before)
		equal(t, stream.calls, 2)
	}
	for _, b := range [][]byte{nil, {0, 0, 0}, {1, 2, 3, 4, 5, 6, 7, 8}} {
		q := open(t, store.Config{Rand: bytes.NewReader(b)})
		d := draft("short")
		d.Listed = false
		before := snapshot(t, q)
		got, e := q.Create(ctx, d)
		equal(t, got, store.Site{})
		catalog(t, e)
		equal(t, snapshot(t, q), before)
	}
	for _, failure := range []error{store.ErrNotFound, store.ErrNameTaken, store.ErrNotPublic} {
		for _, prefix := range [][]byte{nil, {1, 2, 3, 4, 5, 6, 7, 8}} {
			q := open(t, store.Config{Rand: io.MultiReader(bytes.NewReader(prefix), failedReader{failure})})
			before := snapshot(t, q)
			d := draft("broken")
			d.Listed = false
			got, e := q.Create(ctx, d)
			equal(t, got, store.Site{})
			catalog(t, e)
			equal(t, snapshot(t, q), before)
		}
	}
	// Both an ID collision and a slug collision must consume the next candidate.
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	b = append(b, b...)
	b = append(b, 9, 10, 11, 12, 13, 14, 15, 16, 14, 91, 124, 41, 0, 0, 0, 2)
	q := open(t, store.Config{Rand: bytes.NewReader(b)})
	_ = create(t, q, draft("x-0e5b7c29"))
	d = draft("x")
	d.Listed = false
	got := create(t, q, d)
	equal(t, got.ID, "sit_090a0b0c0d0e0f10")
	equal(t, got.Slug, "x-00000002")
}

func TestReadSelection(t *testing.T) {
	// R-162C-SEN0 R-1T8G-21Q7 R-1UGC-FTGW R-1VO8-TL7L R-1WW5-7CYA R-1Y41-L4OZ
	s := open(t, store.Config{})
	a := create(t, s, draft("z-last"))
	d := draft("a-hidden")
	d.Listed = false
	b := create(t, s, d)
	d = draft("b-private")
	d.Owner = "bob"
	d.Visibility = store.Private
	c := create(t, s, d)
	d = draft("c-secret")
	d.Owner = "bob"
	d.Listed = false
	secret := create(t, s, d)
	got, e := s.Find(ctx, a.Owner, a.Name)
	equal(t, e, nil)
	equal(t, got, a)
	for _, pair := range [][2]string{{"bob", a.Name}, {"alice", b.Slug}, {"alice", a.ID}, {"", a.Name}, {"alice", ""}} {
		got, e = s.Find(ctx, pair[0], pair[1])
		equal(t, got, store.Site{})
		checkErr(t, e, store.ErrNotFound)
	}
	for _, x := range []store.Site{a, b, c, secret} {
		got, e = s.BySlug(ctx, x.Slug)
		equal(t, e, nil)
		equal(t, got, x)
	}
	for _, slug := range []string{b.Name, "missing", strings.ToUpper(a.Slug), ""} {
		got, e = s.BySlug(ctx, slug)
		equal(t, got, store.Site{})
		checkErr(t, e, store.ErrNotFound)
	}
	xs, e := s.List(ctx, "alice")
	equal(t, e, nil)
	equal(t, xs, []store.Site{b, a})
	xs, e = s.List(ctx, "nobody")
	equal(t, e, nil)
	equal(t, xs, []store.Site{})
	xs, e = s.Visible(ctx, "alice")
	equal(t, e, nil)
	equal(t, xs, []store.Site{b, c, a})
	xs, e = s.Visible(ctx, "bob")
	equal(t, e, nil)
	equal(t, xs, []store.Site{c, secret, a})
	xs, e = s.Visible(ctx, "")
	equal(t, e, nil)
	equal(t, xs, []store.Site{c, a})
	for _, name := range []string{a.Name, b.Name, b.Slug, secret.Slug} {
		taken, e := s.Taken(ctx, name)
		equal(t, e, nil)
		equal(t, taken, true)
	}
	taken, e := s.Taken(ctx, "missing")
	equal(t, e, nil)
	equal(t, taken, false)
}

func TestPublishUpdateAndApex(t *testing.T) {
	// R-17A9-66DP R-18I5-JY4E R-1ZBX-YWFO R-20JU-CO6D R-22ZN-47NR R-247J-HZEG R-2AB1-EU3X R-2BIX-SLUM R-AOGI-TUZD R-2CQU-6DLB
	now := stamp
	s := open(t, store.Config{Now: func() time.Time { return now }})
	x := create(t, s, draft("docs"))
	d := draft("private")
	d.Visibility = store.Private
	p := create(t, s, d)
	d = draft("other")
	d.Owner = "bob"
	other := create(t, s, d)
	before := snapshot(t, s)
	for _, id := range []string{"missing", p.ID} {
		got, e := s.SetApex(ctx, id)
		equal(t, got, store.Site{})
		if id == p.ID {
			checkErr(t, e, store.ErrNotPublic)
		} else {
			checkErr(t, e, store.ErrNotFound)
		}
		equal(t, snapshot(t, s), before)
	}
	for _, id := range []string{x.ID, x.ID, other.ID, x.ID} {
		got, e := s.SetApex(ctx, id)
		equal(t, e, nil)
		a, set, e := s.Apex(ctx)
		equal(t, e, nil)
		equal(t, set, true)
		equal(t, a, got)
		after := snapshot(t, s)
		equal(t, after.Alice, before.Alice)
		equal(t, after.Bob, before.Bob)
	}
	for _, commit := range []string{strings.Repeat("a", 40), strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		now = now.Add(time.Second)
		got, e := s.Publish(ctx, x.ID, commit)
		equal(t, e, nil)
		expected := x
		expected.Commit = commit
		expected.Published = now.UTC().Truncate(time.Second)
		equal(t, got, expected)
		x = got
		a, e := s.Find(ctx, x.Owner, x.Name)
		equal(t, e, nil)
		equal(t, a, x)
		a, e = s.BySlug(ctx, x.Slug)
		equal(t, e, nil)
		equal(t, a, x)
		xs, e := s.List(ctx, "alice")
		equal(t, e, nil)
		equal(t, xs[0], x)
		xs, e = s.Visible(ctx, "alice")
		equal(t, e, nil)
		equal(t, xs[0], x)
		a, set, e := s.Apex(ctx)
		equal(t, e, nil)
		equal(t, set, true)
		equal(t, a, x)
	}
	before = snapshot(t, s)
	for _, commit := range []string{"", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("G", 40)} {
		got, e := s.Publish(ctx, "missing", commit)
		equal(t, got, store.Site{})
		catalog(t, e)
		equal(t, snapshot(t, s), before)
	}
	got, e := s.Publish(ctx, "missing", strings.Repeat("a", 40))
	equal(t, got, store.Site{})
	checkErr(t, e, store.ErrNotFound)
	equal(t, snapshot(t, s), before)
	private, listed, ref := store.Private, false, "next"
	bad, empty := "other", ""
	for _, c := range []store.Change{{Visibility: &bad, Listed: &listed, Ref: &ref}, {Ref: &empty, Listed: &listed}, {Visibility: &private, Listed: &listed, Ref: &ref}} {
		got, fields, e := s.Update(ctx, x.ID, c)
		equal(t, got, store.Site{})
		equal(t, fields, []string(nil))
		if c.Visibility == &private {
			checkErr(t, e, store.ErrNotPublic)
		} else {
			catalog(t, e)
		}
		equal(t, snapshot(t, s), before)
	}
	got, fields, e := s.Update(ctx, "missing", store.Change{})
	equal(t, got, store.Site{})
	equal(t, fields, []string(nil))
	checkErr(t, e, store.ErrNotFound)
	equal(t, s.ClearApex(ctx), nil)
	equal(t, s.ClearApex(ctx), nil)
	a, set, e := s.Apex(ctx)
	equal(t, e, nil)
	equal(t, set, false)
	equal(t, a, store.Site{})
	after := snapshot(t, s)
	equal(t, after.Alice, before.Alice)
	equal(t, after.Bob, before.Bob)
	got, fields, e = s.Update(ctx, x.ID, store.Change{Visibility: &private, Listed: &listed, Ref: &ref})
	equal(t, e, nil)
	equal(t, fields, []string{"visibility", "listed", "ref"})
	expected := x
	expected.Visibility = private
	expected.Listed = listed
	expected.Ref = ref
	equal(t, got, expected)
	for _, c := range []store.Change{{}, {Visibility: &private, Listed: &listed, Ref: &ref}} {
		got, fields, e = s.Update(ctx, x.ID, c)
		equal(t, e, nil)
		equal(t, got, expected)
		equal(t, fields, []string{})
	}
}

func TestDelete(t *testing.T) {
	// R-25FF-VR55 R-27V8-NAMJ R-2935-12D8
	path := filepath.Join(t.TempDir(), "db")
	s := open(t, store.Config{Source: path})
	x := create(t, s, draft("docs"))
	other := create(t, s, draft("other"))
	d := draft("hidden")
	d.Listed = false
	hidden := create(t, s, d)
	d = draft("survivor")
	d.Owner = "bob"
	survivor := create(t, s, d)
	_, e := s.SetApex(ctx, x.ID)
	equal(t, e, nil)
	before := snapshot(t, s)
	was, e := s.Delete(ctx, "missing")
	equal(t, was, false)
	checkErr(t, e, store.ErrNotFound)
	equal(t, snapshot(t, s), before)
	was, e = s.Delete(ctx, other.ID)
	equal(t, e, nil)
	equal(t, was, false)
	a, set, e := s.Apex(ctx)
	equal(t, e, nil)
	equal(t, set, true)
	equal(t, a, x)
	was, e = s.Delete(ctx, x.ID)
	equal(t, e, nil)
	equal(t, was, true)
	was, e = s.Delete(ctx, hidden.ID)
	equal(t, e, nil)
	equal(t, was, false)
	for _, dead := range []store.Site{x, other, hidden} {
		assertDeleted(t, s, dead)
	}
	got, e := s.Find(ctx, survivor.Owner, survivor.Name)
	equal(t, e, nil)
	equal(t, got, survivor)
	_, set, e = s.Apex(ctx)
	equal(t, e, nil)
	equal(t, set, false)
	equal(t, s.Close(), nil)
	s = open(t, store.Config{Source: path})
	for _, dead := range []store.Site{x, other, hidden} {
		a, e := s.Find(ctx, dead.Owner, dead.Name)
		equal(t, a, store.Site{})
		checkErr(t, e, store.ErrNotFound)
		a, e = s.BySlug(ctx, dead.Slug)
		equal(t, a, store.Site{})
		checkErr(t, e, store.ErrNotFound)
		for _, name := range []string{dead.Name, dead.Slug} {
			taken, e := s.Taken(ctx, name)
			equal(t, e, nil)
			equal(t, taken, false)
		}
	}
	got, e = s.Find(ctx, survivor.Owner, survivor.Name)
	equal(t, e, nil)
	equal(t, got, survivor)
	d = draft(x.Name)
	d.Owner = "bob"
	_ = create(t, s, d)
}
func assertDeleted(t *testing.T, s *store.Store, dead store.Site) {
	t.Helper()
	a, e := s.Find(ctx, dead.Owner, dead.Name)
	equal(t, a, store.Site{})
	checkErr(t, e, store.ErrNotFound)
	a, e = s.BySlug(ctx, dead.Slug)
	equal(t, a, store.Site{})
	checkErr(t, e, store.ErrNotFound)
	for _, name := range []string{dead.Name, dead.Slug} {
		taken, e := s.Taken(ctx, name)
		equal(t, e, nil)
		equal(t, taken, false)
	}
}
func calls(c context.Context, s *store.Store) []error {
	_, a := s.Find(c, "alice", "docs")
	_, b := s.BySlug(c, "docs")
	_, d := s.List(c, "alice")
	_, e := s.Visible(c, "alice")
	_, f := s.Taken(c, "docs")
	_, g := s.Create(c, draft("new"))
	_, h := s.Publish(c, "missing", strings.Repeat("a", 40))
	_, _, i := s.Update(c, "missing", store.Change{})
	_, j := s.Delete(c, "missing")
	_, _, k := s.Apex(c)
	_, l := s.SetApex(c, "missing")
	m := s.ClearApex(c)
	return []error{a, b, d, e, f, g, h, i, j, k, l, m}
}
func TestContextCloseAndPrecedence(t *testing.T) {
	// R-UG7A-4JTR R-2HMF-PGK3 R-I2EZ-KHLO
	s := open(t, store.Config{})
	_ = create(t, s, draft("docs"))
	before := snapshot(t, s)
	done, cancel := context.WithCancel(ctx)
	cancel()
	for _, e := range calls(done, s) {
		catalog(t, e)
		checkErr(t, e, context.Canceled)
	}
	equal(t, snapshot(t, s), before)
	equal(t, s.Close(), nil)
	for _, e := range calls(ctx, s) {
		catalog(t, e)
	}
	for _, e := range calls(done, s) {
		checkErr(t, e, context.Canceled)
	}
	_, e := s.Create(ctx, store.Draft{})
	catalog(t, e)
	q := open(t, store.Config{Rand: bytes.NewReader(nil)})
	_, e = q.Create(ctx, draft("docs"))
	catalog(t, e)

	bad := "other"
	_, _, e = q.Update(ctx, "missing", store.Change{Visibility: &bad})
	catalog(t, e)
}

func TestConcurrentCreates(t *testing.T) {
	// R-1S0J-O9ZI R-UHF6-IBKG R-2IUC-38AS
	s := open(t, store.Config{})
	start := make(chan struct{})
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() { <-start; _, e := s.Create(ctx, draft("same")); errs <- e })
	}
	close(start)
	wg.Wait()
	close(errs)
	wins := 0
	for e := range errs {
		if e == nil {
			wins++
		} else {
			checkErr(t, e, store.ErrNameTaken)
		}
	}
	equal(t, wins, 1)
	xs, e := s.List(ctx, "alice")
	equal(t, e, nil)
	equal(t, len(xs), 1)
	// In either serialized order, a candidate slug cannot become another site's name.
	stream := bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8, 14, 91, 124, 41, 0, 0, 0, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 14, 91, 124, 41, 0, 0, 0, 9})
	q := open(t, store.Config{Rand: stream})
	wg.Go(func() {
		d := draft("x")
		d.Listed = false
		_, e := q.Create(ctx, d)
		if e != nil {
			t.Error(e)
		}
	})
	wg.Go(func() {
		d := draft("x-0e5b7c29")
		d.Owner = "bob"
		_, e := q.Create(ctx, d)
		if e != nil && !errors.Is(e, store.ErrNameTaken) {
			t.Error(e)
		}
	})
	wg.Wait()
	a, e := q.List(ctx, "alice")
	equal(t, e, nil)
	b, e := q.List(ctx, "bob")
	equal(t, e, nil)
	for _, x := range a {
		for _, y := range b {
			if x.Name == y.Name || x.Name == y.Slug || x.Slug == y.Name || x.Slug == y.Slug {
				t.Fatal("namespace overlap")
			}
		}
	}
}

func assertPair(t *testing.T, a store.Site) {
	t.Helper()
	if a.Commit != "" && a.Commit != fmt.Sprintf("%040x", a.Published.Unix()) {
		t.Error("mixed publish state")
	}
}

func TestConcurrentAtomicPublish(t *testing.T) {
	// R-21RQ-QFX2 R-2IUC-38AS
	var tick int64
	s := open(t, store.Config{Now: func() time.Time { tick++; return time.Unix(tick, 0) }})
	x := create(t, s, draft("docs"))
	_, e := s.SetApex(ctx, x.ID)
	equal(t, e, nil)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Go(func() {
		<-start
		for i := int64(2); i < 102; i++ {
			commit := fmt.Sprintf("%040x", i)
			_, e := s.Publish(ctx, x.ID, commit)
			if e != nil {
				t.Error(e)
			}
		}
	})
	for range 4 {
		wg.Go(func() {
			<-start
			for range 100 {
				a, e := s.Find(ctx, x.Owner, x.Name)
				if e != nil {
					t.Error(e)
					return
				}
				if a.Commit != "" && a.Commit != fmt.Sprintf("%040x", a.Published.Unix()) {
					t.Error("mixed publish state")
				}
				a, e = s.BySlug(ctx, x.Slug)
				if e != nil {
					t.Error(e)
				}
				assertPair(t, a)
				xs, e := s.List(ctx, x.Owner)
				if e != nil {
					t.Error(e)
				}
				for _, a := range xs {
					assertPair(t, a)
				}
				xs, e = s.Visible(ctx, x.Owner)
				if e != nil {
					t.Error(e)
				}
				for _, a := range xs {
					assertPair(t, a)
				}
				a, set, e := s.Apex(ctx)
				if e != nil {
					t.Error(e)
				}
				if set {
					assertPair(t, a)
				}
				_, e = s.Taken(ctx, x.Name)
				if e != nil {
					t.Error(e)
				}
			}
		})
	}
	close(start)
	wg.Wait()
}
func TestConcurrentPublicApex(t *testing.T) {
	// R-2DYQ-K5C0 R-2IUC-38AS
	for range 20 {
		s := open(t, store.Config{})
		x := create(t, s, draft("docs"))
		start := make(chan struct{})
		var wg sync.WaitGroup
		var apexErr, updateErr error
		wg.Go(func() { <-start; _, apexErr = s.SetApex(ctx, x.ID) })
		wg.Go(func() {
			<-start
			p := store.Private
			_, _, updateErr = s.Update(ctx, x.ID, store.Change{Visibility: &p})
		})
		close(start)
		wg.Wait()
		if apexErr == nil && updateErr == nil {
			t.Fatal("both changes succeeded")
		}
		a, set, e := s.Apex(ctx)
		equal(t, e, nil)
		if set {
			equal(t, a.Visibility, store.Public)
			found, e := s.BySlug(ctx, a.Slug)
			equal(t, e, nil)
			equal(t, found, a)
		}
		wg.Go(func() {
			if e := s.ClearApex(ctx); e != nil {
				t.Error(e)
			}
		})
		wg.Go(func() {
			if _, e := s.Delete(ctx, x.ID); e != nil {
				t.Error(e)
			}
		})
		wg.Wait()
		_, set, e = s.Apex(ctx)
		equal(t, e, nil)
		equal(t, set, false)
	}
}
