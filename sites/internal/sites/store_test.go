package sites

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlkit "appkit/db"

	"sites/internal/db"
)

// newTestStore returns a Store wired to a fresh, migrated temp-file SQLite
// database with a deterministic, monotonically-increasing clock so created_at /
// updated_at ordering is stable. t.TempDir() cleans up the file.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sites_test.db")
	conn, err := sqlkit.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := sqlkit.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := sqlkit.Migrate(context.Background(), conn, migs); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	clk := &time.Time{}
	*clk = time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	s := NewStore(conn)
	s.Now = func() time.Time {
		*clk = clk.Add(time.Millisecond)
		return *clk
	}
	return s
}

func TestCRUDRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.Create(ctx, "alpha", "", "", Private)
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if a.Name != "alpha" || a.Visibility != Private {
		t.Fatalf("create alpha: unexpected row %+v", a)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatalf("create alpha: timestamps unset %+v", a)
	}

	if _, err := s.Create(ctx, "bravo", "", "", Private); err != nil {
		t.Fatalf("create bravo: %v", err)
	}

	// List returns both, sorted by name.
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list: want 2, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "bravo" {
		t.Fatalf("list: not sorted: %q, %q", list[0].Name, list[1].Name)
	}

	// Get returns one.
	got, err := s.Get(ctx, "alpha")
	if err != nil {
		t.Fatalf("get alpha: %v", err)
	}
	if got.Name != "alpha" {
		t.Fatalf("get alpha: got %q", got.Name)
	}

	// Delete removes it; subsequent get is ErrNotFound.
	if err := s.Delete(ctx, "alpha"); err != nil {
		t.Fatalf("delete alpha: %v", err)
	}
	if _, err := s.Get(ctx, "alpha"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: want ErrNotFound, got %v", err)
	}

	// And only bravo remains.
	list, err = s.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 1 || list[0].Name != "bravo" {
		t.Fatalf("list after delete: want [bravo], got %+v", list)
	}
}

func TestSlugReject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	bad := []struct {
		name, slug string
	}{
		{"uppercase", "Foo"},
		{"underscore", "a_b"},
		{"leading-hyphen", "-lead"},
		{"space", "has space"},
		{"too-long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, // 64 chars
		{"empty", ""},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Create(ctx, tc.slug, "", "", Private)
			if !errors.Is(err, ErrInvalidSlug) {
				t.Fatalf("create %q: want ErrInvalidSlug, got %v", tc.slug, err)
			}
		})
	}
}

func TestReservedNameReject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"mcp", ".well-known"} {
		t.Run(name, func(t *testing.T) {
			_, err := s.Create(ctx, name, "", "", Private)
			if !errors.Is(err, ErrReservedName) {
				t.Fatalf("create %q: want ErrReservedName, got %v", name, err)
			}
		})
	}
}

func TestCreateDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, "dup", "", "", Private); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := s.Create(ctx, "dup", "", "", Private)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate create: want ErrExists, got %v", err)
	}
}

func TestGetDeleteNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Get(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: want ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: want ErrNotFound, got %v", err)
	}
}

