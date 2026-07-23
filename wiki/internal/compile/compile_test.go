package compile

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"wiki/internal/extract"
	"wiki/internal/llm"
	"wiki/internal/llmtest"
	"wiki/internal/model"
)

func TestCompileRendersSubjectIdentityAndCompleteClaimSet(t *testing.T) {
	// R-FQLB-QWS6
	prov := &scriptedProvider{responses: []string{`{"title":"Acme Robotics","body":"Acme Robotics opened a Tulsa lab and hired Mira Patel."}`}}
	site := DefaultCallSite()
	site.Config.Model = "compile-model"
	compiler := New(llmtest.NewClient(t, prov), site, nil)

	title, body, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), []model.Claim{
		{ID: "claim-001", SubjectID: "subj-acme", JobID: "job-001", Body: "Acme Robotics opened a research lab in Tulsa."},
		{ID: "claim-002", SubjectID: "subj-acme", JobID: "job-002", Body: "Mira Patel leads Acme Robotics' Tulsa lab."},
	})
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if title != "Acme Robotics" || body != "Acme Robotics opened a Tulsa lab and hired Mira Patel." {
		t.Fatalf("Compile result = %q/%q, want decoded page", title, body)
	}

	if system := prov.requests[0].System; !strings.Contains(system, "Rebuild one wiki page") || !strings.Contains(system, "Never use or imagine a prior page body, source document") {
		t.Fatalf("system prompt %q does not contain compile boundary", system)
	}
	prompt := onlyPrompt(t, prov, 0)
	for _, want := range []string{
		"Hard body limit: 12000 characters.",
		"Subject identity:",
		"Complete claims:",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt %q does not contain compile boundary %q", prompt, want)
		}
	}
	for _, want := range []string{
		"id: subj-acme",
		"name: Acme Robotics",
		"norm_name: acme-robotics",
		"type: entity",
		"[job-001] Acme Robotics opened a research lab in Tulsa.",
		"[job-002] Mira Patel leads Acme Robotics' Tulsa lab.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt %q does not contain %q", prompt, want)
		}
	}
}

func TestCompileUsesInjectedCallSiteWithoutTools(t *testing.T) {
	// R-FT14-IG9K
	temp := 0.0
	prov := &scriptedProvider{responses: []string{`{"title":"Acme Robotics","body":"Acme Robotics operates a Tulsa research lab."}`}}
	site := llm.CallSite{
		Model:       "compile-model",
		Temperature: &temp,
		Reasoning:   llmtest.DisableReasoning(),
		System:      "compile from claims",
	}
	compiler := New(llmtest.NewClient(t, prov), site, nil)

	if _, _, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), acmeClaims()); err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests len = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.Model != site.Model || req.System != site.System {
		t.Fatalf("request config = %#v, want injected call site model/system", req)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("request tools len = %d, want tool-less compile generation", len(req.Tools))
	}
	if req.Gen.Temperature == nil || *req.Gen.Temperature != temp || !req.Gen.Reasoning.Disabled() {
		t.Fatalf("gen settings = %#v, want injected temperature and disabled reasoning", req.Gen)
	}
}

func TestDefaultCallSiteUsesLunaSettings(t *testing.T) {
	// R-9X9P-QYWE
	site := DefaultCallSite()
	if site.Stage != "compile" || site.System != DefaultPromptInstructions || site.Config.Provider != "openai" || site.Config.Model != "gpt-5.6-luna" || site.Config.Effort != "low" || site.Config.MaxTokens < 16384 {
		t.Fatalf("DefaultCallSite = %#v, want production Luna compile settings", site)
	}
	if site.Config.Temperature != nil || site.Config.Thinking != nil || site.Temperature != nil || site.Reasoning != nil {
		t.Fatalf("DefaultCallSite = %#v, want no temperature or thinking pins", site)
	}
	site.Config.Model = "compile-model"

	prov := &scriptedProvider{responses: []string{`{"title":"Acme Robotics","body":"Acme Robotics operates a Tulsa research lab."}`}}
	compiler := New(llmtest.NewClient(t, prov), site, nil)
	if _, _, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), acmeClaims()); err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests len = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.System != DefaultPromptInstructions || req.Gen.Temperature != nil || req.Gen.Reasoning.Disabled() {
		t.Fatalf("request = %#v, want system prompt with unpinned temperature/reasoning", req)
	}
	if req.Gen.MaxTokens != site.Config.MaxTokens {
		t.Fatalf("request max tokens = %d, want default ceiling %d", req.Gen.MaxTokens, site.Config.MaxTokens)
	}
}

