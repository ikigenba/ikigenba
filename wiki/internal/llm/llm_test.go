package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"appkit/httpclient"
	"appkit/telemetry"
	"eventplane/correlation"
)

type fixture struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

func TestClientPropagatesOnlyContextChainToLoopbackPrompts(t *testing.T) {
	// R-XLHZ-WPT5
	type received struct{ path, chain string }
	var calls []received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, received{path: r.URL.Path, chain: r.Header.Get(correlation.Header)})
		if r.URL.Path == "/embed" {
			_ = json.NewEncoder(w).Encode(map[string]any{"vectors": [][]float32{{1}}})
			return
		}
		writeItem(t, w, http.StatusAccepted, Item{ID: "q1", Status: "queued"})
	}))
	defer server.Close()
	client := New(server.URL, httpclient.New(httpclient.Options{Recorder: &telemetry.Recorder{}, Timeout: -1}))
	chainID := "01KZ6V08B73Q7W1G5GR3C2E5MK"
	for _, ctx := range []context.Context{correlation.WithContext(context.Background(), chainID), context.Background()} {
		if _, err := client.Ensure(ctx, Request{Key: "key", Site: CallSite{Stage: "extract"}, Attempt: 1, Messages: []Message{{Role: "user", Text: "prompt"}}}); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		if _, err := client.Embed(ctx, EmbedSite{Name: "wiki.embed-query", Dims: 1}, Attribution{}, "query", []string{"prompt"}); err != nil {
			t.Fatalf("Embed: %v", err)
		}
	}
	want := []received{{"/completions", chainID}, {"/embed", chainID}, {"/completions", ""}, {"/embed", ""}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %+v, want %+v", calls, want)
	}
}

func TestProductionTreeDoesNotConstructHTTPClients(t *testing.T) {
	// R-XMPW-AHJU
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "project" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "http.Client{") {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production tree: %v", err)
	}
	if len(offenders) != 0 {
		t.Fatalf("production constructs private HTTP clients: %v", offenders)
	}
}

