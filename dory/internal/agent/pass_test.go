package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/dory/internal/agent"
	"github.com/ikigenba/ikigenba/dory/internal/model"
	"github.com/ikigenba/ikigenba/dory/internal/store"
)

const (
	rootPrompt   = "coordinate fixture inspection"
	childPrompt  = "inspect fixture"
	secondPrompt = "finish without text"
)

type passTrace struct {
	mu      sync.Mutex
	events  []tracedEvent
	errors  []tracedError
	records []agentkit.LogRecord
}

type tracedEvent struct {
	address string
	event   agentkit.Event
}

type tracedError struct {
	address string
	err     error
}

func (t *passTrace) Event(address string, event agentkit.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, tracedEvent{address: address, event: event})
}

func (t *passTrace) Error(address string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.errors = append(t.errors, tracedError{address: address, err: err})
}

func (t *passTrace) Record(record agentkit.LogRecord) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, record)
	return renderedRecord(record)
}

type passProvider struct {
	t      *testing.T
	store  *store.Store
	server *httptest.Server

	mu       sync.Mutex
	requests []map[string]any
}

func newPassProvider(t *testing.T, session *store.Store) *passProvider {
	t.Helper()
	provider := &passProvider{t: t, store: session}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *passProvider) serveHTTP(w http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		p.t.Errorf("read provider request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		p.t.Errorf("decode provider request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.requests = append(p.requests, decoded)
	requestNumber := len(p.requests)
	p.mu.Unlock()

	if requestNumber == 1 {
		assertStoredPrompt(p.t, p.store, "1", rootPrompt)
	}
	if requestNumber == 2 {
		assertStoredPrompt(p.t, p.store, "1.1", childPrompt)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	switch requestNumber {
	case 1:
		writeToolCall(w, "delegate-call", "delegate", `{"role":"worker","prompt":"inspect fixture"}`)
	case 2:
		writeToolCall(w, "read-call", "Read", `{"file_path":"fixture.txt"}`)
	case 3:
		writeToolCall(w, "glob-call", "Glob", `{"pattern":"**/*"}`)
	case 4:
		writeToolCall(w, "grep-call", "Grep", `{"pattern":"private marker"}`)
	case 5:
		writeText(w, "child ", "report")
	case 6:
		writeText(w, "root ", "report")
	case 7:
		writeEmptyMessage(w)
	default:
		p.t.Errorf("unexpected provider request %d", requestNumber)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (p *passProvider) capturedRequests() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.requests...)
}

// R-J2SN-RF8I
// R-J40K-56Z7
// R-J58G-IYPW
// R-J6GC-WQGL
// R-J7O9-AI7A
// R-JJV9-47M8
func TestRunPassPersistsAndRunsRootAndDelegatedAgents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte("rooted fixture contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "private.txt"), []byte("private marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := createPassStore(t, root)
	provider := newPassProvider(t, session)
	factory := openPassFactory(t, provider.server.URL)
	trace := &passTrace{}
	cfg := agent.Config{
		Store:      session,
		Supervisor: factory,
		Worker:     factory,
		Root:       root,
		Now:        passTime,
		Trace:      trace,
	}

	first, err := agent.RunPass(context.Background(), cfg, rootPrompt)
	if err != nil {
		t.Fatalf("first RunPass: %v", err)
	}
	second, err := agent.RunPass(context.Background(), cfg, secondPrompt)
	if err != nil {
		t.Fatalf("second RunPass: %v", err)
	}
	if first.Address != "1" || second.Address != "2" {
		t.Fatalf("RunPass addresses = %q, %q, want decimal 1, 2", first.Address, second.Address)
	}
	if first.Report != "root report" || second.Report != "" {
		t.Fatalf("RunPass reports = %q, %q, want root report and empty report", first.Report, second.Report)
	}

	entries := allPassEntries(t, session)
	assertEntry(t, entries, "1", store.KindPrompt, rootPrompt)
	assertEntry(t, entries, "1.1", store.KindPrompt, childPrompt)
	assertEntry(t, entries, "2", store.KindPrompt, secondPrompt)
	assertEntry(t, entries, "1.1", store.KindReport, "child report")
	assertEntry(t, entries, "1", store.KindReport, "root report")
	assertEntry(t, entries, "2", store.KindReport, "")
	assertTranscriptIDsAndSummaries(t, entries, []string{"1", "1.1", "2"})
	assertTraceRenderedTranscripts(t, entries, trace)

	requests := provider.capturedRequests()
	if len(requests) != 7 {
		t.Fatalf("provider requests = %d, want 7", len(requests))
	}
	assertMessages(t, requests[0], "You are the supervisor for a single pass. Direct the work toward the user's request, delegate focused tasks when useful, and return a clear final report.", rootPrompt)
	assertMessages(t, requests[1], "You are a worker within a single pass. Complete the task assigned by the supervisor and return a concise, accurate report of the result.", childPrompt)
	// agentkit advertises eager tools in its canonical name order.
	assertToolNames(t, requests[0], []string{"delegate", "fetch", "remember", "search"})
	assertToolNames(t, requests[1], []string{"Bash", "Edit", "Glob", "Grep", "Read", "Write"})
	if got := toolResultContent(t, requests[2]); got == "" || !contains(got, "rooted fixture contents") {
		t.Fatalf("worker Read result = %q, want rooted fixture contents", got)
	}
	if got := toolResultContent(t, requests[3]); contains(got, ".git") || contains(got, "private.txt") {
		t.Fatalf("worker Glob result = %q, want .git tree skipped", got)
	}
	if got := toolResultContent(t, requests[4]); contains(got, ".git") || contains(got, "private marker") {
		t.Fatalf("worker Grep result = %q, want .git tree skipped", got)
	}
}

// R-JHFG-CO4U
func TestRunPassJoinsLastAssistantTextBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"first block\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"output_index\":1,\"delta\":\"second block\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{}}}\n\n")
	}))
	t.Cleanup(server.Close)
	session := createPassStore(t, t.TempDir())
	factory := openPassFactoryForOffering(t, server.URL, agentkit.OfferingOpenAIResponses)
	result, err := agent.RunPass(context.Background(), agent.Config{
		Store: session, Supervisor: factory, Worker: factory, Root: t.TempDir(), Now: passTime, Trace: &passTrace{},
	}, "join blocks")
	if err != nil {
		t.Fatalf("RunPass: %v", err)
	}
	if result.Report != "first block\nsecond block" {
		t.Fatalf("report = %q, want text blocks joined by newline", result.Report)
	}
	entries := allPassEntries(t, session)
	assertEntry(t, entries, "1", store.KindReport, "first block\nsecond block")
	assertTranscriptIDsAndSummaries(t, entries, []string{"1"})
}

