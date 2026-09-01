// Package agentkit provides the stable conversation seam used by LLM provider
// adapters.
package agentkit

import "encoding/json"

// Event is one message-granular item decoded from a provider response. Concrete
// event variants are defined with the streaming vocabulary in a later phase.
type Event any

// ProviderOptions is an untyped, wire-specific escape hatch merged shallowly at
// the top level of the request body. agentkit enumerates no keys; each wire and
// endpoint declares the keys it reserves, and a consumer key colliding with a
// reserved one fails at Send. There is no override.
type ProviderOptions map[string]json.RawMessage

// RequestState is the immutable input the Provider consumes for one round-trip.
// It is a snapshot: History and Tools reflect this round-trip only, while Model,
// Settings, and Options are fixed for the conversation.
type RequestState struct {
	Model    string
	History  []Message
	Settings Settings
	Options  ProviderOptions
	Tools    []Tool
}

// Stream is the result of advancing a conversation. Its event-facing API is
// completed with the streaming state machine in a later phase.
type Stream struct {
	events []Event
	err    error
}

// Err reports the terminal error that ended the turn, if any.
func (s *Stream) Err() error {
	if s == nil {
		return nil
	}
	return s.err
}
