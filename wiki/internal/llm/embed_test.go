package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	vectors, err := New(server.URL, server.Client()).Embed(context.Background(), EmbedSite{
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