func TestCompileSendsEmbeddedInstructionsAsSystemOnly(t *testing.T) {
	// R-9YHM-4QN3
	prov := &scriptedProvider{responses: []string{`{"title":"Acme Robotics","body":"Acme Robotics is an entity. [job-001]"}`}}
	site := DefaultCallSite()
	site.Config.Model = "compile-model"
	compiler := New(llmtest.NewClient(t, prov), site, nil)

	if _, _, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), acmeClaims()[:1]); err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	req := prov.requests[0]
	if req.System != DefaultPromptInstructions {
		t.Fatalf("system = %q, want embedded compile instructions", req.System)
	}
	user := onlyPrompt(t, prov, 0)
	if strings.Contains(user, DefaultPromptInstructions) || !strings.Contains(user, "Subject identity:\n") || !strings.Contains(user, "[job-001]") {
		t.Fatalf("user prompt = %q, want only rendered identity and citation-tagged claims", user)
	}
}

func TestExtractAndCompileDefaultCallSitesCarryOutputTokenCeilings(t *testing.T) {
	// R-MW86-M158
	prov := &scriptedProvider{responses: []string{
		`{"subjects":[]}`,
		`{"title":"Acme Robotics","body":"Acme Robotics operates a Tulsa research lab."}`,
	}}
	extractSite := extract.DefaultCallSite()
	extractSite.Config.Model = "extract-model"
	compileSite := DefaultCallSite()
	compileSite.Config.Model = "compile-model"
	const minOutputBudget = 16384
	if extractSite.Config.MaxTokens < minOutputBudget || compileSite.Config.MaxTokens < minOutputBudget {
		t.Fatalf("default config max tokens = extract:%d compile:%d, want both at least %d", extractSite.Config.MaxTokens, compileSite.Config.MaxTokens, minOutputBudget)
	}

	extractor := extract.New(llmtest.NewClient(t, prov), extractSite)
	if _, err := extractor.Extract(context.Background(), llm.Attribution{}, extract.DocumentHeader{}, "source text"); err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	compiler := New(llmtest.NewClient(t, prov), compileSite, nil)
	if _, _, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), acmeClaims()); err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("requests len = %d, want extract and compile calls", len(prov.requests))
	}
	if prov.requests[0].Gen.MaxTokens != extractSite.Config.MaxTokens {
		t.Fatalf("extract request max tokens = %d, want %d", prov.requests[0].Gen.MaxTokens, extractSite.Config.MaxTokens)
	}
	if prov.requests[1].Gen.MaxTokens != compileSite.Config.MaxTokens {
		t.Fatalf("compile request max tokens = %d, want %d", prov.requests[1].Gen.MaxTokens, compileSite.Config.MaxTokens)
	}
}

func TestCompileRebuildsFromClaimsWithoutPriorGeneratedBody(t *testing.T) {
	// R-FU90-W809
	prov := &scriptedProvider{responses: []string{
		`{"title":"Acme Robotics","body":"STALE GENERATED BODY should not be reused."}`,
		`{"title":"Acme Robotics","body":"Acme Robotics opened a Denver lab."}`,
	}}
	compiler := New(llmtest.NewClient(t, prov), llm.CallSite{Model: "compile-model"}, nil)

	if _, _, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), []model.Claim{
		{ID: "claim-001", SubjectID: "subj-acme", JobID: "job-001", Body: "Acme Robotics opened a Tulsa lab."},
	}); err != nil {
		t.Fatalf("first Compile returned error: %v", err)
	}
	title, body, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), []model.Claim{
		{ID: "claim-003", SubjectID: "subj-acme", JobID: "job-003", Body: "Acme Robotics opened a Denver lab."},
	})
	if err != nil {
		t.Fatalf("second Compile returned error: %v", err)
	}
	if title != "Acme Robotics" || body != "Acme Robotics opened a Denver lab." {
		t.Fatalf("second result = %q/%q, want rebuilt page from second claim set", title, body)
	}

	secondPrompt := onlyPrompt(t, prov, 1)
	if strings.Contains(secondPrompt, "STALE GENERATED BODY") {
		t.Fatalf("second prompt contains prior generated body: %q", secondPrompt)
	}
	if strings.Contains(secondPrompt, "Tulsa lab") || !strings.Contains(secondPrompt, "Denver lab") {
		t.Fatalf("second prompt = %q, want only the new complete claim set", secondPrompt)
	}
}

