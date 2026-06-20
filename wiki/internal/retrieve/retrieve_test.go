package retrieve

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"wiki/internal/db"
	wikipkg "wiki/internal/wiki"
)

func TestSearchLimitsResolveClampsCallerK(t *testing.T) {
	// R-CLF2-TMI8
	limits := SearchLimits{Default: 5, Cap: 20}

	tests := map[string]struct {
		k    int
		want int
	}{
		"default for zero":     {k: 0, want: 5},
		"default for negative": {k: -1, want: 5},
		"inside cap":           {k: 12, want: 12},
		"cap high value":       {k: 100, want: 20},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := limits.Resolve(tc.k); got != tc.want {
				t.Fatalf("Resolve(%d) = %d, want %d", tc.k, got, tc.want)
			}
		})
	}

	if got := (SearchLimits{Default: 5}).Resolve(1); got != 0 {
		t.Fatalf("Resolve with zero cap = %d, want 0", got)
	}
}

func TestKeywordSearchUsesFTSAndBM25Order(t *testing.T) {
	// R-CMMZ-7E8X
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, "subject-alpha", "Alpha", "concept", "Alpha Notes", "alpha alpha retrieval lane")
	savePage(t, ctx, conn, "subject-beta", "Beta", "concept", "Beta Notes", "alpha retrieval")
	savePage(t, ctx, conn, "subject-gamma", "Gamma", "concept", "Gamma Notes", "unrelated")

	hits, err := NewKeyword(conn).Search(ctx, "alpha", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2: %+v", len(hits), hits)
	}
	if hits[0].SubjectID != "subject-alpha" || hits[0].Title != "Alpha Notes" {
		t.Fatalf("first hit = %+v, want subject-alpha ordered first by BM25", hits[0])
	}
	if !strings.Contains(hits[0].Snippet, "alpha") {
		t.Fatalf("first snippet = %q, want matched excerpt containing alpha", hits[0].Snippet)
	}
	if hits[1].SubjectID != "subject-beta" {
		t.Fatalf("second hit = %+v, want subject-beta", hits[1])
	}
}

func TestKeywordHitPopulatesStablePageKeyAndVersion(t *testing.T) {
	// R-CNUV-L5ZM
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `ALTER TABLE pages ADD COLUMN version INTEGER NOT NULL DEFAULT 7`); err != nil {
		t.Fatalf("add pages.version: %v", err)
	}
	savePage(t, ctx, conn, "subject-versioned", "Versioned", "entity", "Versioned Page", "version provenance keyword")

	hits, err := NewKeyword(conn).Search(ctx, "provenance", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1: %+v", len(hits), hits)
	}
	if hits[0].SubjectID != "subject-versioned" || hits[0].PageID != "subject-versioned" {
		t.Fatalf("hit identity = subject %q page %q, want subject id as stable phase-one page key",
			hits[0].SubjectID, hits[0].PageID)
	}
	if hits[0].Version != 7 {
		t.Fatalf("hit.Version = %d, want pages.version provenance 7", hits[0].Version)
	}
}

func TestServicePinsExactRegistryMatchFirstAndDedupes(t *testing.T) {
	// R-CP2R-YXQB
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, "subject-cafe", "Café Noir", "entity", "Café Noir", "registry pin body")

	retriever := &scriptedRetriever{hits: []Hit{
		{SubjectID: "subject-other", PageID: "subject-other", Title: "Keyword Winner"},
		{SubjectID: "subject-cafe", PageID: "subject-cafe", Title: "Duplicate Keyword Hit"},
	}}
	service := NewService(conn, retriever, SearchLimits{Default: 10, Cap: 10})

	hits, err := service.Search(ctx, "  Cafe\u0301 Noir  ", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want pinned hit plus one deduped keyword hit: %+v", len(hits), hits)
	}
	if hits[0].SubjectID != "subject-cafe" || hits[0].Title != "Café Noir" {
		t.Fatalf("first hit = %+v, want exact registry match pinned first", hits[0])
	}
	if hits[1].SubjectID != "subject-other" {
		t.Fatalf("second hit = %+v, want non-duplicate retriever hit", hits[1])
	}
}

func TestServiceResolvesLimitsBeforeRetrieverAndCapsPinnedResults(t *testing.T) {
	// R-CQAO-CPH0
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, "subject-pin", "Pinned", "concept", "Pinned", "pinned body")

	retriever := &scriptedRetriever{hits: []Hit{
		{SubjectID: "subject-a", PageID: "subject-a", Title: "A"},
		{SubjectID: "subject-b", PageID: "subject-b", Title: "B"},
		{SubjectID: "subject-c", PageID: "subject-c", Title: "C"},
	}}
	service := NewService(conn, retriever, SearchLimits{Default: 3, Cap: 2})

	hits, err := service.Search(ctx, "Pinned", 99)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if retriever.gotK != 2 {
		t.Fatalf("retriever k = %d, want resolved cap 2", retriever.gotK)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want capped output of 2: %+v", len(hits), hits)
	}
	if hits[0].SubjectID != "subject-pin" || hits[1].SubjectID != "subject-a" {
		t.Fatalf("hits = %+v, want pinned result followed by first retriever hit within cap", hits)
	}
}

type scriptedRetriever struct {
	hits []Hit
	gotK int
}

func (r *scriptedRetriever) Search(_ context.Context, _ string, k int) ([]Hit, error) {
	r.gotK = k
	return r.hits, nil
}

func migratedDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	conn, err := db.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx, conn); err != nil {
		conn.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return conn
}

func savePage(t *testing.T, ctx context.Context, conn *sql.DB, subjectID, name, typ, title, body string) {
	t.Helper()

	subjects := wikipkg.NewSubjectStore(conn)
	if err := subjects.Save(ctx, wikipkg.Subject{ID: subjectID, Name: name, Type: typ}); err != nil {
		t.Fatalf("Save subject %s: %v", subjectID, err)
	}
	pages := wikipkg.NewPageStore(conn)
	if err := pages.Upsert(ctx, wikipkg.Page{ID: subjectID, SubjectID: subjectID, Title: title, Body: body}); err != nil {
		t.Fatalf("Upsert page %s: %v", subjectID, err)
	}
}
