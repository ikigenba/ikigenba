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
	script []passResponse
	server *httptest.Server

	mu       sync.Mutex
	requests []map[string]any
}

type passResponse struct {
	before func()
	write  func(io.Writer)
}

type supervisorToolCall struct {
	name  string
	input string
}

func newPassProvider(t *testing.T, script []passResponse) *passProvider {
	t.Helper()
	provider := &passProvider{t: t, script: script}
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

	w.Header().Set("Content-Type", "text/event-stream")
	if requestNumber > len(p.script) {
		p.t.Errorf("unexpected provider request %d", requestNumber)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	response := p.script[requestNumber-1]
	if response.before != nil {
		response.before()
	}
	response.write(w)
}

func (p *passProvider) capturedRequests() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.requests...)
}

// R-J8W5-O9XZ
func TestSupervisorToolsAdvertiseExactSchemas(t *testing.T) {
	session := createPassStore(t, t.TempDir())
	requests, _, err := runSupervisorToolCalls(t, session, nil)
	if err != nil {
		t.Fatalf("RunPass: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(requests))
	}

	want := map[string]string{
		"search":   `{"type":"object","properties":{"query":{"type":"string"},"kind":{"type":"string"},"address":{"type":"string"},"page":{"type":"integer","minimum":1}},"required":["query"]}`,
		"fetch":    `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`,
		"remember": `{"type":"object","properties":{"text":{"type":"string","minLength":1}},"required":["text"]}`,
		"delegate": `{"type":"object","properties":{"role":{"type":"string","enum":["supervisor","worker"]},"prompt":{"type":"string","minLength":1}},"required":["role","prompt"]}`,
	}
	got := advertisedToolSchemas(t, requests[0])
	if len(got) != len(want) {
		t.Fatalf("advertised schemas = %v, want exactly search, fetch, remember, and delegate", got)
	}
	for name, rawWant := range want {
		var decodedWant map[string]any
		if err := json.Unmarshal([]byte(rawWant), &decodedWant); err != nil {
			t.Fatalf("decode expected %s schema: %v", name, err)
		}
		if !reflect.DeepEqual(got[name], decodedWant) {
			t.Errorf("%s schema = %#v, want exactly %#v", name, got[name], decodedWant)
		}
	}
}

// R-JA42-21OO
func TestSupervisorSearchTranslatesFiltersAndReportsUnknownKinds(t *testing.T) {
	session := createPassStore(t, t.TempDir())
	for index := range store.PageSize + 1 {
		address := "3"
		if index%2 != 0 {
			address = "3.1"
		}
		addPassEntry(t, session, address, store.KindNote, fmt.Sprintf("needle note %02d", index), nil)
	}
	addPassEntry(t, session, "3.2", store.KindReport, "needle excluded report", nil)
	addPassEntry(t, session, "3.2", store.KindTranscript, "needle excluded transcript", json.RawMessage(`{"record":true}`))
	addPassEntry(t, session, "31", store.KindNote, "needle wrong address", nil)
	addPassEntry(t, session, "3", store.KindNote, "different query", nil)

	want := store.Page{
		Hits:  []store.Hit{{ID: 1, Address: "3", Kind: store.KindNote, Preview: "needle note 00"}},
		Page:  2,
		Pages: 2,
		Total: store.PageSize + 1,
	}
	requests, trace, err := runSupervisorToolCalls(t, session, []supervisorToolCall{
		{name: "search", input: `{"query":"needle","kind":"note,report,-report","address":"3","page":2}`},
		{name: "search", input: `{"query":"needle","kind":"note,unknown-kind"}`},
	})
	if err != nil {
		t.Fatalf("RunPass: %v", err)
	}
	var got store.Page
	if err := json.Unmarshal([]byte(toolResultForCall(t, requests[1], "tool-call-1")), &got); err != nil {
		t.Fatalf("decode search result: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("search result = %#v, want Store.Search result %#v", got, want)
	}
	unknown := toolResultForCall(t, requests[2], "tool-call-2")
	if !contains(unknown, "unknown-kind") {
		t.Errorf("unknown-kind result = %q, want offending kind named", unknown)
	}
	assertToolReturnError(t, trace, "tool-call-2")
}

// R-JBBY-FTFD
func TestSupervisorFetchReturnsFullEntryAndNamesMissingID(t *testing.T) {
	session := createPassStore(t, t.TempDir())
	id := addPassEntry(t, session, "7.2", store.KindTranscript, "full fetched entry", json.RawMessage(`{"detail":"kept"}`))
	want := store.Entry{
		ID: id, Address: "7.2", Kind: store.KindTranscript, Text: "full fetched entry",
		Raw: json.RawMessage(`{"detail":"kept"}`), Created: passTime(),
	}
	missingID := int64(987654)
	requests, trace, err := runSupervisorToolCalls(t, session, []supervisorToolCall{
		{name: "fetch", input: fmt.Sprintf(`{"id":%d}`, id)},
		{name: "fetch", input: fmt.Sprintf(`{"id":%d}`, missingID)},
	})
	if err != nil {
		t.Fatalf("RunPass: %v", err)
	}
	var got store.Entry
	if err := json.Unmarshal([]byte(toolResultForCall(t, requests[1], "tool-call-1")), &got); err != nil {
		t.Fatalf("decode fetch result: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fetch result = %#v, want full entry %#v", got, want)
	}
	missing := toolResultForCall(t, requests[2], "tool-call-2")
	if !contains(missing, fmt.Sprint(missingID)) {
		t.Errorf("missing fetch result = %q, want id %d", missing, missingID)
	}
	assertToolReturnError(t, trace, "tool-call-2")
}

// R-JCJU-TL62
func TestSupervisorRememberStoresNoteAtCallingAddress(t *testing.T) {
	session := createPassStore(t, t.TempDir())
	requests, _, err := runSupervisorToolCalls(t, session, []supervisorToolCall{
		{name: "remember", input: `{"text":"retain this exact note"}`},
	})
	if err != nil {
		t.Fatalf("RunPass: %v", err)
	}
	if got := toolResultForCall(t, requests[1], "tool-call-1"); got != "ok" {
		t.Fatalf("remember result = %q, want exactly ok", got)
	}
	assertEntry(t, allPassEntries(t, session), "1", store.KindNote, "retain this exact note")
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
	provider := newPassProvider(t, []passResponse{
		{
			before: func() { assertStoredPrompt(t, session, "1", rootPrompt) },
			write: func(w io.Writer) {
				writeToolCall(w, "delegate-call", "delegate", `{"role":"worker","prompt":"inspect fixture"}`)
			},
		},
		{
			before: func() { assertStoredPrompt(t, session, "1.1", childPrompt) },
			write:  func(w io.Writer) { writeToolCall(w, "read-call", "Read", `{"file_path":"fixture.txt"}`) },
		},
		{write: func(w io.Writer) { writeToolCall(w, "glob-call", "Glob", `{"pattern":"**/*"}`) }},
		{write: func(w io.Writer) { writeToolCall(w, "grep-call", "Grep", `{"pattern":"private marker"}`) }},
		{write: func(w io.Writer) { writeText(w, "child ", "report") }},
		{write: func(w io.Writer) { writeText(w, "root ", "report") }},
		{write: writeEmptyMessage},
	})
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
	provider := newPassProvider(t, []passResponse{
		{write: func(w io.Writer) { writeToolCall(w, "search-call", "search", `{}`) }},
		{write: func(w io.Writer) {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"received before error\"},\"finish_reason\":\"stop\"}]}\n\n")
			_, _ = io.WriteString(w, "data: {not-json}\n\n")
		}},
	})
	trace := &passTrace{}
	factory := openPassFactory(t, provider.server.URL)
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

// R-JDRR-7CWR
func TestDelegateAssignsCallOrderAddressesAndBlocksForReports(t *testing.T) {
	session := createPassStore(t, t.TempDir())
	provider := newPassProvider(t, []passResponse{
		{write: func(w io.Writer) {
			writeToolCall(w, "first-delegate", "delegate", `{"role":"worker","prompt":"first child"}`)
		}},
		{write: func(w io.Writer) { writeText(w, "first report") }},
		{
			before: func() { assertStoredReport(t, session, "1.1", "first report") },
			write: func(w io.Writer) {
				writeToolCall(w, "second-delegate", "delegate", `{"role":"worker","prompt":"second child"}`)
			},
		},
		{write: func(w io.Writer) { writeText(w, "second report") }},
		{
			before: func() { assertStoredReport(t, session, "1.2", "second report") },
			write:  func(w io.Writer) { writeText(w, "parent report") },
		},
	})
	factory := openPassFactory(t, provider.server.URL)
	result, err := agent.RunPass(context.Background(), agent.Config{
		Store: session, Supervisor: factory, Worker: factory, Root: t.TempDir(), Now: passTime, Trace: &passTrace{},
	}, "delegate twice")
	if err != nil {
		t.Fatalf("RunPass: %v", err)
	}
	if result.Report != "parent report" {
		t.Fatalf("parent report = %q, want parent report", result.Report)
	}
	requests := provider.capturedRequests()
	if len(requests) != 5 {
		t.Fatalf("provider requests = %d, want parent/child/parent/child/parent", len(requests))
	}
	if got := toolResultForCall(t, requests[2], "first-delegate"); got != "first report" {
		t.Errorf("first delegate content = %q, want unchanged child report", got)
	}
	if got := toolResultForCall(t, requests[4], "second-delegate"); got != "second report" {
		t.Errorf("second delegate content = %q, want unchanged child report", got)
	}
	entries := allPassEntries(t, session)
	assertEntry(t, entries, "1.1", store.KindPrompt, "first child")
	assertEntry(t, entries, "1.1", store.KindReport, "first report")
	assertEntry(t, entries, "1.2", store.KindPrompt, "second child")
	assertEntry(t, entries, "1.2", store.KindReport, "second report")
}

// R-JEZN-L4NG
func TestDelegateReturnsChildErrorsInBandAndParentContinues(t *testing.T) {
	t.Run("provider error", func(t *testing.T) {
		assertChildFailureContinues(t, -1, func(w io.Writer) {
			_, _ = io.WriteString(w, "data: {not-json}\n\n")
		}, "invalid character", 3)
	})
	t.Run("context limit", func(t *testing.T) {
		assertChildFailureContinues(t, 1, func(w io.Writer) {
			writeToolCallWithUsage(w, "child-read", "Read", `{"file_path":"missing"}`, 9, 2, 4, 1)
		}, "agentkit: limit exceeded", 3)
	})
}

func assertChildFailureContinues(
	t *testing.T,
	childMaxContext int64,
	childResponse func(io.Writer),
	wantError string,
	wantRequests int,
) {
	t.Helper()
	session := createPassStore(t, t.TempDir())
	provider := newPassProvider(t, []passResponse{
		{write: func(w io.Writer) {
			writeToolCall(w, "delegate-error", "delegate", `{"role":"worker","prompt":"fail child"}`)
		}},
		{write: childResponse},
		{write: func(w io.Writer) { writeText(w, "parent recovered") }},
	})
	supervisor := openPassFactory(t, provider.server.URL)
	worker := openPassFactoryWithMaxContext(t, provider.server.URL, childMaxContext)
	trace := &passTrace{}
	result, err := agent.RunPass(context.Background(), agent.Config{
		Store: session, Supervisor: supervisor, Worker: worker, Root: t.TempDir(), Now: passTime, Trace: trace,
	}, "delegate failing child")
	if err != nil {
		t.Fatalf("RunPass parent error: %v", err)
	}
	if result.Report != "parent recovered" {
		t.Errorf("parent report = %q, want parent recovered", result.Report)
	}
	requests := provider.capturedRequests()
	if len(requests) != wantRequests {
		t.Fatalf("provider requests = %d, want %d proving parent continued", len(requests), wantRequests)
	}
	toolError := toolResultForCall(t, requests[len(requests)-1], "delegate-error")
	if !contains(toolError, "child 1.1 terminated") || !contains(toolError, wantError) {
		t.Errorf("delegate error = %q, want child address and %q", toolError, wantError)
	}
	assertToolReturnError(t, trace, "delegate-error")
	entries := allPassEntries(t, session)
	assertEntry(t, entries, "1.1", store.KindPrompt, "fail child")
	assertNoEntry(t, entries, "1.1", store.KindReport)
	assertEntry(t, entries, "1", store.KindReport, "parent recovered")
}

// R-JINC-QFVJ
func TestRunPassSumsAllAgentSummariesIncludingErrors(t *testing.T) {
	t.Run("errored descendant", func(t *testing.T) {
		session := createPassStore(t, t.TempDir())
		provider := newPassProvider(t, []passResponse{
			{write: func(w io.Writer) {
				writeToolCallWithUsage(w, "delegate-accounting", "delegate", `{"role":"worker","prompt":"count failed child"}`, 11, 2, 5, 1)
			}},
			{write: func(w io.Writer) {
				writeTextWithUsage(w, "partial child", 17, 3, 7, 2)
				_, _ = io.WriteString(w, "data: {not-json}\n\n")
			}},
			{write: func(w io.Writer) { writeTextWithUsage(w, "accounted parent", 23, 4, 9, 3) }},
		})
		factory := openPassFactory(t, provider.server.URL)
		result, err := agent.RunPass(context.Background(), agent.Config{
			Store: session, Supervisor: factory, Worker: factory, Root: t.TempDir(), Now: passTime, Trace: &passTrace{},
		}, "account tree")
		if err != nil {
			t.Fatalf("RunPass: %v", err)
		}
		storedUsage, storedCost, summaryCount := summedStoredSummaries(t, allPassEntries(t, session))
		if summaryCount != 2 {
			t.Fatalf("stored summaries = %d, want root and errored child", summaryCount)
		}
		wantUsage := agentkit.Usage{
			InputTokens: 42, CachedTokens: 9, OutputTokens: 15, ReasoningTokens: 6,
		}
		const wantCost = agentkit.Cost(422250)
		if result.Usage != wantUsage || result.Cost != wantCost {
			t.Errorf("Result accounting = %#v/%d, want fixture total %#v/%d", result.Usage, result.Cost, wantUsage, wantCost)
		}
		if storedUsage != wantUsage || storedCost != wantCost {
			t.Errorf("stored summary sum = %#v/%d, want fixture total %#v/%d", storedUsage, storedCost, wantUsage, wantCost)
		}
		assertEveryUsageFieldCompared(t, result.Usage, wantUsage)
		assertChatUsageAndCostNonzero(t, wantUsage, wantCost)
	})

	t.Run("errored root", func(t *testing.T) {
		session := createPassStore(t, t.TempDir())
		provider := newPassProvider(t, []passResponse{{write: func(w io.Writer) {
			writeTextWithUsage(w, "partial root", 29, 5, 11, 4)
			_, _ = io.WriteString(w, "data: {not-json}\n\n")
		}}})
		factory := openPassFactory(t, provider.server.URL)
		result, err := agent.RunPass(context.Background(), agent.Config{
			Store: session, Supervisor: factory, Worker: factory, Root: t.TempDir(), Now: passTime, Trace: &passTrace{},
		}, "root terminal error")
		if err == nil {
			t.Fatal("RunPass error = nil, want root terminal error")
		}
		storedUsage, storedCost, summaryCount := summedStoredSummaries(t, allPassEntries(t, session))
		wantUsage := agentkit.Usage{
			InputTokens: 24, CachedTokens: 5, OutputTokens: 7, ReasoningTokens: 4,
		}
		const wantCost = agentkit.Cost(226250)
		if result.Address != "1" || summaryCount != 1 || result.Usage != wantUsage || result.Cost != wantCost {
			t.Errorf("errored root result = %#v with %d summaries, want address 1 and fixture accounting %#v/%d", result, summaryCount, wantUsage, wantCost)
		}
		if storedUsage != wantUsage || storedCost != wantCost {
			t.Errorf("stored root summary = %#v/%d, want fixture total %#v/%d", storedUsage, storedCost, wantUsage, wantCost)
		}
		assertEveryUsageFieldCompared(t, result.Usage, wantUsage)
		assertChatUsageAndCostNonzero(t, wantUsage, wantCost)
	})
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
	return openPassFactoryWithMaxContext(t, baseURL, -1)
}

func openPassFactoryWithMaxContext(t *testing.T, baseURL string, maxContext int64) *model.Factory {
	t.Helper()
	return openPassFactoryForOfferingWithMaxContext(t, baseURL, agentkit.OfferingOpenAIChat, maxContext)
}

func openPassFactoryForOffering(t *testing.T, baseURL string, offeringID agentkit.OfferingID) *model.Factory {
	t.Helper()
	return openPassFactoryForOfferingWithMaxContext(t, baseURL, offeringID, -1)
}

func openPassFactoryForOfferingWithMaxContext(
	t *testing.T,
	baseURL string,
	offeringID agentkit.OfferingID,
	maxContext int64,
) *model.Factory {
	t.Helper()
	var cfg model.Config
	found := false
	for _, entry := range agentkit.Catalog() {
		for _, offering := range entry.Offerings {
			if offering.ID == offeringID {
				cfg = model.Config{
					Provider: string(offering.Host), Model: entry.Model, Wire: string(offering.WireName),
					Auth: string(agentkit.AuthModeAPIKey), BaseURL: baseURL, MaxContext: maxContext,
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

func runSupervisorToolCalls(
	t *testing.T,
	session *store.Store,
	calls []supervisorToolCall,
) ([]map[string]any, *passTrace, error) {
	t.Helper()
	var mu sync.Mutex
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decode provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, decoded)
		requestIndex := len(requests) - 1
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if requestIndex < len(calls) {
			call := calls[requestIndex]
			writeToolCall(w, fmt.Sprintf("tool-call-%d", requestIndex+1), call.name, call.input)
			return
		}
		writeText(w, "tool scenario complete")
	}))
	t.Cleanup(server.Close)
	trace := &passTrace{}
	factory := openPassFactory(t, server.URL)
	_, err := agent.RunPass(context.Background(), agent.Config{
		Store: session, Supervisor: factory, Worker: factory, Root: t.TempDir(), Now: passTime, Trace: trace,
	}, "exercise supervisor tools")
	mu.Lock()
	defer mu.Unlock()
	return append([]map[string]any(nil), requests...), trace, err
}

func advertisedToolSchemas(t *testing.T, request map[string]any) map[string]map[string]any {
	t.Helper()
	rawTools, ok := request["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %#v", request["tools"])
	}
	result := make(map[string]map[string]any, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			t.Fatalf("tool = %#v", rawTool)
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatalf("tool function = %#v", tool["function"])
		}
		name := fmt.Sprint(function["name"])
		schema, ok := function["parameters"].(map[string]any)
		if !ok {
			t.Fatalf("%s parameters = %#v", name, function["parameters"])
		}
		result[name] = schema
	}
	return result
}

func toolResultForCall(t *testing.T, request map[string]any, callID string) string {
	t.Helper()
	messages, ok := request["messages"].([]any)
	if !ok {
		t.Fatalf("messages = %#v", request["messages"])
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if ok && message["role"] == "tool" && message["tool_call_id"] == callID {
			return fmt.Sprint(message["content"])
		}
	}
	t.Fatalf("no tool result for call %q in %#v", callID, messages)
	return ""
}

func assertToolReturnError(t *testing.T, trace *passTrace, callID string) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	for _, event := range trace.events {
		returned, ok := event.event.(agentkit.ToolReturn)
		if ok && returned.Result.ToolUseID == callID {
			if !returned.Result.IsError {
				t.Errorf("tool return %q IsError = false, want true", callID)
			}
			return
		}
	}
	t.Errorf("missing traced tool return for call %q", callID)
}

