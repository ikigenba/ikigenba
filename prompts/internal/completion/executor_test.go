package completion

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"path/filepath"
	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/prompt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	appkitdb "appkit/db"
	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"

	promptsdb "prompts/internal/db"
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

// R-05IS-4Q3G
func TestExecutorLostLeaseCancelsProviderAndWritesNoTerminalResult(t *testing.T) {
	database := openCompletionTestDB(t)
	now := newMutableTime(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC))
	ownerA := NewStore(database, "owner-a", now.Now)
	ownerB := NewStore(database, "owner-b", now.Now)
	clock := newManualClock()
	provider := &scriptedProvider{slow: true, started: make(chan struct{}), canceled: make(chan struct{})}
	executor := testExecutor(ownerA, calls.NewStore(database), provider, clock, nil)
	item := enqueueCompletion(t, ownerA, "lost-lease", "hold this call")

	done := make(chan struct{})
	go func() {
		_, _ = executor.ExecuteNext(t.Context())
		close(done)
	}()
	<-provider.started
	now.Advance(LeaseTTL + time.Second)
	reclaimed, err := ownerB.Claim(t.Context())
	if err != nil || reclaimed.ID != item.ID || reclaimed.Owner != "owner-b" {
		t.Fatalf("reclaim = %#v, %v", reclaimed, err)
	}
	clock.Tick()
	<-provider.canceled
	<-done

	got, err := ownerB.Get(t.Context(), item.ID, item.Consumer)
	if err != nil || got.Status != StatusRunning || got.Owner != "owner-b" || got.Result != "" || got.Error != "" || !got.FinishedAt.IsZero() {
		t.Fatalf("item after old owner cancellation = %#v, %v", got, err)
	}
}

// R-ZOG6-RXPQ
func TestExecutorRenewalStoreFailureAbandonsNonTerminalItemAndLogsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renewal-failure.db")
	database := openCompletionTestDBAt(t, path, true)
	observer := openCompletionTestDBAt(t, path, false)
	now := newMutableTime(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC))
	ownerA := NewStore(database, "owner-a", now.Now)
	ownerB := NewStore(observer, "owner-b", now.Now)
	clock := newManualClock()
	logger, logs, _ := testLogger()
	provider := &scriptedProvider{slow: true, started: make(chan struct{}), canceled: make(chan struct{})}
	executor := testExecutor(ownerA, calls.NewStore(database), provider, clock, logger)
	item := enqueueCompletion(t, ownerA, "renew-error", "hold this call")

	done := make(chan struct{})
	go func() {
		_, _ = executor.ExecuteNext(t.Context())
		close(done)
	}()
	<-provider.started
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	clock.Tick()
	<-provider.canceled
	<-done

	got, err := ownerB.Get(t.Context(), item.ID, item.Consumer)
	if err != nil || got.Status != StatusRunning || got.Result != "" || got.Error != "" || !got.FinishedAt.IsZero() {
		t.Fatalf("abandoned item = %#v, %v", got, err)
	}
	now.Advance(LeaseTTL + time.Second)
	reclaimed, err := ownerB.Claim(t.Context())
	if err != nil || reclaimed.ID != item.ID || reclaimed.Owner != "owner-b" {
		t.Fatalf("later claim = %#v, %v", reclaimed, err)
	}
	if text := logs.String(); !strings.Contains(text, "lease renewal failed; abandoning item") || !strings.Contains(text, item.ID) {
		t.Fatalf("abandonment log = %q", text)
	}
}

// R-ZQVZ-JH74
func TestExecutorRetriesTerminalStoreFailureLogsItemAndKeepsRunning(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	clock := newManualClock()
	logger, logs, records := testLogger()
	provider := &scriptedProvider{results: []*agentkit.RoundTrip{testRoundTrip(`{"status":"ok","result":"paid"}`, agentkit.Usage{Total: 1}, nil)}, closeDB: database}
	executor := testExecutor(store, calls.NewStore(database), provider, clock, logger)
	item := enqueueCompletion(t, store, "terminal-error", "close before terminal write")
	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- executor.Run(ctx, 1) }()

	waitForLog(t, records, "completion terminal write failed after retries")
	waitForLog(t, records, "completion claim failed")
	select {
	case err := <-runDone:
		t.Fatalf("Run exited before cancellation: %v", err)
	default:
	}
	if text := logs.String(); !strings.Contains(text, item.ID) || !strings.Contains(text, "attempts=3") || !strings.Contains(text, "level=ERROR") {
		t.Fatalf("terminal failure log = %q", text)
	}
	cancel()
	if err := <-runDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context cancellation", err)
	}
}

