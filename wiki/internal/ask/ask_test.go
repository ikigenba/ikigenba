package ask

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	appdb "appkit/db"
	agentkit "github.com/ikigenba/agentkit"

	wikidb "wiki/internal/db"
	"wiki/internal/llm"
	"wiki/internal/retrieve"
	"wiki/internal/wiki"
)

func TestAskRetrievesAnalyzedQuestionAndSynthesizesRetrievedPages(t *testing.T) {
	// R-BAFW-D24P
	// R-6A8D-0RK9
	// R-05CG-3H6Y
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada body should not be sent to synthesis.",
	})
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-grace", Name: "Grace", Type: "entity"}, wiki.Page{
		ID:        "page-grace",
		SubjectID: "subject-grace",
		Title:     "Grace",
		Body:      "Grace owns the scheduler.",
	})
	search := &scriptedSearch{result: retrieve.Result{
		Hits:     []retrieve.Hit{{PageID: "subject-grace", Path: "entity/grace", Title: "Grace"}},
		TopDense: 0.72,
	}}
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{
			"sub_queries": ["Ada"],
			"keywords": ["scheduler"],
			"aliases": ["Amazing Grace"]
		}`),
		textRoundTrip(`{
			"found": true,
			"text": "Grace owns the scheduler.",
			"citations": [{"path":"entity/grace","title":"Grace"}]
		}`),
	}}

	got, err := New(search, wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who owns the scheduler?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || got.Text != "Grace owns the scheduler." {
		t.Fatalf("Ask = %+v, want synthesized found answer", got)
	}
	if want := []Citation{{Path: "entity/grace", Title: "Grace"}}; !reflect.DeepEqual(got.Citations, want) {
		t.Fatalf("citations = %+v, want %+v", got.Citations, want)
	}
	citationsJSON, err := json.Marshal(got.Citations)
	if err != nil {
		t.Fatalf("Marshal citations: %v", err)
	}
	if strings.Contains(string(citationsJSON), "subject-grace") {
		t.Fatalf("citations JSON = %s, want no internal subject id", citationsJSON)
	}
	if len(search.calls) != 1 {
		t.Fatalf("SearchAnalyzed calls = %d, want 1", len(search.calls))
	}
	if want := (wiki.QueryAnalysis{SubQueries: []string{"Ada"}, Keywords: []string{"scheduler"}, Aliases: []string{"Amazing Grace"}}); !reflect.DeepEqual(search.calls[0].qa, want) {
		t.Fatalf("SearchAnalyzed qa = %+v, want %+v", search.calls[0].qa, want)
	}
	if search.calls[0].limits.Limit != defaultFinalK {
		t.Fatalf("SearchAnalyzed limit = %d, want default finalK %d", search.calls[0].limits.Limit, defaultFinalK)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want analysis then synthesis", len(prov.requests))
	}
	synthText := requestText(prov.requests[1])
	if !strings.Contains(synthText, "Grace owns the scheduler.") {
		t.Fatalf("synth prompt = %q, want retrieved Grace page body", synthText)
	}
	if strings.Contains(synthText, "Ada body should not be sent") || strings.Contains(synthText, "subject-grace") {
		t.Fatalf("synth prompt = %q, want retrieved public page context without exact-name or internal-id grounding", synthText)
	}
}

func TestAskHonestEmptyFloorSkipsSynthesisUnlessPinnedOrDenseEnough(t *testing.T) {
	// R-BBNS-QTVE
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada wrote the note.",
	})

	lowSearch := &scriptedSearch{result: retrieve.Result{
		Hits:     []retrieve.Hit{{PageID: "subject-ada", Path: "entity/ada", Title: "Ada"}},
		TopDense: 0.29,
		Pinned:   false,
	}}
	lowProv := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{"found":true,"text":"should not run","citations":[{"path":"entity/ada","title":"Ada"}]}`),
	}}
	got, err := New(lowSearch, wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(lowProv, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask below floor returned error: %v", err)
	}
	if got.Found || got.Text != honestEmptyText || len(got.Citations) != 0 {
		t.Fatalf("Ask below floor = %+v, want honest-empty answer", got)
	}
	if len(lowProv.requests) != 1 {
		t.Fatalf("provider requests below floor = %d, want analysis only", len(lowProv.requests))
	}

	pinnedSearch := &scriptedSearch{result: retrieve.Result{
		Hits:     []retrieve.Hit{{PageID: "subject-ada", Path: "entity/ada", Title: "Ada"}},
		TopDense: 0.01,
		Pinned:   true,
	}}
	pinnedProv := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{"found":true,"text":"Ada wrote the note.","citations":[{"path":"entity/ada","title":"Ada"}]}`),
	}}
	got, err = New(pinnedSearch, wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(pinnedProv, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask pinned returned error: %v", err)
	}
	if !got.Found || got.Text != "Ada wrote the note." {
		t.Fatalf("Ask pinned = %+v, want synthesis despite low dense score", got)
	}
	if len(pinnedProv.requests) != 2 {
		t.Fatalf("provider requests pinned = %d, want analysis and synthesis", len(pinnedProv.requests))
	}
}

func TestAskRelevanceFloorIsConfigurableThreshold(t *testing.T) {
	// R-BCVP-4LM3
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada wrote the note.",
	})
	result := retrieve.Result{
		Hits:     []retrieve.Hit{{PageID: "subject-ada", Path: "entity/ada", Title: "Ada"}},
		TopDense: 0.42,
	}

	highProv := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{"found":true,"text":"should not run","citations":[{"path":"entity/ada","title":"Ada"}]}`),
	}}
	got, err := New(&scriptedSearch{result: result}, wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(highProv, nil), testExtractSite(), testSynthSite(), WithRelevanceFloor(0.50)).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask high floor returned error: %v", err)
	}
	if got.Found || len(highProv.requests) != 1 {
		t.Fatalf("high floor Ask = %+v with %d provider requests, want honest-empty before synthesis", got, len(highProv.requests))
	}

	lowProv := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{"found":true,"text":"Ada wrote the note.","citations":[{"path":"entity/ada","title":"Ada"}]}`),
	}}
	got, err = New(&scriptedSearch{result: result}, wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(lowProv, nil), testExtractSite(), testSynthSite(), WithRelevanceFloor(0.40)).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask low floor returned error: %v", err)
	}
	if !got.Found || len(lowProv.requests) != 2 {
		t.Fatalf("low floor Ask = %+v with %d provider requests, want synthesis", got, len(lowProv.requests))
	}
}

func TestAskDowngradesFoundAnswerWithoutGroundedCitations(t *testing.T) {
	// R-5UPD-VVNA
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada wrote the note.",
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{"found":true,"text":"Ada wrote the note.","citations":[]}`),
	}}

	got, err := New(oneHitSearch("subject-ada", 0.8), wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if got.Found || got.Text != honestEmptyText || len(got.Citations) != 0 {
		t.Fatalf("Ask = %+v, want found-without-citations downgraded to honest-empty", got)
	}
}

