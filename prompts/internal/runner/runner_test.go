package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appkitdb "appkit/db"
	"eventplane/correlation"

	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/db"
	"prompts/internal/ids"
	"prompts/internal/prompt"
	runprovider "prompts/internal/provider"
	"prompts/internal/sandbox"

	"github.com/ikigenba/agentkit"
)

// fakeProvider implements agentkit.Provider with a pre-canned one-turn
// response. If block is true, RoundTrip waits for the context to finish before
// returning, modelling a hung provider call without any network dependency.
type fakeProvider struct {
	block      bool
	roundTrips []*agentkit.RoundTrip

	mu       sync.Mutex
	requests []*agentkit.Request
	next     int
}

type serialProvider struct {
	mu           sync.Mutex
	next         int
	started      chan int
	releaseFirst chan struct{}
}

func (p *serialProvider) Identity() agentkit.Identity {
	return agentkit.Identity{Provider: "serial"}
}

func (p *serialProvider) RoundTrip(ctx context.Context, _ *agentkit.Request) *agentkit.RoundTrip {
	p.mu.Lock()
	p.next++
	call := p.next
	p.mu.Unlock()
	p.started <- call
	if call == 1 {
		select {
		case <-p.releaseFirst:
		case <-ctx.Done():
			return agentkit.NewRoundTrip(agentkit.Message{}, agentkit.FinishOther, agentkit.Usage{}, nil, ctx.Err(), 0, false)
		}
	}
	return agentkit.NewRoundTrip(
		agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: "done"}}},
		agentkit.FinishStop, agentkit.Usage{InputUncached: 1, Output: 1, Total: 2}, nil, nil, 0, false,
	)
}

func (f *fakeProvider) Identity() agentkit.Identity {
	return agentkit.Identity{Provider: "fake"}
}

func (f *fakeProvider) RoundTrip(ctx context.Context, req *agentkit.Request) *agentkit.RoundTrip {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	if f.block {
		f.mu.Unlock()
		<-ctx.Done()
		return agentkit.NewRoundTrip(agentkit.Message{}, agentkit.FinishOther, agentkit.Usage{}, nil, ctx.Err(), 0, false)
	}
	if len(f.roundTrips) > 0 {
		next := f.next
		f.next++
		f.mu.Unlock()
		if next >= len(f.roundTrips) {
			return agentkit.NewRoundTrip(agentkit.Message{}, agentkit.FinishOther, agentkit.Usage{}, nil, errors.New("fake provider script exhausted"), 0, false)
		}
		return f.roundTrips[next]
	}
	f.mu.Unlock()

	return agentkit.NewRoundTrip(
		agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: "all done"}}},
		agentkit.FinishStop,
		agentkit.Usage{InputUncached: 12, Output: 7, Total: 19},
		nil,
		nil,
		0,
		false,
	)
}

func (f *fakeProvider) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeProvider) request(i int) *agentkit.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.requests) {
		return nil
	}
	return f.requests[i]
}

func (f *fakeProvider) lastRequest() *agentkit.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return nil
	}
	return f.requests[len(f.requests)-1]
}

func scriptedRoundTrip(blocks ...agentkit.Block) *agentkit.RoundTrip {
	return agentkit.NewRoundTrip(
		agentkit.Message{Role: agentkit.RoleAssistant, Blocks: blocks},
		agentkit.FinishToolUse,
		agentkit.Usage{InputUncached: 1, Output: 1, Total: 2},
		nil,
		nil,
		0,
		false,
	)
}

func scriptedTextRoundTrip(text string) *agentkit.RoundTrip {
	return agentkit.NewRoundTrip(
		agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: text}}},
		agentkit.FinishStop,
		agentkit.Usage{InputUncached: 1, Output: 1, Total: 2},
		nil,
		nil,
		0,
		false,
	)
}

// newTestRunner builds a Runner backed by a real temp store + sandbox, with
// the provider factory replaced by one that always returns fp.
func newTestRunner(t *testing.T, ttl time.Duration, fp agentkit.Provider) (*Runner, *prompt.Store) {
	t.Helper()
	ctx := context.Background()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatalf("appkitdb.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("appkitdb.LoadMigrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("appkitdb.Migrate: %v", err)
	}
	store := prompt.NewStore(conn)
	store.Calls = calls.NewStore(conn)

	sb, err := sandbox.New(filepath.Join(t.TempDir(), "sandboxes"))
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	r := New(store, sb, admit.New(8, 8), ttl, t.TempDir(), func(int) bool { return false }, "")
	r.buildProvider = func(prompt.Config, func(string) string) (agentkit.Provider, error) { return fp, nil }
	return r, store
}

// seedRunning inserts a prompt, makes a run-scoped sandbox, materializes the
// run's input/ on disk (what the runner now reads from), then opens a running
// run on it — mirroring Service.Run, so the runner can take it terminal.
func seedRunning(t *testing.T, store *prompt.Store, sb *sandbox.Manager, runsDir string) (prompt.Prompt, prompt.Run) {
	return seedRunningWithConfig(t, store, sb, runsDir, prompt.Config{Provider: "anthropic", Model: "claude-haiku-4-5"})
}

