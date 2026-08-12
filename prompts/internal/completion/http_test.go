package completion

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/agentkit"

	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/prompt"
)

// R-J7QG-FRDY
func TestHTTPEnsurePersistsQueuedItemWithoutExecutingIt(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))

	response := postEnsure(t, handler, validEnsureRequest("service:wiki", "job-1"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("Ensure status = %d, want 202: %s", response.Code, response.Body.String())
	}
	var accepted ensureResponse
	decodeHTTPResponse(t, response, &accepted)
	item, err := store.Get(t.Context(), accepted.ID, "service:wiki")
	if err != nil || accepted.Status != StatusQueued || item.Status != StatusQueued || item.StartedAt.IsZero() == false {
		t.Fatalf("accepted/item = %#v/%#v, err=%v", accepted, item, err)
	}
	if got := countRows(t, database, "completions"); got != 1 {
		t.Fatalf("completion rows = %d, want 1", got)
	}
	if got := countRows(t, database, "calls"); got != 0 {
		t.Fatalf("call rows = %d, want 0 before an executor claims the item", got)
	}
}

// R-J8YC-TJ4N
func TestHTTPEnsureIsIdempotentWithinConsumerAndDistinctAcrossConsumers(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))

	first := postEnsure(t, handler, validEnsureRequest("service:wiki", "same-key"))
	var accepted ensureResponse
	decodeHTTPResponse(t, first, &accepted)
	provider := &scriptedProvider{results: []*agentkit.RoundTrip{testRoundTrip(`{"status":"ok","result":{"answer":42}}`, agentkit.Usage{Total: 3}, nil)}}
	build := func(prompt.Config, func(string) string) (agentkit.Provider, error) { return provider, nil }
	executor := NewExecutor(store, calls.NewStore(database), admit.New(2, 1), build, func(string) string { return "test-key" }, func() bool { return false },
		time.Hour, LeaseTTL, DefaultRenewalInterval, NewSystemClock(), nil)
	if didWork, err := executor.ExecuteNext(t.Context()); err != nil || !didWork {
		t.Fatalf("initial execution = %v, %v", didWork, err)
	}

	duplicate := postEnsure(t, handler, validEnsureRequest("service:wiki", "same-key"))
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d, want 200: %s", duplicate.Code, duplicate.Body.String())
	}
	var existing map[string]any
	decodeHTTPResponse(t, duplicate, &existing)
	if existing["id"] != accepted.ID || existing["status"] != StatusDone || existing["result"].(map[string]any)["answer"] != float64(42) {
		t.Fatalf("duplicate response = %#v", existing)
	}
	if didWork, err := executor.ExecuteNext(t.Context()); err != nil || didWork {
		t.Fatalf("execution after duplicate = %v, %v; want empty queue", didWork, err)
	}
	other := postEnsure(t, handler, validEnsureRequest("service:other", "same-key"))
	var otherAccepted ensureResponse
	decodeHTTPResponse(t, other, &otherAccepted)
	if other.Code != http.StatusAccepted || otherAccepted.ID == accepted.ID {
		t.Fatalf("other consumer response = %d %#v, first id %q", other.Code, otherAccepted, accepted.ID)
	}
	if got := countRows(t, database, "completions"); got != 2 {
		t.Fatalf("completion rows = %d, want 2", got)
	}
	if len(provider.Requests()) != 1 || countRows(t, database, "calls") != 1 {
		t.Fatalf("provider requests/call rows = %d/%d, want exactly the original execution", len(provider.Requests()), countRows(t, database, "calls"))
	}
}

