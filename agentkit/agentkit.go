// Package agentkit provides the stable conversation seam used by LLM provider
// adapters.
package agentkit

// Event is one message-granular item decoded from a provider response. Concrete
// event variants are defined with the streaming vocabulary in a later phase.
type Event any

// RequestState is the immutable snapshot supplied to a Provider for one
// round-trip.
type RequestState struct {
	Model    string
	History  History
	Settings Settings
}

// Stream is the result of advancing a conversation. Its event-facing API is
// completed with the streaming state machine in a later phase.
type Stream struct {
	events []Event
	err    error
}