func addPassEntry(
	t *testing.T,
	session *store.Store,
	address string,
	kind store.Kind,
	text string,
	raw json.RawMessage,
) int64 {
	t.Helper()
	id, err := session.Add(address, kind, text, raw)
	if err != nil {
		t.Fatalf("Add(%q, %q): %v", address, kind, err)
	}
	return id
}

func writeToolCall(w io.Writer, id, name, input string) {
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", id, name, input)
}

func writeToolCallWithUsage(w io.Writer, id, name, input string, prompt, cached, completion, reasoning int64) {
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":%d,\"prompt_tokens_details\":{\"cached_tokens\":%d},\"completion_tokens\":%d,\"completion_tokens_details\":{\"reasoning_tokens\":%d}}}\n\n", id, name, input, prompt, cached, completion, reasoning)
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

func writeTextWithUsage(w io.Writer, text string, prompt, cached, completion, reasoning int64) {
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":%d,\"prompt_tokens_details\":{\"cached_tokens\":%d},\"completion_tokens\":%d,\"completion_tokens_details\":{\"reasoning_tokens\":%d}}}\n\n", text, prompt, cached, completion, reasoning)
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

func assertStoredReport(t *testing.T, session *store.Store, address, text string) {
	t.Helper()
	assertEntry(t, allPassEntries(t, session), address, store.KindReport, text)
}

