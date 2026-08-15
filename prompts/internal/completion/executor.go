package completion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/ids"
	"prompts/internal/prompt"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"
)

const (
	DefaultRuntimeBound    = 4 * time.Hour
	DefaultRenewalInterval = LeaseTTL / 5
	maxCorrections         = 3
	maxTerminalAttempts    = 3
)

const (
	envelopeInstruction   = `Reply with only this JSON envelope: {"status":"ok"|"error","result":<any JSON value>,"message":"<why, when error>"}. For status ok, result is required. For status error, a non-empty message is required.`
	correctiveInstruction = `Your previous reply did not satisfy the required JSON envelope. Reply again with only the required envelope, with status ok and a result JSON value, or status error and a non-empty message.`
)

type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Request is the inference payload stored durably with an item.
type Request struct {
	Model    string        `json:"model"`
	Provider string        `json:"provider,omitempty"`
	Config   prompt.Config `json:"config,omitempty"`
	System   string        `json:"system,omitempty"`
	Messages []Message     `json:"messages"`
}

type ProviderFactory func(prompt.Config, func(string) string) (agentkit.Provider, error)

type CallStore interface {
	Insert(context.Context, calls.Row) error
}

// Clock and Ticker make executor waits and lease heartbeats deterministic in tests.
type Clock interface {
	NewTicker(time.Duration) Ticker
	After(time.Duration) <-chan time.Time
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type (
	systemClock  struct{}
	systemTicker struct{ ticker *time.Ticker }
)

func NewSystemClock() Clock                                { return systemClock{} }
func (systemClock) NewTicker(d time.Duration) Ticker       { return systemTicker{ticker: time.NewTicker(d)} }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (t systemTicker) C() <-chan time.Time                 { return t.ticker.C }
func (t systemTicker) Stop()                               { t.ticker.Stop() }

type Executor struct {
	queue            *Store
	calls            CallStore
	gate             *admit.Gate
	buildProvider    ProviderFactory
	getenv           func(string) string
	runtimeBound     time.Duration
	leaseDuration    time.Duration
	renewalInterval  time.Duration
	clock            Clock
	logger           *slog.Logger
	subAuthAvailable func() bool
}

func NewExecutor(queue *Store, callStore CallStore, gate *admit.Gate, build ProviderFactory, getenv func(string) string, subAuthAvailable func() bool, runtimeBound, leaseDuration, renewalInterval time.Duration, clock Clock, logger *slog.Logger) *Executor {
	if runtimeBound <= 0 {
		runtimeBound = DefaultRuntimeBound
	}
	if leaseDuration <= 0 {
		leaseDuration = LeaseTTL
	}
	if renewalInterval <= 0 || leaseDuration < 5*renewalInterval {
		renewalInterval = leaseDuration / 5
	}
	if clock == nil {
		clock = NewSystemClock()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		queue: queue, calls: callStore, gate: gate, buildProvider: build, getenv: getenv,
		subAuthAvailable: subAuthAvailable, runtimeBound: runtimeBound, leaseDuration: leaseDuration,
		renewalInterval: renewalInterval, clock: clock, logger: logger,
	}
}

// Run executes items with a fixed-size pool until ctx is cancelled.
func (e *Executor) Run(ctx context.Context, workers int) error {
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				didWork, err := e.ExecuteNext(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					e.logger.Error("completion claim failed", "error", err)
				}
				if err != nil || !didWork {
					select {
					case <-ctx.Done():
					case <-e.clock.After(100 * time.Millisecond):
					}
				}
			}
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// ExecuteNext claims and executes one queued item. It reports false when the
// queue is empty.
func (e *Executor) ExecuteNext(ctx context.Context) (bool, error) {
	item, err := e.queue.Claim(ctx)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	e.executeClaimed(ctx, item)
	return true, nil
}

func (e *Executor) executeClaimed(parent context.Context, item Item) {
	defer func() {
		if recovered := recover(); recovered != nil {
			e.writeTerminal(item, StatusFailed, "", fmt.Sprintf("panic executing completion: %v", recovered), "", 0)
		}
		if parent.Err() != nil {
			releaseCtx, cancel := context.WithTimeout(context.Background(), e.renewalInterval)
			defer cancel()
			if _, err := e.queue.Release(releaseCtx, item.ID); err != nil {
				e.logger.Error("completion lease release failed", "id", item.ID, "error", err)
			}
		}
	}()
	e.execute(parent, item)
}

func (e *Executor) execute(parent context.Context, item Item) {
	fail := func(reason, usageJSON string, cost float64) {
		e.writeTerminal(item, StatusFailed, "", reason, usageJSON, cost)
	}
	var req Request
	if err := json.Unmarshal([]byte(item.Request), &req); err != nil {
		fail("decode stored request: "+err.Error(), "", 0)
		return
	}
	if len(req.Messages) == 0 {
		fail("decode stored request: messages must be non-empty", "", 0)
		return
	}
	cfg := req.Config
	cfg.Provider, cfg.Model = req.Provider, req.Model
	validated, err := prompt.ValidateConfig(cfg, e.getenv, e.subAuthAvailable)
	if err != nil {
		fail("validate stored request: "+err.Error(), "", 0)
		return
	}
	resolution := catalog.Resolve(prompt.CatalogProviderID(validated.Provider), validated.Model)
	if resolution.Coverage == catalog.Unrouted {
		fail(fmt.Sprintf("provider %q does not route model %q", validated.Provider, validated.Model), "", 0)
		return
	}
	provider, err := e.buildProvider(validated, e.getenv)
	if err != nil {
		fail("create provider: "+err.Error(), "", 0)
		return
	}

	execCtx, cancel := context.WithTimeout(parent, e.runtimeBound)
	defer cancel()
	lease := e.renewLease(execCtx, cancel, item.ID)
	defer lease.stop()
	history := makeHistory(req.Messages[:len(req.Messages)-1])
	userText := req.Messages[len(req.Messages)-1].Text
	var aggregate agentkit.Usage
	var totalCost float64
	for round := 0; round <= maxCorrections; round++ {
		if round > 0 && !lease.checkOwnership() {
			return
		}
		text, usage, cost, callErr := e.roundTrip(execCtx, provider, resolution, validated, req.System, history, userText)
		aggregate = addUsage(aggregate, usage)
		totalCost += cost
		if parent.Err() != nil || lease.abandoned() {
			return
		}
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			callErr = fmt.Errorf("runtime bound %s exceeded: %w", e.runtimeBound, execCtx.Err())
		}
		if err := e.recordCall(context.WithoutCancel(parent), item, req, resolution, text, usage, cost, callErr); err != nil {
			fail("record completion call: "+err.Error(), marshalUsage(aggregate), totalCost)
			return
		}
		if callErr != nil {
			fail(callErr.Error(), marshalUsage(aggregate), totalCost)
			return
		}
		envelope, err := parseEnvelope(text)
		if err == nil {
			if envelope.Status == "error" {
				fail(envelope.Message, marshalUsage(aggregate), totalCost)
				return
			}
			e.writeTerminal(item, StatusDone, string(envelope.Result), "", marshalUsage(aggregate), totalCost)
			return
		}
		if round == maxCorrections {
			fail("reply violated completion envelope after 3 corrective round trips: "+err.Error(), marshalUsage(aggregate), totalCost)
			return
		}
		history = append(history,
			agentkit.Message{Role: agentkit.RoleUser, Blocks: []agentkit.Block{agentkit.TextBlock{Text: userText}}},
			agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: text}}})
		userText = correctiveInstruction
	}
}

