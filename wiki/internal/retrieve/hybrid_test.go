package retrieve

import (
	"context"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"wiki/internal/llm"

	wikidomain "wiki/internal/wiki"
)

func TestHybridRetrieverFusesRRFAndDedupesOverlappingPages(t *testing.T) {
	// R-79KD-1622
	// R-7AS9-EXSR
	ctx := context.Background()
	keyword := newSpyRetriever(map[string][]Hit{
		"launch": {
			{PageID: "keyword-only", Title: "Keyword Only"},
			{PageID: "shared", Title: "Shared"},
		},
	})
	vector := newSpyRetriever(map[string][]Hit{
		"launch": {
			{PageID: "vector-only", Title: "Vector Only", Score: 0.9},
			{PageID: "shared", Title: "Shared", Score: 0.8},
		},
	})
	retriever := NewHybridRetriever(keyword, vector, nil, nil, FusionConfig{RRFk: 60, PerLane: 3, FinalK: 3})

	got, err := retriever.Search(ctx, "default", "launch", SearchLimits{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Hits) != 3 {
		t.Fatalf("hits = %+v, want three fused hits", got.Hits)
	}
	if got.Hits[0].PageID != "shared" {
		t.Fatalf("first hit = %+v, want overlapping page to outrank rank-1 one-lane pages", got.Hits[0])
	}
	wantScore := 1.0/62.0 + 1.0/62.0
	if math.Abs(got.Hits[0].Score-wantScore) > 0.0000001 {
		t.Fatalf("shared score = %.10f, want RRF %.10f", got.Hits[0].Score, wantScore)
	}
	if countPage(got.Hits, "shared") != 1 {
		t.Fatalf("hits = %+v, want shared page deduped exactly once", got.Hits)
	}
}

func TestHybridRetrieverPinsExactAliasSubjectOnce(t *testing.T) {
	// R-7C05-SPJG
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	subjects := wikidomain.NewSubjectStore(conn)
	if err := subjects.Save(ctx, wikidomain.Subject{
		ID:   "subject-acme",
		Name: "Acme Robotics",
		Type: "entity",
	}); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := wikidomain.NewAliasStore(conn).Insert(ctx, wikidomain.Alias{
		Name:      "The Widget",
		SubjectID: "subject-acme",
		OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
		CreatedAt: "2026-06-24T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	pages := wikidomain.NewPageStore(conn)
	if err := pages.Upsert(ctx, wikidomain.Page{
		ID:        "page-acme",
		SubjectID: "subject-acme",
		Title:     "Acme Robotics Page",
		Body:      "Acme page body.",
	}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}

	keyword := newSpyRetriever(map[string][]Hit{
		"The Widget": {
			{PageID: "other", Title: "Other"},
			{PageID: "subject-acme", Title: "Acme from lane"},
		},
		"unknown subject": {
			{PageID: "other", Title: "Other"},
		},
	})
	vector := newSpyRetriever(nil)
	retriever := NewHybridRetriever(keyword, vector, wikidomain.NewResolver(conn), pages, FusionConfig{FinalK: 3})

	got, err := retriever.Search(ctx, "default", "The Widget", SearchLimits{})
	if err != nil {
		t.Fatalf("Search exact alias: %v", err)
	}
	if !got.Pinned {
		t.Fatalf("Pinned = false, want exact alias pin")
	}
	if len(got.Hits) == 0 || got.Hits[0].PageID != "subject-acme" || got.Hits[0].Path != "entity/acme-robotics" {
		t.Fatalf("first hit = %+v, want pinned Acme page at rank 1", got.Hits)
	}
	if countPage(got.Hits, "subject-acme") != 1 {
		t.Fatalf("hits = %+v, want pinned page not duplicated lower", got.Hits)
	}

	got, err = retriever.Search(ctx, "default", "unknown subject", SearchLimits{})
	if err != nil {
		t.Fatalf("Search unknown subject: %v", err)
	}
	if got.Pinned {
		t.Fatalf("Pinned = true for unknown subject, want fused list without pin")
	}
	if len(got.Hits) != 1 || got.Hits[0].PageID != "other" {
		t.Fatalf("unknown hits = %+v, want unpinned fused list", got.Hits)
	}
}

func TestHybridRetrieverPinsExactNameWithinAskedScope(t *testing.T) {
	// R-RG1Q-6WK2
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if _, err := wikidomain.NewScopeStore(conn).Create(ctx, "team"); err != nil {
		t.Fatalf("Create team scope: %v", err)
	}
	subjects := wikidomain.NewSubjectStore(conn)
	if err := subjects.Save(ctx, "team", wikidomain.Subject{ID: "team-lumen", Name: "Project Lumen", Type: "entity"}); err != nil {
		t.Fatalf("Save team subject: %v", err)
	}
	pages := wikidomain.NewPageStore(conn)
	if err := pages.Upsert(ctx, wikidomain.Page{ID: "page-team-lumen", SubjectID: "team-lumen", Title: "Project Lumen", Body: "Team Lumen body."}); err != nil {
		t.Fatalf("Upsert team page: %v", err)
	}
	retriever := NewHybridRetriever(nil, nil, wikidomain.NewResolver(conn), pages, FusionConfig{})

	got, err := retriever.Search(ctx, "team", "Project Lumen", SearchLimits{})
	if err != nil {
		t.Fatalf("Search team: %v", err)
	}
	if !got.Pinned || len(got.Hits) != 1 || got.Hits[0].PageID != "team-lumen" {
		t.Fatalf("Search team = %+v, want team subject pinned first by subject id", got)
	}
}

func TestHybridRetrieverExactNamePinCannotCrossScopeWall(t *testing.T) {
	// R-RH9M-KOAR
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	scopes := wikidomain.NewScopeStore(conn)
	for _, scope := range []string{"team-a", "team-b"} {
		if _, err := scopes.Create(ctx, scope); err != nil {
			t.Fatalf("Create %s: %v", scope, err)
		}
	}
	subjects := wikidomain.NewSubjectStore(conn)
	pages := wikidomain.NewPageStore(conn)
	for _, item := range []struct {
		scope string
		id    string
		name  string
	}{
		{scope: "default", id: "default-only", name: "Default Secret"},
		{scope: "team-a", id: "team-a-lumen", name: "Project Lumen"},
		{scope: "team-b", id: "team-b-lumen", name: "Project Lumen"},
	} {
		if err := subjects.Save(ctx, item.scope, wikidomain.Subject{ID: item.id, Name: item.name, Type: "entity"}); err != nil {
			t.Fatalf("Save %s subject: %v", item.scope, err)
		}
		if err := pages.Upsert(ctx, wikidomain.Page{ID: "page-" + item.id, SubjectID: item.id, Title: item.name, Body: item.scope + " body"}); err != nil {
			t.Fatalf("Upsert %s page: %v", item.scope, err)
		}
	}
	retriever := NewHybridRetriever(nil, nil, wikidomain.NewResolver(conn), pages, FusionConfig{})

	foreign, err := retriever.Search(ctx, "team-a", "Default Secret", SearchLimits{})
	if err != nil {
		t.Fatalf("Search foreign-only name: %v", err)
	}
	if foreign.Pinned || len(foreign.Hits) != 0 {
		t.Fatalf("foreign-only Search = %+v, want no cross-scope pin", foreign)
	}
	for _, tc := range []struct {
		scope string
		want  string
	}{{scope: "team-a", want: "team-a-lumen"}, {scope: "team-b", want: "team-b-lumen"}} {
		got, err := retriever.Search(ctx, tc.scope, "Project Lumen", SearchLimits{})
		if err != nil {
			t.Fatalf("Search %s same-name subject: %v", tc.scope, err)
		}
		if !got.Pinned || len(got.Hits) != 1 || got.Hits[0].PageID != tc.want {
			t.Fatalf("Search %s = %+v, want only %s pinned", tc.scope, got, tc.want)
		}
	}
}

func TestHybridRetrieverRequestsPerLaneAndHonorsFinalK(t *testing.T) {
	// R-7D82-6HA5
	ctx := context.Background()
	laneHits := []Hit{
		{PageID: "a", Title: "A"},
		{PageID: "b", Title: "B"},
		{PageID: "c", Title: "C"},
		{PageID: "d", Title: "D"},
	}
	keyword := newSpyRetriever(map[string][]Hit{"query": laneHits})
	vector := newSpyRetriever(map[string][]Hit{"query": laneHits})
	retriever := NewHybridRetriever(keyword, vector, nil, nil, FusionConfig{PerLane: 4, FinalK: 3})

	got, err := retriever.Search(ctx, "default", "query", SearchLimits{})
	if err != nil {
		t.Fatalf("Search FinalK 3: %v", err)
	}
	if len(got.Hits) != 3 {
		t.Fatalf("hits = %+v, want FinalK 3", got.Hits)
	}
	if keyword.calls[0].limits.Limit != 4 || vector.calls[0].limits.Limit != 4 {
		t.Fatalf("lane limits = keyword %d vector %d, want PerLane 4", keyword.calls[0].limits.Limit, vector.calls[0].limits.Limit)
	}

	retriever.cfg.FinalK = 1
	got, err = retriever.Search(ctx, "default", "query", SearchLimits{})
	if err != nil {
		t.Fatalf("Search FinalK 1: %v", err)
	}
	if len(got.Hits) != 1 {
		t.Fatalf("hits = %+v, want changed FinalK 1", got.Hits)
	}
}

func TestHybridRetrieverSearchAnalyzedFansOutAndRoutesLaneQueries(t *testing.T) {
	// R-Q8RI-7POG
	// R-NDHM-I6Z9
	ctx := context.Background()
	qa := wikidomain.QueryAnalysis{
		SubQueries: []string{
			"How does Alpha Lab handle launch?",
			"How does Beta Lab handle launch?",
		},
		Keywords: []string{"launch", "cost"},
		Aliases:  []string{"Alpha Lab", "Beta Lab"},
	}
	keywordQuery := "launch cost Alpha Lab Beta Lab"
	keyword := newSpyRetriever(map[string][]Hit{
		keywordQuery: {
			{PageID: "shared-launch", Title: "Shared Launch"},
		},
	})
	vector := newSpyRetriever(map[string][]Hit{
		"How does Alpha Lab handle launch?": {
			{PageID: "subject-alpha", Title: "Alpha Lab", Score: 0.9},
		},
		"How does Beta Lab handle launch?": {
			{PageID: "subject-beta", Title: "Beta Lab", Score: 0.8},
		},
		"Compare Alpha Lab and Beta Lab": {
			{PageID: "blended-only", Title: "Blended Only", Score: 0.7},
		},
	})
	retriever := NewHybridRetriever(keyword, vector, nil, nil, FusionConfig{PerLane: 5, FinalK: 5})

	blended, err := retriever.Search(ctx, "default", "Compare Alpha Lab and Beta Lab", SearchLimits{})
	if err != nil {
		t.Fatalf("Search blended: %v", err)
	}
	if containsPage(blended.Hits, "subject-alpha") || containsPage(blended.Hits, "subject-beta") {
		t.Fatalf("blended hits = %+v, want no separate Alpha/Beta pages", blended.Hits)
	}
	keyword.calls = nil
	vector.calls = nil

	got, err := retriever.SearchAnalyzed(ctx, "default", llm.Attribution{}, qa, SearchLimits{})
	if err != nil {
		t.Fatalf("SearchAnalyzed: %v", err)
	}
	if !containsPage(got.Hits, "subject-alpha") || !containsPage(got.Hits, "subject-beta") {
		t.Fatalf("analyzed hits = %+v, want both sub-query pages in one fused list", got.Hits)
	}
	wantMeaning := []string{qa.SubQueries[0], qa.SubQueries[1]}
	if gotMeaning := callQueries(vector.calls); !reflect.DeepEqual(gotMeaning, wantMeaning) {
		t.Fatalf("meaning lane queries = %#v, want %#v", gotMeaning, wantMeaning)
	}
	wantKeyword := []string{keywordQuery, keywordQuery}
	if gotKeyword := callQueries(keyword.calls); !reflect.DeepEqual(gotKeyword, wantKeyword) {
		t.Fatalf("keyword lane queries = %#v, want %#v", gotKeyword, wantKeyword)
	}
	for _, term := range strings.Fields(keywordQuery) {
		if term == "OR" {
			t.Fatalf("keyword lane query %q contains injected OR operator", keywordQuery)
		}
	}
}

func TestPorterStemmedFTSMatchesMorphologicalVariant(t *testing.T) {
	// R-NC9Q-4F8K
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `INSERT INTO subjects (id, scope, name, norm_name, type) VALUES ('subject-king', 'default', 'King', 'king', 'concept')`); err != nil {
		t.Fatalf("insert subject: %v", err)
	}
	if err := wikidomain.NewPageStore(conn).Upsert(ctx, wikidomain.Page{
		ID: "page-king", SubjectID: "subject-king", Title: "King", Body: "A king rules the realm.",
	}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}

	var title string
	if err := conn.QueryRowContext(ctx, `
		SELECT p.title
		FROM pages_fts
		JOIN pages p ON p.rowid = pages_fts.rowid
		WHERE pages_fts MATCH '"kings"'`).Scan(&title); err != nil {
		t.Fatalf("MATCH morphological variant kings: %v", err)
	}
	if title != "King" {
		t.Fatalf("MATCH kings title = %q, want base-form King page", title)
	}
}

func TestHybridRetrieverExplicitLimitOverridesConfiguredDefaultAndClamps(t *testing.T) {
	// R-NEPI-VYPY
	ctx := context.Background()
	laneHits := make([]Hit, 25)
	for i := range laneHits {
		laneHits[i] = Hit{PageID: "page-" + strconv.Itoa(i), Title: "Page " + strconv.Itoa(i)}
	}
	qa := wikidomain.QueryAnalysis{SubQueries: []string{"full natural language question"}, Keywords: []string{"keyword"}}
	keyword := newSpyRetriever(map[string][]Hit{"keyword": laneHits})
	retriever := NewHybridRetriever(keyword, nil, nil, nil, FusionConfig{FinalK: 8, PerLane: 25})

	got, err := retriever.SearchAnalyzed(ctx, "default", llm.Attribution{}, qa, SearchLimits{Limit: 20})
	if err != nil {
		t.Fatalf("SearchAnalyzed limit 20: %v", err)
	}
	if len(got.Hits) != 20 {
		t.Fatalf("limit 20 returned %d hits, want 20 (more than configured FinalK 8)", len(got.Hits))
	}

	got, err = retriever.SearchAnalyzed(ctx, "default", llm.Attribution{}, qa, SearchLimits{Limit: LimitCap + 10})
	if err != nil {
		t.Fatalf("SearchAnalyzed above cap: %v", err)
	}
	if len(got.Hits) != LimitCap {
		t.Fatalf("limit above cap returned %d hits, want LimitCap %d", len(got.Hits), LimitCap)
	}
}

type spyRetriever struct {
	results map[string][]Hit
	calls   []spyCall
}

type spyCall struct {
	query  string
	limits SearchLimits
}

func newSpyRetriever(results map[string][]Hit) *spyRetriever {
	if results == nil {
		results = map[string][]Hit{}
	}
	return &spyRetriever{results: results}
}

func (s *spyRetriever) Search(_ context.Context, _ string, query string, limits SearchLimits) (Result, error) {
	s.calls = append(s.calls, spyCall{query: query, limits: limits})
	return Result{Hits: append([]Hit(nil), s.results[query]...)}, nil
}

func countPage(hits []Hit, pageID string) int {
	var count int
	for _, hit := range hits {
		if hit.PageID == pageID {
			count++
		}
	}
	return count
}

func containsPage(hits []Hit, pageID string) bool {
	return countPage(hits, pageID) > 0
}

func callQueries(calls []spyCall) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, call.query)
	}
	return out
}