// R-JA69-7AVC
func TestHTTPEnsureRejectsInvalidEnvelopeWithoutDurableSideEffects(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ensureRequest)
		want string
	}{
		{"out of catalog", func(in *ensureRequest) { in.Model = "not-a-model" }, "unknown prompt model"},
		{"consumer", func(in *ensureRequest) { in.Consumer = "wiki" }, "consumer"},
		{"origin", func(in *ensureRequest) { in.Origin = "wiki" }, "origin"},
		{"name", func(in *ensureRequest) { in.Name = "wiki" }, "name"},
		{"key", func(in *ensureRequest) { in.Key = "" }, "key"},
		{"messages", func(in *ensureRequest) { in.Messages = nil }, "messages"},
		{"final role", func(in *ensureRequest) { in.Messages[len(in.Messages)-1].Role = "assistant" }, "final message"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openCompletionTestDB(t)
			handler := completionMux(NewHTTP(NewStore(database, "owner-a", time.Now), func(string) string { return "test-key" }, func() bool { return false }))
			request := validEnsureRequest("service:wiki", "job-1")
			test.edit(&request)
			response := postEnsure(t, handler, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("response = %d %s, want 400 containing %q", response.Code, response.Body.String(), test.want)
			}
			if got := countRows(t, database, "completions"); got != 0 {
				t.Fatalf("completion rows = %d, want 0", got)
			}
			if got := countRows(t, database, "calls"); got != 0 {
				t.Fatalf("call rows = %d, want 0", got)
			}
		})
	}
}

// R-JJXG-9GSW
func TestHTTPGetReturnsStageSpecificReadShapesAndNotFound(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))
	response := postEnsure(t, handler, validEnsureRequest("service:wiki", "stages"))
	var accepted ensureResponse
	decodeHTTPResponse(t, response, &accepted)

	assertReadShape(t, getCompletion(t, handler, accepted.ID), StatusQueued, false, false)
	if _, err := store.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertReadShape(t, getCompletion(t, handler, accepted.ID), StatusRunning, false, false)
	if err := store.Complete(t.Context(), accepted.ID, `{"answer":[1,2]}`, `{"total":9}`, 1.5); err != nil {
		t.Fatal(err)
	}
	done := getCompletion(t, handler, accepted.ID)
	assertReadShape(t, done, StatusDone, true, false)
	if done["context"].(map[string]any)["document"] != "abc" || done["cost_usd"] != 1.5 || done["usage"].(map[string]any)["total"] != float64(9) {
		t.Fatalf("done response = %#v", done)
	}

	failedItem, _, err := store.Ensure(t.Context(), Item{Consumer: "service:wiki", Origin: "service:wiki", Key: "failed", Context: `"opaque"`, Name: "wiki.extract", Request: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Fail(t.Context(), failedItem.ID, "provider exploded", `{}`, 0); err != nil {
		t.Fatal(err)
	}
	failed := getCompletion(t, handler, failedItem.ID)
	assertReadShape(t, failed, StatusFailed, false, true)
	if failed["error"] != "provider exploded" {
		t.Fatalf("failed response = %#v", failed)
	}

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/completions/unknown?consumer=service:wiki", nil))
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), ErrNotFound.Error()) {
		t.Fatalf("missing response = %d %s", missing.Code, missing.Body.String())
	}
}

// R-JL5C-N8JL
func TestHTTPInboxReturnsOnlyConsumerTerminalItemsOldestFirst(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	seedHTTPItem(t, database, "newer-failed", "service:wiki", StatusFailed, now.Add(time.Minute), "key-b", `["context-b"]`, "", "bad input")
	seedHTTPItem(t, database, "older-done", "service:wiki", StatusDone, now, "key-a", `{"context":"a"}`, `{"ok":true}`, "")
	seedHTTPItem(t, database, "queued", "service:wiki", StatusQueued, now.Add(-time.Hour), "key-q", `{"context":"q"}`, "", "")
	seedHTTPItem(t, database, "other", "service:other", StatusDone, now.Add(-time.Hour), "key-o", `{"context":"o"}`, `false`, "")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/completions?consumer=service:wiki", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("Inbox status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var items []map[string]any
	decodeHTTPResponse(t, recorder, &items)
	if len(items) != 2 || items[0]["id"] != "older-done" || items[1]["id"] != "newer-failed" {
		t.Fatalf("inbox = %#v", items)
	}
	if items[0]["key"] != "key-a" || items[0]["context"].(map[string]any)["context"] != "a" || items[0]["result"].(map[string]any)["ok"] != true {
		t.Fatalf("done item = %#v", items[0])
	}
	if items[1]["key"] != "key-b" || items[1]["context"].([]any)[0] != "context-b" || items[1]["error"] != "bad input" {
		t.Fatalf("failed item = %#v", items[1])
	}
}

