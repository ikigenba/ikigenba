//go:build live

package llm

import (
	"context"
	"net/http"
	"registry"
	"testing"
)

func TestEmbedAgainstLivePrompts(t *testing.T) {
	// R-15NY-IF46
	baseURL := registry.BaseURL("prompts")
	inputs := []string{"wiki live embedding smoke one", "wiki live embedding smoke two"}
	vectors, err := New(baseURL, http.DefaultClient).Embed(context.Background(), EmbedSite{
		Name: "wiki.embed-query", Model: "text-embedding-3-small", Dims: 512,
	}, Attribution{Origin: "service:wiki", GroupID: "wiki-live-embed-smoke"}, "query", inputs)
	if err != nil {
		t.Fatalf("live Embed: %v", err)
	}
	if len(vectors) != len(inputs) {
		t.Fatalf("vectors = %d, want one per input (%d)", len(vectors), len(inputs))
	}
	for i, vector := range vectors {
		if len(vector) != 512 {
			t.Fatalf("vector %d dims = %d, want 512", i, len(vector))
		}
	}
}
