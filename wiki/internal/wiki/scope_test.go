package wiki

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"wiki/internal/page"
)

func TestValidateScopeNameAcceptsOnlyExactScopeSlugs(t *testing.T) {
	// R-GTUU-TON2
	valid := []string{"platform", "team-x", "a", strings.Repeat("a", 64)}
	for _, name := range valid {
		if err := ValidateScopeName(name); err != nil {
			t.Errorf("ValidateScopeName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", strings.Repeat("a", 65), "Team", "team x", "téam", "-x", "x-"}
	for _, name := range invalid {
		err := ValidateScopeName(name)
		if !errors.Is(err, ErrInvalidScopeName) {
			t.Errorf("ValidateScopeName(%q) = %v, want ErrInvalidScopeName", name, err)
		}
	}
}

func TestScopeStoreCreatesListsAndUpdatesRegistryRows(t *testing.T) {
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewScopeStore(db)
	for _, name := range []string{"zeta", "alpha"} {
		scope, err := store.Create(ctx, name)
		if err != nil {
			t.Fatalf("Create(%q): %v", name, err)
		}
		if scope.Name != name || scope.Visibility != "private" || scope.CreatedAt == 0 {
			t.Errorf("Create(%q) = %+v, want private scope with creation time", name, scope)
		}
	}
	if _, err := store.Create(ctx, "default"); !errors.Is(err, ErrScopeExists) {
		t.Fatalf("Create(default) = %v, want ErrScopeExists", err)
	}
	if _, err := store.Create(ctx, "Bad Name"); !errors.Is(err, ErrInvalidScopeName) {
		t.Fatalf("Create(Bad Name) = %v, want ErrInvalidScopeName", err)
	}
	if err := store.SetVisibility(ctx, "alpha", "public"); err != nil {
		t.Fatalf("SetVisibility(alpha): %v", err)
	}
	alpha, err := store.Get(ctx, "alpha")
	if err != nil || alpha.Visibility != "public" {
		t.Fatalf("Get(alpha) = %+v, %v; want public", alpha, err)
	}
	scopes, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	gotNames := make([]string, len(scopes))
	for i, scope := range scopes {
		gotNames[i] = scope.Name
	}
	if got, want := strings.Join(gotNames, ","), "alpha,default,zeta"; got != want {
		t.Fatalf("List names = %q, want %q", got, want)
	}
	if err := store.SetVisibility(ctx, "missing", "public"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("SetVisibility(missing) = %v, want ErrScopeNotFound", err)
	}
}

func TestScopeStoreDeleteProtectsDefaultAndDeletesOnlyTargetScopeContent(t *testing.T) {
	// R-H3M1-VUKM
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewScopeStore(db)
	for _, name := range []string{"default", "team-a", "team-b"} {
		if name != "default" {
			if _, err := store.Create(ctx, name); err != nil {
				t.Fatalf("Create(%q): %v", name, err)
			}
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO jobs (id, scope, status, owner_id, owner_email) VALUES (?, ?, 'done', 'owner', 'owner@example.com')`, "job-"+name, name); err != nil {
			t.Fatalf("seed %s job: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO subjects (id, scope, name, norm_name, type) VALUES (?, ?, ?, 'same', 'entity')`, "subject-"+name, name, name); err != nil {
			t.Fatalf("seed %s subject: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO claims (id, subject_id, job_id, body) VALUES (?, ?, ?, 'claim')`, "claim-"+name, "subject-"+name, "job-"+name); err != nil {
			t.Fatalf("seed %s claim: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO pages (id, subject_id, title, body) VALUES (?, ?, 'title', 'body')`, "page-"+name, "subject-"+name); err != nil {
			t.Fatalf("seed %s page: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO aliases (scope, norm_name, subject_id, name, owner_id, owner_email, created_at) VALUES (?, 'old', ?, 'Old', 'owner', 'owner@example.com', 'now')`, name, "subject-"+name); err != nil {
			t.Fatalf("seed %s alias: %v", name, err)
		}
	}

	if err := store.Delete(ctx, "default"); !errors.Is(err, ErrScopeIsDefault) {
		t.Fatalf("Delete(default) = %v, want ErrScopeIsDefault", err)
	}
	if got := countRows(t, ctx, db, `SELECT COUNT(*) FROM scopes`); got != 3 {
		t.Fatalf("scope count after Delete(default) = %d, want 3", got)
	}
	if got := countRows(t, ctx, db, `SELECT COUNT(*) FROM subjects WHERE scope = 'default'`); got != 1 {
		t.Fatalf("default subject count after refused delete = %d, want 1", got)
	}
	if err := store.Delete(ctx, "team-a"); err != nil {
		t.Fatalf("Delete(team-a): %v", err)
	}
	for table, query := range map[string]string{
		"scope":    `SELECT COUNT(*) FROM scopes WHERE name = 'team-a'`,
		"subjects": `SELECT COUNT(*) FROM subjects WHERE scope = 'team-a'`,
		"claims":   `SELECT COUNT(*) FROM claims WHERE subject_id = 'subject-team-a'`,
		"pages":    `SELECT COUNT(*) FROM pages WHERE subject_id = 'subject-team-a'`,
		"aliases":  `SELECT COUNT(*) FROM aliases WHERE scope = 'team-a'`,
		"jobs":     `SELECT COUNT(*) FROM jobs WHERE scope = 'team-a'`,
	} {
		if got := countRows(t, ctx, db, query); got != 0 {
			t.Errorf("deleted %s count = %d, want 0", table, got)
		}
	}
	for _, scope := range []string{"default", "team-b"} {
		for table, query := range map[string]string{
			"scope":    `SELECT COUNT(*) FROM scopes WHERE name = '` + scope + `'`,
			"subjects": `SELECT COUNT(*) FROM subjects WHERE scope = '` + scope + `'`,
			"claims":   `SELECT COUNT(*) FROM claims WHERE subject_id = 'subject-` + scope + `'`,
			"pages":    `SELECT COUNT(*) FROM pages WHERE subject_id = 'subject-` + scope + `'`,
			"aliases":  `SELECT COUNT(*) FROM aliases WHERE scope = '` + scope + `'`,
			"jobs":     `SELECT COUNT(*) FROM jobs WHERE scope = '` + scope + `'`,
		} {
			if got := countRows(t, ctx, db, query); got != 1 {
				t.Errorf("other-scope %s %s count = %d, want 1", scope, table, got)
			}
		}
	}
}

func TestSubjectListInUnknownScopeReturnsNotFoundWithoutCreatingIt(t *testing.T) {
	// R-H4TY-9MBB
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	_, _, err := NewSubjectStore(db).ListInScope(ctx, "typo", "", "", page.Params{})
	if !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("ListInScope(typo) = %v, want ErrScopeNotFound", err)
	}
	if got := countRows(t, ctx, db, `SELECT COUNT(*) FROM scopes WHERE name = 'typo'`); got != 0 {
		t.Fatalf("unknown scope row count = %d, want 0", got)
	}
}

func countRows(t *testing.T, ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}