func TestAskDowngradesFoundAnswerWithoutText(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada wrote the note.",
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{"found":true,"text":"   ","citations":[{"path":"entity/ada","title":"Ada"}]}`),
	}}

	got, err := New(oneHitSearch("subject-ada", 0.8), wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if got.Found || got.Text != honestEmptyText || len(got.Citations) != 0 {
		t.Fatalf("Ask = %+v, want empty answer text downgraded to honest-empty", got)
	}
}

func TestAskDropsCitationsOutsideRetrievedPages(t *testing.T) {
	// R-5VXA-9NDZ
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada wrote the note.",
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{
			"found": true,
			"text": "Ada wrote the note.",
			"citations": [
				{"path":"entity/grace","title":"Grace"},
				{"path":"entity/ada","title":"Ada"}
			]
		}`),
	}}

	got, err := New(oneHitSearch("subject-ada", 0.8), wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if want := []Citation{{Path: "entity/ada", Title: "Ada"}}; !reflect.DeepEqual(got.Citations, want) {
		t.Fatalf("citations = %+v, want only retrieved citation %+v", got.Citations, want)
	}
}

func TestAskSynthesisUsesOnlyRetrievedPageBodies(t *testing.T) {
	// R-690G-MZTK
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Compiled page body says Ada approved the release.",
	})
	if err := wiki.NewClaimStore(conn).Save(ctx, wiki.Claim{
		ID:        "claim-raw",
		SubjectID: "subject-ada",
		JobID:     "job-secret",
		Body:      "RAW CLAIM TEXT SHOULD NOT REACH SYNTHESIS",
	}); err != nil {
		t.Fatalf("Save claim: %v", err)
	}
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`{
			"found": true,
			"text": "Ada approved the release.",
			"citations": [{"path":"entity/ada","title":"Ada"}]
		}`),
	}}

	got, err := New(oneHitSearch("subject-ada", 0.8), wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who approved the release?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || got.Text != "Ada approved the release." {
		t.Fatalf("Ask = %+v, want page-grounded answer", got)
	}
	for i, req := range prov.requests {
		if len(req.Tools) != 0 {
			t.Fatalf("request %d tools = %#v, want tool-less ask pipeline", i, req.Tools)
		}
	}
	synthText := requestText(prov.requests[1])
	if !strings.Contains(synthText, "Compiled page body says Ada approved the release.") {
		t.Fatalf("synth prompt = %q, want compiled page body", synthText)
	}
	for _, forbidden := range []string{"RAW CLAIM TEXT SHOULD NOT REACH SYNTHESIS", "job-secret", "read_source", "claims"} {
		if strings.Contains(synthText, forbidden) {
			t.Fatalf("synth prompt = %q, want no %q", synthText, forbidden)
		}
	}
}