func TestCreatePersistsOwnerIdentityAndRequestedVisibility(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, "creator-site", "id-alpha", "shared@example.com", Private)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// R-QSLO-SAIQ
	if created.Visibility != Private {
		t.Fatalf("Create Visibility = %q, want private", created.Visibility)
	}
	// R-Z3ZN-5BFE
	if created.OwnerID != "id-alpha" || created.OwnerEmail != "shared@example.com" {
		t.Fatalf("Create owner = (%q, %q), want (%q, %q)", created.OwnerID, created.OwnerEmail, "id-alpha", "shared@example.com")
	}

	got, err := s.Get(ctx, "creator-site")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OwnerID != "id-alpha" || got.OwnerEmail != "shared@example.com" {
		t.Fatalf("Get owner = (%q, %q), want (%q, %q)", got.OwnerID, got.OwnerEmail, "id-alpha", "shared@example.com")
	}
	if got.Visibility != Private {
		t.Fatalf("Get Visibility = %q, want private", got.Visibility)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if list[0].OwnerID != "id-alpha" || list[0].OwnerEmail != "shared@example.com" {
		t.Fatalf("List owner = (%q, %q), want (%q, %q)", list[0].OwnerID, list[0].OwnerEmail, "id-alpha", "shared@example.com")
	}
	if list[0].Visibility != Private {
		t.Fatalf("List Visibility = %q, want private", list[0].Visibility)
	}

	publicCreated, err := s.Create(ctx, "public-creator-site", "unrelated-id-beta", "shared@example.com", Public)
	if err != nil {
		t.Fatalf("create public: %v", err)
	}
	// R-QSLO-SAIQ
	if publicCreated.Visibility != Public {
		t.Fatalf("Create public Visibility = %q, want public", publicCreated.Visibility)
	}
	publicGot, err := s.Get(ctx, "public-creator-site")
	if err != nil {
		t.Fatalf("get public: %v", err)
	}
	if publicGot.Visibility != Public {
		t.Fatalf("Get public Visibility = %q, want public", publicGot.Visibility)
	}

	list, err = s.List(ctx)
	if err != nil {
		t.Fatalf("list after public create: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	byName := map[string]Site{}
	for _, site := range list {
		byName[site.Name] = site
	}
	if byName["creator-site"].OwnerID != "id-alpha" || byName["public-creator-site"].OwnerID != "unrelated-id-beta" {
		t.Fatalf("same-email sites owner ids = (%q, %q), want distinct passed ids", byName["creator-site"].OwnerID, byName["public-creator-site"].OwnerID)
	}
	if byName["public-creator-site"].OwnerEmail != "shared@example.com" {
		t.Fatalf("public site OwnerEmail = %q, want snapshot %q", byName["public-creator-site"].OwnerEmail, "shared@example.com")
	}
	if byName["creator-site"].Visibility != Private {
		t.Fatalf("List creator-site Visibility = %q, want private", byName["creator-site"].Visibility)
	}
	if byName["public-creator-site"].Visibility != Public {
		t.Fatalf("List public-creator-site Visibility = %q, want public", byName["public-creator-site"].Visibility)
	}

	for _, column := range []string{"visibility", "owner_id", "owner_email"} {
		var found int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('sites') WHERE name = ?`, column).Scan(&found); err != nil {
			t.Fatalf("query schema column %q: %v", column, err)
		}
		if found != 1 {
			t.Fatalf("schema column %q count = %d, want 1", column, found)
		}
	}
}

func TestSitesSchemaDropsPublishLifecycleColumns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_info('sites')`)
	if err != nil {
		t.Fatalf("query schema columns: %v", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan schema column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("schema rows: %v", err)
	}

	// R-Z57J-J363
	// R-H31B-PAEW
	for _, name := range []string{"visibility", "owner_id", "owner_email"} {
		if !columns[name] {
			t.Fatalf("schema missing %q column; columns=%v", name, columns)
		}
	}
	for _, name := range []string{"public", "created_by", "tier", "published", "published_at"} {
		if columns[name] {
			t.Fatalf("schema still has %q column; columns=%v", name, columns)
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sites
		(name, visibility, owner_id, owner_email, created_at, updated_at)
		VALUES ('bad-visibility', 'bogus', 'owner', 'owner@example.com', 'now', 'now')`)
	if err == nil {
		t.Fatal("SQLite accepted visibility outside the CHECK enum")
	}
}

func TestCreatePersistsEveryVisibilityVerbatim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	want := map[string]Visibility{"a-public": Public, "b-private": Private, "c-unlisted": Unlisted}
	for name, visibility := range want {
		created, err := s.Create(ctx, name, "owner", "owner@example.com", visibility)
		if err != nil {
			t.Fatalf("Create(%q, %q): %v", name, visibility, err)
		}
		// R-H0LI-XQXI
		if created.Visibility != visibility {
			t.Fatalf("Create(%q) visibility = %q, want %q", name, created.Visibility, visibility)
		}
		got, err := s.Get(ctx, name)
		if err != nil || got.Visibility != visibility {
			t.Fatalf("Get(%q) = %+v, %v; want visibility %q", name, got, err, visibility)
		}
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
	for _, site := range list {
		if site.Visibility != want[site.Name] {
			t.Fatalf("List site %q visibility = %q, want %q", site.Name, site.Visibility, want[site.Name])
		}
	}
}

func TestSetVisibilityUpdatesAndRenamesAtomically(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	site, err := s.Create(ctx, "visibility", "owner-id", "owner@example.com", Private)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// R-H1TF-BIO7
	if err := s.SetVisibility(ctx, "visibility", Public, ""); err != nil {
		t.Fatalf("set public: %v", err)
	}
	public, err := s.Get(ctx, "visibility")
	if err != nil {
		t.Fatalf("get public: %v", err)
	}
	if public.Visibility != Public || public.Name != "visibility" {
		t.Fatalf("after set without rename = %+v, want same name and public", public)
	}
	if !public.UpdatedAt.After(site.UpdatedAt) {
		t.Fatalf("after SetVisibility true UpdatedAt = %v, want after %v", public.UpdatedAt, site.UpdatedAt)
	}

	if err := s.SetVisibility(ctx, "visibility", Unlisted, "fresh-token"); err != nil {
		t.Fatalf("set unlisted and rename: %v", err)
	}
	if _, err := s.Get(ctx, "visibility"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old name after rename: want ErrNotFound, got %v", err)
	}
	unlisted, err := s.Get(ctx, "fresh-token")
	if err != nil {
		t.Fatalf("get renamed: %v", err)
	}
	if unlisted.Visibility != Unlisted {
		t.Fatalf("renamed Visibility = %q, want unlisted", unlisted.Visibility)
	}
	if !unlisted.UpdatedAt.After(public.UpdatedAt) {
		t.Fatalf("renamed UpdatedAt = %v, want after %v", unlisted.UpdatedAt, public.UpdatedAt)
	}

	if err := s.SetVisibility(ctx, "missing", Public, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetVisibility: want ErrNotFound, got %v", err)
	}
	other, err := s.Create(ctx, "occupied", "other-id", "other@example.com", Private)
	if err != nil {
		t.Fatalf("create collision target: %v", err)
	}
	if err := s.SetVisibility(ctx, "fresh-token", Public, "occupied"); !errors.Is(err, ErrExists) {
		t.Fatalf("collision: want ErrExists, got %v", err)
	}
	unchanged, err := s.Get(ctx, "fresh-token")
	if err != nil || unchanged.Visibility != Unlisted {
		t.Fatalf("source changed after collision: %+v, %v", unchanged, err)
	}
	stillOther, err := s.Get(ctx, "occupied")
	if err != nil || stillOther != other {
		t.Fatalf("destination changed after collision: %+v, %v; want %+v", stillOther, err, other)
	}
}

func TestVisibilityMigrationCarriesBooleanRowsAcross(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.db")
	conn, err := sqlkit.Open(path)
	if err != nil {
		t.Fatalf("open migration db: %v", err)
	}
	defer conn.Close()
	entries, err := fs.ReadDir(db.FS, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var enumSQL []byte
	for _, entry := range entries {
		body, err := fs.ReadFile(db.FS, "migrations/"+entry.Name())
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if strings.HasSuffix(entry.Name(), "_visibility_enum.sql") {
			enumSQL = body
			continue
		}
		if _, err := conn.Exec(string(body)); err != nil {
			t.Fatalf("apply old migration %s: %v", entry.Name(), err)
		}
	}
	if len(enumSQL) == 0 {
		t.Fatal("visibility enum migration not found")
	}
	_, err = conn.Exec(`INSERT INTO sites
		(name, public, owner_id, owner_email, created_at, updated_at) VALUES
		('was-public', 1, 'one', 'one@example.com', 'created', 'updated'),
		('was-private', 0, 'two', 'two@example.com', 'created', 'updated')`)
	if err != nil {
		t.Fatalf("seed pre-enum rows: %v", err)
	}
	if _, err := conn.Exec(string(enumSQL)); err != nil {
		t.Fatalf("apply visibility migration: %v", err)
	}
	// R-H498-325L
	rows, err := conn.Query(`SELECT name, visibility FROM sites ORDER BY name`)
	if err != nil {
		t.Fatalf("query migrated rows: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, visibility string
		if err := rows.Scan(&name, &visibility); err != nil {
			t.Fatalf("scan migrated row: %v", err)
		}
		got[name] = visibility
	}
	if got["was-public"] != "public" || got["was-private"] != "private" || len(got) != 2 {
		t.Fatalf("migrated rows = %v, want both rows mapped", got)
	}
}
