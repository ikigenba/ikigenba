package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appkitdb "appkit/db"
	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"

	"prompts/internal/admit"
	"prompts/internal/calls"
	promptsdb "prompts/internal/db"
	"prompts/internal/prompt"
)

const testModel = "claude-sonnet-4-6"

func TestCompleteReturnsFakeReplyUsageAndCatalogCost(t *testing.T) {
	// R-5P5E-5623
	store := newTestStore(t)
	usage := agentkit.Usage{InputUncached: 17, CacheReadInput: 3, Output: 5, Total: 25}
	provider := &fakeProvider{result: roundTrip("finished text", usage, nil)}
	handler := testHandler(store, provider)

	recorder := postComplete(t, handler, validRequest())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var got response
	decodeResponse(t, recorder, &got)
	if got.CallID == "" || got.Text != "finished text" || got.Usage != usage {
		t.Fatalf("response = %#v, want call id, fake text, and usage %#v", got, usage)
	}
	_, _, entry, ok := catalog.Resolve("anthropic", testModel)
	if !ok {
		t.Fatal("test model did not resolve")
	}
	wantCost := entry.Pricing.Cost(usage).USD()
	if math.Abs(got.CostUSD-wantCost) > 1e-12 {
		t.Fatalf("cost_usd = %.12f, want %.12f", got.CostUSD, wantCost)
	}
}

func TestCompleteRecordsExactlyOneCompletionCall(t *testing.T) {
	// R-5QDA-IXSS
	store := newTestStore(t)
	provider := &fakeProvider{result: roundTrip("record me", agentkit.Usage{InputUncached: 2, Output: 1, Total: 3}, nil)}
	req := validRequest()
	postComplete(t, testHandler(store, provider), req)

	rows, err := store.List(context.Background(), calls.Filter{})
	if err != nil {
		t.Fatalf("List calls: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("calls rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Class != calls.ClassCompletion || row.Origin != req.Origin || row.Name != req.Name ||
		row.GroupID != req.GroupID || row.Attempt != req.Attempt || row.Model != req.Model {
		t.Fatalf("recorded envelope = %#v, want request metadata", row)
	}
	if row.RequestBody == nil || !strings.Contains(*row.RequestBody, "correct this") || !strings.Contains(*row.RequestBody, req.System) {
		t.Fatalf("request_body = %v, want messages and system", row.RequestBody)
	}
	if row.ResponseBody == nil || *row.ResponseBody != "record me" {
		t.Fatalf("response_body = %v, want record me", row.ResponseBody)
	}
}

func TestCompleteRejectsInvalidModelRoutingAndReasoningWithoutRecording(t *testing.T) {
	// R-5ST3-AHA6
	cases := []struct {
		name string
		edit func(*Request)
		want string
	}{
		{"unknown model", func(req *Request) { req.Model = "not-a-catalog-model" }, "unknown prompt model"},
		{"unroutable provider", func(req *Request) { req.Provider = "openai" }, "does not route"},
		{"invalid reasoning", func(req *Request) { req.Config.Effort = "ultra" }, "reasoning levels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			req := validRequest()
			tc.edit(&req)
			recorder := postComplete(t, testHandler(store, &fakeProvider{result: roundTrip("unused", agentkit.Usage{}, nil)}), req)
			assertErrorContains(t, recorder, http.StatusBadRequest, tc.want)
			assertNoCalls(t, store)
		})
	}
}

func TestCompleteCarriesSubscriptionAuthToProviderFactory(t *testing.T) {
	// R-T1TD-KYWQ
	store := newTestStore(t)
	fake := &fakeProvider{result: roundTrip("subscription reply", agentkit.Usage{Total: 1}, nil)}
	var got prompt.Config
	build := func(cfg prompt.Config, _ func(string) string) (agentkit.Provider, error) {
		got = cfg
		return fake, nil
	}
	executor := NewExecutor(store, admit.New(2, 2), build, func(string) string { return "" }, func() bool { return true })
	req := validRequest()
	req.Model = "gpt-5.5"
	req.Config.Auth = "sub"
	recorder := postComplete(t, executor.CompleteHandler(), req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got.Auth != "sub" || got.Provider != "openai" || got.Model != req.Model {
		t.Fatalf("factory config = %#v, want openai subscription config", got)
	}
}

func TestCompleteRejectsInvalidEnvelopeWithoutRecording(t *testing.T) {
	// R-5U0Z-O90V
	cases := []struct {
		name string
		edit func(*Request)
		want string
	}{
		{"origin", func(req *Request) { req.Origin = "wiki" }, "invalid origin"},
		{"name", func(req *Request) { req.Name = "wiki" }, "invalid name"},
		{"empty messages", func(req *Request) { req.Messages = nil }, "non-empty"},
		{"final assistant", func(req *Request) { req.Messages[len(req.Messages)-1].Role = "assistant" }, "final message"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			req := validRequest()
			tc.edit(&req)
			recorder := postComplete(t, testHandler(store, &fakeProvider{result: roundTrip("unused", agentkit.Usage{}, nil)}), req)
			assertErrorContains(t, recorder, http.StatusBadRequest, tc.want)
			assertNoCalls(t, store)
		})
	}
}

func TestCompleteReplaysFullCorrectiveHistory(t *testing.T) {
	// R-5V8W-20RK
	store := newTestStore(t)
	provider := &fakeProvider{result: roundTrip("corrected", agentkit.Usage{Total: 1}, nil)}
	recorder := postComplete(t, testHandler(store, provider), validRequest())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if provider.request == nil || len(provider.request.Messages) != 3 {
		t.Fatalf("provider messages = %#v, want three turns", provider.request)
	}
	wantRoles := []agentkit.Role{agentkit.RoleUser, agentkit.RoleAssistant, agentkit.RoleUser}
	wantTexts := []string{"extract this", "bad reply", "correct this"}
	for i, message := range provider.request.Messages {
		if message.Role != wantRoles[i] || messageText(message) != wantTexts[i] {
			t.Fatalf("provider message %d = %#v/%q, want %s/%q", i, message, messageText(message), wantRoles[i], wantTexts[i])
		}
	}
	if len(provider.request.Tools) != 0 {
		t.Fatalf("provider tools = %d, want tool-less request", len(provider.request.Tools))
	}
}

func TestCompleteProviderFailureReturnsBadGatewayAndRecordsError(t *testing.T) {
	// R-5WGS-FSI9
	store := newTestStore(t)
	provider := &fakeProvider{result: roundTrip("", agentkit.Usage{}, errors.New("provider exploded"))}
	recorder := postComplete(t, testHandler(store, provider), validRequest())
	assertErrorContains(t, recorder, http.StatusBadGateway, "provider exploded")
	rows, err := store.List(context.Background(), calls.Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("List calls = %#v, %v; want one", rows, err)
	}
	if rows[0].Error != "provider exploded" || rows[0].Class != calls.ClassCompletion ||
		rows[0].Origin != "service:wiki" || rows[0].Name != "wiki.extract" {
		t.Fatalf("failure row = %#v", rows[0])
	}
}

func TestCompleteRecordingFailureDoesNotReportSuccess(t *testing.T) {
	// R-5XOO-TK8Y
	store := failingStore{err: errors.New("disk unavailable")}
	provider := &fakeProvider{result: roundTrip("would succeed", agentkit.Usage{Total: 1}, nil)}
	recorder := postComplete(t, testHandler(store, provider), validRequest())
	assertErrorContains(t, recorder, http.StatusInternalServerError, "record completion")
}

func TestCompleteRejectsNonPOSTAndOversizedBody(t *testing.T) {
	handler := testHandler(failingStore{err: errors.New("should not insert")}, &fakeProvider{})
	nonPOST := httptest.NewRecorder()
	handler.ServeHTTP(nonPOST, httptest.NewRequest(http.MethodGet, "/complete", nil))
	assertErrorContains(t, nonPOST, http.StatusMethodNotAllowed, "POST")

	oversized := httptest.NewRecorder()
	body := strings.NewReader(`{"origin":"service:wiki","padding":"` + strings.Repeat("x", maxCompleteBody) + `"}`)
	handler.ServeHTTP(oversized, httptest.NewRequest(http.MethodPost, "/complete", body))
	assertErrorContains(t, oversized, http.StatusBadRequest, "10 MiB")
}

type fakeProvider struct {
	request *agentkit.Request
	result  *agentkit.RoundTrip
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) RoundTrip(_ context.Context, req *agentkit.Request) *agentkit.RoundTrip {
	copy := *req
	p.request = &copy
	return p.result
}

type failingStore struct{ err error }

func (s failingStore) Insert(context.Context, calls.Row) error { return s.err }

func roundTrip(text string, usage agentkit.Usage, err error) *agentkit.RoundTrip {
	message := agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: text}}}
	return agentkit.NewRoundTrip(message, agentkit.FinishStop, usage, nil, err, 0, false)
}

