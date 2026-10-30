package bookmarks_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/home"
	"github.com/ikigenba/ikigenba/home/internal/bookmarks"
)

var clockValue = time.Date(2025, 2, 3, 4, 5, 6, 789, time.FixedZone("test", 7200))

func clock() time.Time { return clockValue }
func openStore(t *testing.T, path string, random io.Reader) (*db.DB, *bookmarks.Store) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: home.Migrations(), Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d, bookmarks.New(d, bookmarks.Config{Now: clock, Rand: random})
}
func entropy() io.Reader {
	var b bytes.Buffer
	for i := 1; i < 200; i++ {
		b.Write(bytes.Repeat([]byte{byte(i)}, 8))
	}
	return &b
}
func add(t *testing.T, s *bookmarks.Store, name string, ic bookmarks.Icon) bookmarks.Bookmark {
	t.Helper()
	b, err := s.Create(context.Background(), bookmarks.Draft{Name: name, URL: " https://example.test/a?b=c ", CreatedBy: "uid", Icon: ic})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func list(t *testing.T, s *bookmarks.Store) []bookmarks.Bookmark {
	t.Helper()
	bs, err := s.List(context.Background(), "uid")
	if err != nil {
		t.Fatal(err)
	}
	return bs
}

// R-BBDM-ZJWF R-BCLJ-DBN4 R-BDTF-R3DT R-BF1C-4V4I R-BNKM-T9BD R-BOSJ-7122 R-BQ0F-KSSR R-BR8B-YKJG R-C0ZJ-0QH0 R-C27F-EI7P R-C3FB-S9YE R-C5V4-JTFS R-BYJQ-96ZM
func TestValidation(t *testing.T) {
	if bookmarks.MaxNameRunes != 40 || bookmarks.MaxURLBytes != 2048 {
		t.Fatal("limits")
	}
	messages := []string{bookmarks.NameRequiredMessage, bookmarks.NameTooLongMessage, bookmarks.NameTakenMessage, bookmarks.URLInvalidMessage, bookmarks.URLTooLongMessage}
	seen := map[string]bool{}
	for _, s := range messages {
		if s == "" || seen[s] {
			t.Fatal("messages must be distinct and nonempty")
		}
		seen[s] = true
	}
	if bookmarks.Unreachable == "" {
		t.Fatal("empty unavailable message")
	}
	for _, e := range []bookmarks.FieldErrors{{}, {Name: "x"}, {URL: "y"}, {Name: "x", URL: "y"}} {
		if e.Any() != (e.Name != "" || e.URL != "") {
			t.Fatal(e)
		}
	}
	names := []struct{ name, want string }{{" \t\u2003 ", bookmarks.NameRequiredMessage}, {strings.Repeat("é", 40), ""}, {strings.Repeat("a", 41), bookmarks.NameTooLongMessage}, {" x ", ""}}
	urls := []struct{ url, want string }{{"https://example.com/", ""}, {" http://intranet.test:8080/wiki ", ""}, {"HTTP://Example.com/", ""}, {"", bookmarks.URLInvalidMessage}, {"example.com", bookmarks.URLInvalidMessage}, {"//example.com/", bookmarks.URLInvalidMessage}, {"ftp://example.com/", bookmarks.URLInvalidMessage}, {"mailto:a@example.com", bookmarks.URLInvalidMessage}, {"http:example.com", bookmarks.URLInvalidMessage}, {"http://", bookmarks.URLInvalidMessage}, {"http://[::1", bookmarks.URLInvalidMessage}, {strings.Repeat("a", bookmarks.MaxURLBytes), bookmarks.URLInvalidMessage}, {strings.Repeat("a", bookmarks.MaxURLBytes+1), bookmarks.URLTooLongMessage}, {"https://x/" + strings.Repeat("a", bookmarks.MaxURLBytes-len("https://x/")), ""}}
	for _, n := range names {
		for _, u := range urls {
			e := bookmarks.Validate(bookmarks.Submission{Name: n.name, URL: u.url})
			if e.Name != n.want || e.URL != u.want {
				t.Errorf("%q %q: %+v", n.name, u.url, e)
			}
			if e.Name == bookmarks.NameTakenMessage || e.URL == bookmarks.NameTakenMessage {
				t.Fatal("validation checked uniqueness")
			}
		}
	}
}

// R-BSG8-CCA5
func TestSentinels(t *testing.T) {
	es := []error{bookmarks.ErrNotFound, bookmarks.ErrNameTaken, bookmarks.ErrInvalid}
	for i, e := range es {
		if e == nil {
			t.Fatal("nil sentinel")
		}
		for j, other := range es {
			if i != j && errors.Is(e, other) {
				t.Fatal("sentinels overlap")
			}
		}
	}
}

// R-4120-R97O R-429X-50YD R-3OV0-XJSQ R-3Q2X-BBJF R-3RAT-P3A4 R-48DF-1VNU R-4AT7-TF58 R-4C14-76VX R-4D90-KYMM R-4EGW-YQDB R-4GWP-Q9UP
// R-BA5Q-LS5Q R-BG98-IMV7 R-BHH4-WELW R-BIP1-A6CL R-BJWX-NY3A R-BMCQ-FHKO R-BUW1-3VRJ R-C9IT-P4NV R-CAQQ-2WEK R-CZ4P-QB8G
func TestLifecycleAndPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state", "home.db")
	r := bytes.NewReader(append(append(bytes.Repeat([]byte{0x5a}, 16), bytes.Repeat([]byte{1}, 8)...), bytes.Repeat([]byte{2}, 16)...))
	d, s := openStore(t, path, r)
	if bs := list(t, s); bs == nil || len(bs) != 0 {
		t.Fatal(bs)
	}
	for _, id := range []string{"", "unknown"} {
		ic, err := s.Icon(ctx, "uid", id)
		if !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
			t.Fatal(ic, err)
		}
	}
	original := []byte{1, 2, 3}
	first := add(t, s, " Docs ", bookmarks.Icon{Type: "custom/type", Data: original})
	if first.ID != bookmarks.IDPrefix+strings.Repeat("5a", 8) || bookmarks.IDPrefix != "bmk_" || r.Len() != 32 {
		t.Fatal(first, r.Len())
	}
	if first.Name != "Docs" || first.URL != "https://example.test/a?b=c" || first.CreatedBy != "uid" || !first.HasIcon || !first.Created.Equal(clockValue.UTC().Truncate(time.Second)) || first.Created.Location() != time.UTC || first.Created.Nanosecond() != 0 {
		t.Fatal(first)
	}
	second := add(t, s, "Other", bookmarks.Icon{Type: "unused"})
	if second.ID != bookmarks.IDPrefix+strings.Repeat("01", 8) || r.Len() != 16 {
		t.Fatal(second, r.Len())
	}
	if second.HasIcon {
		t.Fatal(second)
	}
	if !reflect.DeepEqual(list(t, s), []bookmarks.Bookmark{first, second}) {
		t.Fatal(list(t, s))
	}
	ic, err := s.Icon(ctx, "uid", first.ID)
	if err != nil || ic.Type != "custom/type" || !bytes.Equal(ic.Data, original) {
		t.Fatal(ic, err)
	}
	ic.Data[0] = 99
	ic, err = s.Icon(ctx, "uid", first.ID)
	if err != nil || !bytes.Equal(ic.Data, original) {
		t.Fatal("icon read changed stored bytes")
	}
	for _, id := range []string{second.ID, first.Name, ""} {
		ic, err = s.Icon(ctx, "uid", id)
		if !errors.Is(err, bookmarks.ErrNotFound) || ic.Data != nil || ic.Type != "" {
			t.Fatal(ic, err)
		}
	}
	before := list(t, s)
	removed, err := s.Delete(ctx, "uid", "missing")
	if removed || err != nil || !reflect.DeepEqual(before, list(t, s)) {
		t.Fatal(removed, err)
	}
	removed, err = s.Delete(ctx, "uid", first.ID)
	if !removed || err != nil || !reflect.DeepEqual(list(t, s), []bookmarks.Bookmark{second}) {
		t.Fatal(removed, err)
	}
	if _, err = s.Icon(ctx, "uid", first.ID); !errors.Is(err, bookmarks.ErrNotFound) {
		t.Fatal(err)
	}
	third := add(t, s, "Docs", bookmarks.Icon{Type: "image/test", Data: original})
	want := list(t, s)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_, reopened := openStore(t, path, entropy())
	if !reflect.DeepEqual(want, list(t, reopened)) {
		t.Fatal("restart changed bookmarks")
	}
	for _, b := range list(t, reopened) {
		if b.Created.Location() != time.UTC || b.Created.Nanosecond() != 0 {
			t.Fatal(b)
		}
	}
	ic, err = reopened.Icon(ctx, "uid", third.ID)
	if err != nil || !bytes.Equal(ic.Data, original) || ic.Type != "image/test" {
		t.Fatal(ic, err)
	}
	if ic, err := reopened.Icon(ctx, "uid", second.ID); !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
		t.Fatal(ic, err)
	}
	if _, err = reopened.Icon(ctx, "uid", first.ID); !errors.Is(err, bookmarks.ErrNotFound) {
		t.Fatal(err)
	}
}

