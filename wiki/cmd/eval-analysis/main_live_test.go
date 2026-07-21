//go:build eval_live

package main

import (
	"context"
	"os"
	"testing"

	"github.com/ikigenba/agentkit"

	"wiki/internal/ask"
	"wiki/internal/eval"
)

func TestLiveProvidersAnalyzeAndEmbed(t *testing.T) {
	// R-BY7J-B037
	chat, err := eval.BuildChatProvider("anthropic", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	embeddingProvider, err := eval.BuildEmbeddingProvider("openai", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := analyzeCase(context.Background(), chat, eval.EvalCall{Model: "claude-sonnet-4-6", MaxTokens: intPointer(16384), MaxParseRetries: intPointer(2)}, ask.DefaultAnalysisInstructions, eval.AnalysisGoldCase{Question: "Who opened a research laboratory in Tulsa?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.SubQueries) == 0 {
		t.Fatal("live analysis returned no sub_queries")
	}
	embedder := &agentkit.Embedder{Provider: embeddingProvider, Model: "text-embedding-3-small", Dimensions: 1536}
	result, err := embedder.Embed(context.Background(), []string{"Tulsa research laboratory"}, agentkit.InputDocument)
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

func intPointer(value int) *int { return &value }
