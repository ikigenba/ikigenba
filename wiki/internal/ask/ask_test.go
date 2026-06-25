package ask

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	agentkit "github.com/ikigenba/agentkit"

	"wiki/internal/db"
	"wiki/internal/llm"
	"wiki/internal/wiki"
)

func TestAskRunsExtractionThenSynthesizesFromResolvedSubjectPages(t *testing.T) {
	// R-644V-3WUS
	// R-65CR-HOLH
	// R-6A8D-0RK9
	// R-05CG-3H6Y
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-cafe", Name: "Café Noir", Type: "entity"}, wiki.Page{
		ID:        "page-cafe",
		SubjectID: "subject-cafe",
		Title:     "Café Noir",
		Body:      "Café Noir keeps the deployment checklist.",
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["  cafe noir  "]}`),
		textRoundTrip(`{
			"found": true,
			"text": "Café Noir keeps the deployment checklist.",
			"citations": [{"subject":"subject-cafe","title":"Café Noir"}]
		}`),
	}}
	extractSite := llm.CallSite{Model: "extract-model", System: "extract system"}
	synthSite := llm.CallSite{Model: "synth-model", System: "synth system"}

	got, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), extractSite, synthSite).
		Ask(ctx, "owner@example.com", "Where is Café Noir's checklist?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || got.Text != "Café Noir keeps the deployment checklist." {
		t.Fatalf("Ask = %+v, want synthesized found answer", got)
	}
	if want := []Citation{{Path: "entity/cafe-noir", Title: "Café Noir"}}; !reflect.DeepEqual(got.Citations, want) {
		t.Fatalf("citations = %+v, want %+v", got.Citations, want)
	}
	citationsJSON, err := json.Marshal(got.Citations)
	if err != nil {
		t.Fatalf("Marshal citations: %v", err)
	}
	if strings.Contains(string(citationsJSON), "subject-cafe") {
		t.Fatalf("citations JSON = %s, want no internal subject id", citationsJSON)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want extract then synth", len(prov.requests))
	}
	if prov.requests[0].Model != "extract-model" || prov.requests[0].System != "extract system" {
		t.Fatalf("extract request model/system = %q/%q", prov.requests[0].Model, prov.requests[0].System)
	}
	if prov.requests[1].Model != "synth-model" || prov.requests[1].System != "synth system" {
		t.Fatalf("synth request model/system = %q/%q", prov.requests[1].Model, prov.requests[1].System)
	}
	extractText := requestText(prov.requests[0])
	if !strings.Contains(extractText, "Where is Café Noir's checklist?") {
		t.Fatalf("extract prompt = %q, want original question", extractText)
	}
	synthText := requestText(prov.requests[1])
	if !strings.Contains(synthText, "subject-cafe") || !strings.Contains(synthText, "Café Noir keeps the deployment checklist.") {
		t.Fatalf("synth prompt = %q, want resolved page context", synthText)
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

func TestAskUsesAnalyzedSubQueriesForPageResolution(t *testing.T) {
	// R-QDN3-QSN8
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada owns the release notes.",
	})
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-grace", Name: "Grace", Type: "entity"}, wiki.Page{
		ID:        "page-grace",
		SubjectID: "subject-grace",
		Title:     "Grace",
		Body:      "Grace owns the scheduler.",
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{
			"sub_queries": ["Ada", "Grace"],
			"keywords": ["release notes", "scheduler"],
			"aliases": ["Amazing Grace"]
		}`),
		textRoundTrip(`{
			"found": true,
			"text": "Ada owns the release notes and Grace owns the scheduler.",
			"citations": [
				{"subject":"subject-ada","title":"Ada"},
				{"subject":"subject-grace","title":"Grace"}
			]
		}`),
	}}

	got, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "What do Ada and Grace own?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || got.Text != "Ada owns the release notes and Grace owns the scheduler." {
		t.Fatalf("Ask = %+v, want synthesized answer from analyzed subqueries", got)
	}
	if len(got.Citations) != 2 {
		t.Fatalf("citations = %+v, want both analyzed subjects cited", got.Citations)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want analysis then synthesis", len(prov.requests))
	}
	analysisText := requestText(prov.requests[0])
	if !strings.Contains(analysisText, "sub_queries") || strings.Contains(analysisText, "subjects array") {
		t.Fatalf("analysis prompt = %q, want prepared query analysis prompt", analysisText)
	}
	synthText := requestText(prov.requests[1])
	for _, want := range []string{"Ada owns the release notes.", "Grace owns the scheduler."} {
		if !strings.Contains(synthText, want) {
			t.Fatalf("synth prompt = %q, want page body %q", synthText, want)
		}
	}
}

