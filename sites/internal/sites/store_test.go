package sites

import (
	"context"
	"errors"
	"path/filepath"
	"sites/internal/db"
	"strings"
	"testing"
	"time"

	sqlkit "appkit/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	conn, err := sqlkit.Open(filepath.Join(t.TempDir(), "sites_test.db"))
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
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	s := NewStore(conn)
	s.Now = func() time.Time {
		now = now.Add(time.Millisecond)
		return now
	}
	return s
}

func TestCreatePersistsSlugNameVisibilityAndOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cases := []struct {
		slug, name, ownerID, ownerEmail string
		visibility                      Visibility
	}{
		{"a-public", "Q3 Launch Page", "owner-alpha", "shared@example.com", Public},
		{"b-private", "Private label", "owner-beta", "shared@example.com", Private},
		{"c-unlisted", "秘密のサイト", "owner-gamma", "gamma@example.com", Unlisted},
	}
	for _, tc := range cases {
		created, err := s.Create(ctx, tc.slug, tc.name, tc.ownerID, tc.ownerEmail, tc.visibility, "")
		if err != nil {
			t.Fatalf("Create(%q): %v", tc.slug, err)
		}
		// R-Z9L9-PCXQ
		if created.Slug != tc.slug || created.Name != tc.name || created.Visibility != tc.visibility {
			t.Fatalf("Create(%q) = %+v, want slug/name/visibility verbatim", tc.slug, created)
		}
		// R-ZD8Y-UO5T
		if created.OwnerID != tc.ownerID || created.OwnerEmail != tc.ownerEmail {
			t.Fatalf("Create(%q) owner = (%q,%q), want (%q,%q)", tc.slug, created.OwnerID, created.OwnerEmail, tc.ownerID, tc.ownerEmail)
		}
		got, err := s.Get(ctx, tc.slug)
		if err != nil || got != created {
			t.Fatalf("Get(%q) = %+v, %v; want %+v", tc.slug, got, err, created)
		}
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != len(cases) {
		t.Fatalf("List len = %d, want %d", len(list), len(cases))
	}
	for i, tc := range cases {
		if list[i].Slug != tc.slug || list[i].Name != tc.name || list[i].Visibility != tc.visibility || list[i].OwnerID != tc.ownerID || list[i].OwnerEmail != tc.ownerEmail {
			t.Fatalf("List[%d] = %+v, want verbatim values for %+v", i, list[i], tc)
		}
	}
	if err := s.Delete(ctx, "a-public"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "a-public"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if _, err := s.Create(ctx, "b-private", "duplicate slug", "x", "x@example.com", Public, ""); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate Create = %v, want ErrExists", err)
	}
}