// R-U7PZ-0AIV
func TestHTTPContextAcceptsJSONValuesAndEchoesStoredBytesAtEveryRead(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := NewStore(database, "owner-a", func() time.Time {
		now = now.Add(time.Second)
		return now
	})
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))

	contexts := []json.RawMessage{
		json.RawMessage(`{"document": "abc", "cursor": 7}`),
		json.RawMessage(`["document", {"cursor": 7}]`),
	}
	ids := make([]string, 0, len(contexts))
	for i, contextValue := range contexts {
		request := validEnsureRequest("service:wiki", fmt.Sprintf("raw-context-%d", i))
		response := postEnsureWithRawContext(t, handler, request, contextValue)
		if response.Code != http.StatusAccepted {
			t.Fatalf("Ensure context %s = %d, want 202: %s", contextValue, response.Code, response.Body.String())
		}
		var accepted ensureResponse
		decodeHTTPResponse(t, response, &accepted)
		ids = append(ids, accepted.ID)
		assertRawContext(t, rawGetCompletion(t, handler, accepted.ID), contextValue)

		claimed, err := store.Claim(t.Context())
		if err != nil || claimed.ID != accepted.ID {
			t.Fatalf("Claim = %#v, %v; want %q", claimed, err, accepted.ID)
		}
		if err := store.Complete(t.Context(), accepted.ID, `true`, `{}`, 0); err != nil {
			t.Fatal(err)
		}
		assertRawContext(t, rawGetCompletion(t, handler, accepted.ID), contextValue)
	}

	inbox := httptest.NewRecorder()
	handler.ServeHTTP(inbox, httptest.NewRequest(http.MethodGet, "/completions?consumer=service:wiki", nil))
	if inbox.Code != http.StatusOK {
		t.Fatalf("Inbox = %d: %s", inbox.Code, inbox.Body.String())
	}
	var items []readResponse
	decodeHTTPResponse(t, inbox, &items)
	if len(items) != len(contexts) {
		t.Fatalf("Inbox items = %#v, want %d", items, len(contexts))
	}
	for i := range contexts {
		if items[i].ID != ids[i] || !bytes.Equal(items[i].Context, contexts[i]) {
			t.Fatalf("Inbox item %d = id %q context %s, want id %q context %s", i, items[i].ID, items[i].Context, ids[i], contexts[i])
		}
	}

	omitted := validEnsureRequest("service:wiki", "omitted-context")
	omitted.Context = nil
	omittedResponse := postEnsure(t, handler, omitted)
	if omittedResponse.Code != http.StatusAccepted {
		t.Fatalf("Ensure omitted context = %d, want 202: %s", omittedResponse.Code, omittedResponse.Body.String())
	}
	var omittedAccepted ensureResponse
	decodeHTTPResponse(t, omittedResponse, &omittedAccepted)
	assertRawContext(t, rawGetCompletion(t, handler, omittedAccepted.ID), json.RawMessage("null"))
	claimed, err := store.Claim(t.Context())
	if err != nil || claimed.ID != omittedAccepted.ID {
		t.Fatalf("Claim omitted context = %#v, %v; want %q", claimed, err, omittedAccepted.ID)
	}
	if err := store.Complete(t.Context(), omittedAccepted.ID, `true`, `{}`, 0); err != nil {
		t.Fatal(err)
	}
	assertRawContext(t, rawGetCompletion(t, handler, omittedAccepted.ID), json.RawMessage("null"))

	omittedInbox := httptest.NewRecorder()
	handler.ServeHTTP(omittedInbox, httptest.NewRequest(http.MethodGet, "/completions?consumer=service:wiki", nil))
	if omittedInbox.Code != http.StatusOK {
		t.Fatalf("Inbox with omitted context = %d: %s", omittedInbox.Code, omittedInbox.Body.String())
	}
	var terminalItems []readResponse
	decodeHTTPResponse(t, omittedInbox, &terminalItems)
	foundOmitted := false
	for _, item := range terminalItems {
		if item.ID == omittedAccepted.ID {
			foundOmitted = true
			if !bytes.Equal(item.Context, json.RawMessage("null")) {
				t.Fatalf("Inbox omitted context = %s, want null", item.Context)
			}
		}
	}
	if !foundOmitted {
		t.Fatalf("Inbox items = %#v, missing omitted-context item %q", terminalItems, omittedAccepted.ID)
	}
}

