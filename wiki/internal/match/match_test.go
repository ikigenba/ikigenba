package match

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"wiki/internal/llm"
	"wiki/internal/llmtest"
	"wiki/internal/model"
)

func TestDefaultCallSiteAndEmbeddedPrompt(t *testing.T) {
	// R-7FBT-OXWC
	site := DefaultCallSite()
	if site.Stage != "match" || site.System != DefaultPromptInstructions || site.Config.Provider != "openrouter" || site.Config.Model != "gpt-5.6-luna" || site.Config.Effort != "medium" || site.Config.MaxTokens < 16384 || site.MaxParseRetries != 2 {
		t.Fatalf("DefaultCallSite = %#v, want production Luna match settings", site)
	}
	if site.Config.Temperature != nil || site.Config.Thinking != nil {
		t.Fatalf("DefaultCallSite config = %#v, want no temperature or thinking pin", site.Config)
	}
	prompt, err := os.ReadFile("prompt.txt")
	if err != nil {
		t.Fatalf("read prompt.txt: %v", err)
	}
	if string(prompt) != DefaultPromptInstructions {
		t.Fatal("embedded prompt differs from prompt.txt")
	}
	for _, phrase := range []string{"Corrections are rulings", "Claims are observations", "very fact", "cannot both hold", "When unsure, output no edge", `{"judgments"`, "Worked example"} {
		if !strings.Contains(DefaultPromptInstructions, phrase) {
			t.Fatalf("prompt does not contain %q", phrase)
		}
	}
}

func TestRenderContainsOnlyIdentityTextNoveltyAndIndices(t *testing.T) {
	unit := testUnit()
	got := Render(unit)
	for _, want := range []string{"name: Acme", "type: entity", "0. [existing] old correction", "1. [new] new correction", "0. [existing] old claim", "1. [new] new claim"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Render = %q, want %q", got, want)
		}
	}
	for _, id := range []string{unit.Subject.ID, unit.Subject.NormName, unit.Corrections[0].Claim.ID, unit.Corrections[0].Claim.JobID, unit.Claims[0].Claim.ID} {
		if strings.Contains(got, id) {
			t.Fatalf("Render contains internal id %q: %q", id, got)
		}
	}
}

func TestValidateAcceptsEmptyAndRejectsEveryOutOfRangeIndex(t *testing.T) {
	if err := Validate(Result{}, 2, 2); err != nil {
		t.Fatalf("empty judgments: %v", err)
	}
	for _, result := range []Result{
		{Judgments: []Judgment{{Correction: 2}}},
		{Judgments: []Judgment{{Correction: 0, Refutes: []int{-1}}}},
		{Judgments: []Judgment{{Correction: 0, Contradicts: []int{2}}}},
	} {
		if err := Validate(result, 2, 2); err == nil {
			t.Fatalf("Validate(%+v) returned nil error", result)
		}
	}
}

func TestMatchStagesOnlyValidNewPairEdges(t *testing.T) {
	// R-7K7F-80V4
	provider := &scriptedProvider{response: `{"judgments":[{"correction":0,"refutes":[0],"contradicts":[]},{"correction":1,"refutes":[0,1],"contradicts":[0,2,3,4]},{"correction":2,"refutes":[],"contradicts":[0]}]}`}
	matcher := New(llmtest.NewClient(t, provider), llm.CallSite{Stage: "match", System: "system", Config: llm.Config{Model: "match-model"}})

	got, err := matcher.Match(context.Background(), llm.Attribution{GroupID: "job"}, testUnit())
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	want := []Edge{
		{Correction: "400-new-correction", Suppressed: "100-old-claim"},
		{Correction: "400-new-correction", Suppressed: "500-new-claim"},
		{Correction: "400-new-correction", Suppressed: "200-old-correction"},
		{Correction: "400-new-correction", Suppressed: "300-existing-newer"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("staged edges = %+v, want only valid new-pair edges %+v", got, want)
	}
	if provider.request == nil || provider.request.System != "system" || !strings.Contains(requestText(provider.request), "[new] new correction") {
		t.Fatalf("request = %#v, want injected system and rendered input", provider.request)
	}
}

func testUnit() Unit {
	return Unit{
		Subject: model.Subject{ID: "subject-id", Name: "Acme", NormName: "acme-internal", Type: "entity"},
		Corrections: []Statement{
			{Claim: model.Claim{ID: "200-old-correction", JobID: "old-job", Body: "old correction"}},
			{Claim: model.Claim{ID: "400-new-correction", JobID: "new-job", Body: "new correction"}, New: true},
			{Claim: model.Claim{ID: "300-existing-newer", JobID: "middle-job", Body: "existing newer correction"}},
			{Claim: model.Claim{ID: "500-forward-correction", JobID: "future-job", Body: "forward correction"}, New: true},
			{Claim: model.Claim{ID: "350-same-job-correction", JobID: "new-job", Body: "same job correction"}},
		},
		Claims: []Statement{
			{Claim: model.Claim{ID: "100-old-claim", JobID: "old-job", Body: "old claim"}},
			{Claim: model.Claim{ID: "500-new-claim", JobID: "new-job", Body: "new claim"}, New: true},
		},
	}
}

type scriptedProvider struct {
	response string
	request  *llmtest.Request
}

func (p *scriptedProvider) RoundTrip(_ context.Context, request *llmtest.Request) *llmtest.RoundTrip {
	p.request = request
	return llmtest.NewRoundTrip(llmtest.Message{Role: llmtest.RoleAssistant, Blocks: []llmtest.Block{llmtest.TextBlock{Text: p.response}}}, llmtest.FinishStop, llmtest.Usage{})
}

func requestText(request *llmtest.Request) string {
	var texts []string
	for _, message := range request.Messages {
		for _, block := range message.Blocks {
			if text, ok := block.(llmtest.TextBlock); ok {
				texts = append(texts, text.Text)
			}
		}
	}
	return strings.Join(texts, "\n")
}
