// Package inference owns stateless inference endpoints.
package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/catalog"

	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/ids"
	"prompts/internal/prompt"
)

const maxCompleteBody = 10 << 20

// Config is the generation configuration accepted inside a completion request.
// Provider and model live at the request envelope level.
type Config struct {
	Temperature    *float64 `json:"temperature,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
	MaxTokens      int      `json:"max_tokens,omitempty"`
	Effort         string   `json:"effort,omitempty"`
	ThinkingBudget *int     `json:"thinking_budget,omitempty"`
	ThinkingLevel  string   `json:"thinking_level,omitempty"`
	Thinking       *bool    `json:"thinking,omitempty"`

	MaxAttempts      int    `json:"max_attempts,omitempty"`
	BaseDelay        string `json:"base_delay,omitempty"`
	MaxDelay         string `json:"max_delay,omitempty"`
	MaxElapsed       string `json:"max_elapsed,omitempty"`
	IgnoreRetryAfter bool   `json:"ignore_retry_after,omitempty"`

	ToolLoopLimit int    `json:"tool_loop_limit,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
}

type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type Request struct {
	Origin   string    `json:"origin"`
	Name     string    `json:"name"`
	GroupID  string    `json:"group_id,omitempty"`
	Attempt  int       `json:"attempt,omitempty"`
	Model    string    `json:"model"`
	Provider string    `json:"provider,omitempty"`
	Config   Config    `json:"config,omitempty"`
	System   string    `json:"system,omitempty"`
	Messages []Message `json:"messages"`
}

type response struct {
	CallID  string         `json:"call_id"`
	Text    string         `json:"text"`
	Usage   agentkit.Usage `json:"usage"`
	CostUSD float64        `json:"cost_usd"`
}

type CallStore interface {
	Insert(context.Context, calls.Row) error
}

type ProviderFactory func(prompt.Config, func(string) string) (agentkit.Provider, error)

// Executor runs and records stateless inference calls.
type Executor struct {
	store         CallStore
	gate          *admit.Gate
	buildProvider ProviderFactory
	getenv        func(string) string
}

func NewExecutor(store CallStore, gate *admit.Gate, build ProviderFactory, getenv func(string) string) *Executor {
	return &Executor{store: store, gate: gate, buildProvider: build, getenv: getenv}
}

// CompleteHandler returns the synchronous, tool-less completion endpoint.
func (e *Executor) CompleteHandler() http.Handler { return http.HandlerFunc(e.complete) }

func (e *Executor) complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method must be POST")
		return
	}

	var req Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCompleteBody))
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, decodeError(err))
		return
	}
	if err := requireEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateEnvelope(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg, err := prompt.ValidateConfig(req.promptConfig(), e.getenv)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	routeProvider, wireModel, entry, ok := catalog.Resolve(cfg.Provider, cfg.Model)
	if !ok || entry.Pricing == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("provider %q does not route model %q", cfg.Provider, cfg.Model))
		return
	}
	prov, err := e.buildProvider(cfg, e.getenv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create provider: "+err.Error())
		return
	}
	release, err := e.gate.AcquireCall(r.Context(), routeProvider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "acquire provider call: "+err.Error())
		return
	}
	defer release()

	history := make([]agentkit.Message, 0, len(req.Messages)-1)
	for _, message := range req.Messages[:len(req.Messages)-1] {
		history = append(history, agentkit.Message{
			Role:   agentkit.Role(message.Role),
			Blocks: []agentkit.Block{agentkit.TextBlock{Text: message.Text}},
		})
	}
	conv := &agentkit.Conversation{
		Provider: prov,
		Model:    wireModel,
		Pricing:  entry.Pricing,
		System:   req.System,
		Gen:      genSettings(cfg),
		Retry:    retryPolicy(cfg),
		History:  history,
	}
	stream := conv.Send(r.Context(), req.Messages[len(req.Messages)-1].Text)
	var text string
	for event := range stream.Events() {
		if done, ok := event.(agentkit.MessageDone); ok {
			text = messageText(done.Message)
		}
	}
	streamErr := stream.Err()
	usage, cost := stream.Usage(), stream.Cost()
	_ = conv.Close()

	requestJSON, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal request: "+err.Error())
		return
	}
	usageJSON, err := json.Marshal(usage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal usage: "+err.Error())
		return
	}
	callID := ids.NewULID()
	attempt := req.Attempt
	if attempt == 0 {
		attempt = 1
	}
	row := calls.Row{
		ID: callID, Class: calls.ClassCompletion, Origin: req.Origin, Name: req.Name,
		GroupID: req.GroupID, Attempt: attempt, Provider: routeProvider, Model: req.Model,
		InputTokens:  usage.InputUncached + usage.CacheReadInput + usage.CacheWriteInput,
		OutputTokens: usage.Output + usage.ReasoningOutput, TotalTokens: usage.Total,
		UsageJSON: string(usageJSON), CostUSD: cost.USD(), RequestBody: stringPointer(string(requestJSON)),
		ResponseBody: stringPointer(text),
	}
	if streamErr != nil {
		row.Error = streamErr.Error()
	}
	// Recording is part of the durable result and must still run when the
	// provider ended because the request context was cancelled.
	if err := e.store.Insert(context.WithoutCancel(r.Context()), row); err != nil {
		writeError(w, http.StatusInternalServerError, "record completion: "+err.Error())
		return
	}
	if streamErr != nil {
		writeError(w, http.StatusBadGateway, streamErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, response{CallID: callID, Text: text, Usage: usage, CostUSD: cost.USD()})
}