func seedRunningWithConfig(t *testing.T, store *prompt.Store, sb *sandbox.Manager, runsDir string, cfg prompt.Config) (prompt.Prompt, prompt.Run) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sess := prompt.Prompt{
		ID:         ids.NewULID(),
		OwnerID:    "owner-id",
		OwnerEmail: "owner@example.com",
		Name:       "n",
		UserPrompt: "do the thing",
		Config:     cfg,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.InsertPrompt(ctx, sess); err != nil {
		t.Fatalf("InsertPrompt: %v", err)
	}
	runID := ids.NewULID()
	// Per-run sandbox is keyed by run_id (runs/<run_id>/sandbox).
	if err := sb.Create(runID); err != nil {
		t.Fatalf("sandbox.Create: %v", err)
	}
	gitTestCommand(t, sb.Root(runID), "init", "-b", "ikigenba/run-"+runID)
	gitTestCommand(t, sb.Root(runID), "-c", "user.name=runner-test", "-c", "user.email=runner-test@localhost", "commit", "--allow-empty", "-m", "run definition")
	run := prompt.Run{
		ID:         runID,
		PromptID:   sess.ID,
		OwnerID:    sess.OwnerID,
		OwnerEmail: sess.OwnerEmail,
		PromptName: sess.Name,
		Status:     prompt.RunRunning,
		StartedAt:  now,
		LogPath:    filepath.Join(runsDir, runID, "output.jsonl"),
	}
	writeRunInput(t, runsDir, runID, sess.UserPrompt, sess.SystemPrompt, sess.Config)
	if err := store.InsertRun(ctx, run); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	return sess, run
}

