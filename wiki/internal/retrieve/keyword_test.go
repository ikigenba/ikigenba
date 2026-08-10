package retrieve

import (
	"context"
	"database/sql"
	"testing"

	appdb "appkit/db"
	wikidb "wiki/internal/db"
	wikidomain "wiki/internal/wiki"
)

func TestFTSPhraseQuotesTermsAndORsLiterals(t *testing.T) {
	// R-23RE-KCMW
	tests := map[string]string{
		`alpha beta`:             `"alpha" OR "beta"`,
		`alpha "beta" NEAR(foo)`: `"alpha" OR """beta""" OR "NEAR(foo)"`,
		`  `:                     "",
	}
	for input, want := range tests {
		if got := ftsPhrase(input); got != want {
			t.Fatalf("ftsPhrase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestKeywordRetrieverSearchReturnsRankedLimitedPageHits(t *testing.T) {
	// R-24ZA-Y4DL
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := wikidomain.NewSubjectStore(conn)
	for _, subject := range []wikidomain.Subject{
		{ID: "subject-alpha", Name: "Alpha Lab", NormName: "alpha-lab", Type: "entity"},
		{ID: "subject-beta", Name: "Beta Lab", NormName: "beta-lab", Type: "entity"},
		{ID: "subject-gamma", Name: "Gamma Lab", NormName: "gamma-lab", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save subject %s: %v", subject.ID, err)
		}
	}
	pages := wikidomain.NewPageStore(conn)
	for _, page := range []wikidomain.Page{
		{ID: "page-alpha", SubjectID: "subject-alpha", Title: "Alpha Lab", Body: "Tulsa alpha launch notes include Tulsa alpha telemetry."},
		{ID: "page-beta", SubjectID: "subject-beta", Title: "Beta Lab", Body: "Tulsa logistics mention the beta warehouse."},
		{ID: "page-gamma", SubjectID: "subject-gamma", Title: "Gamma Lab", Body: "Unrelated archive entry."},
	} {
		if err := pages.Upsert(ctx, page); err != nil {
			t.Fatalf("Upsert page %s: %v", page.ID, err)
		}
	}

	got, err := NewKeywordRetriever(conn).Search(ctx, "default", `Tulsa OR ignored`, SearchLimits{Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Hits) != 1 {
		t.Fatalf("hits = %+v, want exactly one hit capped by limit", got.Hits)
	}
	hit := got.Hits[0]
	if hit.PageID != "subject-alpha" || hit.Path != "entity/alpha-lab" || hit.Title != "Alpha Lab" {
		t.Fatalf("first hit = %+v, want ranked Alpha Lab page identity", hit)
	}
	if hit.Snippet == "" {
		t.Fatalf("first hit snippet is empty, want matched snippet: %+v", hit)
	}
}

func TestKeywordRetrieverSearchIsScopeBounded(t *testing.T) {
	// R-H61U-NE20
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	scopes := wikidomain.NewScopeStore(conn)
	for _, name := range []string{"s1", "s2"} {
		if _, err := scopes.Create(ctx, name); err != nil {
			t.Fatalf("Create scope %s: %v", name, err)
		}
	}
	for _, row := range []struct{ id, scope, name string }{
		{id: "subject-s1", scope: "s1", name: "Quiet Page"},
		{id: "subject-s2", scope: "s2", name: "Matching Page"},
	} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO subjects (id, scope, name, norm_name, type) VALUES (?, ?, ?, ?, 'entity')`, row.id, row.scope, row.name, row.id); err != nil {
			t.Fatalf("insert %s subject: %v", row.scope, err)
		}
	}
	pages := wikidomain.NewPageStore(conn)
	if err := pages.Upsert(ctx, wikidomain.Page{ID: "page-s1", SubjectID: "subject-s1", Title: "Quiet", Body: "No relevant words here."}); err != nil {
		t.Fatalf("Upsert s1 page: %v", err)
	}
	if err := pages.Upsert(ctx, wikidomain.Page{ID: "page-s2", SubjectID: "subject-s2", Title: "Matching", Body: "scopewallneedle appears only over here"}); err != nil {
		t.Fatalf("Upsert s2 page: %v", err)
	}

	got, err := NewKeywordRetriever(conn).Search(ctx, "s1", "scopewallneedle", SearchLimits{})
	if err != nil {
		t.Fatalf("Search s1: %v", err)
	}
	if len(got.Hits) != 0 {
		t.Fatalf("s1 hits = %+v, want no result from matching s2 page", got.Hits)
	}
}

func migratedDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	conn, err := appdb.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	migs, err := appdb.LoadMigrations(wikidb.FS, "migrations")
	if err != nil {
		conn.Close()
		t.Fatalf("LoadMigrations: %v", err)
	}
	if err := appdb.Migrate(ctx, conn, migs); err != nil {
		conn.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return conn
}