func TestAskBestEffortGathersEveryResolvedSubjectPage(t *testing.T) {
	// R-66KN-VGC6
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-ada", Name: "Ada", Type: "entity"}, wiki.Page{
		ID:        "page-ada",
		SubjectID: "subject-ada",
		Title:     "Ada",
		Body:      "Ada owns the parser.",
	})
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-grace", Name: "Grace", Type: "entity"}, wiki.Page{
		ID:        "page-grace",
		SubjectID: "subject-grace",
		Title:     "Grace",
		Body:      "Grace owns the scheduler.",
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada","Missing Person","Grace"]}`),
		textRoundTrip(`{
			"found": true,
			"text": "Ada owns the parser and Grace owns the scheduler.",
			"citations": [
				{"subject":"subject-ada","title":"Ada"},
				{"subject":"subject-grace","title":"Grace"}
			]
		}`),
	}}

	got, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "What do Ada, Missing Person, and Grace own?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || len(got.Citations) != 2 {
		t.Fatalf("Ask = %+v, want answer from two resolved subjects", got)
	}
	synthText := requestText(prov.requests[1])
	for _, want := range []string{"Ada owns the parser.", "Grace owns the scheduler."} {
		if !strings.Contains(synthText, want) {
			t.Fatalf("synth prompt = %q, want %q", synthText, want)
		}
	}
	pagesJSON := synthText[strings.Index(synthText, "Pages: "):]
	if strings.Contains(pagesJSON, `"Missing Person"`) {
		t.Fatalf("synth pages = %q, want unresolved subject omitted", pagesJSON)
	}
}

func TestAskGathersSurvivorPageForExtractedAlias(t *testing.T) {
	// R-BP8Q-CA0P
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	savePage(t, ctx, conn, wiki.Subject{ID: "subject-current", Name: "Current Name", Type: "entity"}, wiki.Page{
		ID:        "page-current",
		SubjectID: "subject-current",
		Title:     "Current Name",
		Body:      "Current Name owns the canonical page.",
	})
	if err := wiki.NewAliasStore(conn).Insert(ctx, wiki.Alias{
		Name:      "Former Name",
		SubjectID: "subject-current",
		CreatedBy: "owner@example.com",
		CreatedAt: "2026-06-23T12:00:00Z",
	}); err != nil {
		t.Fatalf("Insert alias: %v", err)
	}
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Former Name"]}`),
		textRoundTrip(`{
			"found": true,
			"text": "Current Name owns the canonical page.",
			"citations": [{"subject":"subject-current","title":"Current Name"}]
		}`),
	}}

	got, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "What does Former Name own?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || got.Text != "Current Name owns the canonical page." {
		t.Fatalf("Ask = %+v, want answer from survivor page", got)
	}
	if want := []Citation{{Path: "entity/current-name", Title: "Current Name"}}; !reflect.DeepEqual(got.Citations, want) {
		t.Fatalf("citations = %+v, want %+v", got.Citations, want)
	}
	synthText := requestText(prov.requests[1])
	if !strings.Contains(synthText, "subject-current") || strings.Contains(synthText, "Former Name owns") {
		t.Fatalf("synth prompt = %q, want survivor page context only", synthText)
	}
}

func TestAskReturnsHonestEmptyWhenNoExtractedSubjectResolves(t *testing.T) {
	// R-67SK-982V
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip(`{"sub_queries":["Unknown One","Unknown Two"]}`),
		textRoundTrip(`{"found":true,"text":"should not be used","citations":[]}`),
	}}

	got, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "What happened to Unknown One?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if got.Found || got.Text != honestEmptyText || len(got.Citations) != 0 {
		t.Fatalf("Ask = %+v, want honest empty answer", got)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want analysis only with no synthesis", len(prov.requests))
	}
}

func TestAskSynthesisUsesOnlyGatheredPageBodies(t *testing.T) {
	// R-5UPD-VVNA
	// R-690G-MZTK
	// R-5X56-NF4O
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
			"citations": [{"subject":"subject-ada","title":"Ada"}]
		}`),
	}}

	got, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
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
	for _, forbidden := range []string{"RAW CLAIM TEXT SHOULD NOT REACH SYNTHESIS", "job-secret", "read_source"} {
		if strings.Contains(synthText, forbidden) {
			t.Fatalf("synth prompt = %q, want no %q", synthText, forbidden)
		}
	}
}

func TestAskRejectsUngroundedSynthesisCitations(t *testing.T) {
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
			"text": "Grace wrote it.",
			"citations": [{"subject":"subject-grace","title":"Grace"}]
		}`),
	}}

	_, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err == nil || !strings.Contains(err.Error(), "citation not in gathered pages") {
		t.Fatalf("Ask error = %v, want ungrounded citation error", err)
	}
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

func savePage(t *testing.T, ctx context.Context, conn *sql.DB, subject wiki.Subject, page wiki.Page) {
	t.Helper()

	if err := wiki.NewSubjectStore(conn).Save(ctx, subject); err != nil {
		t.Fatalf("Save subject %s: %v", subject.ID, err)
	}
	if err := wiki.NewPageStore(conn).Upsert(ctx, page); err != nil {
		t.Fatalf("Upsert page %s: %v", page.ID, err)
	}
}

func testExtractSite() llm.CallSite {
	return llm.CallSite{Model: "extract-model"}
}

func testSynthSite() llm.CallSite {
	return llm.CallSite{Model: "synth-model"}
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
		Citations: []answerCitation{{Subject: "subject-ada", Title: "Ada"}},
	})
	prov := &askProvider{responses: []*agentkit.RoundTrip{
		textRoundTrip("```json\n{\"sub_queries\":[\"Ada\"]}\n```"),
		textRoundTrip("Here is the answer:\n" + string(answer)),
	}}

	got, err := New(wiki.NewSubjectStore(conn), wiki.NewPageStore(conn), llm.New(prov, nil), testExtractSite(), testSynthSite()).
		Ask(ctx, "owner@example.com", "Who wrote the note?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	if !got.Found || got.Text != "Ada wrote the note." {
		t.Fatalf("Ask = %+v, want found answer from decorated JSON", got)
	}
}