func TestSetVisibilityPreservesNameAndHandlesSlugCollision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	original, err := s.Create(ctx, "old-slug", "Display Name", "owner", "owner@example.com", Private, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVisibility(ctx, original.Slug, Public, ""); err != nil {
		t.Fatalf("SetVisibility without slug: %v", err)
	}
	public, err := s.Get(ctx, original.Slug)
	// R-ZAT6-34OF
	if err != nil || public.Slug != original.Slug || public.Name != original.Name || public.Visibility != Public || !public.UpdatedAt.After(original.UpdatedAt) {
		t.Fatalf("visibility-only update = %+v, %v; original %+v", public, err, original)
	}
	if err := s.SetVisibility(ctx, original.Slug, Unlisted, "new-slug"); err != nil {
		t.Fatalf("SetVisibility with slug: %v", err)
	}
	if _, err := s.Get(ctx, original.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old slug Get = %v, want ErrNotFound", err)
	}
	renamed, err := s.Get(ctx, "new-slug")
	if err != nil || renamed.Slug != "new-slug" || renamed.Name != original.Name || renamed.Visibility != Unlisted || !renamed.UpdatedAt.After(public.UpdatedAt) {
		t.Fatalf("slug+visibility update = %+v, %v", renamed, err)
	}
	if err := s.SetVisibility(ctx, "missing", Public, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetVisibility = %v, want ErrNotFound", err)
	}
	other, err := s.Create(ctx, "occupied", "Other Name", "other", "other@example.com", Private, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVisibility(ctx, "new-slug", Public, "occupied"); !errors.Is(err, ErrExists) {
		t.Fatalf("collision = %v, want ErrExists", err)
	}
	stillSource, sourceErr := s.Get(ctx, "new-slug")
	stillOther, otherErr := s.Get(ctx, "occupied")
	if sourceErr != nil || otherErr != nil || stillSource != renamed || stillOther != other {
		t.Fatalf("collision changed rows: source=%+v/%v other=%+v/%v", stillSource, sourceErr, stillOther, otherErr)
	}
}

func TestRenameChangesOnlyDisplayName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	before, err := s.Create(ctx, "rename-me", "Old Name", "owner-id", "snapshot@example.com", Private, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Rename(ctx, before.Slug, "New Display Name"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	after, err := s.Get(ctx, before.Slug)
	// R-ZC12-GWF4
	if err != nil || after.Name != "New Display Name" || after.Slug != before.Slug || after.Visibility != before.Visibility || after.OwnerID != before.OwnerID || after.OwnerEmail != before.OwnerEmail || after.CreatedAt != before.CreatedAt || !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("after Rename = %+v, %v; before %+v", after, err, before)
	}
	if err := s.Rename(ctx, "missing", "Name"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Rename = %v, want ErrNotFound", err)
	}
}

func TestSitesSchemaNameSlugSplitConstraints(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	type column struct {
		typeName string
		notNull  int
		pk       int
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name, type, "notnull", pk FROM pragma_table_info('sites')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]column{}
	for rows.Next() {
		var name string
		var c column
		if err := rows.Scan(&name, &c.typeName, &c.notNull, &c.pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = c
	}
	// R-ZEGV-8FWI
	if columns["slug"].typeName != "TEXT" || columns["slug"].pk != 1 || columns["name"].typeName != "TEXT" || columns["name"].notNull != 1 || columns["visibility"].typeName != "TEXT" {
		t.Fatalf("required columns wrong: %v", columns)
	}
	for _, retired := range []string{"public", "created_by", "tier", "published", "published_at"} {
		if _, ok := columns[retired]; ok {
			t.Fatalf("retired column %q remains: %v", retired, columns)
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sites (slug,name,visibility,owner_id,owner_email,created_at,updated_at) VALUES ('bad','Bad','bogus','id','e','c','u')`)
	if err == nil {
		t.Fatal("SQLite accepted bogus visibility")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sites (slug,name,visibility,owner_id,owner_email,created_at,updated_at) VALUES ('null-name',NULL,'public','id','e','c','u')`)
	if err == nil {
		t.Fatal("SQLite accepted NULL name")
	}
}

func TestVersionPlaneMigrationPreservesRowsAndStoreAccessors(t *testing.T) {
	ctx := context.Background()
	conn, err := sqlkit.Open(filepath.Join(t.TempDir(), "version_plane_test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := sqlkit.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migs) < 2 {
		t.Fatalf("migration count = %d, want prior set plus additive migration", len(migs))
	}
	if err := sqlkit.Migrate(ctx, conn, migs[:len(migs)-1]); err != nil {
		t.Fatalf("migrate through pre-version-plane schema: %v", err)
	}

	original := []string{
		"middle", "Display \x00 bytes", "private", "owner-\x00id",
		"snapshot@example.com", "/Dropbox/\x00source", "2026-08-01T01:02:03.000000000Z",
		"2026-08-02T04:05:06.000000000Z",
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO sites
		(slug, name, visibility, owner_id, owner_email, source_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		original[0], original[1], original[2], original[3], original[4], original[5], original[6], original[7])
	if err != nil {
		t.Fatalf("insert pre-plane row: %v", err)
	}
	if err := sqlkit.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("apply additive version-plane migration: %v", err)
	}

	type column struct {
		typeName     string
		notNull      int
		defaultValue string
	}
	columns := map[string]column{}
	rows, err := conn.QueryContext(ctx, `SELECT name, type, "notnull", coalesce(dflt_value, '') FROM pragma_table_info('sites')`)
	if err != nil {
		t.Fatalf("read sites columns: %v", err)
	}
	for rows.Next() {
		var name string
		var c column
		if err := rows.Scan(&name, &c.typeName, &c.notNull, &c.defaultValue); err != nil {
			rows.Close()
			t.Fatalf("scan sites column: %v", err)
		}
		columns[name] = c
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close sites columns: %v", err)
	}
	// R-OG3H-7TUS
	if got := columns["repo_sha"]; got.typeName != "TEXT" || got.notNull != 1 || got.defaultValue != "''" {
		t.Fatalf("repo_sha column = %+v, want TEXT NOT NULL DEFAULT ''", got)
	}
	if got := columns["repo_seeded"]; got.typeName != "INTEGER" || got.notNull != 1 || got.defaultValue != "0" {
		t.Fatalf("repo_seeded column = %+v, want INTEGER NOT NULL DEFAULT 0", got)
	}
	// R-3SOP-GMQL
	if got := columns["path"]; got.typeName != "TEXT" || got.notNull != 1 || got.defaultValue != "''" {
		t.Fatalf("path column = %+v, want TEXT NOT NULL DEFAULT ''", got)
	}

	var preserved [8]string
	var defaultSha, defaultPath string
	var defaultSeeded bool
	err = conn.QueryRowContext(ctx, `SELECT slug, name, visibility, owner_id, owner_email, source_path,
		created_at, updated_at, repo_sha, repo_seeded, path FROM sites WHERE slug = ?`, original[0]).Scan(
		&preserved[0], &preserved[1], &preserved[2], &preserved[3], &preserved[4], &preserved[5],
		&preserved[6], &preserved[7], &defaultSha, &defaultSeeded, &defaultPath,
	)
	if err != nil {
		t.Fatalf("read preserved pre-plane row: %v", err)
	}
	for i := range original {
		if preserved[i] != original[i] {
			t.Fatalf("pre-plane column %d = %q, want byte-identical %q", i, preserved[i], original[i])
		}
	}
	if defaultSha != "" || defaultSeeded || defaultPath != "" {
		t.Fatalf("pre-plane defaults = (%q, %t, %q), want (empty, false, empty)", defaultSha, defaultSeeded, defaultPath)
	}

	s := NewStore(conn)
	before, err := s.Get(ctx, original[0])
	if err != nil {
		t.Fatalf("Get pre-plane row: %v", err)
	}
	if err := s.SetRepoSha(ctx, before.Slug, "abc123"); err != nil {
		t.Fatalf("SetRepoSha: %v", err)
	}
	afterSha, err := s.Get(ctx, before.Slug)
	if err != nil {
		t.Fatalf("Get after SetRepoSha: %v", err)
	}
	wantAfterSha := before
	wantAfterSha.RepoSha = "abc123"
	if afterSha != wantAfterSha {
		t.Fatalf("SetRepoSha changed non-sha fields: got %+v, want %+v", afterSha, wantAfterSha)
	}
	if err := s.MarkSeeded(ctx, before.Slug); err != nil {
		t.Fatalf("MarkSeeded first call: %v", err)
	}
	if err := s.MarkSeeded(ctx, before.Slug); err != nil {
		t.Fatalf("MarkSeeded idempotent call: %v", err)
	}
	seeded, err := s.Get(ctx, before.Slug)
	if err != nil || !seeded.RepoSeeded {
		t.Fatalf("Get after MarkSeeded = %+v, %v; want seeded", seeded, err)
	}

	for _, slug := range []string{"zeta", "alpha"} {
		if _, err := s.Create(ctx, slug, slug+" name", "owner", "owner@example.com", Public, ""); err != nil {
			t.Fatalf("Create(%q): %v", slug, err)
		}
	}
	unseeded, err := s.ListUnseeded(ctx)
	if err != nil {
		t.Fatalf("ListUnseeded: %v", err)
	}
	if len(unseeded) != 2 || unseeded[0].Slug != "alpha" || unseeded[1].Slug != "zeta" || unseeded[0].RepoSeeded || unseeded[1].RepoSeeded {
		t.Fatalf("ListUnseeded = %+v, want exactly alpha then zeta", unseeded)
	}

	clock := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	rooted, err := s.Create(ctx, "rooted", "Rooted Site", "root-owner", "root@example.com", Private, "public/output")
	if err != nil {
		t.Fatalf("Create non-empty path: %v", err)
	}
	empty, err := s.Create(ctx, "empty-path", "Empty Path", "empty-owner", "empty@example.com", Public, "")
	if err != nil {
		t.Fatalf("Create empty path: %v", err)
	}
	gotRooted, err := s.Get(ctx, rooted.Slug)
	if err != nil || gotRooted.Path != "public/output" || rooted.Path != "public/output" {
		t.Fatalf("non-empty path Get/Create = %+v/%+v, %v", gotRooted, rooted, err)
	}
	gotEmpty, err := s.Get(ctx, empty.Slug)
	if err != nil || gotEmpty.Path != "" || empty.Path != "" {
		t.Fatalf("empty path Get/Create = %+v/%+v, %v", gotEmpty, empty, err)
	}
	listed, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List paths: %v", err)
	}
	listedPaths := map[string]string{}
	for _, site := range listed {
		listedPaths[site.Slug] = site.Path
	}
	if listedPaths[rooted.Slug] != "public/output" || listedPaths[empty.Slug] != "" {
		t.Fatalf("List paths = %v, want rooted and empty values", listedPaths)
	}
	if err := s.SetRepoSha(ctx, rooted.Slug, "keep-sha"); err != nil {
		t.Fatalf("SetRepoSha before SetPath: %v", err)
	}
	beforePath, err := s.Get(ctx, rooted.Slug)
	if err != nil {
		t.Fatalf("Get before SetPath: %v", err)
	}
	if err := s.SetPath(ctx, rooted.Slug, "dist"); err != nil {
		t.Fatalf("SetPath: %v", err)
	}
	afterPath, err := s.Get(ctx, rooted.Slug)
	if err != nil {
		t.Fatalf("Get after SetPath: %v", err)
	}
	wantAfterPath := beforePath
	wantAfterPath.Path = "dist"
	wantAfterPath.UpdatedAt = afterPath.UpdatedAt
	if afterPath != wantAfterPath || !afterPath.UpdatedAt.After(beforePath.UpdatedAt) {
		t.Fatalf("SetPath changed wrong fields: before=%+v after=%+v want=%+v", beforePath, afterPath, wantAfterPath)
	}
	if err := s.SetPath(ctx, "missing", "dist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetPath = %v, want ErrNotFound", err)
	}
}

func TestValidateName(t *testing.T) {
	// R-ZFOR-M7N7
	got, err := ValidateName("  Q3 Launch Page ")
	if err != nil || got != "Q3 Launch Page" {
		t.Fatalf("ValidateName valid = %q, %v", got, err)
	}
	for _, raw := range []string{"", " \t ", strings.Repeat("界", 101), "a\nb"} {
		if got, err := ValidateName(raw); got != "" || !errors.Is(err, ErrInvalidName) {
			t.Errorf("ValidateName(%q) = %q, %v; want ErrInvalidName", raw, got, err)
		}
	}
}