func TestCompileTightensOverCapBodyFromClaims(t *testing.T) {
	// R-FVGX-9ZQY
	tooLong := strings.Repeat("a", PageCharCap+1)
	prov := &scriptedProvider{responses: []string{
		`{"title":"Acme Robotics","body":"` + tooLong + `"}`,
		`{"title":"Acme Robotics","body":"Acme Robotics runs a concise Tulsa lab page."}`,
	}}
	compiler := New(llmtest.NewClient(t, prov), llm.CallSite{Model: "compile-model"}, nil)

	_, body, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), acmeClaims())
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if body != "Acme Robotics runs a concise Tulsa lab page." {
		t.Fatalf("body = %q, want tightened response", body)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("requests len = %d, want initial compile plus tighten", len(prov.requests))
	}
	secondPrompt := onlyPrompt(t, prov, 1)
	if !strings.Contains(secondPrompt, "previous page is 12001 chars; hard limit 12000") || !strings.Contains(secondPrompt, "[job-001]") {
		t.Fatalf("tighten prompt = %q, want cap warning and original claims", secondPrompt)
	}
	if strings.Contains(secondPrompt, tooLong) {
		t.Fatalf("tighten prompt should not include over-cap generated body")
	}
}

func TestCompileDeterministicallyEnforcesRuneCap(t *testing.T) {
	body := strings.Repeat("é", PageCharCap+7)
	prov := &scriptedProvider{responses: []string{`{"title":"Acme Robotics","body":"` + body + `"}`}}
	site := DefaultCallSite()
	site.Config.Model = "compile-model"
	compiler := New(llmtest.NewClient(t, prov), site, nil)
	compiler.maxTighten = 0

	_, got, err := compiler.Compile(context.Background(), llm.Attribution{}, acmeSubject(), acmeClaims())
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if utf8.RuneCountInString(got) != PageCharCap {
		t.Fatalf("body rune count = %d, want %d", utf8.RuneCountInString(got), PageCharCap)
	}
	if got != strings.Repeat("é", PageCharCap) {
		t.Fatalf("body was not truncated on rune boundaries")
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests len = %d, want 1", len(prov.requests))
	}
	req := prov.requests[0]
	if req.Model != site.Config.Model {
		t.Fatalf("request model = %q, want %q", req.Model, site.Config.Model)
	}
	if req.Gen.Temperature != nil || req.Gen.Reasoning.Disabled() {
		t.Fatalf("gen settings = %#v, want no temperature or thinking pin", req.Gen)
	}
}

func acmeSubject() model.Subject {
	return model.Subject{ID: "subj-acme", Name: "Acme Robotics", NormName: "acme-robotics", Type: "entity"}
}

func acmeClaims() []model.Claim {
	return []model.Claim{
		{ID: "claim-001", SubjectID: "subj-acme", JobID: "job-001", Body: "Acme Robotics opened a research lab in Tulsa."},
		{ID: "claim-002", SubjectID: "subj-acme", JobID: "job-002", Body: "Mira Patel leads Acme Robotics' Tulsa lab."},
	}
}

func onlyPrompt(t *testing.T, prov *scriptedProvider, i int) string {
	t.Helper()
	if len(prov.requests) <= i {
		t.Fatalf("requests len = %d, want request index %d", len(prov.requests), i)
	}
	texts := requestTexts(prov.requests[i])
	if len(texts) != 1 {
		t.Fatalf("request texts = %#v, want one user prompt", texts)
	}
	return texts[0]
}

type scriptedProvider struct {
	responses []string
	requests  []llmtest.Request
}

func (p *scriptedProvider) RoundTrip(_ context.Context, req *llmtest.Request) *llmtest.RoundTrip {
	p.requests = append(p.requests, cloneRequest(req))
	text := `{"title":"Untitled","body":"Empty."}`
	if len(p.responses) > 0 {
		text = p.responses[0]
		p.responses = p.responses[1:]
	}
	return llmtest.NewRoundTrip(
		llmtest.Message{Role: llmtest.RoleAssistant, Blocks: []llmtest.Block{llmtest.TextBlock{Text: text}}},
		llmtest.FinishStop,
		llmtest.Usage{InputUncached: 1, Output: 1, Total: 2},
		nil,
		nil,
		0,
		false,
	)
}

func (p *scriptedProvider) Name() string {
	return "scripted"
}

func (p *scriptedProvider) Pricing(string) (llmtest.Pricing, bool) {
	return llmtest.Pricing{Tiers: []llmtest.RateTier{{MinInputTokens: 0}}}, true
}

func cloneRequest(req *llmtest.Request) llmtest.Request {
	if req == nil {
		return llmtest.Request{}
	}
	return llmtest.Request{
		Model:    req.Model,
		System:   req.System,
		Messages: append([]llmtest.Message(nil), req.Messages...),
		Tools:    append([]llmtest.Tool(nil), req.Tools...),
		Gen:      req.Gen,
	}
}

func requestTexts(req llmtest.Request) []string {
	var out []string
	for _, msg := range req.Messages {
		text := providerText(msg)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func providerText(message llmtest.Message) string {
	var b strings.Builder
	for _, block := range message.Blocks {
		if text, ok := block.(llmtest.TextBlock); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}