func (r Request) promptConfig() prompt.Config {
	return prompt.Config{
		Provider: r.Provider, Model: r.Model,
		Temperature: r.Config.Temperature, TopP: r.Config.TopP, MaxTokens: r.Config.MaxTokens,
		Effort: r.Config.Effort, ThinkingBudget: r.Config.ThinkingBudget,
		ThinkingLevel: r.Config.ThinkingLevel, Thinking: r.Config.Thinking,
		MaxAttempts: r.Config.MaxAttempts, BaseDelay: r.Config.BaseDelay,
		MaxDelay: r.Config.MaxDelay, MaxElapsed: r.Config.MaxElapsed,
		IgnoreRetryAfter: r.Config.IgnoreRetryAfter, BaseURL: r.Config.BaseURL,
	}
}

func validateEnvelope(req Request) error {
	if err := calls.ValidateOrigin(req.Origin); err != nil {
		return err
	}
	if err := calls.ValidateName(req.Name); err != nil {
		return err
	}
	if len(req.Messages) == 0 {
		return errors.New("messages must be non-empty")
	}
	for i, message := range req.Messages {
		if message.Role != string(agentkit.RoleUser) && message.Role != string(agentkit.RoleAssistant) {
			return fmt.Errorf("messages[%d].role must be user or assistant", i)
		}
	}
	if req.Messages[len(req.Messages)-1].Role != string(agentkit.RoleUser) {
		return errors.New("final message role must be user")
	}
	return nil
}

func decodeError(err error) string {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return "request body exceeds 10 MiB"
	}
	return "invalid JSON body: " + err.Error()
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON body: multiple values")
		}
		return errors.New(decodeError(err))
	}
	return nil
}

func messageText(message agentkit.Message) string {
	var text string
	for _, block := range message.Blocks {
		if block, ok := block.(agentkit.TextBlock); ok {
			text += block.Text
		}
	}
	return text
}

func genSettings(cfg prompt.Config) agentkit.GenSettings {
	gen := agentkit.GenSettings{Temperature: cfg.Temperature, TopP: cfg.TopP, MaxTokens: cfg.MaxTokens}
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
	policy := agentkit.RetryPolicy{MaxAttempts: cfg.MaxAttempts, IgnoreRetryAfter: cfg.IgnoreRetryAfter}
	parseDuration(cfg.BaseDelay, &policy.BaseDelay)
	parseDuration(cfg.MaxDelay, &policy.MaxDelay)
	parseDuration(cfg.MaxElapsed, &policy.MaxElapsed)
	return policy
}

func parseDuration(raw string, dst *time.Duration) {
	if duration, err := time.ParseDuration(raw); raw != "" && err == nil {
		*dst = duration
	}
}

func stringPointer(value string) *string { return &value }

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
