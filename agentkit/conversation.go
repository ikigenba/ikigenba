package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Conversation binds a provider and its stable identity to a growing
// transcript.
type Conversation struct {
	provider  Provider
	client    *http.Client
	identity  Identity
	history   History
	settings  Settings
	options   ProviderOptions
	tools     []Tool
	deferred  []DeferredGroup
	output    *OutputContract
	loaded    []string
	validate  func() error
	eventSink eventSink
}

// Config is the construction-time configuration of a Conversation: everything a
// consumer supplies that is not the provider, the model, or the transport. It is
// a plain value; a zero Config means no tools, vendor-default settings, no
// pass-through options, no structured output, and no log. The constructor copies
// it, so later mutation of the caller's slices and maps has no effect.
type Config struct {
	Tools    []Tool
	Deferred []DeferredGroup
	Settings Settings
	Options  ProviderOptions
	Output   *OutputContract
	Log      *Log
}

// DeferredGroup is a named, on-demand bundle of tools. Its Blurb is the
// one-line description shown in the deferred-tool catalog.
type DeferredGroup struct {
	Name  string
	Blurb string
	Tools []Tool
}

// NewConversation constructs a conversation driven by provider. The provider,
// its identity, and the HTTP client cannot be reassigned after construction.
func NewConversation(provider Provider, client *http.Client, cfg Config) *Conversation {
	conversation := &Conversation{
		provider: provider,
		client:   client,
		identity: provider.Identity(),
		settings: cloneSettings(cfg.Settings),
		options:  cloneProviderOptions(cfg.Options),
		tools:    cloneTools(cfg.Tools),
		deferred: cloneDeferredGroups(cfg.Deferred),
		output:   cloneOutputContract(cfg.Output),
	}
	if cfg.Log != nil {
		conversation.eventSink = cfg.Log
	}
	return conversation
}

// Send drives one turn: it appends the user blocks, calls the model, runs any
// tool round-trips to completion, and returns a Stream of message-granular
// events (D13). Provider, endpoint, and model are fixed for the conversation's
// life; only the transcript grows. Send is the one verb — multimodal input
// arrives as additional Block variants, never as a second method.
func (c *Conversation) Send(ctx context.Context, blocks ...Block) *Stream {
	turn := c.snapshotTurn(blocks)
	return &Stream{drive: func(yield func(Event) bool) error {
		log, _ := c.eventSink.(*Log)
		if log.isClosed() {
			return ErrClosed
		}
		log.start(c.identity)
		accounting := turnAccounting{allWireCosts: true}
		var terminal error
		defer func() {
			log.recordError(terminal)
			log.finish(accounting.usage, resolveCost(c.identity.Model, accounting.usage, accounting.wireCost(), accounting.pricing))
		}()
		orchestrator, err := c.prepareOrchestrator()
		if err != nil {
			terminal = invalidConfigError(c.identity, err)
			return terminal
		}
		terminal = c.driveTurn(ctx, orchestrator, turn, yield, &accounting)
		return terminal
	}}
}

type turnAccounting struct {
	usage        Usage
	wireAmount   int64
	wireRounds   int
	rounds       int
	allWireCosts bool
	pricing      map[string]Pricing
}

func (a *turnAccounting) add(round providerAccounting) {
	a.rounds++
	a.usage = addUsage(a.usage, round.usage)
	if round.wireAmount == nil {
		a.allWireCosts = false
	} else {
		a.wireAmount += *round.wireAmount
		a.wireRounds++
	}
	if round.pricing != nil {
		a.pricing = round.pricing
	}
}

func (a *turnAccounting) wireCost() *int64 {
	if a.rounds == 0 || !a.allWireCosts || a.wireRounds != a.rounds {
		return nil
	}
	return &a.wireAmount
}

type providerAccounting struct {
	usage      Usage
	wireAmount *int64
	pricing    map[string]Pricing
}