// R-JG7J-YWE5
// R-JJV9-47M8
func TestRunPassTracesEventsAndTerminalProviderError(t *testing.T) {
	session := createPassStore(t, t.TempDir())
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestNumber := requestCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			writeToolCall(w, "search-call", "search", `{}`)
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"received before error\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: {not-json}\n\n")
	}))
	t.Cleanup(server.Close)
	trace := &passTrace{}
	factory := openPassFactory(t, server.URL)
	result, err := agent.RunPass(context.Background(), agent.Config{
		Store: session, Supervisor: factory, Worker: factory, Root: t.TempDir(), Now: passTime, Trace: trace,
	}, "fail after one event")
	if err == nil {
		t.Fatal("RunPass error = nil, want terminal stream error")
	}
	if result.Address != "1" || result.Report != "" {
		t.Fatalf("failed result = %#v, want address 1 and no report", result)
	}
	if len(trace.events) != 4 {
		t.Fatalf("traced events = %d, want message, call, return, and final message before termination", len(trace.events))
	}
	for index, event := range trace.events {
		if event.address != "1" {
			t.Errorf("event %d address = %q, want 1", index, event.address)
		}
	}
	wantTypes := []any{agentkit.MessageDone{}, agentkit.ToolCall{}, agentkit.ToolReturn{}, agentkit.MessageDone{}}
	for index, want := range wantTypes {
		if reflect.TypeOf(trace.events[index].event) != reflect.TypeOf(want) {
			t.Errorf("event %d type = %T, want %T", index, trace.events[index].event, want)
		}
	}
	message, ok := trace.events[3].event.(agentkit.MessageDone)
	if !ok || messageTextForTest(message.Message) != "received before error" {
		t.Fatalf("last traced event = %#v, want completed pre-error assistant message", trace.events[3].event)
	}
	if len(trace.errors) != 1 || trace.errors[0].address != "1" || !errors.Is(err, trace.errors[0].err) {
		t.Fatalf("traced errors = %#v, want exactly the returned error at address 1", trace.errors)
	}
	entries := allPassEntries(t, session)
	assertNoEntry(t, entries, "1", store.KindReport)
	assertTranscriptIDsAndSummaries(t, entries, []string{"1"})
}

func createPassStore(t *testing.T, root string) *store.Store {
	t.Helper()
	session, err := store.Create(filepath.Join(t.TempDir(), "session.sqlite"), "pass-test", root, passTime)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return session
}

func openPassFactory(t *testing.T, baseURL string) *model.Factory {
	t.Helper()
	return openPassFactoryForOffering(t, baseURL, agentkit.OfferingOpenAIChat)
}

func openPassFactoryForOffering(t *testing.T, baseURL string, offeringID agentkit.OfferingID) *model.Factory {
	t.Helper()
	var cfg model.Config
	found := false
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if offering.ID == offeringID {
				cfg = model.Config{
					Provider: string(offering.Host), Model: entry.Model, Wire: string(offering.WireName),
					Auth: string(agentkit.AuthModeAPIKey), BaseURL: baseURL, MaxContext: -1,
					Home: t.TempDir(), Getenv: func(string) string { return "pass-test-key" },
				}
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("catalog has no %s offering", offeringID)
	}
	factory, err := model.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return factory
}

func passTime() time.Time { return time.Date(2036, 2, 3, 4, 5, 6, 0, time.UTC) }

