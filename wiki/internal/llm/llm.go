// Package llm defines the injectable LLM boundary for later wiki phases.
package llm

import "context"

// Message is one provider-neutral chat message.
type Message struct {
	Role    string
	Content string
}

// Provider is the narrow seam domain packages consume.
type Provider interface {
	Complete(ctx context.Context, messages []Message) (string, error)
}