type countedReader struct {
	io.Reader
	calls int
}

func (r *countedReader) Read(p []byte) (int, error) { r.calls++; return r.Reader.Read(p) }

// R-43HT-ISP2 R-4I4M-41LE R-3ZU4-DHGZ
// R-CBYM-GO59 R-CRTB-FOSA
func TestRefusalAndFailureLeaveContent(t *testing.T) {
	ctx := context.Background()
	random := &countedReader{Reader: bytes.NewReader(bytes.Repeat([]byte{1}, 16))}
	d, s := openStore(t, filepath.Join(t.TempDir(), "db"), random)
	held := add(t, s, "Docs", bookmarks.Icon{Type: "image/test", Data: []byte{4}})
	foreignStore := bookmarks.New(d, bookmarks.Config{Now: clock, Rand: entropy()})
	foreign := addFor(t, foreignStore, "other", "Docs", bookmarks.Icon{Type: "other/type", Data: []byte{6}})
	before := list(t, s)
	check := func() {
		t.Helper()
		if !reflect.DeepEqual(before, list(t, s)) {
			t.Fatal("content changed")
		}
		if !reflect.DeepEqual(listFor(t, s, "other"), []bookmarks.Bookmark{foreign}) {
			t.Fatal("foreign content changed")
		}
		if ic, err := s.Icon(ctx, "other", foreign.ID); err != nil || ic.Type != "other/type" || !bytes.Equal(ic.Data, []byte{6}) {
			t.Fatal(ic, err)
		}
		ic, err := s.Icon(ctx, "uid", held.ID)
		if err != nil || (!bytes.Equal(ic.Data, []byte{4}) || ic.Type != "image/test") {
			t.Fatal(ic, err)
		}
	}
	for _, name := range []string{" docs ", "DOCS", "Docs"} {
		b, err := s.Create(ctx, bookmarks.Draft{Name: name, URL: "https://x/", CreatedBy: "uid"})
		if b != (bookmarks.Bookmark{}) || !errors.Is(err, bookmarks.ErrNameTaken) || random.calls != 1 {
			t.Fatal(b, err)
		}
		check()
	}
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		b, err := s.Create(ctx, bookmarks.Draft{Name: " ", URL: "bad"})
		if b != (bookmarks.Bookmark{}) || !errors.Is(err, bookmarks.ErrInvalid) || random.calls != 1 {
			t.Fatal(b, err)
		}
		d.SetFailing(false)
		check()
	}
	d.SetFailing(true)
	_, e1 := s.List(ctx, "uid")
	_, e2 := s.Icon(ctx, "uid", held.ID)
	_, e3 := s.Delete(ctx, "uid", held.ID)
	_, _, e5 := s.Move(ctx, "uid", held.ID, bookmarks.Direction("bad"))
	_, _, e6 := s.Move(ctx, "uid", held.ID, bookmarks.Right)
	_, e7 := s.List(ctx, "other")
	_, e8 := s.Icon(ctx, "other", foreign.ID)
	_, e9 := s.Delete(ctx, "other", foreign.ID)
	_, e4 := s.Create(ctx, bookmarks.Draft{Name: "Docs", URL: "https://x/", CreatedBy: "uid"})
	for _, err := range []error{e1, e2, e3, e4, e5, e6, e7, e8, e9} {
		if err == nil || errors.Is(err, bookmarks.ErrInvalid) || errors.Is(err, bookmarks.ErrNameTaken) || errors.Is(err, bookmarks.ErrNotFound) {
			t.Fatal(err)
		}
	}
	d.SetFailing(false)
	check()
	b, err := s.Create(ctx, bookmarks.Draft{Name: "Fresh", URL: "https://x/", CreatedBy: "uid"})
	if b != (bookmarks.Bookmark{}) || err == nil || errors.Is(err, bookmarks.ErrInvalid) || errors.Is(err, bookmarks.ErrNameTaken) || errors.Is(err, bookmarks.ErrNotFound) {
		t.Fatal(b, err)
	}
	check()
	random.Reader = entropy()
	recovered, err := s.Create(ctx, bookmarks.Draft{Name: "Recovered", URL: "https://x/", CreatedBy: "uid"})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok, err := s.Move(ctx, "uid", recovered.ID, bookmarks.Left); err != nil || !ok || m != (bookmarks.Moved{From: 1, To: 0}) {
		t.Fatal(m, ok, err)
	}
	if removed, err := s.Delete(ctx, "uid", recovered.ID); err != nil || !removed {
		t.Fatal(removed, err)
	}
	check()

}

