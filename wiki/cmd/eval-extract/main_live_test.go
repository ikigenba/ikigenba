//go:build eval_live

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ikigenba/agentkit"

	"wiki/internal/eval"
	"wiki/internal/extract"
)

func TestLiveProvidersExtractAndEmbed(t *testing.T) {
	// R-L5J2-NTZH
	chat, err := buildChatProvider("anthropic", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	embeddingProvider, err := buildEmbeddingProvider("openai", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	gold := eval.GoldCase{
		Name:     "live-smoke",
		Header:   extract.DocumentHeader{Source: "live-test", Title: "Tulsa lab", ReceivedAt: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)},
		Document: "Acme Robotics opened a research laboratory in Tulsa on July 20, 2026.",
	}
	maxTokens, maxParseRetries := 16384, 2
	subjects, err := extractCase(context.Background(), chat, eval.EvalCall{Model: "claude-sonnet-4-6", MaxTokens: &maxTokens, MaxParseRetries: &maxParseRetries}, extract.DefaultPromptInstructions, gold)
	if err != nil {
		t.Fatal(err)
	}
	if len(subjects) == 0 {
		t.Fatal("live extraction returned no subjects")
	}
	if err := extract.Validate(subjects); err != nil {
		t.Fatalf("live extraction invalid: %v", err)
	}
	embedder := &agentkit.Embedder{Provider: embeddingProvider, Model: "text-embedding-3-small", Dimensions: 1536}
	result, err := embedder.Embed(context.Background(), []string{"Acme Robotics opened a laboratory."}, agentkit.InputDocument)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Vectors) != 1 {
		t.Fatalf("live embedding vector count = %d", len(result.Vectors))
	}
	if len(result.Vectors[0]) != 1536 {
		t.Fatalf("live embedding dimensions = %d", len(result.Vectors[0]))
	}
}
