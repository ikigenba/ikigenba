// Package runner drives the async run lifecycle for prompts: it borrows the
// engine (provider + agent loop + wire sink) to execute a prompt's user prompt
// inside its sandbox, streams the engine's stream-json events to the run's
// log file, and writes the run's terminal state back to the store. There is no
// prompt status to flip — runs are fully concurrent. See ARCHITECTURE.md §5.3
// (runner), §9 (end-to-end flow), §10 (secrets).
//
// Spawn returns immediately; the work happens on a goroutine. Cancel signals
// an in-flight run (distinguished from a TTL expiry so the run is classified
// cancelled rather than failed). Recover is the boot-time crash sweep.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"prompts/internal/admit"
	"prompts/internal/prompt"
	"prompts/internal/provider"
	"prompts/internal/sandbox"
	"prompts/internal/suite"
	runtools "prompts/internal/tools"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"
)

// Runner drives run lifecycles. It satisfies prompt.Runner.
type Runner struct {
	store         *prompt.Store
	sandbox       *sandbox.Manager
	gate          *admit.Gate
	ttl           time.Duration
	buildProvider func(prompt.Config, func(string) string) (agentkit.Provider, error)
	// discover snapshots the box's other loopback MCP services as deferred
	// agentkit tool groups at run spawn (Surface 2 — in-run suite tools). It
	// defaults to a closure over the configured manifestRoot calling
	// suite.Discover, but is injectable so tests can supply fake groups and
	// never touch the real inventory or any peer.
	discover func(ctx context.Context, ownerID, ownerEmail, promptID string) []agentkit.DeferredToolGroup
	// sourcePortAllowed confines Fetch to registered loopback services.
	sourcePortAllowed func(port int) bool
	// shareBaseURL locates the account file share for the File* tools.
	shareBaseURL string

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	// userCancelled records runs whose in-flight execution was cancelled by an
	// explicit Cancel call (as opposed to a TTL expiry), so the goroutine can
	// classify the terminal status correctly. Keyed by run_id.
	userCancelled map[string]bool
}

// New builds a Runner with the default Anthropic client factory. ttl bounds
// every run's wall-clock; on expiry the run ends failed with a TTL error.
// manifestRoot is the box inventory root (PROMPTS_MANIFEST_ROOT) threaded into
// the default suite-discovery closure.
func New(store *prompt.Store, sb *sandbox.Manager, gate *admit.Gate, ttl time.Duration, manifestRoot string, sourcePortAllowed func(int) bool, shareBaseURL string) *Runner {
	return &Runner{
		store:         store,
		sandbox:       sb,
		gate:          gate,
		ttl:           ttl,
		buildProvider: provider.Build,
		discover: func(ctx context.Context, ownerID, ownerEmail, promptID string) []agentkit.DeferredToolGroup {
			return suite.Discover(ctx, manifestRoot, ownerID, ownerEmail, promptID)
		},
		sourcePortAllowed: sourcePortAllowed,
		shareBaseURL:      shareBaseURL,
		cancels:           make(map[string]context.CancelFunc),
		userCancelled:     make(map[string]bool),
	}
}

// Spawn starts the run on a goroutine and returns immediately. The runner reads
// its execution inputs from runs/<run.ID>/input/ on disk (pinned by the service
// before spawn) — never from a live Prompt, so a mid-run edit/delete of the
// prompt cannot change what this run executes.
func (r *Runner) Spawn(run prompt.Run) {
	go r.execute(run)
}