// R-45XM-AC6G R-475I-O3X5 R-CT17-TGIZ R-CU94-789O
func TestOrderAndConcurrentUse(t *testing.T) {
	ctx := context.Background()
	_, s := openStore(t, filepath.Join(t.TempDir(), "db"), entropy())
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			_, err := s.Create(ctx, bookmarks.Draft{Name: "SAME", URL: "https://x/", CreatedBy: "uid"})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, bookmarks.ErrNameTaken) {
			t.Fatal(err)
		}
	}
	if wins != 1 || len(list(t, s)) != 1 {
		t.Fatal(wins)
	}
	for i := range 12 {
		wg.Go(func() {
			user := fmt.Sprintf("user-%d", i%2)
			b, err := s.Create(ctx, bookmarks.Draft{Name: fmt.Sprintf("unique-%d", i), URL: "https://x/", CreatedBy: user, Icon: bookmarks.Icon{Type: "image/x", Data: []byte{1}}})
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := s.List(ctx, user); err != nil {
				t.Error(err)
			}
			if _, err := s.Icon(ctx, user, b.ID); err != nil {
				t.Error(err)
			}
			if _, _, err := s.Move(ctx, user, b.ID, bookmarks.Left); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, user := range []string{"user-0", "user-1"} {
		bs := listFor(t, s, user)
		if len(bs) != 6 {
			t.Fatal(user, bs)
		}
		for _, b := range bs {
			wg.Go(func() {
				if ok, err := s.Delete(ctx, user, b.ID); !ok || err != nil {
					t.Error(ok, err)
				}
			})
		}
	}
	wg.Wait()
	if len(list(t, s)) != 1 || len(listFor(t, s, "user-0")) != 0 || len(listFor(t, s, "user-1")) != 0 {
		t.Fatal("unexpected content")
	}
}

// R-4JCI-HTC3 R-4KKE-VL2S
// R-CVH0-L00D R-CWOW-YRR2
func TestDatabaseContract(t *testing.T) {
	ctx := context.Background()
	d, s := openStore(t, filepath.Join(t.TempDir(), "db"), entropy())
	with := add(t, s, "Docs", bookmarks.Icon{Type: "image/custom", Data: []byte{2, 3}})
	without := add(t, s, "Émile", bookmarks.Icon{Type: "ignored"})
	err := d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if err != nil {
			return err
		}
		var tables []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			tables = append(tables, n)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !reflect.DeepEqual(tables, []string{"bookmarks", "schema_migrations"}) {
			t.Fatal(tables)
		}
		rows, err = tx.QueryContext(ctx, "PRAGMA table_info(bookmarks)")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		types := []string{"TEXT", "TEXT", "TEXT", "BLOB", "TEXT", "TEXT", "TEXT", "INTEGER"}
		columns := []string{"id", "name", "url", "icon", "icon_type", "created_by", "created_at", "position"}
		i := 0
		for rows.Next() {
			var cid, nn, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
				return err
			}
			if i >= 8 || cid != i || name != columns[i] || typ != types[i] || pk != boolInt(i == 0) || nn != boolInt(i == 1 || i == 2 || i == 5 || i == 6) {
				t.Fatalf("column %d %s %s %d %d", cid, name, typ, nn, pk)
			}
			i++
		}
		if i != 8 {
			t.Fatal(i)
		}
		for _, b := range []bookmarks.Bookmark{with, without} {
			var id, name, u, by, created string
			var icon []byte
			var typ sql.NullString
			if err := tx.QueryRowContext(ctx, "SELECT id,name,url,icon,icon_type,created_by,created_at FROM bookmarks WHERE id = ?", b.ID).Scan(&id, &name, &u, &icon, &typ, &by, &created); err != nil {
				return err
			}
			if id != b.ID || name != b.Name || u != b.URL || by != b.CreatedBy || created != b.Created.Format(time.RFC3339) {
				t.Fatal("row mismatch")
			}
			if b.HasIcon {
				if !bytes.Equal(icon, []byte{2, 3}) || !typ.Valid || typ.String != "image/custom" {
					t.Fatal(icon, typ)
				}
			} else if icon != nil || typ.Valid {
				t.Fatal(icon, typ)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id, name, user string) error {
		return d.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO bookmarks (id,name,url,created_by,created_at) VALUES (?,?,?,?,?)", id, name, "https://x/", user, clock().UTC().Truncate(time.Second).Format(time.RFC3339))
			return err
		})
	}
	before := list(t, s)
	for i, n := range []string{"DOCS", "docs"} {
		if err := insert(fmt.Sprint(i), n, "uid"); err == nil {
			t.Fatal("raw insert bypassed uniqueness")
		}
		if !reflect.DeepEqual(before, list(t, s)) {
			t.Fatal("failed insert changed content")
		}
	}
	if err := insert("different", "émile", "uid"); err != nil {
		t.Fatal(err)
	}
	if err := insert("other-user", "docs", "other"); err != nil {
		t.Fatal(err)
	}
	if len(listFor(t, s, "other")) != 1 {
		t.Fatal("other user row missing")
	}
	if len(list(t, s)) != 3 {
		t.Fatal("distinct unicode names refused")
	}
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func listFor(t *testing.T, s *bookmarks.Store, user string) []bookmarks.Bookmark {
	t.Helper()
	bs, err := s.List(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	return bs
}
func addFor(t *testing.T, s *bookmarks.Store, user, name string, icon bookmarks.Icon) bookmarks.Bookmark {
	t.Helper()
	b, err := s.Create(context.Background(), bookmarks.Draft{Name: name, URL: " https://example.test/ ", CreatedBy: user, Icon: icon})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// R-44PP-WKFR R-48DF-1VNU R-429X-50YD R-4C14-76VX R-4EGW-YQDB R-4120-R97O
func TestUserIsolationAndAppend(t *testing.T) {
	ctx := context.Background()
	_, s := openStore(t, filepath.Join(t.TempDir(), "db"), entropy())
	for _, user := range []string{"uid", "other", ""} {
		if bs := listFor(t, s, user); bs == nil || len(bs) != 0 {
			t.Fatal(user, bs)
		}
		if ic, err := s.Icon(ctx, user, "unknown"); !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
			t.Fatal(ic, err)
		}
	}
	var own []bookmarks.Bookmark
	for _, name := range []string{"Zeta", "apple", "Docs", "Émile", "émile"} {
		own = append(own, addFor(t, s, "uid", name, bookmarks.Icon{Type: "image/x", Data: []byte{2}}))
	}
	other := addFor(t, s, "other", "docs", bookmarks.Icon{Type: "image/x", Data: []byte{5}})
	empty := addFor(t, s, "", "Docs", bookmarks.Icon{})
	if !reflect.DeepEqual(listFor(t, s, "uid"), own) || !reflect.DeepEqual(listFor(t, s, "other"), []bookmarks.Bookmark{other}) || !reflect.DeepEqual(listFor(t, s, ""), []bookmarks.Bookmark{empty}) {
		t.Fatal("user contents differ")
	}
	if bs := listFor(t, s, "unknown"); bs == nil || len(bs) != 0 {
		t.Fatal(bs)
	}
	for _, user := range []string{"uid", "unknown", ""} {
		if ic, err := s.Icon(ctx, user, other.ID); !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
			t.Fatal(ic, err)
		}
		if ok, err := s.Delete(ctx, user, other.ID); ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	if !reflect.DeepEqual(listFor(t, s, "uid"), own) || !reflect.DeepEqual(listFor(t, s, "other"), []bookmarks.Bookmark{other}) || !reflect.DeepEqual(listFor(t, s, ""), []bookmarks.Bookmark{empty}) {
		t.Fatal("foreign access changed contents")
	}
	if ic, err := s.Icon(ctx, "other", other.ID); err != nil || ic.Type != "image/x" || !bytes.Equal(ic.Data, []byte{5}) {
		t.Fatal(ic, err)
	}
}

// R-3SIQ-2V0T R-3TQM-GMRI R-3UYI-UEI7 R-3YM7-ZPQA R-4O84-0WAV R-4PG0-EO1K R-4QNW-SFS9 R-4D90-KYMM R-4GWP-Q9UP
func TestMovesAndPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	d, s := openStore(t, path, entropy())
	if string(bookmarks.Left) != "left" || string(bookmarks.Right) != "right" {
		t.Fatal("directions")
	}
	var own []bookmarks.Bookmark
	for _, name := range []string{"a", "b", "c"} {
		own = append(own, addFor(t, s, "uid", name, bookmarks.Icon{Type: name, Data: []byte(name)}))
	}
	other := addFor(t, s, "other", "a", bookmarks.Icon{Type: "foreign", Data: []byte{9}})
	check := func() {
		t.Helper()
		if !reflect.DeepEqual(list(t, s), own) || !reflect.DeepEqual(listFor(t, s, "other"), []bookmarks.Bookmark{other}) {
			t.Fatal("move changed contents", list(t, s), own)
		}
		for _, b := range append(append([]bookmarks.Bookmark{}, own...), other) {
			ic, err := s.Icon(ctx, b.CreatedBy, b.ID)
			want := []byte(b.Name)
			typ := b.Name
			if b.CreatedBy == "other" {
				want = []byte{9}
				typ = "foreign"
			}
			if err != nil || ic.Type != typ || !bytes.Equal(ic.Data, want) {
				t.Fatal(ic, err)
			}
		}
	}
	move := func(id string, dir bookmarks.Direction, from, to int) {
		t.Helper()
		m, ok, err := s.Move(ctx, "uid", id, dir)
		if err != nil || !ok || m != (bookmarks.Moved{From: from, To: to}) {
			t.Fatal(m, ok, err)
		}
		own[from], own[to] = own[to], own[from]
		check()
	}
	move(own[2].ID, bookmarks.Left, 2, 1)
	move(own[0].ID, bookmarks.Right, 0, 1)
	for _, tc := range []struct {
		user, id string
		dir      bookmarks.Direction
	}{
		{"uid", own[0].ID, bookmarks.Left}, {"uid", own[2].ID, bookmarks.Right},
		{"uid", "", bookmarks.Left}, {"uid", "unknown", bookmarks.Right}, {"uid", other.ID, bookmarks.Left},
		{"uid", own[1].ID, ""}, {"uid", own[1].ID, "up"}, {"uid", own[1].ID, "LEFT"},
		{"other", other.ID, bookmarks.Left}, {"other", other.ID, bookmarks.Right},
	} {
		m, ok, err := s.Move(ctx, tc.user, tc.id, tc.dir)
		if err != nil || ok || m != (bookmarks.Moved{}) {
			t.Fatal(tc, m, ok, err)
		}
		check()
	}
	// Two moves of the same trailing bookmark each observe the preceding move.
	trailing := own[2].ID
	results := make(chan bookmarks.Moved, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			m, ok, err := s.Move(ctx, "uid", trailing, bookmarks.Left)
			if err != nil || !ok {
				t.Error(m, ok, err)
				return
			}
			results <- m
		})
	}
	wg.Wait()
	close(results)
	seen := map[bookmarks.Moved]bool{}
	for m := range results {
		seen[m] = true
	}
	if len(seen) != 2 || !seen[bookmarks.Moved{From: 2, To: 1}] || !seen[bookmarks.Moved{From: 1, To: 0}] {
		t.Fatal(seen)
	}
	own = []bookmarks.Bookmark{own[2], own[0], own[1]}
	check()
	removed := own[1]
	if ok, err := s.Delete(ctx, "uid", removed.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	own = []bookmarks.Bookmark{own[0], own[2]}
	own = append(own, addFor(t, s, "uid", removed.Name, bookmarks.Icon{Type: removed.Name, Data: []byte(removed.Name)}))
	check()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_, reopened := openStore(t, path, entropy())
	s = reopened
	check()
	if ic, err := s.Icon(ctx, "uid", removed.ID); !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
		t.Fatal(ic, err)
	}
}

