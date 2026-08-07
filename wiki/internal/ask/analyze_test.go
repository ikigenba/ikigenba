package ask

import (
	"context"
	"strings"
	"testing"

	"wiki/internal/llm"
	"wiki/internal/llmtest"
)

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
