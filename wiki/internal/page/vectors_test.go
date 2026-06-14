package page

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"wiki/internal/db"

	_ "modernc.org/sqlite"
)

func newVecTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "wiki.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return conn
}

func vecExec(t *testing.T, conn *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(q, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

// seedPage inserts a subject + page at the given version, plus optional aliases.
func seedPage(t *testing.T, conn *sql.DB, id, name, body string, version int, aliases ...string) {
	t.Helper()
	vecExec(t, conn, `INSERT INTO subjects (id, type, kind, canonical_name, created_by_run) VALUES (?, 'entity','company',?, 'r')`, id, name)
	vecExec(t, conn, `INSERT INTO pages (subject, title, body, version) VALUES (?,?,?,?)`, id, name, body, version)
	for _, a := range aliases {
		vecExec(t, conn, `INSERT INTO aliases (type, norm, subject_id) VALUES ('entity',?,?)`, Normalize(a), id)
	}
}

func TestVectorEncodeDecodeRoundTrip(t *testing.T) {
	in := []float32{0, 1, -1, 3.14159, 1e-7, -2.5}
	out, err := decodeVector(encodeVector(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("len = %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("dim %d = %v, want %v", i, out[i], in[i])
		}
	}
}

func TestDecodeVectorRejectsCorruptBlob(t *testing.T) {
	if _, err := decodeVector([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error on non-multiple-of-4 blob")
	}
}

func TestUpsertAndLoadVectors(t *testing.T) {
	conn := newVecTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()
	seedPage(t, conn, "01SUBJECTAAAAAAAAAAAAAAAAA", "Acme", "body a", 1)

	if err := s.UpsertVector(ctx, "01SUBJECTAAAAAAAAAAAAAAAAA", 1, "m@2", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	// Upsert again (replace) at a new version + model.
	if err := s.UpsertVector(ctx, "01SUBJECTAAAAAAAAAAAAAAAAA", 2, "m@2", []float32{0, 1}); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadVectors(ctx, "m@2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d vectors, want 1", len(got))
	}
	if got[0].Vector[1] != 1 {
		t.Fatalf("vector not replaced on upsert: %v", got[0].Vector)
	}
	// A different model tag returns nothing (cross-model rows are read-invalid).
	other, err := s.LoadVectors(ctx, "other@2")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("cross-model load returned %d, want 0", len(other))
	}
}

func TestVectorWorkList(t *testing.T) {
	conn := newVecTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()

	// missing: never embedded
	seedPage(t, conn, "01MISSINGAAAAAAAAAAAAAAAAA", "Missing", "mbody", 1, "MissAlias")
	// stale: embedded under an older version
	seedPage(t, conn, "01STALEAAAAAAAAAAAAAAAAAAA", "Stale", "sbody", 5)
	if err := s.UpsertVector(ctx, "01STALEAAAAAAAAAAAAAAAAAAA", 3, "m@2", []float32{1}); err != nil {
		t.Fatal(err)
	}
	// wrong-model: embedded under a different model tag
	seedPage(t, conn, "01WRONGAAAAAAAAAAAAAAAAAAA", "Wrong", "wbody", 2)
	if err := s.UpsertVector(ctx, "01WRONGAAAAAAAAAAAAAAAAAAA", 2, "old@2", []float32{1}); err != nil {
		t.Fatal(err)
	}
	// current: embedded at the right version + model — must NOT appear
	seedPage(t, conn, "01CURRENTAAAAAAAAAAAAAAAAA", "Current", "cbody", 4)
	if err := s.UpsertVector(ctx, "01CURRENTAAAAAAAAAAAAAAAAA", 4, "m@2", []float32{1}); err != nil {
		t.Fatal(err)
	}

	work, err := s.VectorWorkList(ctx, "m@2", 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]VectorWork{}
	for _, w := range work {
		got[w.Subject] = w
	}
	if len(got) != 3 {
		t.Fatalf("work list = %d entries, want 3 (missing/stale/wrong): %v", len(got), got)
	}
	if _, ok := got["01CURRENTAAAAAAAAAAAAAAAAA"]; ok {
		t.Fatal("current page must not be in the work list")
	}
	// The work text carries canonical name + alias keys + body.
	mw := got["01MISSINGAAAAAAAAAAAAAAAAA"]
	if mw.Version != 1 {
		t.Fatalf("missing version = %d, want 1", mw.Version)
	}
	if want := Normalize("MissAlias"); !contains(mw.Text, want) {
		t.Fatalf("work text %q missing alias key %q", mw.Text, want)
	}
	if !contains(mw.Text, "Missing") || !contains(mw.Text, "mbody") {
		t.Fatalf("work text missing name/body: %q", mw.Text)
	}
}

func TestWholePagesByIDsPreservesOrderAndSkipsMissing(t *testing.T) {
	conn := newVecTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()
	seedPage(t, conn, "01AAAAAAAAAAAAAAAAAAAAAAAA", "A", "abody", 1)
	seedPage(t, conn, "01BBBBBBBBBBBBBBBBBBBBBBBB", "B", "bbody", 1)

	out, err := s.WholePagesByIDs(ctx, []string{"01BBBBBBBBBBBBBBBBBBBBBBBB", "01NOSUCHIDXXXXXXXXXXXXXXXX", "01AAAAAAAAAAAAAAAAAAAAAAAA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d pages, want 2 (missing id skipped)", len(out))
	}
	if out[0].Subject != "01BBBBBBBBBBBBBBBBBBBBBBBB" || out[1].Subject != "01AAAAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("order not preserved: %v", out)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