// R-ZTBS-B0OI
func TestExecutorClaimErrorRecoversWithoutDrainingWorker(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	if _, err := database.ExecContext(t.Context(), "ALTER TABLE completions RENAME TO completions_hidden"); err != nil {
		t.Fatal(err)
	}
	clock := newManualClock()
	logger, _, records := testLogger()
	provider := &scriptedProvider{results: []*agentkit.RoundTrip{testRoundTrip(`{"status":"ok","result":"recovered"}`, agentkit.Usage{Total: 1}, nil)}}
	executor := testExecutor(store, calls.NewStore(database), provider, clock, logger)
	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- executor.Run(ctx, 1) }()
	waitForLog(t, records, "completion claim failed")

	if _, err := database.ExecContext(t.Context(), "ALTER TABLE completions_hidden RENAME TO completions"); err != nil {
		t.Fatal(err)
	}
	item := enqueueCompletion(t, store, "after-claim-error", "finish after recovery")
	clock.AdvanceWaits()
	got := waitForTerminalItem(t, store, item)
	if got.Status != StatusDone || got.Result != `"recovered"` {
		t.Fatalf("recovered item = %#v", got)
	}
	select {
	case err := <-runDone:
		t.Fatalf("Run exited after transient claim error: %v", err)
	default:
	}
	cancel()
	if err := <-runDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context cancellation", err)
	}
}

// R-ZUJO-OSF7
func TestExecutorPanicFailsOnlyItsItemAndPoolKeepsRunning(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, "owner-a", time.Now)
	provider := &scriptedProvider{panicText: "panic reply", defaultResult: testRoundTrip(`{"status":"ok","result":"still-running"}`, agentkit.Usage{Total: 1}, nil)}
	executor := testExecutor(store, calls.NewStore(database), provider, NewSystemClock(), nil)
	panicking := enqueueCompletion(t, store, "panic-item", "panic reply")
	healthy := enqueueCompletion(t, store, "healthy-item", "ordinary reply")
	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- executor.Run(ctx, 2) }()

	panicResult := waitForTerminalItem(t, store, panicking)
	healthyResult := waitForTerminalItem(t, store, healthy)
	if panicResult.Status != StatusFailed || !strings.Contains(panicResult.Error, "panic") || !strings.Contains(panicResult.Error, "malformed provider reply") {
		t.Fatalf("panicking item = %#v", panicResult)
	}
	if healthyResult.Status != StatusDone || healthyResult.Result != `"still-running"` {
		t.Fatalf("healthy item = %#v", healthyResult)
	}
	select {
	case err := <-runDone:
		t.Fatalf("Run exited after item panic: %v", err)
	default:
	}
	cancel()
	if err := <-runDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context cancellation", err)
	}
}

func testExecutor(store *Store, callStore CallStore, provider agentkit.Provider, clock Clock, logger *slog.Logger) *Executor {
	build := func(prompt.Config, func(string) string) (agentkit.Provider, error) { return provider, nil }
	return NewExecutor(store, callStore, admit.New(4, 1), build, func(string) string { return "test-key" }, func() bool { return false },
		time.Hour, LeaseTTL, DefaultRenewalInterval, clock, logger)
}

func enqueueCompletion(t *testing.T, store *Store, key, text string) Item {
	t.Helper()
	request := Request{Model: completionTestModel, Messages: []Message{{Role: "user", Text: text}}}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	item, created, err := store.Ensure(t.Context(), Item{Consumer: "service:wiki", Origin: "trigger:test", Key: key, Name: "test.completion", Request: string(encoded)})
	if err != nil || !created {
		t.Fatalf("Ensure = %#v, %v, created=%v", item, err, created)
	}
	return item
}

func waitForTerminalItem(t *testing.T, store *Store, want Item) Item {
	t.Helper()
	for range 100000 {
		item, err := store.Get(t.Context(), want.ID, want.Consumer)
		if err == nil && (item.Status == StatusDone || item.Status == StatusFailed) {
			return item
		}
		runtime.Gosched()
	}
	t.Fatalf("item %s did not become terminal", want.ID)
	return Item{}
}