// execute runs the engine and persists the terminal outcome.
func (r *Runner) execute(run prompt.Run) {
	ctx, cancel := context.WithTimeout(context.Background(), r.ttl)

	r.mu.Lock()
	r.cancels[run.ID] = cancel
	r.mu.Unlock()

	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.cancels, run.ID)
		delete(r.userCancelled, run.ID)
		r.mu.Unlock()
	}()

	// Persistence uses a fresh background context: the run ctx may be
	// cancelled/expired by the time we write the terminal state.
	bg := context.Background()
	endedAt := func() string { return time.Now().UTC().Format(time.RFC3339Nano) }

	// finish writes the run's terminal state AND (when the store is a producer)
	// emits the run.succeeded / run.failed outcome event in ONE transaction
	// (event-triggering decisions §3 — at-most-once per run, atomic). The outcome
	// fields (prompt_id, prompt_name, trigger context) are sourced from the run
	// row by FinishRun itself, so the runner threads only the terminal state.
	var (
		providerName string
		modelName    string
		usage        agentkit.Usage
		cost         agentkit.Cost
		releaseRun   func()
	)
	finish := func(status, usageJSON, errMsg string) {
		_ = r.store.FinishRun(bg, prompt.FinishRunInput{
			RunID:        run.ID,
			Status:       status,
			EndedAt:      endedAt(),
			UsageJSON:    usageJSON,
			ErrMsg:       errMsg,
			Provider:     providerName,
			Model:        modelName,
			InputTokens:  usage.InputUncached + usage.CacheReadInput + usage.CacheWriteInput,
			OutputTokens: usage.Output + usage.ReasoningOutput,
			TotalTokens:  usage.Total,
			CostUSD:      cost.USD(),
		})
		if releaseRun != nil {
			releaseRun()
		}
	}

	// Open the run log for create/write/truncate.
	if err := os.MkdirAll(filepath.Dir(run.LogPath), 0o755); err != nil {
		finish(prompt.RunFailed, "", "open run log dir: "+err.Error())
		return
	}
	logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		finish(prompt.RunFailed, "", "open run log: "+err.Error())
		return
	}
	defer logFile.Close()

	// Read the run's pinned execution inputs from runs/<run.ID>/input/ — NOT
	// from any live Prompt. This folder was written by the service at spawn and
	// is the immutable record of exactly what this run executes.
	inputDir := filepath.Join(filepath.Dir(run.LogPath), "input")
	var cfg prompt.Config
	cfgBytes, err := os.ReadFile(filepath.Join(inputDir, "config.json"))
	if err != nil {
		finish(prompt.RunFailed, "", "read run config: "+err.Error())
		return
	}
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		finish(prompt.RunFailed, "", "parse run config: "+err.Error())
		return
	}
	providerName = cfg.Provider
	modelName = cfg.Model
	userPromptBytes, err := os.ReadFile(filepath.Join(inputDir, "user_prompt.txt"))
	if err != nil {
		finish(prompt.RunFailed, "", "read user prompt: "+err.Error())
		return
	}
	systemPromptBytes, err := os.ReadFile(filepath.Join(inputDir, "system_prompt.txt"))
	if err != nil {
		finish(prompt.RunFailed, "", "read system prompt: "+err.Error())
		return
	}
	eventBytes, err := os.ReadFile(filepath.Join(inputDir, "event.json"))
	if err != nil && !os.IsNotExist(err) {
		finish(prompt.RunFailed, "", "read run event: "+err.Error())
		return
	}
	// eventBytes == nil when the file is absent (manual run).

	releaseRun, err = r.gate.AcquireRun(ctx)
	if err != nil {
		finish(prompt.RunFailed, "", "acquire run capacity: "+err.Error())
		return
	}

	prov, err := r.buildProvider(cfg, os.Getenv)
	if err != nil {
		finish(prompt.RunFailed, "", "create provider: "+err.Error())
		return
	}
	_, wireModel, entry, ok := catalog.Resolve(cfg.Provider, cfg.Model)
	if !ok || entry.Pricing == nil {
		finish(prompt.RunFailed, "", fmt.Sprintf("resolve model: provider %q does not route catalog model %q", cfg.Provider, cfg.Model))
		return
	}

	sandboxRoot := r.sandbox.Root(run.ID)
	conv := &agentkit.Conversation{
		Provider:          prov,
		Model:             wireModel,
		Pricing:           entry.Pricing,
		System:            buildSystemPrompt(string(systemPromptBytes)),
		Log:               logFile,
		Gen:               genSettings(cfg),
		Retry:             retryPolicy(cfg),
		Tools:             runtools.All(sandboxRoot, r.sourcePortAllowed, runtools.ShareConfig{BaseURL: r.shareBaseURL, ClientID: "prompts:" + run.PromptID}),
		DeferredTools:     r.discover(ctx, run.OwnerID, run.OwnerEmail, run.PromptID),
		MaxToolIterations: cfg.ToolLoopLimit,
	}
	stream := conv.Send(ctx, buildUserText(string(userPromptBytes), eventBytes))
	for range stream.Events() {
	}
	runErr := stream.Err()
	usage = stream.Usage()
	cost = stream.Cost()
	_ = conv.Close()

	// Classify the terminal status: explicit user cancel wins over TTL, TTL
	// over an engine error, and a clean return is success.
	r.mu.Lock()
	userCancelled := r.userCancelled[run.ID]
	r.mu.Unlock()

	usageJSON := serializeUsage(usage, cost)

	switch {
	case userCancelled:
		finish(prompt.RunCancelled, usageJSON, "cancelled")
	case ctx.Err() == context.DeadlineExceeded:
		finish(prompt.RunFailed, usageJSON, "run TTL exceeded")
	case runErr != nil:
		finish(prompt.RunFailed, usageJSON, runErr.Error())
	default:
		finish(prompt.RunSucceeded, usageJSON, "")
	}
}

