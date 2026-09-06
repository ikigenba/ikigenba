// Package session resolves configuration and opens agent conversations.
package session

import (
	"context"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Config describes the choices and dependencies needed to open a session.
type Config struct {
	Provider   string
	Model      string
	Wire       string
	Auth       string
	AuthFile   string
	BaseURL    string
	SystemFile string
	Settings   map[string]string
	Home       string
	Getenv     func(string) string
	Root       string
	Log        *agentkit.Log
}

// Plan records every decision made before a session is opened.
type Plan struct {
	Offering agentkit.Offering
	Model    string
	AuthMode agentkit.AuthMode
	EnvVar   string
	AuthFile string
	BaseURL  string
}

// Session is an opened agent conversation and its resolved plan.
type Session struct {
	plan         Plan
	conversation *agentkit.Conversation
}

// Plan returns the decisions used to open the session.
func (s *Session) Plan() Plan {
	return s.plan
}

// Send starts a turn containing one text block.
func (s *Session) Send(ctx context.Context, prompt string) *agentkit.Stream {
	return s.conversation.Send(ctx, agentkit.Text{Text: prompt})
}
