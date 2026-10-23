package bookmarks_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
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
	bs, err := s.List(context.Background())
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

// R-BA5Q-LS5Q R-BG98-IMV7 R-BHH4-WELW R-BIP1-A6CL R-BJWX-NY3A R-BMCQ-FHKO R-BTO4-Q40U R-BUW1-3VRJ R-BW3X-HNI8 R-BXBT-VF8X R-C730-XL6H R-C8AX-BCX6 R-C9IT-P4NV R-CAQQ-2WEK R-CEEF-87MN R-CJA0-RALF R-CKHX-52C4 R-CLPT-IU2T R-CO5M-ADK7 R-CPDI-O5AW R-CZ4P-QB8G
func TestLifecycleAndPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state", "home.db")
	r := bytes.NewReader(append(append(bytes.Repeat([]byte{0x5a}, 16), bytes.Repeat([]byte{1}, 8)...), bytes.Repeat([]byte{2}, 16)...))
	d, s := openStore(t, path, r)
	if bs := list(t, s); bs == nil || len(bs) != 0 {
		t.Fatal(bs)
	}
	for _, id := range []string{"", "unknown"} {
		ic, err := s.Icon(ctx, id)
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
	ic, err := s.Icon(ctx, first.ID)
	if err != nil || ic.Type != "custom/type" || !bytes.Equal(ic.Data, original) {
		t.Fatal(ic, err)
	}
	ic.Data[0] = 99
	ic, err = s.Icon(ctx, first.ID)
	if err != nil || !bytes.Equal(ic.Data, original) {
		t.Fatal("icon read changed stored bytes")
	}
	for _, id := range []string{second.ID, first.Name, ""} {
		ic, err = s.Icon(ctx, id)
		if !errors.Is(err, bookmarks.ErrNotFound) || ic.Data != nil || ic.Type != "" {
			t.Fatal(ic, err)
		}
	}
	before := list(t, s)
	removed, err := s.Delete(ctx, "missing")
	if removed || err != nil || !reflect.DeepEqual(before, list(t, s)) {
		t.Fatal(removed, err)
	}
	removed, err = s.Delete(ctx, first.ID)
	if !removed || err != nil || !reflect.DeepEqual(list(t, s), []bookmarks.Bookmark{second}) {
		t.Fatal(removed, err)
	}
	if _, err = s.Icon(ctx, first.ID); !errors.Is(err, bookmarks.ErrNotFound) {
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
	ic, err = reopened.Icon(ctx, third.ID)
	if err != nil || !bytes.Equal(ic.Data, original) || ic.Type != "image/test" {
		t.Fatal(ic, err)
	}
	if ic, err := reopened.Icon(ctx, second.ID); !errors.Is(err, bookmarks.ErrNotFound) || ic.Type != "" || ic.Data != nil {
		t.Fatal(ic, err)
	}
	if _, err = reopened.Icon(ctx, first.ID); !errors.Is(err, bookmarks.ErrNotFound) {
		t.Fatal(err)
	}
}

type countedReader struct {
	io.Reader
	calls int
}

func (r *countedReader) Read(p []byte) (int, error) { r.calls++; return r.Reader.Read(p) }

// R-CBYM-GO59 R-CD6I-UFVY R-CQLF-1X1L R-CRTB-FOSA R-YMA8-D93S
func TestRefusalAndFailureLeaveContent(t *testing.T) {
	ctx := context.Background()
	random := &countedReader{Reader: bytes.NewReader(bytes.Repeat([]byte{1}, 16))}
	d, s := openStore(t, filepath.Join(t.TempDir(), "db"), random)
	held := add(t, s, "Docs", bookmarks.Icon{Type: "image/test", Data: []byte{4}})
	before := list(t, s)
	check := func() {
		t.Helper()
		if !reflect.DeepEqual(before, list(t, s)) {
			t.Fatal("content changed")
		}
		ic, err := s.Icon(ctx, held.ID)
		if err != nil || (!bytes.Equal(ic.Data, []byte{4}) || ic.Type != "image/test") {
			t.Fatal(ic, err)
		}
	}
	for _, name := range []string{" docs ", "DOCS", "Docs"} {
		b, err := s.Create(ctx, bookmarks.Draft{Name: name, URL: "https://x/"})
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
	_, e1 := s.List(ctx)
	_, e2 := s.Icon(ctx, held.ID)
	_, e3 := s.Delete(ctx, held.ID)
	_, e4 := s.Create(ctx, bookmarks.Draft{Name: "Docs", URL: "https://x/"})
	for _, err := range []error{e1, e2, e3, e4} {
		if err == nil || errors.Is(err, bookmarks.ErrInvalid) || errors.Is(err, bookmarks.ErrNameTaken) || errors.Is(err, bookmarks.ErrNotFound) {
			t.Fatal(err)
		}
	}
	d.SetFailing(false)
	check()
	b, err := s.Create(ctx, bookmarks.Draft{Name: "Fresh", URL: "https://x/"})
	if b != (bookmarks.Bookmark{}) || err == nil || errors.Is(err, bookmarks.ErrInvalid) || errors.Is(err, bookmarks.ErrNameTaken) || errors.Is(err, bookmarks.ErrNotFound) {
		t.Fatal(b, err)
	}
	check()
	random.Reader = entropy()
	recovered, err := s.Create(ctx, bookmarks.Draft{Name: "Recovered", URL: "https://x/"})
	if err != nil {
		t.Fatal(err)
	}
	if removed, err := s.Delete(ctx, recovered.ID); err != nil || !removed {
		t.Fatal(removed, err)
	}
	check()

}

// R-CI24-DIUQ R-CFMB-LZDC R-CGU7-ZR41 R-CT17-TGIZ R-CU94-789O
func TestOrderAndConcurrentUse(t *testing.T) {
	ctx := context.Background()
	_, s := openStore(t, filepath.Join(t.TempDir(), "db"), entropy())
	for _, n := range []string{"Zeta", "apple", "émile", "_x", "Émile"} {
		add(t, s, n, bookmarks.Icon{})
	}
	var names []string
	for _, b := range list(t, s) {
		names = append(names, b.Name)
	}
	if !reflect.DeepEqual(names, []string{"_x", "apple", "Zeta", "Émile", "émile"}) {
		t.Fatal(names)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Go(func() { _, err := s.Create(ctx, bookmarks.Draft{Name: "SAME", URL: "https://x/"}); results <- err })
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
	if wins != 1 || len(list(t, s)) != 6 {
		t.Fatal(wins)
	}
	for i := range 12 {
		wg.Go(func() {
			b, err := s.Create(ctx, bookmarks.Draft{Name: fmt.Sprintf("unique-%d", i), URL: "https://x/", Icon: bookmarks.Icon{Type: "image/x", Data: []byte{1}}})
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := s.List(ctx); err != nil {
				t.Error(err)
			}
			if _, err := s.Icon(ctx, b.ID); err != nil {
				t.Error(err)
			}
			if ok, err := s.Delete(ctx, b.ID); !ok || err != nil {
				t.Error(ok, err)
			}
		})
	}
	wg.Wait()
	if len(list(t, s)) != 6 {
		t.Fatal("unexpected content")
	}
}

// R-CVH0-L00D R-CWOW-YRR2 R-CXWT-CJHR R-YNI4-R0UH
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
		types := []string{"TEXT", "TEXT", "TEXT", "BLOB", "TEXT", "TEXT", "TEXT"}
		columns := []string{"id", "name", "url", "icon", "icon_type", "created_by", "created_at"}
		i := 0
		for rows.Next() {
			var cid, nn, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
				return err
			}
			if i >= 7 || cid != i || name != columns[i] || typ != types[i] || pk != boolInt(i == 0) || nn != boolInt(i == 1 || i == 2 || i == 5 || i == 6) {
				t.Fatalf("column %d %s %s %d %d", cid, name, typ, nn, pk)
			}
			i++
		}
		if i != 7 {
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
	insert := func(id, name string) error {
		return d.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO bookmarks (id,name,url,created_by,created_at) VALUES (?,?,?,?,?)", id, name, "https://x/", "who", clock().UTC().Truncate(time.Second).Format(time.RFC3339))
			return err
		})
	}
	before := list(t, s)
	for i, n := range []string{"DOCS", "docs"} {
		if err := insert(fmt.Sprint(i), n); err == nil {
			t.Fatal("raw insert bypassed uniqueness")
		}
		if !reflect.DeepEqual(before, list(t, s)) {
			t.Fatal("failed insert changed content")
		}
	}
	if err := insert("different", "émile"); err != nil {
		t.Fatal(err)
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