func TestEnsureCarriesFullQueueWireContract(t *testing.T) {
	// R-JW4G-367U
	// R-MSKH-GPX5
	temp, thinking := 0.25, false
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/completions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		var err error
		raw, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		writeItem(t, w, http.StatusAccepted, Item{ID: "item-1", Key: "base/a2", Status: "queued", Context: []byte(`{"job":"j1"}`)})
	}))
	defer server.Close()
	site := CallSite{Stage: "compile", System: "base system", Config: Config{Model: "m", Provider: "p", Temperature: &temp, MaxTokens: 321, Effort: "low", Thinking: &thinking}}
	ctx := WithSystemComposer(context.Background(), func(system string) string { return system + " + scope" })
	_, err := New(server.URL).Ensure(ctx, Request{Key: "base/a2", Context: []byte(`{"job":"j1"}`), Site: site, Attr: Attribution{Origin: "user:a", GroupID: "chain-1"}, Attempt: 2, Messages: []Message{{Role: "user", Text: "original"}, {Role: "assistant", Text: `{"bad":true}`}, {Role: "user", Text: "correct it"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"consumer": Consumer, "key": "base/a2", "origin": "user:a", "group_id": "chain-1", "attempt": float64(2), "name": "wiki.compile", "model": "m", "provider": "p", "system": "base system + scope"} {
		if got[key] != want {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
	if !reflect.DeepEqual(got["context"], map[string]any{"job": "j1"}) {
		t.Errorf("context = %#v", got["context"])
	}
	config := got["config"].(map[string]any)
	if !reflect.DeepEqual(config, map[string]any{"temperature": 0.25, "max_tokens": float64(321), "effort": "low", "thinking": false}) {
		t.Errorf("config = %#v", config)
	}
	messages := got["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %#v", messages)
	}
	for i, message := range messages {
		fields := message.(map[string]any)
		if len(fields) != 2 || fields["role"] == nil || fields["text"] == nil {
			t.Errorf("message %d = %#v; want exactly role and text", i, fields)
		}
	}
	if messages[2].(map[string]any)["role"] != "user" {
		t.Errorf("final message = %#v", messages[2])
	}
}

func TestLiveAndHandoffQueueShapesUseSeparateConsumerPartitions(t *testing.T) {
	// R-UCLK-JDHN
	var doEnsure, handoffEnsure []byte
	var inboxQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/completions":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var body ensureRequest
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode Ensure body: %v", err)
			}
			if body.Key == "ask/a1" {
				doEnsure = raw
				writeItem(t, w, http.StatusAccepted, Item{ID: "ask-item", Status: "done"})
				return
			}
			handoffEnsure = raw
			writeItem(t, w, http.StatusAccepted, Item{ID: "pipeline-item", Status: "queued"})
		case r.Method == http.MethodDelete && r.URL.Path == "/completions/ask-item":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/completions":
			inboxQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode([]wireItem{})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := New(server.URL)
	if _, err := c.Do(context.Background(), Request{Key: "ask/a1"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if _, err := c.Ensure(context.Background(), Request{Key: "job/extract/unit/a1"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := c.Inbox(context.Background()); err != nil {
		t.Fatalf("Inbox: %v", err)
	}

	consumer := func(raw []byte) string {
		t.Helper()
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode captured raw body: %v", err)
		}
		value, _ := body["consumer"].(string)
		return value
	}
	if got := consumer(doEnsure); got != ConsumerAsk {
		t.Errorf("Do Ensure consumer = %q, want %q; raw=%s", got, ConsumerAsk, doEnsure)
	}
	if got := consumer(handoffEnsure); got != Consumer {
		t.Errorf("handoff Ensure consumer = %q, want %q; raw=%s", got, Consumer, handoffEnsure)
	}
	if inboxQuery != "consumer=service%3Awiki" {
		t.Errorf("Inbox query = %q, want only pipeline consumer", inboxQuery)
	}
}

func TestPerItemQueueVerbsPinOwningConsumerPartition(t *testing.T) {
	// R-9S84-C6J2
	type call struct {
		method   string
		path     string
		consumer string
	}
	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/completions":
			writeItem(t, w, http.StatusAccepted, Item{ID: "ask-item", Status: "queued"})
		case r.URL.Path == "/completions/pipeline-item":
			calls = append(calls, call{method: r.Method, path: r.URL.Path, consumer: r.URL.Query().Get("consumer")})
			if r.Method == http.MethodGet {
				writeItem(t, w, http.StatusOK, Item{ID: "pipeline-item", Status: "done"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/completions/ask-item":
			calls = append(calls, call{method: r.Method, path: r.URL.Path, consumer: r.URL.Query().Get("consumer")})
			if r.Method == http.MethodGet {
				writeItem(t, w, http.StatusOK, Item{ID: "ask-item", Status: "done"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := New(server.URL)
	c.pollInterval = time.Millisecond
	if _, err := c.Get(context.Background(), "pipeline-item"); err != nil {
		t.Fatalf("handoff Get: %v", err)
	}
	if err := c.Ack(context.Background(), "pipeline-item"); err != nil {
		t.Fatalf("handoff Ack: %v", err)
	}
	if _, err := c.Do(context.Background(), Request{Key: "ask/a1"}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	want := []call{
		{method: http.MethodGet, path: "/completions/pipeline-item", consumer: Consumer},
		{method: http.MethodDelete, path: "/completions/pipeline-item", consumer: Consumer},
		{method: http.MethodGet, path: "/completions/ask-item", consumer: ConsumerAsk},
		{method: http.MethodDelete, path: "/completions/ask-item", consumer: ConsumerAsk},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("per-item calls = %#v, want %#v", calls, want)
	}
}

func TestQueueGetInboxAndAckVerbs(t *testing.T) {
	// R-JXCC-GXYJ
	var inboxQuery string
	var deletes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/completions/queued":
			writeItem(t, w, http.StatusOK, Item{ID: "queued", Status: "queued"})
		case r.Method == http.MethodGet && r.URL.Path == "/completions/running":
			writeItem(t, w, http.StatusOK, Item{ID: "running", Status: "running"})
		case r.Method == http.MethodGet && r.URL.Path == "/completions/done":
			writeItem(t, w, http.StatusOK, Item{ID: "done", Key: "key", Status: "done", Context: []byte(`{"x":1}`), Result: json.RawMessage(`{"title":"ok"}`), Usage: json.RawMessage(`{"output":7}`), CostUSD: 0.12})
		case r.Method == http.MethodGet && r.URL.Path == "/completions/failed":
			writeItem(t, w, http.StatusOK, Item{ID: "failed", Status: "failed", Error: "provider broke"})
		case r.Method == http.MethodGet && r.URL.Path == "/completions/gone":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/completions":
			inboxQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode([]wireItem{{ID: "i1", Status: "done", Result: json.RawMessage(`{"ok":true}`)}})
		case r.Method == http.MethodDelete:
			deletes = append(deletes, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	c := New(server.URL)
	for _, status := range []string{"queued", "running", "done", "failed"} {
		item, err := c.Get(context.Background(), status)
		if err != nil || item.Status != status {
			t.Fatalf("Get(%s) = %+v, %v", status, item, err)
		}
		if (status == "queued" || status == "running") && len(item.Result) != 0 {
			t.Errorf("Get(%s) result = %s", status, item.Result)
		}
		if status == "done" && (string(item.Context) != `{"x":1}` || string(item.Result) != `{"title":"ok"}` || string(item.Usage) != `{"output":7}` || item.CostUSD != 0.12) {
			t.Errorf("done item = %+v", item)
		}
		if status == "failed" && item.Error != "provider broke" {
			t.Errorf("failed item = %+v", item)
		}
	}
	if _, err := c.Get(context.Background(), "gone"); !errors.Is(err, ErrGone) {
		t.Fatalf("gone error = %v", err)
	}
	items, err := c.Inbox(context.Background())
	if err != nil || len(items) != 1 || items[0].ID != "i1" || inboxQuery != "consumer=service%3Awiki" {
		t.Fatalf("Inbox = %+v, %v, query %q", items, err, inboxQuery)
	}
	if err := c.Ack(context.Background(), "gone"); err != nil || !reflect.DeepEqual(deletes, []string{"/completions/gone"}) {
		t.Fatalf("Ack = %v, deletes=%v", err, deletes)
	}
}

func TestDoPollsUntilDoneAndHonorsCancellation(t *testing.T) {
	// R-JYK8-UPP8
	var gets, deletes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeItem(t, w, http.StatusAccepted, Item{ID: "q1", Status: "queued"})
		case http.MethodGet:
			gets++
			status := "running"
			if gets == 2 {
				status = "done"
			}
			writeItem(t, w, http.StatusOK, Item{ID: "q1", Status: status, Result: json.RawMessage(`{"title":"ok"}`)})
		case http.MethodDelete:
			deletes++
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	c := New(server.URL)
	c.pollInterval = time.Millisecond
	item, err := c.Do(context.Background(), Request{Key: "base/a1", Attempt: 1})
	server.Close()
	if err != nil || item.Status != "done" || gets != 2 || deletes != 1 {
		t.Fatalf("Do = %+v, %v; gets=%d deletes=%d", item, err, gets, deletes)
	}

	gets = 0
	ctx, cancel := context.WithCancel(context.Background())
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writeItem(t, w, http.StatusAccepted, Item{ID: "q2", Status: "queued"})
			return
		}
		gets++
		cancel()
		writeItem(t, w, http.StatusOK, Item{ID: "q2", Status: "running"})
	}))
	defer server.Close()
	c = New(server.URL)
	c.pollInterval = time.Millisecond
	_, err = c.Do(ctx, Request{Key: "base/a1", Attempt: 1})
	if !errors.Is(err, context.Canceled) || gets != 1 {
		t.Fatalf("cancel Do error=%v gets=%d", err, gets)
	}
	time.Sleep(3 * time.Millisecond)
	if gets != 1 {
		t.Fatalf("polls continued after cancellation: %d", gets)
	}
}

func TestDoFailedAndGoneAreTerminal(t *testing.T) {
	// R-JZS5-8HFX
	for name, getStatus := range map[string]int{"failed": http.StatusOK, "gone": http.StatusNotFound} {
		t.Run(name, func(t *testing.T) {
			var ensures, gets, acks int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodPost:
					ensures++
					writeItem(t, w, http.StatusAccepted, Item{ID: "q", Status: "queued"})
				case http.MethodGet:
					gets++
					if getStatus == http.StatusNotFound {
						w.WriteHeader(http.StatusNotFound)
					} else {
						writeItem(t, w, http.StatusOK, Item{ID: "q", Status: "failed", Error: "model exploded"})
					}
				case http.MethodDelete:
					acks++
					w.WriteHeader(http.StatusBadGateway) // failure ack remains tolerant
				}
			}))
			defer server.Close()
			c := New(server.URL)
			c.pollInterval = time.Millisecond
			_, err := c.Do(context.Background(), Request{Key: "base/a1", Attempt: 1})
			if err == nil || ensures != 1 || gets != 1 {
				t.Fatalf("Do error=%v ensures=%d gets=%d", err, ensures, gets)
			}
			if name == "failed" && (!strings.Contains(err.Error(), "model exploded") || acks != 1) {
				t.Fatalf("failed error=%v acks=%d", err, acks)
			}
			if name == "gone" && (!errors.Is(err, ErrGone) || acks != 0) {
				t.Fatalf("gone error=%v acks=%d", err, acks)
			}
		})
	}
}

func TestDoGoneErrorNamesThePolledItem(t *testing.T) {
	// R-UDTG-X58C
	const vanishedID = "completion-that-vanished"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeItem(t, w, http.StatusAccepted, Item{ID: vanishedID, Status: "queued"})
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	c := New(server.URL)
	c.pollInterval = time.Millisecond

	_, err := c.Do(context.Background(), Request{Key: "ask/a1"})
	if !errors.Is(err, ErrGone) || !strings.Contains(err.Error(), vanishedID) {
		t.Fatalf("Do error = %v, want ErrGone naming %q", err, vanishedID)
	}
}

func TestJSONReturnsValidatedDoneResult(t *testing.T) {
	// R-K101-M96M
	server := terminalServer(t, []json.RawMessage{json.RawMessage(`{"title":"ok","count":2}`)}, nil)
	defer server.Close()
	validated := false
	got, err := JSON(context.Background(), New(server.URL), CallSite{}, Attribution{}, "base", "prompt", func(v *fixture) error {
		validated = true
		if v.Title == "" {
			return errors.New("title required")
		}
		return nil
	})
	if err != nil || got != (fixture{Title: "ok", Count: 2}) || !validated {
		t.Fatalf("JSON = %#v, %v, validated=%v", got, err, validated)
	}
}

func TestJSONSemanticFailureEnsuresCorrectiveReplay(t *testing.T) {
	// R-K27Y-00XB
	var requests []ensureRequest
	results := []json.RawMessage{json.RawMessage(`{"title":""}`), json.RawMessage(`{"title":"fixed"}`)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req ensureRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, req)
		writeItem(t, w, http.StatusAccepted, Item{ID: "q", Status: "done", Result: results[len(requests)-1]})
	}))
	defer server.Close()
	got, err := JSON(context.Background(), New(server.URL), CallSite{MaxParseRetries: 1}, Attribution{}, "base", "original", func(v *fixture) error {
		if v.Title == "" {
			return errors.New("title required")
		}
		return nil
	})
	if err != nil || got.Title != "fixed" || len(requests) != 2 {
		t.Fatalf("JSON = %#v, %v, requests=%+v", got, err, requests)
	}
	if requests[0].Key != "base/a1" || requests[0].Attempt != 1 || len(requests[0].Messages) != 1 || requests[1].Key != "base/a2" || requests[1].Attempt != 2 {
		t.Fatalf("attempts = %+v", requests)
	}
	wantRoles := []string{"user", "assistant", "user"}
	var roles []string
	for _, message := range requests[1].Messages {
		roles = append(roles, message.Role)
	}
	if !reflect.DeepEqual(roles, wantRoles) || requests[1].Messages[0].Text != "original" || requests[1].Messages[1].Text != `{"title":""}` {
		t.Fatalf("corrective replay = %+v", requests[1].Messages)
	}
}

func TestJSONExhaustsExactlyConfiguredCorrectiveAttempts(t *testing.T) {
	// R-K3FU-DSO0
	var ensures int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		ensures++
		writeItem(t, w, http.StatusAccepted, Item{ID: "q", Status: "done", Result: json.RawMessage(`"not-json"`)})
	}))
	defer server.Close()
	got, err := JSON[fixture](context.Background(), New(server.URL), CallSite{MaxParseRetries: 2}, Attribution{}, "base", "prompt", nil)
	if err == nil || got != (fixture{}) || ensures != 3 || !strings.Contains(err.Error(), "after 3 attempt") {
		t.Fatalf("JSON = %#v, %v, ensures=%d", got, err, ensures)
	}
}