type mutableTime struct {
	mu  sync.Mutex
	now time.Time
}

func newMutableTime(now time.Time) *mutableTime { return &mutableTime{now: now} }
func (c *mutableTime) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mutableTime) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type manualClock struct {
	mu      sync.Mutex
	tickers []*manualTicker
	waits   []chan time.Time
}

type manualTicker struct{ ch chan time.Time }

func newManualClock() *manualClock { return &manualClock{} }
func (c *manualClock) NewTicker(time.Duration) Ticker {
	ticker := &manualTicker{ch: make(chan time.Time, 1)}
	c.mu.Lock()
	c.tickers = append(c.tickers, ticker)
	c.mu.Unlock()
	return ticker
}

func (c *manualClock) After(time.Duration) <-chan time.Time {
	wait := make(chan time.Time, 1)
	c.mu.Lock()
	c.waits = append(c.waits, wait)
	c.mu.Unlock()
	return wait
}

func (c *manualClock) Tick() {
	c.mu.Lock()
	tickers := append([]*manualTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, ticker := range tickers {
		ticker.ch <- time.Time{}
	}
}

func (c *manualClock) AdvanceWaits() {
	c.mu.Lock()
	waits := append([]chan time.Time(nil), c.waits...)
	c.waits = nil
	c.mu.Unlock()
	for _, wait := range waits {
		wait <- time.Time{}
	}
}
func (t *manualTicker) C() <-chan time.Time { return t.ch }
func (t *manualTicker) Stop()               {}

type signalHandler struct {
	slog.Handler
	records chan string
}

func (h signalHandler) Handle(ctx context.Context, record slog.Record) error {
	err := h.Handler.Handle(ctx, record)
	h.records <- record.Message
	return err
}

func testLogger() (*slog.Logger, *bytes.Buffer, <-chan string) {
	var logs bytes.Buffer
	records := make(chan string, 100)
	handler := signalHandler{Handler: slog.NewTextHandler(&logs, nil), records: records}
	return slog.New(handler), &logs, records
}

func waitForLog(t *testing.T, records <-chan string, message string) {
	t.Helper()
	for {
		select {
		case got := <-records:
			if got == message {
				return
			}
		case <-t.Context().Done():
			t.Fatalf("waiting for log %q: %v", message, t.Context().Err())
		}
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
	executor := NewExecutor(queue, callStore, admit.New(4, 1), build, func(string) string { return "test-key" }, func() bool { return false },
		bound, LeaseTTL, DefaultRenewalInterval, NewSystemClock(), nil)
	return &executorFixture{t: t, queue: queue, calls: callStore, executor: executor, provider: provider}
}

func (f *executorFixture) enqueue(t *testing.T) Item {
	t.Helper()
	request := Request{Model: completionTestModel, System: "extract facts", Messages: []Message{{Role: "user", Text: "extract this"}}}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	item, created, err := f.queue.Ensure(t.Context(), Item{
		Consumer: "service:wiki", Origin: "trigger:dropbox",
		Key: "job-1", Name: "wiki.extract", GroupID: "batch-9", CorrelationID: "corr-7", Attempt: 3, Request: string(encoded),
	})
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
	mu            sync.Mutex
	requests      []*agentkit.Request
	results       []*agentkit.RoundTrip
	slow          bool
	started       chan struct{}
	canceled      chan struct{}
	closeDB       *sql.DB
	panicText     string
	defaultResult *agentkit.RoundTrip
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
	if p.defaultResult != nil {
		result = p.defaultResult
	}
	started := p.started
	canceled := p.canceled
	database := p.closeDB
	panicText := p.panicText
	p.mu.Unlock()
	if panicText != "" && strings.Contains(messageText(request.Messages[len(request.Messages)-1]), panicText) {
		panic("malformed provider reply")
	}
	if started != nil {
		close(started)
	}
	if slow {
		<-ctx.Done()
		if canceled != nil {
			close(canceled)
		}
		return testRoundTrip("", agentkit.Usage{}, ctx.Err())
	}
	if database != nil {
		_ = database.Close()
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
	return openCompletionTestDBAt(t, filepath.Join(t.TempDir(), "completion.db"), true)
}

func openCompletionTestDBAt(t *testing.T, path string, migrate bool) *sql.DB {
	t.Helper()
	database, err := appkitdb.Open(path)
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if !migrate {
		return database
	}
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("migrate DB: %v", err)
	}
	return database
}