func validRequest() Request {
	return Request{
		Origin: "service:wiki", Name: "wiki.extract", GroupID: "job-01", Attempt: 2,
		Model: testModel, System: "return the extracted facts",
		Messages: []Message{
			{Role: "user", Text: "extract this"},
			{Role: "assistant", Text: "bad reply"},
			{Role: "user", Text: "correct this"},
		},
	}
}

func testHandler(store CallStore, provider agentkit.Provider) http.Handler {
	build := func(prompt.Config, func(string) string) (agentkit.Provider, error) { return provider, nil }
	getenv := func(string) string { return "test-key" }
	return NewExecutor(store, admit.New(2, 2), build, getenv, func() bool { return false }).CompleteHandler()
}

func newTestStore(t *testing.T) *calls.Store {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "complete.db"))
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}
	return calls.NewStore(conn)
}

func postComplete(t *testing.T, handler http.Handler, req Request) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal request: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/complete", bytes.NewReader(body)))
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
}

func assertErrorContains(t *testing.T, recorder *httptest.ResponseRecorder, status int, want string) {
	t.Helper()
	if recorder.Code != status || !strings.Contains(recorder.Body.String(), want) {
		t.Fatalf("response = %d %s, want %d containing %q", recorder.Code, recorder.Body.String(), status, want)
	}
}

func assertNoCalls(t *testing.T, store *calls.Store) {
	t.Helper()
	rows, err := store.List(context.Background(), calls.Filter{})
	if err != nil {
		t.Fatalf("List calls: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("calls rows = %d, want none", len(rows))
	}
}