// R-U8XV-E29K
func TestHTTPEnsureRejectsMalformedJSONContextWithoutDurableSideEffects(t *testing.T) {
	database := openCompletionTestDB(t)
	handler := completionMux(NewHTTP(NewStore(database, "owner-a", time.Now), func(string) string { return "test-key" }, func() bool { return false }))
	body := `{"consumer":"service:wiki","origin":"trigger:dropbox","key":"bad-context","context":{"truncated":,"name":"wiki.extract"}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/completions", strings.NewReader(body)))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid JSON body") {
		t.Fatalf("response = %d %s, want 400 naming invalid JSON body", response.Code, response.Body.String())
	}
	if got := countRows(t, database, "completions"); got != 0 {
		t.Fatalf("completion rows = %d, want 0", got)
	}
	if got := countRows(t, database, "calls"); got != 0 {
		t.Fatalf("call rows = %d, want 0", got)
	}
}

// R-JMD9-10AA
func TestHTTPAckRemovesItemAndAllowsKeyToCreateNewWork(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	handler := completionMux(NewHTTP(store, func(string) string { return "test-key" }, func() bool { return false }))
	first := postEnsure(t, handler, validEnsureRequest("service:wiki", "repeatable"))
	var accepted ensureResponse
	decodeHTTPResponse(t, first, &accepted)
	if _, err := database.ExecContext(t.Context(), `UPDATE completions SET status='done',result='true',finished_at=? WHERE id=?`, formatTime(time.Now()), accepted.ID); err != nil {
		t.Fatal(err)
	}

	ack := deleteCompletion(t, handler, accepted.ID)
	if ack.Code != http.StatusNoContent {
		t.Fatalf("Ack status = %d: %s", ack.Code, ack.Body.String())
	}
	if missing := rawGetCompletion(t, handler, accepted.ID); missing.Code != http.StatusNotFound {
		t.Fatalf("Get after Ack = %d, want 404", missing.Code)
	}
	inbox := httptest.NewRecorder()
	handler.ServeHTTP(inbox, httptest.NewRequest(http.MethodGet, "/completions?consumer=service:wiki", nil))
	var items []readResponse
	decodeHTTPResponse(t, inbox, &items)
	if len(items) != 0 {
		t.Fatalf("Inbox after Ack = %#v", items)
	}
	if repeated := deleteCompletion(t, handler, accepted.ID); repeated.Code != http.StatusNotFound {
		t.Fatalf("repeated Ack = %d, want 404", repeated.Code)
	}
	second := postEnsure(t, handler, validEnsureRequest("service:wiki", "repeatable"))
	var secondAccepted ensureResponse
	decodeHTTPResponse(t, second, &secondAccepted)
	if second.Code != http.StatusAccepted || secondAccepted.ID == accepted.ID {
		t.Fatalf("new Ensure = %d %#v, old id %q", second.Code, secondAccepted, accepted.ID)
	}
	provider := &scriptedProvider{results: []*agentkit.RoundTrip{testRoundTrip(`{"status":"ok","result":"recomputed"}`, agentkit.Usage{Total: 1}, nil)}}
	build := func(prompt.Config, func(string) string) (agentkit.Provider, error) { return provider, nil }
	executor := NewExecutor(store, calls.NewStore(database), admit.New(2, 1), build, func(string) string { return "test-key" }, func() bool { return false },
		time.Hour, LeaseTTL, DefaultRenewalInterval, NewSystemClock(), nil)
	if didWork, err := executor.ExecuteNext(t.Context()); err != nil || !didWork {
		t.Fatalf("replacement execution = %v, %v", didWork, err)
	}
	recomputed, err := store.Get(t.Context(), secondAccepted.ID, "service:wiki")
	if err != nil || recomputed.Status != StatusDone || recomputed.Result != `"recomputed"` || len(provider.Requests()) != 1 {
		t.Fatalf("replacement item/provider calls = %#v/%d, err=%v", recomputed, len(provider.Requests()), err)
	}
}

func completionMux(handler *HTTP) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /completions", handler.EnsureHandler())
	mux.Handle("GET /completions", handler.InboxHandler())
	mux.Handle("GET /completions/{id}", handler.GetHandler())
	mux.Handle("DELETE /completions/{id}", handler.AckHandler())
	return mux
}

func validEnsureRequest(consumer, key string) ensureRequest {
	return ensureRequest{Consumer: consumer, Origin: "trigger:dropbox", Key: key, Context: json.RawMessage(`{"document":"abc"}`),
		Name: "wiki.extract", GroupID: "batch-1", Attempt: 2, Model: completionTestModel,
		System: "extract facts", Messages: []Message{{Role: "user", Text: "extract this"}}}
}

func assertRawContext(t *testing.T, recorder *httptest.ResponseRecorder, want json.RawMessage) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("Get = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response readResponse
	decodeHTTPResponse(t, recorder, &response)
	if !bytes.Equal(response.Context, want) {
		t.Fatalf("context = %s, want byte-identical %s", response.Context, want)
	}
}

func postEnsure(t *testing.T, handler http.Handler, request ensureRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/completions", bytes.NewReader(body)))
	return recorder
}

func postEnsureWithRawContext(t *testing.T, handler http.Handler, request ensureRequest, contextValue json.RawMessage) *httptest.ResponseRecorder {
	t.Helper()
	const marker = `"__raw_context_marker__"`
	request.Context = json.RawMessage(marker)
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.Replace(body, []byte(marker), contextValue, 1)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/completions", bytes.NewReader(body)))
	return recorder
}

func rawGetCompletion(t *testing.T, handler http.Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/completions/"+id+"?consumer=service:wiki", nil))
	return recorder
}

func getCompletion(t *testing.T, handler http.Handler, id string) map[string]any {
	t.Helper()
	recorder := rawGetCompletion(t, handler, id)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Get %q = %d: %s", id, recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	decodeHTTPResponse(t, recorder, &response)
	return response
}

func deleteCompletion(t *testing.T, handler http.Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/completions/"+id+"?consumer=service:wiki", nil))
	return recorder
}

func assertReadShape(t *testing.T, response map[string]any, status string, hasResult, hasError bool) {
	t.Helper()
	if response["status"] != status {
		t.Fatalf("response status = %#v, want %q", response, status)
	}
	_, result := response["result"]
	_, failure := response["error"]
	if result != hasResult || failure != hasError {
		t.Fatalf("response = %#v, result/error presence = %v/%v, want %v/%v", response, result, failure, hasResult, hasError)
	}
}

func decodeHTTPResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
}

func countRows(t *testing.T, database *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func seedHTTPItem(t *testing.T, database *sql.DB, id, consumer, status string, created time.Time, key, contextValue, result, failure string) {
	t.Helper()
	finished := ""
	if status == StatusDone || status == StatusFailed {
		finished = formatTime(created.Add(time.Minute))
	}
	_, err := database.ExecContext(t.Context(), `INSERT INTO completions
		(id,consumer,origin,key,context,name,request,status,result,error,usage_json,cost_usd,created_at,finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, consumer, "service:wiki", key, contextValue, "wiki.extract", `{}`,
		status, result, failure, `{}`, 0.5, formatTime(created), finished)
	if err != nil {
		t.Fatal(err)
	}
}