func gitTestCommand(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", root}, args...)
	out, err := exec.Command("git", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestExecute_UsesCatalogRouteWireModel(t *testing.T) {
	// R-1X6X-E3KP
	fp := &fakeProvider{}
	r, store := newTestRunner(t, time.Minute, fp)
	runsDir := filepath.Dir(filepath.Dir(r.sandbox.Root("probe")))
	_, run := seedRunningWithConfig(t, store, r.sandbox, runsDir, prompt.Config{
		Provider: "openrouter",
		Model:    "deepseek-v4-flash",
	})

	r.execute(run)

	req := fp.lastRequest()
	if req == nil {
		t.Fatal("fake provider saw no request")
	}
	if got, want := req.Model, "deepseek/deepseek-v4-flash"; got != want {
		t.Fatalf("provider request model = %q, want catalog route wire model %q", got, want)
	}
}

func TestExecute_PricesUsageFromCatalogRates(t *testing.T) {
	// R-1ZMQ-5N23
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	sess, run := seedRunningWithConfig(t, store, r.sandbox, runsDir, prompt.Config{
		Provider: "openrouter",
		Model:    "deepseek-v4-flash",
	})

	r.execute(run)

	got, err := store.GetLatestRun(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if got == nil {
		t.Fatal("GetLatestRun returned nil run")
	}
	if got.Status != prompt.RunSucceeded {
		t.Fatalf("run status = %q, want succeeded (error=%q)", got.Status, got.Error)
	}
	var usage struct {
		CostUSD float64 `json:"cost_usd"`
	}
	if err := json.Unmarshal([]byte(got.UsageJSON), &usage); err != nil {
		t.Fatalf("parse usage_json %q: %v", got.UsageJSON, err)
	}
	if usage.CostUSD <= 0 {
		t.Fatalf("usage_json cost_usd = %v, want catalog-priced cost greater than zero", usage.CostUSD)
	}
}

func TestExecute_RecordsOneSessionCallFromProviderUsage(t *testing.T) {
	// R-6JMV-PFLG
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	_, run := seedRunningWithConfig(t, store, r.sandbox, runsDir, prompt.Config{
		Provider: "openrouter",
		Model:    "deepseek-v4-flash",
	})

	r.execute(run)

	rows, err := store.Calls.List(context.Background(), calls.Filter{GroupID: run.ID})
	if err != nil {
		t.Fatalf("List calls: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("session calls = %+v, want exactly one", rows)
	}
	row := rows[0]
	if row.Class != calls.ClassSession || row.GroupID != run.ID {
		t.Fatalf("call identity = %+v, want session group %s", row, run.ID)
	}
	if row.InputTokens != 12 || row.OutputTokens != 7 || row.TotalTokens != 19 {
		t.Fatalf("call tokens = (%d,%d,%d), want (12,7,19)", row.InputTokens, row.OutputTokens, row.TotalTokens)
	}
	if row.CostUSD <= 0 || row.Model != "deepseek-v4-flash" || row.Provider != "openrouter" {
		t.Fatalf("call pricing/model = %+v, want priced pinned catalog config", row)
	}
	gotRun, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if row.UsageJSON != gotRun.UsageJSON {
		t.Fatalf("call usage_json = %q, run usage_json = %q", row.UsageJSON, gotRun.UsageJSON)
	}
	if row.RequestBody != nil || row.ResponseBody != nil {
		t.Fatalf("session bodies = (%v,%v), want nil", row.RequestBody, row.ResponseBody)
	}
}

func TestExecute_FailedRunRecordsConsumedUsage(t *testing.T) {
	// R-6NAK-UQTJ
	fp := &fakeProvider{roundTrips: []*agentkit.RoundTrip{
		agentkit.NewRoundTrip(
			agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{
				agentkit.ToolUseBlock{ID: "toolu_consumed", Name: "Bash", Input: json.RawMessage(`{"command":"true"}`)},
			}},
			agentkit.FinishToolUse, agentkit.Usage{InputUncached: 9, Output: 4, Total: 13}, nil, nil, 0, false),
		agentkit.NewRoundTrip(agentkit.Message{}, agentkit.FinishOther,
			agentkit.Usage{}, nil,
			errors.New("provider exploded"), 0, false),
	}}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	_, run := seedRunning(t, store, r.sandbox, runsDir)

	r.execute(run)

	rows, err := store.Calls.List(context.Background(), calls.Filter{GroupID: run.ID})
	if err != nil {
		t.Fatalf("List calls: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("session calls = %+v, want exactly one", rows)
	}
	if rows[0].Error == "" || !strings.Contains(rows[0].Error, "provider exploded") {
		t.Fatalf("call error = %q, want provider failure", rows[0].Error)
	}
	if rows[0].InputTokens != 9 || rows[0].OutputTokens != 4 || rows[0].TotalTokens != 13 {
		t.Fatalf("failed call usage = %+v, want consumed 9/4/13 tokens", rows[0])
	}
}

// writeRunInput pins a run's execution inputs to runs/<run_id>/input/, the
// disk source the runner reads (mirrors Service.materializeInput).
func writeRunInput(t *testing.T, runsDir, runID, userPrompt, sysPrompt string, cfg prompt.Config) {
	t.Helper()
	inputDir := filepath.Join(runsDir, runID, "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("mkdir input: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "user_prompt.txt"), []byte(userPrompt), 0o644); err != nil {
		t.Fatalf("write user_prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "system_prompt.txt"), []byte(sysPrompt), 0o644); err != nil {
		t.Fatalf("write system_prompt: %v", err)
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "config.json"), cfgJSON, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// waitRun polls the store until the run reaches a terminal status or the
// deadline passes. Returns the final run row.
func waitRun(t *testing.T, store *prompt.Store, sessionID string) prompt.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetLatestRun(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("GetLatestRun: %v", err)
		}
		if run != nil && run.Status != prompt.RunRunning {
			return *run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run for session %s did not reach a terminal state", sessionID)
	return prompt.Run{}
}

func TestSpawn_TerminalSuccess(t *testing.T) {
	// R-K7Y2-Q09N
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	sess, run := seedRunning(t, store, r.sandbox, runsDir)

	r.Spawn(run)
	got := waitRun(t, store, sess.ID)

	if got.Status != prompt.RunSucceeded {
		t.Fatalf("run status = %q, want succeeded (error=%q)", got.Status, got.Error)
	}
	if got.Error != "" {
		t.Fatalf("run error = %q, want empty", got.Error)
	}
	if got.EndedAt == "" {
		t.Fatalf("run ended_at empty")
	}

	data, err := os.ReadFile(run.LogPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logStr := string(data)
	if !strings.Contains(logStr, "all done") {
		t.Fatalf("log missing emitted assistant text: %s", logStr)
	}
	if !strings.Contains(logStr, `"type":"message"`) {
		t.Fatalf("log missing message event: %s", logStr)
	}
	if got.UsageJSON == "" {
		t.Fatalf("usage_json empty; want captured usage")
	}
	if !strings.Contains(got.UsageJSON, "usage") {
		t.Fatalf("usage_json = %q, want usage totals", got.UsageJSON)
	}
	if fp.requestCount() != 1 {
		t.Fatalf("provider RoundTrip calls = %d, want 1", fp.requestCount())
	}
}

func TestSpawn_RunCapacitySerializesProviderExecution(t *testing.T) {
	// R-6B3L-11EL
	fp := &serialProvider{started: make(chan int, 2), releaseFirst: make(chan struct{})}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	r.gate = admit.New(8, 1)
	firstPrompt, firstRun := seedRunning(t, store, r.sandbox, runsDir)
	_, secondRun := seedRunning(t, store, r.sandbox, runsDir)

	r.Spawn(firstRun)
	select {
	case call := <-fp.started:
		if call != 1 {
			t.Fatalf("first provider start = %d, want 1", call)
		}
	case <-time.After(time.Second):
		t.Fatal("first provider call did not start")
	}
	r.Spawn(secondRun)
	select {
	case call := <-fp.started:
		t.Fatalf("provider call %d began while first run still held the only slot", call)
	case <-time.After(100 * time.Millisecond):
	}

	close(fp.releaseFirst)
	select {
	case call := <-fp.started:
		if call != 2 {
			t.Fatalf("second provider start = %d, want 2", call)
		}
	case <-time.After(time.Second):
		t.Fatal("second provider call did not start after first completed")
	}
	first, err := store.GetLatestRun(context.Background(), firstPrompt.ID)
	if err != nil {
		t.Fatalf("GetLatestRun(first): %v", err)
	}
	if first == nil || first.Status != prompt.RunSucceeded {
		t.Fatalf("first run at second provider start = %+v, want terminal success", first)
	}
	if got := waitRun(t, store, secondRun.PromptID); got.Status != prompt.RunSucceeded {
		t.Fatalf("second run status = %q, want succeeded", got.Status)
	}
}

func TestSpawn_UsesInjectedProviderFactoryWithoutLiveEnvironment(t *testing.T) {
	// R-K6Q6-C8IY
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	sess, run := seedRunning(t, store, r.sandbox, runsDir)

	var called bool
	var gotCfg prompt.Config
	r.buildProvider = func(cfg prompt.Config, getenv func(string) string) (agentkit.Provider, error) {
		called = true
		gotCfg = cfg
		return fp, nil
	}

	r.Spawn(run)
	got := waitRun(t, store, sess.ID)

	if !called {
		t.Fatalf("injected buildProvider was not called")
	}
	if gotCfg.Provider != "anthropic" || gotCfg.Model != "claude-haiku-4-5" {
		t.Fatalf("buildProvider cfg = %+v, want pinned run config", gotCfg)
	}
	if got.Status != prompt.RunSucceeded {
		t.Fatalf("run status = %q, want succeeded (error=%q)", got.Status, got.Error)
	}
	if fp.requestCount() != 1 {
		t.Fatalf("provider RoundTrip calls = %d, want 1", fp.requestCount())
	}
}

func TestExecuteAttachesEagerMCPToolsWithoutLoader(t *testing.T) {
	// R-ZIVG-1F6T
	peer := newRunnerMCPServer(t, "health", nil)
	defer peer.Close()
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	_, run := seedRunning(t, store, r.sandbox, runsDir)
	r.discover = func(context.Context, string, string, string, string) []agentkit.MCPServer {
		return []agentkit.MCPServer{{Name: "ikigenba_crm", URL: peer.URL}}
	}

	r.execute(run)
	req := fp.request(0)
	if req == nil {
		t.Fatal("provider saw no request")
	}
	names := requestToolNames(req)
	for _, want := range []string{"Bash", "Read", "Write", "Edit", "Glob", "Grep", "ikigenba_crm_health"} {
		if !names[want] {
			t.Fatalf("request tools = %v, missing %q", sortedToolNames(req.Tools), want)
		}
	}
	if names["load_tools"] {
		t.Fatalf("request unexpectedly exposed load_tools: %v", sortedToolNames(req.Tools))
	}
}

func TestExecuteThreadsStoredRunCorrelationThroughDiscoveryAndContext(t *testing.T) {
	// R-HT9J-IK4U
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	_, seeded := seedRunning(t, store, r.sandbox, runsDir)

	correlationID := correlation.New()
	if correlationID == seeded.ID {
		t.Fatalf("test requires correlation id %q to differ from run id", correlationID)
	}
	if err := store.DeleteRun(context.Background(), seeded.ID); err != nil {
		t.Fatalf("DeleteRun: %v", err)
	}
	seeded.CorrelationID = correlationID
	if err := store.InsertRun(context.Background(), seeded); err != nil {
		t.Fatalf("InsertRun with correlation id: %v", err)
	}
	run, err := store.GetRun(context.Background(), seeded.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.CorrelationID != correlationID {
		t.Fatalf("stored run correlation id = %q, want %q", run.CorrelationID, correlationID)
	}

	var gotArgument, gotContext string
	r.discover = func(ctx context.Context, _, _, _, gotCorrelationID string) []agentkit.MCPServer {
		gotArgument = gotCorrelationID
		gotContext = correlation.FromContext(ctx)
		return nil
	}
	r.execute(run)

	if gotArgument != correlationID {
		t.Fatalf("discover correlation id = %q, want stored %q (run id %q)", gotArgument, correlationID, run.ID)
	}
	if gotContext != correlationID {
		t.Fatalf("execute context correlation id = %q, want stored %q", gotContext, correlationID)
	}
}

func TestFramingPromptNamesNoLoaderOrIndividualService(t *testing.T) {
	// R-ZK3C-F6XI
	framing := framingPrompt("ikigenba/run-example", strings.Repeat("a", 40))
	if strings.Contains(framing, "load_tools") || strings.Contains(framing, "ikigenba_") {
		t.Fatalf("framing prompt contains retired loader or service prefix: %q", framing)
	}
	for _, service := range []string{"crm", "gmail", "dropbox", "wiki"} {
		if strings.Contains(strings.ToLower(framing), service) {
			t.Fatalf("framing prompt names individual service %q: %q", service, framing)
		}
	}
}

func TestSystemPromptExplainsFetchContentURLSandboxRole(t *testing.T) {
	// R-6AUG-NHQY
	for _, system := range []string{buildSystemPrompt("", "ikigenba/run-example", strings.Repeat("a", 40)), buildSystemPrompt("Keep the response concise.", "ikigenba/run-example", strings.Repeat("a", 40))} {
		lower := strings.ToLower(system)
		for _, claim := range []string{"glob, grep, fetch", "content url", "lands its bytes as a sandbox file"} {
			if !strings.Contains(lower, claim) {
				t.Errorf("assembled system prompt does not contain fetch claim %q: %q", claim, system)
			}
		}
		if strings.Contains(system, "ikigenba_") {
			t.Errorf("assembled system prompt names service prefix: %q", system)
		}
		for _, service := range []string{"crm", "gmail", "dropbox", "wiki"} {
			if strings.Contains(lower, service) {
				t.Errorf("assembled system prompt names individual service %q: %q", service, system)
			}
		}
	}
}

func TestSystemPromptClaimsPDFCommandsAreAvailableInBash(t *testing.T) {
	// R-6I5U-Y474
	for _, system := range []string{buildSystemPrompt("", "ikigenba/run-example", strings.Repeat("a", 40)), buildSystemPrompt("Keep the response concise.", "ikigenba/run-example", strings.Repeat("a", 40))} {
		lower := strings.ToLower(system)
		if !strings.Contains(system, "PDF tooling is available in Bash") {
			t.Errorf("assembled system prompt does not claim PDF tooling is available in Bash: %q", system)
		}
		for _, command := range []string{"pdftotext", "pdftoppm", "pdfinfo"} {
			if !strings.Contains(system, command) {
				t.Errorf("assembled system prompt does not name PDF command %q: %q", command, system)
			}
		}
		if strings.Contains(system, "ikigenba_") {
			t.Errorf("assembled system prompt names service prefix: %q", system)
		}
		for _, service := range []string{"crm", "gmail", "dropbox", "wiki"} {
			if strings.Contains(lower, service) {
				t.Errorf("assembled system prompt names individual service %q: %q", service, system)
			}
		}
	}
}

func TestSystemPromptExplainsFileShareToolsAndConfinement(t *testing.T) {
	// R-FEGC-LVD7
	for _, system := range []string{buildSystemPrompt("", "ikigenba/run-example", strings.Repeat("a", 40)), buildSystemPrompt("Keep the response concise.", "ikigenba/run-example", strings.Repeat("a", 40))} {
		lower := strings.ToLower(system)
		for _, tool := range []string{"file list", "file get", "file put", "file delete", "file move", "file mkdir"} {
			if !strings.Contains(lower, tool) {
				t.Errorf("assembled system prompt does not name file tool %q: %q", tool, system)
			}
		}
		for _, guidance := range []string{
			"file share is its durable, shared file store",
			"own folder stays private to this prompt",
			"file tools as the channel between it and the file share",
			"You have NO network access from bash",
		} {
			if !strings.Contains(system, guidance) {
				t.Errorf("assembled system prompt does not contain file-share guidance %q: %q", guidance, system)
			}
		}
		if strings.Contains(system, "ikigenba_") {
			t.Errorf("assembled system prompt names service prefix: %q", system)
		}
		for _, service := range []string{"crm", "gmail", "dropbox", "wiki"} {
			if strings.Contains(lower, service) {
				t.Errorf("assembled system prompt names individual service %q: %q", service, system)
			}
		}
	}
}

func TestSystemPromptExplainsRunCheckoutAndConditionalMerge(t *testing.T) {
	// R-S953-GPY3
	branch := "ikigenba/run-example"
	sha := strings.Repeat("b", 40)
	system := buildSystemPrompt("Follow the task precisely.", branch, sha)
	for _, claim := range []string{
		"git clone",
		"branch `" + branch + "`",
		"commit `" + sha + "`",
		"`ikigenba/` namespace",
		"`main` branch is off limits",
		"pushes to it are refused",
		"never be force-pushed",
		"version-control service's merge tool",
		"only when these instructions ask for it",
		"no automatic merge at the end of a run",
		"You have NO network access from bash:",
	} {
		if !strings.Contains(system, claim) {
			t.Errorf("assembled system prompt does not contain checkout guidance %q: %q", claim, system)
		}
	}
	lower := strings.ToLower(system)
	for _, prohibited := range []string{"repos", "ikigenba_", "crm", "gmail", "dropbox", "wiki"} {
		if strings.Contains(lower, prohibited) {
			t.Errorf("assembled system prompt names prohibited service %q: %q", prohibited, system)
		}
	}
}

func TestSpawnedRunSystemPromptUsesRealWorkspaceBranchAndCommit(t *testing.T) {
	// R-SACZ-UHOS
	fp := &fakeProvider{}
	r, store := newTestRunner(t, time.Minute, fp)
	r.discover = func(context.Context, string, string, string, string) []agentkit.MCPServer { return nil }
	runsDir := filepath.Dir(filepath.Dir(r.sandbox.Root("probe")))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p := prompt.Prompt{
		ID:         ids.NewULID(),
		OwnerID:    "owner-id",
		OwnerEmail: "owner@example.com",
		Name:       "real checkout framing",
		UserPrompt: "inspect the checkout",
		Config:     prompt.Config{Provider: "anthropic", Model: "claude-haiku-4-5"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.InsertPrompt(context.Background(), p); err != nil {
		t.Fatalf("InsertPrompt: %v", err)
	}
	svc := prompt.NewService(store, r.sandbox, runsDir, r)
	run, err := svc.Run(context.Background(), p.OwnerID, p.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := waitRun(t, store, p.ID)
	if got.Status != prompt.RunSucceeded {
		t.Fatalf("run status = %q, error=%q; want succeeded", got.Status, got.Error)
	}

	root := r.sandbox.Root(run.ID)
	branchBytes, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse --abbrev-ref HEAD: %v", err)
	}
	shaBytes, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	branch := strings.TrimSpace(string(branchBytes))
	sha := strings.TrimSpace(string(shaBytes))
	req := fp.lastRequest()
	if req == nil {
		t.Fatal("fake provider saw no request")
	}
	if !strings.Contains(req.System, "branch `"+branch+"`") {
		t.Fatalf("system prompt does not contain real workspace branch %q: %q", branch, req.System)
	}
	if !strings.Contains(req.System, "commit `"+sha+"`") {
		t.Fatalf("system prompt does not contain real workspace commit %q: %q", sha, req.System)
	}
}

func TestExecuteCallsAttachedSuiteToolOnFirstRoundTrip(t *testing.T) {
	// R-ZLB8-SYO7
	var called string
	peer := newRunnerMCPServer(t, "health", func(name string) { called = name })
	defer peer.Close()
	fp := &fakeProvider{roundTrips: []*agentkit.RoundTrip{
		scriptedRoundTrip(agentkit.ToolUseBlock{ID: "call-1", Name: "ikigenba_crm_health", Input: json.RawMessage(`{}`)}),
		scriptedTextRoundTrip("done"),
	}}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	sess, run := seedRunning(t, store, r.sandbox, runsDir)
	r.discover = func(context.Context, string, string, string, string) []agentkit.MCPServer {
		return []agentkit.MCPServer{{Name: "ikigenba_crm", URL: peer.URL}}
	}

	r.execute(run)
	got, err := store.GetLatestRun(context.Background(), sess.ID)
	if err != nil || got == nil || got.Status != prompt.RunSucceeded {
		t.Fatalf("run = %#v, err=%v, want succeeded", got, err)
	}
	if called != "health" {
		t.Fatalf("peer tools/call name = %q, want health", called)
	}
	records := readRunnerLogRecords(t, run.LogPath)
	if !hasToolUse(records, "ikigenba_crm_health") || !hasToolResult(records, "ikigenba_crm_health") {
		t.Fatalf("log missing native suite tool events: %v", logToolEvents(records))
	}
}

func TestRunBoundaryRootsOnceAndArchivesBuiltinToolUse(t *testing.T) {
	// R-I48M-YHT3
	fp := &fakeProvider{roundTrips: []*agentkit.RoundTrip{
		scriptedRoundTrip(agentkit.ToolUseBlock{ID: "toolu_boundary", Name: "Bash", Input: json.RawMessage(`{"command":"true"}`)}),
		scriptedTextRoundTrip("done"),
	}}
	r, store := newTestRunner(t, time.Minute, fp)
	runsDir := filepath.Dir(filepath.Dir(r.sandbox.Root("probe")))
	r.discover = func(context.Context, string, string, string, string) []agentkit.MCPServer { return nil }

	now := time.Now().UTC().Format(time.RFC3339Nano)
	sess := prompt.Prompt{
		ID:         ids.NewULID(),
		OwnerID:    "owner-id",
		OwnerEmail: "owner@example.com",
		Name:       "boundary task",
		UserPrompt: "use the sandbox",
		Config:     prompt.Config{Provider: "anthropic", Model: "claude-haiku-4-5"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.InsertPrompt(context.Background(), sess); err != nil {
		t.Fatalf("InsertPrompt: %v", err)
	}
	svc := prompt.NewService(store, r.sandbox, runsDir, r)
	type rootCall struct{ rootID, op string }
	var roots []rootCall
	svc.RootStarter = func(ctx context.Context, rootID, op string) context.Context {
		roots = append(roots, rootCall{rootID: rootID, op: op})
		return ctx
	}

	run, err := svc.Run(context.Background(), sess.OwnerID, sess.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := waitRun(t, store, sess.ID)
	if got.Status != prompt.RunSucceeded {
		t.Fatalf("run status = %q, error=%q; want succeeded", got.Status, got.Error)
	}
	if len(roots) != 1 || roots[0].rootID != run.ID || roots[0].op != "run:"+run.ID {
		t.Fatalf("root boundary calls = %+v, want exactly {%q %q}", roots, run.ID, "run:"+run.ID)
	}

	callRows, err := store.Calls.List(context.Background(), calls.Filter{GroupID: run.ID})
	if err != nil {
		t.Fatalf("list terminal call: %v", err)
	}
	if len(callRows) != 1 {
		t.Fatalf("terminal calls = %+v, want exactly one", callRows)
	}
	encodedBoundary, err := json.Marshal(struct {
		Roots []rootCall
		Calls []calls.Row
	}{roots, callRows})
	if err != nil {
		t.Fatalf("marshal observed boundary: %v", err)
	}
	if strings.Contains(string(encodedBoundary), "Bash") || callRows[0].RequestBody != nil || callRows[0].ResponseBody != nil {
		t.Fatalf("telemetry boundary leaked builtin tool use: %s", encodedBoundary)
	}

	records := readRunnerLogRecords(t, run.LogPath)
	if !hasToolUse(records, "Bash") {
		t.Fatalf("output.jsonl missing Bash tool_use: %v", logToolEvents(records))
	}
}

func TestExecuteFailsWhenAnyAttachedPeerDiscoveryFails(t *testing.T) {
	// R-ZGFN-9VPF
	healthy := newRunnerMCPServer(t, "health", nil)
	defer healthy.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			writeRunnerRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-11-25"})
			return
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "crm unavailable"}})
	}))
	defer bad.Close()
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	sess, run := seedRunning(t, store, r.sandbox, runsDir)
	r.discover = func(context.Context, string, string, string, string) []agentkit.MCPServer {
		return []agentkit.MCPServer{{Name: "ikigenba_crm", URL: bad.URL}, {Name: "ikigenba_gmail", URL: healthy.URL}}
	}
	r.execute(run)
	got, err := store.GetLatestRun(context.Background(), sess.ID)
	if err != nil || got == nil || got.Status != prompt.RunFailed || !strings.Contains(got.Error, "ikigenba_crm") {
		t.Fatalf("run = %#v, err=%v, want failed error naming bad peer", got, err)
	}
	if fp.requestCount() != 0 {
		t.Fatalf("provider round trips = %d, want none when MCP discovery fails", fp.requestCount())
	}
}

func TestExecuteRejectsUnroutedPinnedConfigBeforeProviderRoundTrip(t *testing.T) {
	// R-ZHNJ-NNG4
	fp := &fakeProvider{}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	sess, run := seedRunningWithConfig(t, store, r.sandbox, runsDir, prompt.Config{Provider: "unknown-provider", Model: "unknown-model"})
	r.execute(run)
	got, err := store.GetLatestRun(context.Background(), sess.ID)
	if err != nil || got == nil || got.Status != prompt.RunFailed || !strings.Contains(got.Error, "unknown-provider") || !strings.Contains(got.Error, "unknown-model") {
		t.Fatalf("run = %#v, err=%v, want failed error naming provider and model", got, err)
	}
	if fp.requestCount() != 0 {
		t.Fatalf("provider round trips = %d, want none", fp.requestCount())
	}
}

func TestExecuteMissingCredentialFailsAtSendNotProviderBuild(t *testing.T) {
	// R-ZBK1-QSQN
	t.Setenv("OPENAI_API_KEY", "")
	cfg := prompt.Config{Provider: "openai", Model: "gpt-5.5"}
	if built, err := runprovider.Build(cfg, func(string) string { return "" }); err != nil || built == nil {
		t.Fatalf("provider.Build with absent key = %v, %v; want constructed provider", built, err)
	}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, &fakeProvider{})
	r.buildProvider = runprovider.Build
	sess, run := seedRunningWithConfig(t, store, r.sandbox, runsDir, cfg)
	r.execute(run)
	got, err := store.GetLatestRun(context.Background(), sess.ID)
	if err != nil || got == nil || got.Status != prompt.RunFailed || !strings.Contains(strings.ToLower(got.Error), "api key is absent") {
		t.Fatalf("run = %#v, err=%v, want failed missing-credential error", got, err)
	}
}

func newRunnerMCPServer(t *testing.T, toolName string, called func(string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode MCP request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			writeRunnerRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-11-25"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeRunnerRPC(t, w, req.ID, map[string]any{"tools": []any{map[string]any{"name": toolName, "inputSchema": map[string]any{"type": "object"}}}})
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Errorf("decode tools/call: %v", err)
				return
			}
			if called != nil {
				called(params.Name)
			}
			writeRunnerRPC(t, w, req.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}})
		}
	}))
}

