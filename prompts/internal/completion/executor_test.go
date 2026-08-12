package completion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appkitdb "appkit/db"
	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"

	"prompts/internal/admit"
	"prompts/internal/calls"
	promptsdb "prompts/internal/db"
	"prompts/internal/prompt"
)

const completionTestModel = "claude-sonnet-4-6"

// R-JBE5-L2M1
func TestExecutorCompletesQueuedItemAndRecordsAccounting(t *testing.T) {
	fixture := newExecutorFixture(t, time.Hour)
	usage := agentkit.Usage{InputUncached: 11, CacheReadInput: 2, Output: 5, Total: 18}
	fixture.provider.results = []*agentkit.RoundTrip{testRoundTrip(`prefix {"status":"ok","result":{"facts":[1,"two"]}} suffix`, usage, nil)}
	item := fixture.enqueue(t)
	fixture.execute(t)

	got := fixture.get(t, item.ID)
	if got.Status != StatusDone || got.Result != `{"facts":[1,"two"]}` || got.Error != "" {
		t.Fatalf("completed item = %#v", got)
	}
	var aggregate agentkit.Usage
	if err := json.Unmarshal([]byte(got.UsageJSON), &aggregate); err != nil || aggregate != usage {
		t.Fatalf("usage = %#v, %v; want %#v", aggregate, err, usage)
	}
	resolution := catalog.Resolve(agentkit.ProviderAnthropic, completionTestModel)
	wantCost := resolution.Offering.Pricing.Cost(usage).USD()
	if math.Abs(got.CostUSD-wantCost) > 1e-12 {
		t.Fatalf("cost = %.12f, want %.12f", got.CostUSD, wantCost)
	}
	rows := fixture.callRows(t)
	if len(rows) != 1 {
		t.Fatalf("calls = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Class != calls.ClassCompletion || row.Origin != item.Origin || row.Name != item.Name ||
		row.GroupID != item.GroupID || row.Attempt != item.Attempt {
		t.Fatalf("call attribution = %#v", row)
	}
}

// R-JCM1-YUCQ
func TestExecutorCorrectsInvalidEnvelopeWithReplayedConversation(t *testing.T) {
	fixture := newExecutorFixture(t, time.Hour)
	fixture.provider.results = []*agentkit.RoundTrip{
		testRoundTrip("not json", agentkit.Usage{Total: 2}, nil),
		testRoundTrip(`{"status":"ok","result":["fixed"]}`, agentkit.Usage{Total: 3}, nil),
	}
	item := fixture.enqueue(t)
	fixture.execute(t)
	got := fixture.get(t, item.ID)
	if got.Status != StatusDone || got.Result != `["fixed"]` {
		t.Fatalf("item = %#v", got)
	}
	requests := fixture.provider.Requests()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	second := requests[1].Messages
	if len(second) < 3 || messageText(second[len(second)-2]) != "not json" ||
		second[len(second)-2].Role != agentkit.RoleAssistant ||
		messageText(second[len(second)-1]) != correctiveInstruction || second[len(second)-1].Role != agentkit.RoleUser {
		t.Fatalf("corrective replay = %#v", second)
	}
	if rows := fixture.callRows(t); len(rows) != 2 {
		t.Fatalf("calls = %d, want one per provider round trip", len(rows))
	}
}

// R-JDTY-CM3F
func TestExecutorFailsAfterThreeCorrectiveRoundTrips(t *testing.T) {
	fixture := newExecutorFixture(t, time.Hour)
	for range 4 {
		fixture.provider.results = append(fixture.provider.results, testRoundTrip("still invalid", agentkit.Usage{Total: 1}, nil))
	}
	item := fixture.enqueue(t)
	fixture.execute(t)
	got := fixture.get(t, item.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "envelope") || !strings.Contains(got.Error, "3 corrective") {
		t.Fatalf("item = %#v", got)
	}
	if len(fixture.provider.Requests()) != 4 || len(fixture.callRows(t)) != 4 {
		t.Fatalf("provider calls/call rows = %d/%d, want 4/4", len(fixture.provider.Requests()), len(fixture.callRows(t)))
	}
}

// R-JF1U-QDU4
func TestExecutorStoresEnvelopeErrorWithoutResult(t *testing.T) {
	fixture := newExecutorFixture(t, time.Hour)
	fixture.provider.results = []*agentkit.RoundTrip{testRoundTrip(`{"status":"error","message":"source document is ambiguous"}`, agentkit.Usage{Total: 1}, nil)}
	item := fixture.enqueue(t)
	fixture.execute(t)
	got := fixture.get(t, item.ID)
	if got.Status != StatusFailed || got.Error != "source document is ambiguous" || got.Result != "" {
		t.Fatalf("item = %#v", got)
	}
}

// R-JG9R-45KT
func TestExecutorStoresProviderFailureOnItemAndCall(t *testing.T) {
	fixture := newExecutorFixture(t, time.Hour)
	fixture.provider.results = []*agentkit.RoundTrip{testRoundTrip("", agentkit.Usage{Total: 1}, errors.New("provider exploded"))}
	item := fixture.enqueue(t)
	fixture.execute(t)
	got := fixture.get(t, item.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "provider exploded") {
		t.Fatalf("item = %#v", got)
	}
	rows := fixture.callRows(t)
	if len(rows) != 1 || !strings.Contains(rows[0].Error, "provider exploded") || rows[0].Class != calls.ClassCompletion ||
		rows[0].Origin != item.Origin || rows[0].Name != item.Name {
		t.Fatalf("failure call = %#v", rows)
	}
}

// R-JHHN-HXBI
func TestExecutorRuntimeBoundFailsSlowItem(t *testing.T) {
	fixture := newExecutorFixture(t, 5*time.Millisecond)
	fixture.provider.slow = true
	item := fixture.enqueue(t)
	fixture.execute(t)
	got := fixture.get(t, item.ID)
	if got.Status != StatusFailed || !strings.Contains(got.Error, "runtime bound 5ms exceeded") {
		t.Fatalf("item = %#v", got)
	}
	if got.Status == StatusRunning || got.FinishedAt.IsZero() {
		t.Fatalf("timed-out item was left running: %#v", got)
	}
}

type executorFixture struct {
	t        *testing.T
	queue    *Store
	calls    *calls.Store
	executor *Executor
	provider *scriptedProvider
}

func newExecutorFixture(t *testing.T, bound time.Duration) *executorFixture {
	t.Helper()
	database := openCompletionTestDB(t)
	now := func() time.Time { return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC) }
	queue := NewStore(database, "owner-a", now)
	callStore := calls.NewStore(database)
	provider := &scriptedProvider{}
	build := func(prompt.Config, func(string) string) (agentkit.Provider, error) { return provider, nil }
	executor := NewExecutor(queue, callStore, admit.New(4, 1), build, func(string) string { return "test-key" }, func() bool { return false }, bound)
	return &executorFixture{t: t, queue: queue, calls: callStore, executor: executor, provider: provider}
}

func (f *executorFixture) enqueue(t *testing.T) Item {
	t.Helper()
	request := Request{Model: completionTestModel, System: "extract facts", Messages: []Message{{Role: "user", Text: "extract this"}}}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	item, created, err := f.queue.Ensure(t.Context(), Item{Consumer: "service:wiki", Origin: "trigger:dropbox",
		Key: "job-1", Name: "wiki.extract", GroupID: "batch-9", CorrelationID: "corr-7", Attempt: 3, Request: string(encoded)})
	if err != nil || !created {
		t.Fatalf("Ensure = %#v, %v, created=%v", item, err, created)
	}
	return item
}

func (f *executorFixture) execute(t *testing.T) {
	t.Helper()
	didWork, err := f.executor.ExecuteNext(t.Context())
	if err != nil || !didWork {
		t.Fatalf("ExecuteNext = %v, %v", didWork, err)
	}
}

func (f *executorFixture) get(t *testing.T, id string) Item {
	t.Helper()
	item, err := f.queue.Get(t.Context(), id, "service:wiki")
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *executorFixture) callRows(t *testing.T) []calls.Row {
	t.Helper()
	rows, err := f.calls.List(t.Context(), calls.Filter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

type scriptedProvider struct {
	mu       sync.Mutex
	requests []*agentkit.Request
	results  []*agentkit.RoundTrip
	slow     bool
}

func (p *scriptedProvider) Identity() agentkit.Identity { return agentkit.Identity{Provider: "fake"} }

func (p *scriptedProvider) RoundTrip(ctx context.Context, request *agentkit.Request) *agentkit.RoundTrip {
	p.mu.Lock()
	copy := *request
	copy.Messages = append([]agentkit.Message(nil), request.Messages...)
	p.requests = append(p.requests, &copy)
	index := len(p.requests) - 1
	slow := p.slow
	var result *agentkit.RoundTrip
	if index < len(p.results) {
		result = p.results[index]
	}
	p.mu.Unlock()
	if slow {
		<-ctx.Done()
		return testRoundTrip("", agentkit.Usage{}, ctx.Err())
	}
	if result == nil {
		return testRoundTrip("", agentkit.Usage{}, errors.New("unexpected provider call"))
	}
	return result
}

func (p *scriptedProvider) Requests() []*agentkit.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*agentkit.Request(nil), p.requests...)
}

func testRoundTrip(text string, usage agentkit.Usage, err error) *agentkit.RoundTrip {
	message := agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: text}}}
	return agentkit.NewRoundTrip(message, agentkit.FinishStop, usage, nil, err, 0, false)
}

func openCompletionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "completion.db"))
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}
	return database
}
