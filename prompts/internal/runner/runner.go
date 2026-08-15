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
	"eventplane/correlation"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"prompts/internal/admit"
	"prompts/internal/gateway"
	"prompts/internal/mcpclient"
	"prompts/internal/prompt"
	"prompts/internal/provider"
	"prompts/internal/sandbox"
	"prompts/internal/suite"
	"strings"
	"sync"
	"time"

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
	// discover snapshots the box's other loopback services as gateway peers.
	// It defaults to a closure over the configured manifestRoot calling
	// suite.Discover, but is injectable so tests can supply fake servers and
	// never touch the real inventory or any peer.
	discover func(ctx context.Context, ownerID, ownerEmail, promptID, correlationID string) []suite.Peer
	peerDoer mcpclient.Doer
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
func New(store *prompt.Store, sb *sandbox.Manager, gate *admit.Gate, ttl time.Duration, manifestRoot string, sourcePortAllowed func(int) bool, shareBaseURL string, peerDoer mcpclient.Doer) *Runner {
	return &Runner{
		store:         store,
		sandbox:       sb,
		gate:          gate,
		ttl:           ttl,
		buildProvider: provider.Build,
		discover: func(ctx context.Context, ownerID, ownerEmail, promptID, correlationID string) []suite.Peer {
			return suite.Discover(ctx, manifestRoot, ownerID, ownerEmail, promptID, correlationID)
		},
		peerDoer:          peerDoer,
		sourcePortAllowed: sourcePortAllowed,
		shareBaseURL:      shareBaseURL,
		cancels:           make(map[string]context.CancelFunc),
		userCancelled:     make(map[string]bool),
	}
}

// SetProviderFactory installs the process-wide provider builder assembled by
// the composition root.
func (r *Runner) SetProviderFactory(build func(prompt.Config, func(string) string) (agentkit.Provider, error)) {
	r.buildProvider = build
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
	ctx, cancel := context.WithTimeout(correlation.WithContext(context.Background(), run.CorrelationID), r.ttl)

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

	endedAt := func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
	metrics := &runMetrics{}
	finish := r.finisher(run, endedAt, metrics)

	setup, err := r.prepareRun(run)
	if err != nil {
		finish(prompt.RunFailed, "", err.Error())
		return
	}
	defer setup.logFile.Close()
	cfg := setup.executed.Config
	metrics.providerName, metrics.modelName = cfg.Provider, cfg.Model

	metrics.releaseRun, err = r.gate.AcquireRun(ctx)
	if err != nil {
		r.mu.Lock()
		userCancelled := r.userCancelled[run.ID]
		r.mu.Unlock()
		if userCancelled {
			finish(prompt.RunCancelled, "", "cancelled")
			return
		}
		finish(prompt.RunFailed, "", "acquire run capacity: "+err.Error())
		return
	}

	prov, err := r.buildProvider(cfg, os.Getenv)
	if err != nil {
		finish(prompt.RunFailed, "", "create provider: "+err.Error())
		return
	}
	res := catalog.Resolve(prompt.CatalogProviderID(cfg.Provider), cfg.Model)
	_, modelKnown := catalog.Lookup(cfg.Model)
	if res.Coverage == catalog.Unrouted || (!modelKnown && !knownProvider(cfg.Provider)) {
		finish(prompt.RunFailed, "", fmt.Sprintf("resolve model: provider %q does not route catalog model %q", cfg.Provider, cfg.Model))
		return
	}

	usage, cost, runErr := r.runConversation(ctx, run, setup.executed, cfg, prov, res, setup.logFile, setup.sandboxRoot, setup.branch, setup.sha, setup.eventBytes)
	metrics.usage, metrics.cost = usage, cost

	// Classify the terminal status: explicit user cancel wins over TTL, TTL
	// over an engine error, and a clean return is success.
	r.mu.Lock()
	userCancelled := r.userCancelled[run.ID]
	r.mu.Unlock()

	usageJSON := serializeUsage(usage, cost)

	finishRun(finish, ctx, userCancelled, runErr, usageJSON)
}

type runMetrics struct {
	providerName string
	modelName    string
	usage        agentkit.Usage
	cost         agentkit.Cost
	releaseRun   func()
}

// finisher persists the terminal state and atomically emits any outcome event.
func (r *Runner) finisher(run prompt.Run, endedAt func() string, metrics *runMetrics) func(string, string, string) {
	return func(status, usageJSON, errMsg string) {
		usage := metrics.usage
		_ = r.store.FinishRun(context.Background(), prompt.FinishRunInput{
			RunID: run.ID, Status: status, EndedAt: endedAt(), UsageJSON: usageJSON, ErrMsg: errMsg,
			Provider: metrics.providerName, Model: metrics.modelName,
			InputTokens:  usage.InputUncached + usage.CacheReadInput + usage.CacheWriteInput,
			OutputTokens: usage.Output + usage.ReasoningOutput, TotalTokens: usage.Total,
			CostUSD: metrics.cost.USD(),
		})
		if metrics.releaseRun != nil {
			metrics.releaseRun()
		}
	}
}