func writeRunnerRPC(t *testing.T, w http.ResponseWriter, id json.RawMessage, result any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		t.Errorf("encode MCP response: %v", err)
	}
}

func requestToolNames(req *agentkit.Request) map[string]bool {
	names := make(map[string]bool, len(req.Tools))
	for _, tool := range req.Tools {
		names[tool.Name()] = true
	}
	return names
}

func sortedToolNames(tools []agentkit.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}

type runnerLogRecord struct {
	Type    string               `json:"type"`
	ToolUse *agentkit.ToolUse    `json:"tool_use"`
	Result  *agentkit.ToolResult `json:"tool_result"`
}

func readRunnerLogRecords(t *testing.T, path string) []runnerLogRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var records []runnerLogRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record runnerLogRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("parse log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func hasToolUse(records []runnerLogRecord, name string) bool {
	for _, record := range records {
		if record.Type == "tool_use" && record.ToolUse != nil && record.ToolUse.Name == name {
			return true
		}
	}
	return false
}

func hasToolResult(records []runnerLogRecord, name string) bool {
	for _, record := range records {
		if record.Type == "tool_result" && record.Result != nil && record.Result.Name == name {
			return true
		}
	}
	return false
}

func logToolEvents(records []runnerLogRecord) []string {
	var events []string
	for _, record := range records {
		switch {
		case record.Type == "tool_use" && record.ToolUse != nil:
			events = append(events, "use:"+record.ToolUse.Name)
		case record.Type == "tool_result" && record.Result != nil:
			events = append(events, "result:"+record.Result.Name)
		}
	}
	return events
}