type leaseWatch struct {
	executor *Executor
	id       string
	state    chan error
	done     chan struct{}
	stopOnce sync.Once
	cancel   context.CancelFunc
	ticker   Ticker
}

var errLeaseLost = errors.New("completion lease lost")

func (e *Executor) renewLease(ctx context.Context, cancel context.CancelFunc, id string) *leaseWatch {
	w := &leaseWatch{executor: e, id: id, state: make(chan error, 1), done: make(chan struct{}), cancel: cancel, ticker: e.clock.NewTicker(e.renewalInterval)}
	go func() {
		defer close(w.done)
		select {
		case <-ctx.Done():
			return
		case <-w.ticker.C():
			for {
				if !w.renew() {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-w.ticker.C():
				}
			}
		}
	}()
	return w
}

func (w *leaseWatch) renew() bool {
	ctx, cancel := context.WithTimeout(context.Background(), w.executor.renewalInterval)
	owned, err := w.executor.queue.Renew(ctx, w.id)
	cancel()
	if err != nil {
		w.executor.logger.Warn("completion lease renewal failed; abandoning item", "id", w.id, "error", err)
		w.mark(err)
		return false
	}
	if !owned {
		w.executor.logger.Warn("completion lease lost; cancelling execution", "id", w.id)
		w.mark(errLeaseLost)
		return false
	}
	return true
}

func (w *leaseWatch) mark(err error) {
	select {
	case w.state <- err:
	default:
	}
	w.cancel()
}

func (w *leaseWatch) abandoned() bool {
	select {
	case err := <-w.state:
		w.state <- err
		return true
	default:
		return false
	}
}

func (w *leaseWatch) checkOwnership() bool {
	if w.abandoned() {
		return false
	}
	return w.renew()
}

func (w *leaseWatch) stop() {
	w.stopOnce.Do(func() {
		w.ticker.Stop()
		w.cancel()
		<-w.done
	})
}

func (e *Executor) writeTerminal(item Item, status, result, reason, usageJSON string, cost float64) {
	var err error
	for range maxTerminalAttempts {
		ctx, cancel := context.WithTimeout(context.Background(), e.renewalInterval)
		if status == StatusDone {
			err = e.queue.Complete(ctx, item.ID, result, usageJSON, cost)
		} else {
			err = e.queue.Fail(ctx, item.ID, reason, usageJSON, cost)
		}
		cancel()
		if err == nil {
			return
		}
	}
	e.logger.Error("completion terminal write failed after retries", "id", item.ID, "status", status, "attempts", maxTerminalAttempts, "error", err)
}

