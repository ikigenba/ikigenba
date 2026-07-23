package ask

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"wiki/internal/llm"
	"wiki/internal/llmtest"
	"wiki/internal/wiki"
)

func TestDefaultAnalysisInstructionsMatchPromptFile(t *testing.T) {
	// R-BICU-BZG6
	prompt, err := os.ReadFile("analysis-prompt.txt")
	if err != nil {
		t.Fatalf("ReadFile analysis prompt: %v", err)
	}
	if string(prompt) != DefaultAnalysisInstructions {
		t.Fatalf("DefaultAnalysisInstructions differs from analysis-prompt.txt")
	}
}

func TestAnalyzeSendsInstructionsAsSystemAndQuestionOnlyAsUser(t *testing.T) {
	// R-A0XE-WA4H
	provider := &askProvider{responses: []*llmtest.RoundTrip{
		textRoundTrip(`{"sub_queries":["Ada"]}`),
	}}
	question := "  What did Ada write?  "

	if _, err := Analyze(context.Background(), llmtest.NewClient(t, provider), DefaultSubjectCallSite(), llm.Attribution{}, question); err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	request := provider.requests[0]
	if request.System != DefaultAnalysisInstructions {
		t.Fatalf("analysis system = %q, want embedded instructions %q", request.System, DefaultAnalysisInstructions)
	}
	if len(request.Messages) != 1 || requestText(request) != question {
		t.Fatalf("analysis messages = %#v, want one question-only user turn %q", request.Messages, question)
	}
	if strings.Contains(requestText(request), DefaultAnalysisInstructions) {
		t.Fatalf("analysis user turn contains instruction preamble: %q", requestText(request))
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