func TestJSONEnsure400FailsImmediatelyWithPromptsError(t *testing.T) {
	// R-K4NQ-RKEP
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad model"}`))
	}))
	defer server.Close()
	_, err := JSON[fixture](context.Background(), New(server.URL), CallSite{MaxParseRetries: 3}, Attribution{}, "base", "prompt", nil)
	if err == nil || !strings.Contains(err.Error(), "bad model") || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestJSONTruncationIsDistinctAndTerminal(t *testing.T) {
	// R-MTSD-UHNU
	// R-MV0A-89EJ
	var ensures, acks int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			acks++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		ensures++
		writeItem(t, w, http.StatusAccepted, Item{ID: "q", Status: "done", Result: json.RawMessage(`{"title":"partial"}`), Usage: json.RawMessage(`{"output_tokens":10}`)})
	}))
	defer server.Close()
	_, err := JSON[fixture](context.Background(), New(server.URL), CallSite{Config: Config{MaxTokens: 10}, MaxParseRetries: 3}, Attribution{}, "base", "prompt", nil)
	if !errors.Is(err, ErrTruncated) || ensures != 1 || acks != 1 {
		t.Fatalf("error=%v ensures=%d acks=%d", err, ensures, acks)
	}
}

func terminalServer(t *testing.T, results []json.RawMessage, usages []json.RawMessage) *httptest.Server {
	t.Helper()
	var ensures int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		usage := json.RawMessage(nil)
		if len(usages) > ensures {
			usage = usages[ensures]
		}
		writeItem(t, w, http.StatusAccepted, Item{ID: "q", Status: "done", Result: results[ensures], Usage: usage})
		ensures++
	}))
}

func writeItem(t *testing.T, w http.ResponseWriter, status int, item Item) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(wireItem{ID: item.ID, Key: item.Key, Status: item.Status, Context: json.RawMessage(item.Context), Result: item.Result, Error: item.Error, Usage: item.Usage, CostUSD: item.CostUSD}); err != nil {
		t.Fatal(err)
	}
}