func TestAskDoesNotWriteOnHonestEmptyOrParseFailure(t *testing.T) {
	// R-5X56-NF4O
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada wrote the note.",
	})

	before := totalChanges(t, conn)
	emptyProv := &askProvider{responses: []*agentkit.RoundTrip{textRoundTrip(`{"sub_queries":["Ada"]}`)}}
	got, err := New(&scriptedSearch{result: retrieve.Result{Hits: []retrieve.Hit{{PageID: "subject-ada"}}, TopDense: 0.01}}, wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(emptyProv, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("honest-empty Ask returned error: %v", err)
	}
	if got.Found {
		t.Fatalf("honest-empty Ask = %+v, want not found", got)
	}
	if after := totalChanges(t, conn); after != before {
		t.Fatalf("total_changes after honest-empty = %d, want unchanged %d", after, before)
	}

	parseProv := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
		textRoundTrip(`not json`),
	}}
	_, err = New(oneHitSearch("subject-ada", 0.8), wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(parseProv, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err == nil {
		t.Fatal("parse-failure Ask error = nil, want error")
	}
	if after := totalChanges(t, conn); after != before {
		t.Fatalf("total_changes after parse failure = %d, want unchanged %d", after, before)
	}
}

func TestDefaultAskCallSitesUseSeparateReasoningLowStages(t *testing.T) {
	// R-GHQC-OEYL
	subject := DefaultSubjectCallSite()
	synthesis := DefaultSynthesisCallSite()
	if subject.Stage != "ask-subject" {
		t.Fatalf("subject stage = %q, want ask-subject", subject.Stage)
	}
	if synthesis.Stage != "ask-synthesis" {
		t.Fatalf("synthesis stage = %q, want ask-synthesis", synthesis.Stage)
	}
	for name, site := range map[string]llm.CallSite{
		"subject":   subject,
		"synthesis": synthesis,
	} {
		if site.MaxTokens != 16384 {
			t.Fatalf("%s MaxTokens = %d, want 16384", name, site.MaxTokens)
		}
		if !reflect.DeepEqual(site.Reasoning, agentkit.Level("low")) {
			t.Fatalf("%s reasoning = %#v, want low level", name, site.Reasoning)
		}
	}
}

func TestAnalyzeRunsOneAskSubjectCallAndParsesQueryAnalysis(t *testing.T) {
	// R-QB7A-Z95U
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{
			"sub_queries": ["Ada release", "Grace scheduler"],
			"keywords": ["release", "scheduler"],
			"aliases": ["G. Hopper"]
		}`),
	}}
	site := llm.CallSite{Stage: "ask-subject", Model: "analysis-model", System: "analysis system", MaxTokens: 123}

	got, err := Analyze(context.Background(), llm.New(prov, nil), site, "How did Ada and Grace handle the release?")
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	want := wiki.QueryAnalysis{
		SubQueries: []string{"Ada release", "Grace scheduler"},
		Keywords:   []string{"release", "scheduler"},
		Aliases:    []string{"G. Hopper"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Analyze = %+v, want %+v", got, want)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want one analysis call", len(prov.requests))
	}
	req := prov.requests[0]
	if req.Model != "analysis-model" || req.System != "analysis system" || req.Gen.MaxTokens != 123 {
		t.Fatalf("request = model %q system %q max_tokens %d, want injected call site", req.Model, req.System, req.Gen.MaxTokens)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("analysis tools = %#v, want tool-less JSON call", req.Tools)
	}
	prompt := requestText(req)
	for _, want := range []string{"sub_queries", "keywords", "aliases", "How did Ada and Grace handle the release?"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("analysis prompt = %q, want %q", prompt, want)
		}
	}
}

func TestAnalyzeNormalizesAndCapsPreparedQuestion(t *testing.T) {
	// R-QCF7-D0WJ
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{
			"sub_queries": ["  Ada  ", "", "Grace", "ada", "Linus", "Margaret", "Katherine"],
			"keywords": [" release ", "", "Release", "scheduler"],
			"aliases": [" G. Hopper ", "g. hopper", "", "Amazing Grace"]
		}`),
	}}

	got, err := Analyze(context.Background(), llm.New(prov, nil), testExtractSite(), "question")
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if want := []string{"Ada", "Grace", "Linus", "Margaret"}; !reflect.DeepEqual(got.SubQueries, want) {
		t.Fatalf("sub_queries = %#v, want trimmed unique values capped to %#v", got.SubQueries, want)
	}
	if want := []string{"release", "scheduler"}; !reflect.DeepEqual(got.Keywords, want) {
		t.Fatalf("keywords = %#v, want %#v", got.Keywords, want)
	}
	if want := []string{"G. Hopper", "Amazing Grace"}; !reflect.DeepEqual(got.Aliases, want) {
		t.Fatalf("aliases = %#v, want %#v", got.Aliases, want)
	}
}