type accountingProvider interface {
	turnAccounting() providerAccounting
}

func (c *Conversation) prepareOrchestrator() (*orchestrator, error) {
	orchestrator := newOrchestrator(c.tools, c.deferred, &c.loaded)
	if err := validateToolSet(orchestrator.inventory); err != nil {
		return nil, err
	}
	if err := c.validateConfig(); err != nil {
		return nil, err
	}
	return orchestrator, nil
}

type turnSnapshot struct {
	baseHistory History
	turn        History
	settings    Settings
	options     ProviderOptions
}

func (c *Conversation) snapshotTurn(blocks []Block) turnSnapshot {
	return turnSnapshot{
		baseHistory: cloneHistory(c.history),
		turn:        History{{Role: RoleUser, Blocks: cloneBlocks(blocks)}},
		settings:    cloneSettings(c.settings),
		options:     cloneProviderOptions(c.options),
	}
}

func (c *Conversation) driveTurn(ctx context.Context, orchestrator *orchestrator, snapshot turnSnapshot, yield func(Event) bool, accounting *turnAccounting) error {
	for {
		candidate := append(cloneHistory(snapshot.baseHistory), cloneHistory(snapshot.turn)...)
		roundEvents, completed, err := c.roundTrip(ctx, RequestState{
			Model:    c.identity.Model,
			History:  candidate,
			Settings: cloneSettings(snapshot.settings),
			Options:  cloneProviderOptions(snapshot.options),
			Tools:    orchestrator.advertisedSnapshot(),
		}, yield)
		if provider, ok := c.provider.(accountingProvider); ok {
			accounting.add(provider.turnAccounting())
		} else {
			accounting.add(providerAccounting{})
		}
		if !completed {
			return err
		}
		if err != nil {
			return err
		}

		assistant, calls := completedAssistantMessages(roundEvents)
		snapshot.turn = append(snapshot.turn, assistant...)
		if len(calls) == 0 {
			c.history = append(c.history, cloneHistory(snapshot.turn)...)
			return nil
		}

		results := make([]Block, 0, len(calls))
		for _, call := range calls {
			result := orchestrator.dispatch(ctx, call)
			results = append(results, result)
			if !publishEvent(c.eventSink, yield, ToolReturn{Result: result}) {
				return nil
			}
		}
		toolMessage := Message{Role: RoleTool, Blocks: results}
		snapshot.turn = append(snapshot.turn, toolMessage)
	}
}

func (c *Conversation) validateConfig() error {
	if c.output != nil {
		if err := ValidateOutputSchema(c.output.Schema); err != nil {
			return fmt.Errorf("output schema: %w", err)
		}
		if c.output.MaxAttempts < 0 {
			return fmt.Errorf("output MaxAttempts must not be negative: %d", c.output.MaxAttempts)
		}
	}
	if c.validate != nil {
		if err := c.validate(); err != nil {
			return err
		}
	}
	reservedProvider, ok := c.provider.(interface{ reservedKeys() []string })
	if !ok || len(c.options) == 0 {
		return nil
	}
	for _, key := range reservedProvider.reservedKeys() {
		if _, collision := c.options[key]; collision {
			return fmt.Errorf("%w: ProviderOptions key %q is reserved", ErrInvalidConfig, key)
		}
	}
	return nil
}

func cloneOutputContract(contract *OutputContract) *OutputContract {
	if contract == nil {
		return nil
	}
	clone := *contract
	clone.Schema = append(json.RawMessage(nil), contract.Schema...)
	return &clone
}

func (c *Conversation) roundTrip(ctx context.Context, state RequestState, yield func(Event) bool) ([]Event, bool, error) {
	request, err := c.buildRequest(ctx, state)
	if err != nil {
		return nil, true, wrapProviderError(err, CategoryUnknown, 0, c.identity)
	}

	response, err := c.execute(request)
	if err != nil {
		return nil, false, wrapProviderError(err, CategoryTransport, 0, c.identity)
	}

	return c.consumeResponse(ctx, response, yield)
}