func writeToolCall(w io.Writer, id, name, input string) {
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", id, name, input)
}

func writeText(w io.Writer, fragments ...string) {
	for index, fragment := range fragments {
		finish := "null"
		if index == len(fragments)-1 {
			finish = `"stop"`
		}
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":%s}]}\n\n", fragment, finish)
	}
}

func writeEmptyMessage(w io.Writer) {
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"finish_reason\":\"stop\"}]}\n\n")
}

func allPassEntries(t *testing.T, session *store.Store) []store.Entry {
	t.Helper()
	var entries []store.Entry
	for id := int64(1); ; id++ {
		entry, err := session.Fetch(id)
		if errors.Is(err, store.ErrNotFound) {
			return entries
		}
		if err != nil {
			t.Fatalf("Fetch(%d): %v", id, err)
		}
		entries = append(entries, entry)
	}
}

func assertStoredPrompt(t *testing.T, session *store.Store, address, text string) {
	t.Helper()
	assertEntry(t, allPassEntries(t, session), address, store.KindPrompt, text)
}

func assertEntry(t *testing.T, entries []store.Entry, address string, kind store.Kind, text string) {
	t.Helper()
	for _, entry := range entries {
		if entry.Address == address && entry.Kind == kind && entry.Text == text {
			return
		}
	}
	t.Errorf("missing entry address=%q kind=%q text=%q", address, kind, text)
}

func assertNoEntry(t *testing.T, entries []store.Entry, address string, kind store.Kind) {
	t.Helper()
	for _, entry := range entries {
		if entry.Address == address && entry.Kind == kind {
			t.Errorf("unexpected entry address=%q kind=%q: %#v", address, kind, entry)
		}
	}
}

func assertTranscriptIDsAndSummaries(t *testing.T, entries []store.Entry, addresses []string) {
	t.Helper()
	summaries := make(map[string]int)
	for _, entry := range entries {
		if entry.Kind != store.KindTranscript {
			continue
		}
		var record agentkit.LogRecord
		if err := json.Unmarshal(entry.Raw, &record); err != nil {
			t.Fatalf("decode transcript %d: %v", entry.ID, err)
		}
		if record.ID != entry.Address {
			t.Errorf("transcript %d record id = %q, want entry address %q", entry.ID, record.ID, entry.Address)
		}
		if record.Type == agentkit.RecordSummary {
			summaries[entry.Address]++
		}
	}
	for _, address := range addresses {
		if summaries[address] != 1 {
			t.Errorf("summary transcripts at %q = %d, want 1", address, summaries[address])
		}
	}
}

func assertTraceRenderedTranscripts(t *testing.T, entries []store.Entry, trace *passTrace) {
	t.Helper()
	trace.mu.Lock()
	records := append([]agentkit.LogRecord(nil), trace.records...)
	trace.mu.Unlock()
	transcriptCount := 0
	for _, entry := range entries {
		if entry.Kind != store.KindTranscript {
			continue
		}
		transcriptCount++
		var record agentkit.LogRecord
		if err := json.Unmarshal(entry.Raw, &record); err != nil {
			t.Fatalf("decode transcript %d: %v", entry.ID, err)
		}
		if entry.Text != renderedRecord(record) {
			t.Errorf("transcript %d text = %q, want Trace.Record rendering %q", entry.ID, entry.Text, renderedRecord(record))
		}
	}
	if len(records) != transcriptCount {
		t.Errorf("Trace.Record calls = %d, want one for each of %d transcripts", len(records), transcriptCount)
	}
}

func renderedRecord(record agentkit.LogRecord) string {
	return fmt.Sprintf("rendered %s %s %d", record.ID, record.Type, record.Seq)
}

func assertMessages(t *testing.T, request map[string]any, system, user string) {
	t.Helper()
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("messages = %#v, want system followed by user", request["messages"])
	}
	want := []struct{ role, content string }{{"system", system}, {"user", user}}
	for index, expected := range want {
		message, ok := messages[index].(map[string]any)
		if !ok || message["role"] != expected.role || message["content"] != expected.content {
			t.Errorf("message %d = %#v, want role=%q content=%q", index, messages[index], expected.role, expected.content)
		}
	}
}

func assertToolNames(t *testing.T, request map[string]any, want []string) {
	t.Helper()
	raw, ok := request["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %#v", request["tools"])
	}
	got := make([]string, 0, len(raw))
	for _, item := range raw {
		tool := item.(map[string]any)
		function := tool["function"].(map[string]any)
		got = append(got, fmt.Sprint(function["name"]))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tool names = %q, want exactly %q", got, want)
	}
}

func toolResultContent(t *testing.T, request map[string]any) string {
	t.Helper()
	messages, ok := request["messages"].([]any)
	if !ok {
		t.Fatalf("messages = %#v", request["messages"])
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if ok && message["role"] == "tool" {
			return fmt.Sprint(message["content"])
		}
	}
	return ""
}

func messageTextForTest(message agentkit.Message) string {
	var result string
	for _, block := range message.Blocks {
		if text, ok := block.(agentkit.Text); ok {
			result += text.Text
		}
	}
	return result
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
