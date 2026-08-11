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

func TestScopeStoreInstructionsRoundTripClearAndUnknownScope(t *testing.T) {
	// R-8H3G-3MDO
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewScopeStore(db)
	if _, err := store.Create(ctx, "team-a"); err != nil {
		t.Fatalf("Create(team-a): %v", err)
	}
	text := "  standing context\nwith trailing whitespace \n"
	if err := store.SetInstructions(ctx, "team-a", text); err != nil {
		t.Fatalf("SetInstructions(team-a): %v", err)
	}
	scope, err := store.Get(ctx, "team-a")
	if err != nil || scope.Instructions != text {
		t.Fatalf("Get(team-a) instructions = %q, %v; want %q", scope.Instructions, err, text)
	}
	scopes, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var listed string
	for _, item := range scopes {
		if item.Name == "team-a" {
			listed = item.Instructions
		}
	}
	if listed != text {
		t.Fatalf("List team-a instructions = %q, want %q", listed, text)
	}
	if err := store.SetInstructions(ctx, "team-a", ""); err != nil {
		t.Fatalf("clear instructions: %v", err)
	}
	if scope, err := store.Get(ctx, "team-a"); err != nil || scope.Instructions != "" {
		t.Fatalf("Get(team-a) after clear = %+v, %v; want empty instructions", scope, err)
	}
	if err := store.SetInstructions(ctx, "missing", "do not store"); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("SetInstructions(missing) = %v, want ErrScopeNotFound", err)
	}
	if got := countRows(t, ctx, db, `SELECT COUNT(*) FROM scopes WHERE name = 'missing'`); got != 0 {
		t.Fatalf("unknown scope row count = %d, want 0", got)
	}
}

func TestScopeStoreInstructionsEnforcesUnicodeCharacterCapWithoutChangingValue(t *testing.T) {
	// R-8JJ8-V5V2
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewScopeStore(db)
	if _, err := store.Create(ctx, "team-a"); err != nil {
		t.Fatalf("Create(team-a): %v", err)
	}
	if err := store.SetInstructions(ctx, "team-a", "original"); err != nil {
		t.Fatalf("set original instructions: %v", err)
	}
	if err := store.SetInstructions(ctx, "team-a", strings.Repeat("界", InstructionsCharCap+1)); !errors.Is(err, ErrInstructionsTooLong) {
		t.Fatalf("SetInstructions(4001 runes) = %v, want ErrInstructionsTooLong", err)
	}
	if scope, err := store.Get(ctx, "team-a"); err != nil || scope.Instructions != "original" {
		t.Fatalf("Get(team-a) after rejection = %+v, %v; want original instructions", scope, err)
	}
	want := strings.Repeat("界", InstructionsCharCap)
	if err := store.SetInstructions(ctx, "team-a", want); err != nil {
		t.Fatalf("SetInstructions(4000 runes): %v", err)
	}
	if scope, err := store.Get(ctx, "team-a"); err != nil || scope.Instructions != want {
		t.Fatalf("Get(team-a) accepted instruction rune count = %d, %v; want %d", len([]rune(scope.Instructions)), err, InstructionsCharCap)
	}
}

func TestComposeSystemPreservesEmptyBaseAndWrapsInstructionsExactly(t *testing.T) {
	// R-8KR5-8XLR
	base := "base system\nwith exact ending "
	instructions := "  scope text\n"
	tests := []struct {
		name, instructions, want string
	}{
		{"empty", "", base},
		{"non-empty", instructions, base + "\n\nScope instructions (these override the general rules above where they conflict):\n" + instructions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ComposeSystem(base, test.instructions); got != test.want {
				t.Fatalf("ComposeSystem() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSetInstructionsInvalidatesOnlyChangedScopesCachedSystem(t *testing.T) {
	// R-0EYF-BC3U
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewScopeStore(db)
	for _, name := range []string{"s1", "s2"} {
		if _, err := store.Create(ctx, name); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	cache := map[string]string{}
	computes := map[string]int{}
	ask := func(scope string) string {
		if answer, ok := cache[scope]; ok {
			return answer
		}
		registered, err := store.Get(ctx, scope)
		if err != nil {
			t.Fatalf("Get(%s): %v", scope, err)
		}
		computes[scope]++
		cache[scope] = ComposeSystem("base synthesis system", registered.Instructions)
		return cache[scope]
	}
	store.AskInvalidate = func(scope string) { delete(cache, scope) }
	if got := ask("s1"); got != "base synthesis system" {
		t.Fatalf("initial s1 system = %q", got)
	}
	_ = ask("s2")
	if err := store.SetInstructions(ctx, "s1", "prefer current runbooks"); err != nil {
		t.Fatalf("SetInstructions: %v", err)
	}
	want := ComposeSystem("base synthesis system", "prefer current runbooks")
	if got := ask("s1"); got != want || computes["s1"] != 2 {
		t.Fatalf("s1 recomputation = %q with %d computes, want %q with 2", got, computes["s1"], want)
	}
	if got := ask("s2"); got != "base synthesis system" || computes["s2"] != 1 {
		t.Fatalf("s2 cache = %q with %d computes, want unchanged hit", got, computes["s2"])
	}
}

func TestDeleteInvalidatesCachedAnswerBeforeSameNamedScopeCanReturn(t *testing.T) {
	// R-0G6B-P3UJ
	ctx := context.Background()
	db := migratedDB(t, ctx)
	defer db.Close()
	store := NewScopeStore(db)
	if _, err := store.Create(ctx, "s1"); err != nil {
		t.Fatalf("Create(s1): %v", err)
	}
	cache := map[string]string{"s1": "answer from deleted corpus"}
	computes := 0
	ask := func() (string, error) {
		if answer, ok := cache["s1"]; ok {
			return answer, nil
		}
		registered, err := store.Get(ctx, "s1")
		if err != nil {
			return "", err
		}
		computes++
		answer := "empty scope answer: " + registered.Instructions
		cache["s1"] = answer
		return answer, nil
	}
	store.AskInvalidate = func(scope string) { delete(cache, scope) }
	if err := store.Delete(ctx, "s1"); err != nil {
		t.Fatalf("Delete(s1): %v", err)
	}
	if got, ok := cache["s1"]; ok {
		t.Fatalf("post-delete ask could serve cached %q", got)
	}
	if _, err := ask(); !errors.Is(err, ErrScopeNotFound) {
		t.Fatalf("fresh post-delete ask = %v, want ErrScopeNotFound", err)
	}
	if _, err := store.Create(ctx, "s1"); err != nil {
		t.Fatalf("recreate s1: %v", err)
	}
	answer, err := ask()
	if err != nil || answer != "empty scope answer: " || computes != 1 {
		t.Fatalf("ask after recreate = %q, %v with %d computes; want freshly computed empty answer", answer, err, computes)
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