func (c *Conversation) buildRequest(ctx context.Context, state RequestState) (*http.Request, error) {
	return c.provider.BuildRequest(ctx, state)
}

func completedAssistantMessages(events []Event) (History, []ToolUse) {
	var messages History
	var calls []ToolUse
	for _, event := range events {
		completed, ok := event.(MessageDone)
		if !ok || completed.Message.Role != RoleAssistant {
			continue
		}
		message := completed.Message
		cloned := Message{Role: message.Role, Blocks: cloneBlocks(message.Blocks)}
		messages = append(messages, cloned)
		for _, block := range cloned.Blocks {
			if call, ok := block.(ToolUse); ok {
				calls = append(calls, call)
			}
		}
	}
	return messages, calls
}

func cloneHistory(history History) History {
	clone := make(History, len(history))
	for index, message := range history {
		clone[index] = Message{Role: message.Role, Blocks: cloneBlocks(message.Blocks)}
	}
	return clone
}

func cloneBlocks(blocks []Block) []Block {
	clone := make([]Block, len(blocks))
	for index, block := range blocks {
		switch value := block.(type) {
		case Text:
			value.Provider = append([]byte(nil), value.Provider...)
			clone[index] = value
		case Reasoning:
			value.Provider = append([]byte(nil), value.Provider...)
			clone[index] = value
		case ToolUse:
			value.Input = append([]byte(nil), value.Input...)
			value.Provider = append([]byte(nil), value.Provider...)
			clone[index] = value
		case ToolResult:
			value.Provider = append([]byte(nil), value.Provider...)
			clone[index] = value
		}
	}
	return clone
}

func cloneProviderOptions(options ProviderOptions) ProviderOptions {
	if options == nil {
		return nil
	}
	clone := make(ProviderOptions, len(options))
	for key, value := range options {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func cloneTools(tools []Tool) []Tool {
	return append([]Tool(nil), tools...)
}

func cloneDeferredGroups(groups []DeferredGroup) []DeferredGroup {
	clone := make([]DeferredGroup, len(groups))
	for index, group := range groups {
		clone[index] = group
		clone[index].Tools = cloneTools(group.Tools)
	}
	return clone
}

func (c *Conversation) execute(request *http.Request) (*http.Response, error) {
	if c.client == nil {
		return nil, errors.New("agentkit: nil HTTP client")
	}

	return c.client.Do(request)
}

func (c *Conversation) consumeResponse(ctx context.Context, response *http.Response, yield func(Event) bool) ([]Event, bool, error) {
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			contextual := fmt.Errorf("provider error response body ended before it could be read completely: %w", readErr)
			return nil, true, wrapProviderError(contextual, CategoryUnknown, response.StatusCode, c.identity)
		}

		classified := c.provider.Classify(response.StatusCode, response.Header, body)
		return nil, true, wrapProviderError(classified, CategoryUnknown, response.StatusCode, c.identity)
	}

	var events []Event
	for event, decodeErr := range c.provider.Decode(ctx, response) {
		if decodeErr != nil {
			// Provider.Decode exposes one canonical terminal error channel. Preserve
			// that seam as CategoryUnknown without inferring provider-private detail.
			contextual := fmt.Errorf("provider response decoding ended before completion: %w", decodeErr)
			return events, true, wrapProviderError(contextual, CategoryUnknown, response.StatusCode, c.identity)
		}
		events = append(events, event)
		if !publishEvent(c.eventSink, yield, event) {
			return events, false, nil
		}
		if completed, ok := event.(MessageDone); ok {
			for _, block := range completed.Message.Blocks {
				use, ok := block.(ToolUse)
				if ok && !publishEvent(c.eventSink, yield, ToolCall{Use: use}) {
					return events, false, nil
				}
			}
		}
	}

	return events, true, nil
}