// R-49LB-FNEJ R-4LSB-9CTH
func TestStoredPositionsAndOlderInsert(t *testing.T) {
	ctx := context.Background()
	d, s := openStore(t, filepath.Join(t.TempDir(), "db"), entropy())
	a := addFor(t, s, "uid", "a", bookmarks.Icon{})
	b := addFor(t, s, "uid", "b", bookmarks.Icon{})
	other := addFor(t, s, "other", "other", bookmarks.Icon{})
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO bookmarks (id,name,url,icon,icon_type,created_by,created_at) VALUES (?,?,?,?,?,?,?)", "older", "c", "https://x/", nil, nil, "uid", clock().UTC().Format(time.RFC3339))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c := list(t, s)[2]
	last := addFor(t, s, "uid", "d", bookmarks.Icon{})
	if !reflect.DeepEqual(list(t, s), []bookmarks.Bookmark{a, b, c, last}) || c.ID != "older" || !reflect.DeepEqual(listFor(t, s, "other"), []bookmarks.Bookmark{other}) {
		t.Fatal(list(t, s))
	}
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		for i, id := range []string{"bmk_b", "bmk_c", "bmk_a"} {
			positions := []int{2, 1, 2}
			if _, err := tx.ExecContext(ctx, "INSERT INTO bookmarks (id,name,url,created_by,created_at,position) VALUES (?,?,?,?,?,?)", id, id, "https://x/", "positions", clock().UTC().Format(time.RFC3339), positions[i]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	bs := listFor(t, s, "positions")
	var ids []string
	for _, b := range bs {
		ids = append(ids, b.ID)
	}
	if !reflect.DeepEqual(ids, []string{"bmk_c", "bmk_a", "bmk_b"}) {
		t.Fatal(ids)
	}

	for _, tc := range []struct {
		id       string
		dir      bookmarks.Direction
		from, to int
	}{
		{"bmk_b", bookmarks.Left, 2, 1}, {"bmk_c", bookmarks.Right, 0, 1},
	} {
		if err := d.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE bookmarks SET position = CASE id WHEN 'bmk_c' THEN 1 ELSE 2 END WHERE created_by = ?", "positions")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		m, ok, err := s.Move(ctx, "positions", tc.id, tc.dir)
		if err != nil || !ok || m != (bookmarks.Moved{From: tc.from, To: tc.to}) {
			t.Fatal(m, ok, err)
		}
		want := append([]bookmarks.Bookmark{}, bs...)
		want[tc.from], want[tc.to] = want[tc.to], want[tc.from]
		if got := listFor(t, s, "positions"); !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
	}

}

// R-BHIK-MY23
func TestUpgradeFromOriginalSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	original, err := fs.ReadFile(home.Migrations(), "0001_bookmarks.sql")
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: fstest.MapFS{"0001_bookmarks.sql": {Data: original}}, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	var own []bookmarks.Bookmark
	for i, name := range []string{"Zeta", "apple", "émile", "_x", "Émile", "Docs"} {
		user := "uid"
		if name == "Docs" {
			user = "other"
		}
		hasIcon := i%2 == 0
		b := bookmarks.Bookmark{ID: fmt.Sprintf("old-%d", i), Name: name, URL: fmt.Sprintf("https://x/%d", i), CreatedBy: user, HasIcon: hasIcon, Created: clock().UTC().Truncate(time.Second)}
		if err := d.Write(ctx, func(tx *sql.Tx) error {
			var data, typ any
			if hasIcon {
				data = []byte(name)
				typ = "type/" + name
			}
			_, err := tx.ExecContext(ctx, "INSERT INTO bookmarks (id,name,url,icon,icon_type,created_by,created_at) VALUES (?,?,?,?,?,?,?)", b.ID, b.Name, b.URL, data, typ, b.CreatedBy, b.Created.Format(time.RFC3339))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		own = append(own, b)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_, s := openStore(t, path, entropy())
	if !reflect.DeepEqual(list(t, s), []bookmarks.Bookmark{own[3], own[1], own[0], own[4], own[2]}) || !reflect.DeepEqual(listFor(t, s, "other"), []bookmarks.Bookmark{own[5]}) {
		t.Fatal(list(t, s))
	}
	for _, b := range own {
		ic, err := s.Icon(ctx, b.CreatedBy, b.ID)
		if b.HasIcon {
			if err != nil || ic.Type != "type/"+b.Name || !bytes.Equal(ic.Data, []byte(b.Name)) {
				t.Fatal(ic, err)
			}
		} else if !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
			t.Fatal(ic, err)
		}
	}
	next := addFor(t, s, "uid", "new", bookmarks.Icon{})
	if !reflect.DeepEqual(list(t, s), []bookmarks.Bookmark{own[3], own[1], own[0], own[4], own[2], next}) {
		t.Fatal(list(t, s))
	}
}

// R-429X-50YD R-CAQQ-2WEK
func TestIDCollisionAcrossUsers(t *testing.T) {
	random := bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 16), bytes.Repeat([]byte{1}, 8)...))
	_, s := openStore(t, filepath.Join(t.TempDir(), "db"), random)
	first := addFor(t, s, "first", "Docs", bookmarks.Icon{})
	second := addFor(t, s, "second", "Docs", bookmarks.Icon{})
	if first.ID != bookmarks.IDPrefix+strings.Repeat("5a", 8) || second.ID != bookmarks.IDPrefix+strings.Repeat("01", 8) || random.Len() != 0 {
		t.Fatal(first, second, random.Len())
	}
	if !reflect.DeepEqual(listFor(t, s, "first"), []bookmarks.Bookmark{first}) || !reflect.DeepEqual(listFor(t, s, "second"), []bookmarks.Bookmark{second}) {
		t.Fatal("collision changed ownership or contents")
	}
}