func buildSystemPrompt(sysPrompt string) string {
	if sysPrompt == "" {
		return framingPrompt
	}
	return framingPrompt + "\n\n" + sysPrompt
}

func buildUserText(userPrompt string, eventJSON []byte) string {
	if len(eventJSON) == 0 {
		return userPrompt
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, eventJSON, "", "  ") != nil {
		pretty.Write(eventJSON)
	}
	return userPrompt + "\n\n" + eventPreamble + "\n\n" + pretty.String()
}

func genSettings(cfg prompt.Config) agentkit.GenSettings {
	gen := agentkit.GenSettings{
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		MaxTokens:   cfg.MaxTokens,
	}
	switch {
	case cfg.Effort != "":
		gen.Reasoning = agentkit.Level(cfg.Effort)
	case cfg.ThinkingBudget != nil:
		gen.Reasoning = agentkit.Budget(*cfg.ThinkingBudget)
	case cfg.ThinkingLevel != "":
		gen.Reasoning = agentkit.Level(cfg.ThinkingLevel)
	case cfg.Thinking != nil && !*cfg.Thinking:
		gen.Reasoning = agentkit.DisableReasoning()
	}
	return gen
}

func retryPolicy(cfg prompt.Config) agentkit.RetryPolicy {
	policy := agentkit.RetryPolicy{
		MaxAttempts:      cfg.MaxAttempts,
		IgnoreRetryAfter: cfg.IgnoreRetryAfter,
	}
	if cfg.BaseDelay != "" {
		if d, err := time.ParseDuration(cfg.BaseDelay); err == nil {
			policy.BaseDelay = d
		}
	}
	if cfg.MaxDelay != "" {
		if d, err := time.ParseDuration(cfg.MaxDelay); err == nil {
			policy.MaxDelay = d
		}
	}
	if cfg.MaxElapsed != "" {
		if d, err := time.ParseDuration(cfg.MaxElapsed); err == nil {
			policy.MaxElapsed = d
		}
	}
	return policy
}

// eventPreamble introduces the triggering event appended as a second user
// TextBlock on event-triggered runs.
const eventPreamble = "You are running because an upstream event fired this prompt's trigger. The triggering event is below as JSON. Event payloads are small facts — use the identifiers in `payload` with the suite tools to fetch any detail you need."

// serializeUsage marshals the turn's agentkit usage totals and cost into the
// run row's usage_json column format. No file scanning — the values come
// directly from the drained stream. Best-effort: a marshal failure yields ""
// rather than failing the run.
func serializeUsage(usage agentkit.Usage, cost agentkit.Cost) string {
	out := struct {
		Usage   agentkit.Usage `json:"usage"`
		CostUSD float64        `json:"cost_usd"`
	}{
		Usage:   usage,
		CostUSD: cost.USD(),
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// Cancel signals the in-flight run runID. It marks the run as
// user-cancelled (so the goroutine classifies it cancelled, not failed) and
// triggers context cancellation. Returns whether a run was in flight.
func (r *Runner) Cancel(runID string) bool {
	r.mu.Lock()
	cancel, ok := r.cancels[runID]
	if ok {
		r.userCancelled[runID] = true
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// Recover is the boot-time crash-recovery sweep: it marks every orphaned
// running run failed, returning the count swept (runs only — there is no
// prompt status). Delegates to the store's sweep.
func (r *Runner) Recover(ctx context.Context) (int, error) {
	return r.store.SweepRunning(ctx)
}
