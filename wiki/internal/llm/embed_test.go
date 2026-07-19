package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func TestEmbedCarriesCallSiteAttributionRoleAndInputs(t *testing.T) {
	// R-1385-QVMS
	var got embedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/embed" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"vectors": [][]float32{{1, 2}, {3, 4}}})
	}))
	defer server.Close()

	wantVectors := [][]float32{{1, 2}, {3, 4}}
	vectors, err := New(server.URL).Embed(context.Background(), EmbedSite{
		Name: "wiki.embed-page", Model: "embed-model", Dims: 2,
	}, Attribution{Origin: "service:wiki", GroupID: "job-123"}, "document", []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got.Origin != "service:wiki" || got.Name != "wiki.embed-page" || got.GroupID != "job-123" || got.Model != "embed-model" || got.Dimensions != 2 || got.Role != "document" || !reflect.DeepEqual(got.Inputs, []string{"first", "second"}) {
		t.Fatalf("request = %+v", got)
	}
	if !reflect.DeepEqual(vectors, wantVectors) {
		t.Fatalf("vectors = %#v, want %#v in response order", vectors, wantVectors)
	}
}

func TestEmbedAgainstLivePrompts(t *testing.T) {
	// R-15NY-IF46
	// Operator-run: set WIKI_LIVE_PROMPTS_URL to the real prompts loopback base URL.
	baseURL := os.Getenv("WIKI_LIVE_PROMPTS_URL")
	if baseURL == "" {
		t.Skip("set WIKI_LIVE_PROMPTS_URL to run the operator live /embed smoke")
	}
	inputs := []string{"wiki live embedding smoke one", "wiki live embedding smoke two"}
	vectors, err := New(baseURL).Embed(context.Background(), EmbedSite{
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