// R-429X-50YD R-4O84-0WAV R-4LSB-9CTH
func TestAppendAtIntegerPositionBoundary(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			ctx := context.Background()
			d, s := openStore(t, filepath.Join(t.TempDir(), "db"), entropy())
			foreign := addFor(t, s, "other", "foreign", bookmarks.Icon{Type: "other/type", Data: []byte{7}})
			before := []bookmarks.Bookmark{
				addFor(t, s, "uid", "first", bookmarks.Icon{Type: "first/type", Data: []byte{1}}),
				addFor(t, s, "uid", "second", bookmarks.Icon{Type: "second/type", Data: []byte{2}}),
				addFor(t, s, "uid", "third", bookmarks.Icon{}),
			}
			// Include a tie and an order different from ID order in the overflow snapshot.
			before[0], before[1] = before[1], before[0]
			if err := d.Write(ctx, func(tx *sql.Tx) error {
				for i, b := range before {
					position := int64(math.MaxInt64 - 1)
					if i == 0 {
						position--
					}
					if _, err := tx.ExecContext(ctx, "UPDATE bookmarks SET position = ? WHERE id = ?", position, b.ID); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for i := range 3 {
				if legacy {
					id := fmt.Sprintf("legacy-%d", i)
					if err := d.Write(ctx, func(tx *sql.Tx) error {
						_, err := tx.ExecContext(ctx, "INSERT INTO bookmarks (id,name,url,icon,icon_type,created_by,created_at) VALUES (?,?,?,?,?,?,?)", id, id, "https://x/", nil, nil, "uid", clock().UTC().Truncate(time.Second).Format(time.RFC3339))
						return err
					}); err != nil {
						t.Fatal(err)
					}
					got := list(t, s)
					if len(got) != len(before)+1 || !reflect.DeepEqual(got[:len(before)], before) || got[len(before)].ID != id {
						t.Fatal(got, before)
					}
					before = append(before, got[len(before)])
				} else {
					before = append(before, addFor(t, s, "uid", fmt.Sprintf("new-%d", i), bookmarks.Icon{}))
					if got := list(t, s); !reflect.DeepEqual(got, before) {
						t.Fatal(got, before)
					}
				}
			}
			// Both insertion paths keep later Create and Move usable after the boundary.
			last := addFor(t, s, "uid", "last", bookmarks.Icon{})
			before = append(before, last)
			if got := list(t, s); !reflect.DeepEqual(got, before) {
				t.Fatal(got, before)
			}
			m, ok, err := s.Move(ctx, "uid", last.ID, bookmarks.Left)
			if err != nil || !ok || m != (bookmarks.Moved{From: len(before) - 1, To: len(before) - 2}) {
				t.Fatal(m, ok, err)
			}
			before[m.From], before[m.To] = before[m.To], before[m.From]
			if got := list(t, s); !reflect.DeepEqual(got, before) {
				t.Fatal(got, before)
			}
			for _, b := range append(append([]bookmarks.Bookmark{}, before...), foreign) {
				ic, err := s.Icon(ctx, b.CreatedBy, b.ID)
				if !b.HasIcon {
					if !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
						t.Fatal(ic, err)
					}
					continue
				}
				want := []byte{1}
				typ := "first/type"
				if b.Name == "second" {
					want = []byte{2}
					typ = "second/type"
				}
				if b.CreatedBy == "other" {
					want = []byte{7}
					typ = "other/type"
				}
				if err != nil || ic.Type != typ || !bytes.Equal(ic.Data, want) {
					t.Fatal(ic, err)
				}
			}
			if !reflect.DeepEqual(listFor(t, s, "other"), []bookmarks.Bookmark{foreign}) {
				t.Fatal("foreign contents changed")
			}
		})
	}
}