type runSetup struct {
	logFile     *os.File
	executed    prompt.Executed
	eventBytes  []byte
	sandboxRoot string
	branch      string
	sha         string
}

func (r *Runner) prepareRun(run prompt.Run) (runSetup, error) {
	if err := os.MkdirAll(filepath.Dir(run.LogPath), 0o755); err != nil {
		return runSetup{}, fmt.Errorf("open run log dir: %w", err)
	}
	logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return runSetup{}, fmt.Errorf("open run log: %w", err)
	}
	runsDir := filepath.Dir(filepath.Dir(run.LogPath))
	executed, err := prompt.LoadFromRun(runsDir, run.ID, run.DefinitionSha)
	if err != nil {
		_ = logFile.Close()
		return runSetup{}, fmt.Errorf("load run prompt: %w", err)
	}
	eventBytes, err := os.ReadFile(filepath.Join(runsDir, run.ID, "input", "event.json"))
	if err != nil && !os.IsNotExist(err) {
		_ = logFile.Close()
		return runSetup{}, fmt.Errorf("read run event: %w", err)
	}
	sandboxRoot := r.sandbox.Root(run.ID)
	branch, sha, err := workspaceGitState(sandboxRoot)
	if err != nil {
		_ = logFile.Close()
		return runSetup{}, fmt.Errorf("read run workspace git state: %w", err)
	}
	return runSetup{logFile: logFile, executed: executed, eventBytes: eventBytes, sandboxRoot: sandboxRoot, branch: branch, sha: sha}, nil
}

func (r *Runner) runConversation(ctx context.Context, run prompt.Run, executed prompt.Executed, cfg prompt.Config, prov agentkit.Provider, res catalog.Resolution, logFile *os.File, sandboxRoot, branch, sha string, eventBytes []byte) (agentkit.Usage, agentkit.Cost, error) {
	peers := r.discover(ctx, run.OwnerID, run.OwnerEmail, run.PromptID, run.CorrelationID)
	clients := func(peer suite.Peer) gateway.Client {
		return mcpclient.New(r.peerDoer, peer.BaseURL, peer.Headers)
	}
	catalogEntries := func(ctx context.Context) []suite.CatalogEntry {
		return suite.Catalog(ctx, peers, func(peer suite.Peer) suite.InstructionLister {
			return clients(peer)
		})
	}
	tools := runtools.All(sandboxRoot, r.sourcePortAllowed, runtools.ShareConfig{BaseURL: r.shareBaseURL, ClientID: "prompts:" + run.PromptID})
	tools = append(tools, gateway.Tools(peers, catalogEntries, clients)...)
	conv := &agentkit.Conversation{
		Provider: prov, Model: res.WireModel, Pricing: res.Offering.Pricing,
		System: buildSystemPrompt(executed.SystemPrompt, branch, sha), Log: logFile,
		Gen: genSettings(cfg), Retry: retryPolicy(cfg), Tools: tools,
		MaxToolIterations: cfg.ToolLoopLimit,
	}
	stream := conv.Send(ctx, buildUserText(executed.UserPrompt, eventBytes))
	for range stream.Events() {
	}
	runErr := stream.Err()
	usage := stream.Usage()
	cost := stream.Cost()
	_ = conv.Close()
	return usage, cost, runErr
}

func finishRun(finish func(string, string, string), ctx context.Context, userCancelled bool, runErr error, usageJSON string) {
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

func knownProvider(name string) bool {
	switch name {
	case "anthropic", "openai", "google", "zai", "openrouter":
		return true
	default:
		return false
	}
}

func workspaceGitState(root string) (branch, revision string, resultErr error) {
	read := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	branch, err := read("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", err
	}
	sha, err := read("rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	return branch, sha, nil
}

func buildSystemPrompt(sysPrompt, branch, sha string) string {
	framing := framingPrompt(branch, sha)
	if sysPrompt == "" {
		return framing
	}
	return framing + "\n\n" + sysPrompt
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
	case cfg.ThinkingLevel != "":
		gen.Reasoning = agentkit.Level(cfg.ThinkingLevel)
	case cfg.ThinkingBudget != nil:
		gen.Reasoning = agentkit.Budget(*cfg.ThinkingBudget)
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