func TestAnalyzeFallsBackToWholeQuestionWhenNoSubQueries(t *testing.T) {
	// R-QDN3-QSN8
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{
			"sub_queries": ["", "   "],
			"keywords": ["release"],
			"aliases": []
		}`),
	}}

	question := "How did Ada handle the release?"
	got, err := Analyze(context.Background(), llm.New(prov, nil), testExtractSite(), question)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if want := []string{question}; !reflect.DeepEqual(got.SubQueries, want) {
		t.Fatalf("sub_queries = %#v, want single fallback %#v", got.SubQueries, want)
	}
}

func TestAskParsesDecoratedJSONResponses(t *testing.T) {
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada wrote the note.",
	})
	answer, _ := json.Marshal(answerResult{
		Found:     true,
		Text:      "Ada wrote the note.",
		Citations: []Citation{{Path: "entity/ada", Title: "Ada"}},
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip("```json\n{\"sub_queries\":[\"Ada\"]}\n```"),
		textRoundTrip("Here is the answer:\n" + string(answer)),
	}}

	got, err := New(oneHitSearch("subject-ada", 0.8), wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || got.Text != "Ada wrote the note." {
		t.Fatalf("Ask = %+v, want found answer from decorated JSON", got)
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

func savePage(t *testing.T, ctx context.Context, conn *sql.DB, subject wiki.Subject, page wiki.Page) {
	t.Helper()

	if err := wiki.NewSubjectStore(conn).Save(ctx, subject); err != nil {
		t.Fatalf("Save subject %s: %v", subject.ID, err)
	}
	if err := wiki.NewPageStore(conn).Upsert(ctx, page); err != nil {
		t.Fatalf("Upsert page %s: %v", page.ID, err)
	}
}

func totalChanges(t *testing.T, conn *sql.DB) int {
	t.Helper()

	var changes int
	if err := conn.QueryRow(`SELECT total_changes()`).Scan(&changes); err != nil {
		t.Fatalf("total_changes: %v", err)
	}
	return changes
}

func oneHitSearch(subjectID string, topDense float64) *scriptedSearch {
	return &scriptedSearch{result: retrieve.Result{
		Hits:     []retrieve.Hit{{PageID: subjectID}},
		TopDense: topDense,
	}}
}

func testExtractSite() llm.CallSite {
	return llm.CallSite{Model: "extract-model"}
}

func testSynthSite() llm.CallSite {
	return llm.CallSite{Model: "synth-model"}
}

type scriptedSearch struct {
	result retrieve.Result
	err    error
	calls  []searchCall
}

type searchCall struct {
	qa     any
	limits retrieve.SearchLimits
}

func (s *scriptedSearch) SearchAnalyzed(_ context.Context, qa any, limits retrieve.SearchLimits) (retrieve.Result, error) {
	s.calls = append(s.calls, searchCall{qa: qa, limits: limits})
	return s.result, s.err
}

type askProvider struct {
	responses []*agentkit.RoundTrip
	requests  []agentkit.Request
}

func (p *askProvider) RoundTrip(_ context.Context, req *agentkit.Request) *agentkit.RoundTrip {
	p.requests = append(p.requests, cloneRequest(req))
	if len(p.responses) == 0 {
		return textRoundTrip(`{"found":false}`)
	}
	rt := p.responses[0]
	p.responses = p.responses[1:]
	return rt
}

func (p *askProvider) Name() string {
	return "ask-scripted"
}

func (p *askProvider) Pricing(string) (agentkit.Pricing, bool) {
	return agentkit.Pricing{Tiers: []agentkit.RateTier{{MinInputTokens: 0}}}, true
}

func cloneRequest(req *agentkit.Request) agentkit.Request {
	if req == nil {
		return agentkit.Request{}
	}
	return agentkit.Request{
		Model:    req.Model,
		System:   req.System,
		Messages: append([]agentkit.Message(nil), req.Messages...),
		Tools:    append([]agentkit.Tool(nil), req.Tools...),
		Gen:      req.Gen,
	}
}

func textRoundTrip(text string) *agentkit.RoundTrip {
	return agentkit.NewRoundTrip(agentkit.Message{
		Role:   agentkit.RoleAssistant,
		Blocks: []agentkit.Block{agentkit.TextBlock{Text: text}},
	}, agentkit.FinishStop, agentkit.Usage{InputUncached: 1, Output: 1, Total: 2}, nil, nil)
}

func requestText(req agentkit.Request) string {
	var b strings.Builder
	for _, msg := range req.Messages {
		for _, block := range msg.Blocks {
			if text, ok := block.(agentkit.TextBlock); ok {
				b.WriteString(text.Text)
			}
		}
	}
	return b.String()
}