func (e *Executor) roundTrip(ctx context.Context, provider agentkit.Provider, resolution catalog.Resolution, cfg prompt.Config, system string, history []agentkit.Message, userText string) (string, agentkit.Usage, float64, error) {
	release, err := e.gate.AcquireCall(ctx, string(resolution.Provider))
	if err != nil {
		return "", agentkit.Usage{}, 0, err
	}
	defer release()
	conv := &agentkit.Conversation{
		Provider: provider, Model: resolution.WireModel, Pricing: resolution.Offering.Pricing,
		System: strings.TrimSpace(system + "\n\n" + envelopeInstruction), Gen: genSettings(cfg), Retry: retryPolicy(cfg), History: history,
	}
	stream := conv.Send(ctx, userText)
	var text string
	for event := range stream.Events() {
		if done, ok := event.(agentkit.MessageDone); ok {
			text = messageText(done.Message)
		}
	}
	err = stream.Err()
	usage, cost := stream.Usage(), stream.Cost().USD()
	_ = conv.Close()
	return text, usage, cost, err
}

func (e *Executor) recordCall(ctx context.Context, item Item, req Request, resolution catalog.Resolution, response string, usage agentkit.Usage, cost float64, callErr error) error {
	requestBody := item.Request
	row := calls.Row{
		ID: ids.NewULID(), Class: calls.ClassCompletion, Origin: item.Origin, Name: item.Name,
		GroupID: item.GroupID, CorrelationID: item.CorrelationID, Attempt: item.Attempt,
		Provider: string(resolution.Provider), Model: req.Model,
		InputTokens:  usage.InputUncached + usage.CacheReadInput + usage.CacheWriteInput,
		OutputTokens: usage.Output + usage.ReasoningOutput, TotalTokens: usage.Total,
		UsageJSON: marshalUsage(usage), CostUSD: cost, RequestBody: &requestBody, ResponseBody: &response,
	}
	if callErr != nil {
		row.Error = callErr.Error()
	}
	return e.calls.Insert(ctx, row)
}

type replyEnvelope struct {
	Status  string          `json:"status"`
	Result  json.RawMessage `json:"result"`
	Message string          `json:"message"`
}

func parseEnvelope(reply string) (replyEnvelope, error) {
	start, end := strings.IndexByte(reply, '{'), strings.LastIndexByte(reply, '}')
	if start < 0 || end < start {
		return replyEnvelope{}, errors.New("no JSON object")
	}
	var envelope replyEnvelope
	decoder := json.NewDecoder(bytes.NewReader([]byte(reply[start : end+1])))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return replyEnvelope{}, fmt.Errorf("invalid JSON object: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return replyEnvelope{}, errors.New("multiple JSON values")
	}
	switch envelope.Status {
	case "ok":
		if envelope.Result == nil {
			return replyEnvelope{}, errors.New("status ok requires result")
		}
	case "error":
		if strings.TrimSpace(envelope.Message) == "" {
			return replyEnvelope{}, errors.New("status error requires message")
		}
	default:
		return replyEnvelope{}, errors.New(`status must be "ok" or "error"`)
	}
	return envelope, nil
}

func makeHistory(messages []Message) []agentkit.Message {
	history := make([]agentkit.Message, 0, len(messages))
	for _, message := range messages {
		history = append(history, agentkit.Message{Role: agentkit.Role(message.Role), Blocks: []agentkit.Block{agentkit.TextBlock{Text: message.Text}}})
	}
	return history
}

func messageText(message agentkit.Message) string {
	var result string
	for _, block := range message.Blocks {
		if text, ok := block.(agentkit.TextBlock); ok {
			result += text.Text
		}
	}
	return result
}

func addUsage(a, b agentkit.Usage) agentkit.Usage {
	a.InputUncached += b.InputUncached
	a.CacheReadInput += b.CacheReadInput
	a.CacheWriteInput += b.CacheWriteInput
	a.CacheWrite5m += b.CacheWrite5m
	a.CacheWrite1h += b.CacheWrite1h
	a.Output += b.Output
	a.ReasoningOutput += b.ReasoningOutput
	a.Total += b.Total
	return a
}

func marshalUsage(usage agentkit.Usage) string {
	value, _ := json.Marshal(usage)
	return string(value)
}

func genSettings(cfg prompt.Config) agentkit.GenSettings {
	gen := agentkit.GenSettings{Temperature: cfg.Temperature, TopP: cfg.TopP, MaxTokens: cfg.MaxTokens}
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
	policy := agentkit.RetryPolicy{MaxAttempts: cfg.MaxAttempts, IgnoreRetryAfter: cfg.IgnoreRetryAfter}
	parseDuration(cfg.BaseDelay, &policy.BaseDelay)
	parseDuration(cfg.MaxDelay, &policy.MaxDelay)
	parseDuration(cfg.MaxElapsed, &policy.MaxElapsed)
	return policy
}

func parseDuration(raw string, destination *time.Duration) {
	if duration, err := time.ParseDuration(raw); raw != "" && err == nil {
		*destination = duration
	}
}
