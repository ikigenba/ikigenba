package ask

import (
	"context"
	"os"
	"reflect"
	"testing"

	"wiki/internal/llm"
	"wiki/internal/llmtest"
	"wiki/internal/wiki"
)

func TestDefaultAnalysisInstructionsMatchPromptFile(t *testing.T) {
	// R-BICU-BZG6
	prompt, err := os.ReadFile("../../eval/analysis/prompt.txt")
	if err != nil {
		t.Fatalf("ReadFile analysis prompt: %v", err)
	}
	if string(prompt) != DefaultAnalysisInstructions {
		t.Fatalf("DefaultAnalysisInstructions differs from eval/analysis/prompt.txt")
	}
}

func TestAnalyzeSendsRenderedAnalysisPrompt(t *testing.T) {
	// R-BJKQ-PR6V
	provider := &askProvider{responses: []*llmtest.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
	}}
	question := "  What did Ada write?  "

	if _, err := Analyze(context.Background(), llmtest.NewClient(t, provider), testExtractSite(), llm.Attribution{}, question); err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if got, want := requestText(provider.requests[0]), RenderAnalysis(DefaultAnalysisInstructions, question); got != want {
		t.Fatalf("production prompt = %q, want RenderAnalysis output %q", got, want)
	}
}

func TestAnalyzeUsesExportedNormalization(t *testing.T) {
	// R-BJKQ-PR6V
	raw := wiki.QueryAnalysis{
		SubQueries: []string{"  Ada  ", "ada", "Grace", " Linus ", "Margaret", "Katherine"},
		Keywords:   []string{" release ", "RELEASE", "scheduler"},
		Aliases:    []string{" G. Hopper ", "g. hopper", "Amazing Grace"},
	}
	want := raw
	want.SubQueries = append([]string(nil), raw.SubQueries...)
	want.Keywords = append([]string(nil), raw.Keywords...)
	want.Aliases = append([]string(nil), raw.Aliases...)
	NormalizeAnalysis(&want)

	provider := &askProvider{responses: []*llmtest.RoundTrip{
		textRoundTrip(`{"sub_queries":["  Ada  ","ada","Grace"," Linus ","Margaret","Katherine"],"keywords":[" release ","RELEASE","scheduler"],"aliases":[" G. Hopper ","g. hopper","Amazing Grace"]}`),
	}}
	got, err := Analyze(context.Background(), llmtest.NewClient(t, provider), testExtractSite(), llm.Attribution{}, "question")
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("production normalization = %#v, want NormalizeAnalysis result %#v", got, want)
	}
}
