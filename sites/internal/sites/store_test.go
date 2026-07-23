package sites

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlkit "appkit/db"

	"sites/internal/db"
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
		created, err := s.Create(ctx, tc.slug, tc.name, tc.ownerID, tc.ownerEmail, tc.visibility)
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
	if _, err := s.Create(ctx, "b-private", "duplicate slug", "x", "x@example.com", Public); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate Create = %v, want ErrExists", err)
	}
}

func TestSetVisibilityPreservesNameAndHandlesSlugCollision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	original, err := s.Create(ctx, "old-slug", "Display Name", "owner", "owner@example.com", Private)
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
	other, err := s.Create(ctx, "occupied", "Other Name", "other", "other@example.com", Private)
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
	before, err := s.Create(ctx, "rename-me", "Old Name", "owner-id", "snapshot@example.com", Private)
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
