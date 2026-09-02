// Package agentkit provides the stable conversation seam used by LLM provider
// adapters.
package agentkit

import (
	"encoding/json"
	"iter"
)

// Event is one thing that happened during a turn, at message granularity. It is
// a sealed union of MessageDone, ToolCall, ToolReturn, and OutputDone.
type Event interface {
	isEvent()
}

// MessageDone reports that the model finished one assistant message.
type MessageDone struct {
	Message Message
}

// ToolCall reports that the model requested a tool.
type ToolCall struct {
	Use ToolUse
}

// ToolReturn reports that the orchestrator ran a tool and is feeding the result
// back to the model.
type ToolReturn struct {
	Result ToolResult
}

func (MessageDone) isEvent() {}
func (ToolCall) isEvent()    {}
func (ToolReturn) isEvent()  {}
func (OutputDone) isEvent()  {}

// ProviderOptions is an untyped, wire-specific escape hatch merged shallowly at
// the top level of the request body. agentkit enumerates no keys; each wire and
// endpoint declares the keys it reserves, and a consumer key colliding with a
// reserved one fails at Send. There is no override.
type ProviderOptions map[string]json.RawMessage

// RequestState is the immutable input the Provider consumes for one round-trip.
// It is a snapshot: History and Tools reflect this round-trip only, while Model,
// Settings, Options, and Output are fixed for the conversation.
type RequestState struct {
	Model    string
	History  []Message
	Settings Settings
	Options  ProviderOptions
	Tools    []Tool
	Output   *OutputContract
}

// Stream is the live view of one turn's events.
type Stream struct {
	drive    func(func(Event) bool) error
	err      error
	consumed bool
}

// Events returns the turn's events in order. A Stream is single-use.
func (s *Stream) Events() iter.Seq[Event] {
	return func(yield func(Event) bool) {
		if s == nil || s.consumed {
			return
		}
		s.consumed = true
		if s.drive != nil {
			s.err = s.drive(yield)
			s.drive = nil
		}
	}
}

// Err reports the terminal error that ended the turn, if any.
func (s *Stream) Err() error {
	if s == nil {
		return nil
	}
	return s.err
}
