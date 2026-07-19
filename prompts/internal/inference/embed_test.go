package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"

	"prompts/internal/admit"
	"prompts/internal/calls"
)

const testEmbedModel = "text-embedding-3-small"

func TestEmbedReturnsVectorsUsageCostAndRecordsOneEmbeddingCall(t *testing.T) {
	// R-604H-L3QC
	store := newTestStore(t)
	usage := agentkit.EmbeddingUsage{InputTokens: 10, Total: 10}
	provider := &fakeEmbeddingProvider{trip: agentkit.NewEmbedRoundTrip(
		[][]float32{{1, 0}, {0, 1}}, usage, nil, nil,
	)}
	req := validEmbedRequest()
	recorder := postEmbed(t, testEmbedHandler(store, provider), req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var got embedResponse
	decodeResponse(t, recorder, &got)
	if got.CallID == "" || !reflect.DeepEqual(got.Vectors, [][]float32{{1, 0}, {0, 1}}) || got.Usage != usage {
		t.Fatalf("response = %#v, want call id, ordered vectors, and usage %#v", got, usage)
	}
	entry, ok := catalog.Lookup(testEmbedModel)
	if !ok || entry.Embedding == nil {
		t.Fatal("test embedding model is missing from catalog")
	}
	wantCost := entry.Embedding.Pricing.Cost(usage).USD()
	if math.Abs(got.CostUSD-wantCost) > 1e-12 {
		t.Fatalf("cost_usd = %.12f, want %.12f", got.CostUSD, wantCost)
	}
	if provider.request == nil || provider.request.Role != agentkit.InputDocument || provider.request.Dimensions != req.Dimensions ||
		!reflect.DeepEqual(provider.request.Inputs, req.Inputs) {
		t.Fatalf("provider request = %#v, want document inputs in order", provider.request)
	}

	rows, err := store.List(context.Background(), calls.Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("List calls = %#v, %v; want one", rows, err)
	}
	row := rows[0]
	if row.Class != calls.ClassEmbedding || row.InputTokens != usage.InputTokens || row.OutputTokens != 0 ||
		row.TotalTokens != usage.Total || math.Abs(row.CostUSD-wantCost) > 1e-12 {
		t.Fatalf("recorded usage = %#v, want embedding usage and catalog cost", row)
	}
	if row.RequestBody == nil || *row.RequestBody != `["first","second"]` {
		t.Fatalf("request_body = %v, want marshaled inputs", row.RequestBody)
	}
	if row.ResponseBody != nil {
		t.Fatalf("response_body = %v, want NULL", row.ResponseBody)
	}
}

func TestEmbedRejectsCatalogModelWithoutEmbeddingEntry(t *testing.T) {
	// R-61CD-YVH1
	store := newTestStore(t)
	req := validEmbedRequest()
	req.Model = testModel
	recorder := postEmbed(t, testEmbedHandler(store, &fakeEmbeddingProvider{}), req)
	assertErrorContains(t, recorder, http.StatusBadRequest, "not a catalog embedding model")
	assertNoCalls(t, store)
}

func TestEmbedRejectsDimensionsOutsideCatalogRange(t *testing.T) {
	// R-62KA-CN7Q
	store := newTestStore(t)
	req := validEmbedRequest()
	req.Dimensions = 1537
	recorder := postEmbed(t, testEmbedHandler(store, &fakeEmbeddingProvider{}), req)
	assertErrorContains(t, recorder, http.StatusBadRequest, "between 1 and 1536")
	assertNoCalls(t, store)
}

func TestEmbedRejectsProviderWithoutEmbedder(t *testing.T) {
	// R-63S6-QEYF
	store := newTestStore(t)
	req := validEmbedRequest()
	req.Provider = "anthropic"
	recorder := postEmbed(t, testEmbedHandler(store, &fakeEmbeddingProvider{}), req)
	assertErrorContains(t, recorder, http.StatusBadRequest, "does not support embeddings")
	assertNoCalls(t, store)
}

func TestEmbedRejectsEmptyInputsWithoutRecording(t *testing.T) {
	// R-6503-46P4
	cases := []struct {
		name   string
		inputs []string
	}{
		{name: "empty batch", inputs: []string{}},
		{name: "empty item", inputs: []string{"usable", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			req := validEmbedRequest()
			req.Inputs = tc.inputs
			recorder := postEmbed(t, testEmbedHandler(store, &fakeEmbeddingProvider{}), req)
			assertErrorContains(t, recorder, http.StatusBadRequest, "non-empty")
			assertNoCalls(t, store)
		})
	}
}

func TestEmbedProviderFailureReturnsBadGatewayAndRecordsError(t *testing.T) {
	// R-667Z-HYFT
	store := newTestStore(t)
	provider := &fakeEmbeddingProvider{trip: agentkit.NewEmbedRoundTrip(nil, agentkit.EmbeddingUsage{}, nil, errors.New("embedding provider exploded"))}
	recorder := postEmbed(t, testEmbedHandler(store, provider), validEmbedRequest())
	assertErrorContains(t, recorder, http.StatusBadGateway, "embedding provider exploded")
	rows, err := store.List(context.Background(), calls.Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("List calls = %#v, %v; want one", rows, err)
	}
	if rows[0].Class != calls.ClassEmbedding || rows[0].Error != "embedding provider exploded" || rows[0].ResponseBody != nil {
		t.Fatalf("failure row = %#v, want embedding error and NULL response", rows[0])
	}
}

func TestEmbedRejectsInvalidRoleNonPOSTAndOversizedBody(t *testing.T) {
	store := newTestStore(t)
	req := validEmbedRequest()
	req.Role = "search"
	assertErrorContains(t, postEmbed(t, testEmbedHandler(store, &fakeEmbeddingProvider{}), req), http.StatusBadRequest, "document or query")
	assertNoCalls(t, store)

	handler := testEmbedHandler(failingStore{err: errors.New("should not insert")}, &fakeEmbeddingProvider{})
	nonPOST := httptest.NewRecorder()
	handler.ServeHTTP(nonPOST, httptest.NewRequest(http.MethodGet, "/embed", nil))
	assertErrorContains(t, nonPOST, http.StatusMethodNotAllowed, "POST")

	oversized := httptest.NewRecorder()
	body := strings.NewReader(`{"origin":"service:wiki","padding":"` + strings.Repeat("x", maxCompleteBody) + `"}`)
	handler.ServeHTTP(oversized, httptest.NewRequest(http.MethodPost, "/embed", body))
	assertErrorContains(t, oversized, http.StatusBadRequest, "10 MiB")
}

type fakeEmbeddingProvider struct {
	request *agentkit.EmbedRequest
	trip    *agentkit.EmbedRoundTrip
}

func (p *fakeEmbeddingProvider) Name() string { return "fake-embedding" }

func (p *fakeEmbeddingProvider) Embed(_ context.Context, req *agentkit.EmbedRequest) *agentkit.EmbedRoundTrip {
	copy := *req
	copy.Inputs = append([]string(nil), req.Inputs...)
	p.request = &copy
	if p.trip == nil {
		return agentkit.NewEmbedRoundTrip(nil, agentkit.EmbeddingUsage{}, nil, nil)
	}
	return p.trip
}

func validEmbedRequest() EmbedRequest {
	return EmbedRequest{
		Origin: "service:wiki", Name: "wiki.embed-page", GroupID: "job-01", Attempt: 2,
		Model: testEmbedModel, Dimensions: 2, Role: "document", Inputs: []string{"first", "second"},
	}
}

func testEmbedHandler(store CallStore, provider agentkit.EmbeddingProvider) http.Handler {
	build := func(string, func(string) string) (agentkit.EmbeddingProvider, error) { return provider, nil }
	return NewEmbedExecutor(store, admit.New(2, 2), build, func(string) string { return "test-key" }).EmbedHandler()
}

func postEmbed(t *testing.T, handler http.Handler, req EmbedRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal request: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/embed", bytes.NewReader(body)))
	return recorder
}