// TestNew_DefaultDiscoverWired confirms the default construction (no seam
// override) installs a working discover closure over the configured
// manifestRoot — a smoke assertion that the default path is wired and returns a
// non-nil group slice (suite.Discover's best-effort contract) without standing up
// real peers.
func TestNew_DefaultDiscoverWired(t *testing.T) {
	ctx := context.Background()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatalf("appkitdb.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("appkitdb.LoadMigrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("appkitdb.Migrate: %v", err)
	}
	sb, err := sandbox.New(filepath.Join(t.TempDir(), "sandboxes"))
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	r := New(prompt.NewStore(conn), sb, admit.New(8, 8), time.Minute, t.TempDir(), func(int) bool { return false }, "")
	if r.discover == nil {
		t.Fatalf("New left discover seam nil")
	}
	if groups := r.discover(ctx, "owner-id", "owner@example.com", "p_123", "chain-123"); groups == nil {
		t.Fatalf("default discover returned nil group slice; want non-nil (best-effort contract)")
	}
}

func TestCancel(t *testing.T) {
	fp := &fakeProvider{block: true}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	sess, run := seedRunning(t, store, r.sandbox, runsDir)

	r.Spawn(run)

	// Wait until the run goroutine has registered its cancel func, then cancel
	// by run_id.
	var cancelled bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.Cancel(run.ID) {
			cancelled = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !cancelled {
		t.Fatalf("Cancel never returned true")
	}

	got := waitRun(t, store, sess.ID)
	if got.Status != prompt.RunCancelled {
		t.Fatalf("run status = %q, want cancelled", got.Status)
	}
	if got.Error != "cancelled" {
		t.Fatalf("run error = %q, want \"cancelled\"", got.Error)
	}

	// Cancelling an absent run returns false.
	if r.Cancel("no-such-run") {
		t.Fatalf("Cancel of absent run returned true")
	}
}

// R-K95Z-3S0C
func TestTTLFires(t *testing.T) {
	fp := &fakeProvider{block: true}
	runsDir := t.TempDir()
	r, store := newTestRunner(t, 50*time.Millisecond, fp)
	sess, run := seedRunning(t, store, r.sandbox, runsDir)

	r.Spawn(run)
	got := waitRun(t, store, sess.ID)

	if got.Status != prompt.RunFailed {
		t.Fatalf("run status = %q, want failed", got.Status)
	}
	if got.Error != "run TTL exceeded" {
		t.Fatalf("run error = %q, want \"run TTL exceeded\"", got.Error)
	}
}

func TestRecover(t *testing.T) {
	fp := &fakeProvider{} // unused; Recover does not run the engine
	runsDir := t.TempDir()
	r, store := newTestRunner(t, time.Minute, fp)
	// Seed a running session+run but never spawn it — it is an orphan.
	sess, _ := seedRunning(t, store, r.sandbox, runsDir)

	n, err := r.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if n < 1 {
		t.Fatalf("Recover swept %d runs, want >= 1", n)
	}

	run, err := store.GetLatestRun(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if run == nil || run.Status != prompt.RunFailed {
		t.Fatalf("swept run status = %v, want failed", run)
	}
}