func summedStoredSummaries(t *testing.T, entries []store.Entry) (agentkit.Usage, agentkit.Cost, int) {
	t.Helper()
	var usage agentkit.Usage
	var cost agentkit.Cost
	count := 0
	for _, entry := range entries {
		if entry.Kind != store.KindTranscript {
			continue
		}
		var record agentkit.LogRecord
		if err := json.Unmarshal(entry.Raw, &record); err != nil {
			t.Fatalf("decode transcript %d: %v", entry.ID, err)
		}
		if record.Type != agentkit.RecordSummary {
			continue
		}
		if record.Usage == nil || record.Cost == nil {
			t.Fatalf("summary at %q lacks usage or cost: %#v", entry.Address, record)
		}
		count++
		usage.InputTokens += record.Usage.InputTokens
		usage.CachedTokens += record.Usage.CachedTokens
		usage.CacheWrite5mTokens += record.Usage.CacheWrite5mTokens
		usage.CacheWrite1hTokens += record.Usage.CacheWrite1hTokens
		usage.OutputTokens += record.Usage.OutputTokens
		usage.ReasoningTokens += record.Usage.ReasoningTokens
		cost += *record.Cost
	}
	return usage, cost, count
}

func assertEveryUsageFieldCompared(t *testing.T, got, want agentkit.Usage) {
	t.Helper()
	fields := []struct {
		name      string
		got, want int64
	}{
		{"InputTokens", got.InputTokens, want.InputTokens},
		{"CachedTokens", got.CachedTokens, want.CachedTokens},
		{"CacheWrite5mTokens", got.CacheWrite5mTokens, want.CacheWrite5mTokens},
		{"CacheWrite1hTokens", got.CacheWrite1hTokens, want.CacheWrite1hTokens},
		{"OutputTokens", got.OutputTokens, want.OutputTokens},
		{"ReasoningTokens", got.ReasoningTokens, want.ReasoningTokens},
	}
	for _, field := range fields {
		if field.got != field.want {
			t.Errorf("Result.Usage.%s = %d, want %d", field.name, field.got, field.want)
		}
	}
}

func assertChatUsageAndCostNonzero(t *testing.T, usage agentkit.Usage, cost agentkit.Cost) {
	t.Helper()
	if usage.InputTokens == 0 || usage.CachedTokens == 0 || usage.OutputTokens == 0 || usage.ReasoningTokens == 0 {
		t.Errorf("chat-supported usage buckets must be nonzero, got %#v", usage)
	}
	if usage.CacheWrite5mTokens != 0 || usage.CacheWrite1hTokens != 0 {
		t.Errorf("chat-unsupported cache-write buckets = %d/%d, want zero", usage.CacheWrite5mTokens, usage.CacheWrite1hTokens)
	}
	if cost == 0 {
		t.Error("catalog-priced provider cost = 0, want nonzero test coverage")
	}
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
	if !ok || len(messages) != 2 {
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
