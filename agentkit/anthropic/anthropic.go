// Package anthropic constructs conversations using Anthropic credentials.
package anthropic

import (
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultBaseURL = "https://api.anthropic.com/v1/messages"

type config struct {
	baseURL         string
	hasBaseOverride bool
}

// Option configures an Anthropic conversation.
type Option func(*config) error

// WithBaseURL overrides the Anthropic API-key endpoint.
func WithBaseURL(raw string) Option {
	return func(configuration *config) error {
		configuration.baseURL = raw
		configuration.hasBaseOverride = true
		return nil
	}
}

// New constructs an Anthropic conversation from an Anthropic credential.
func New(credential Credential, options ...Option) (*agentkit.Conversation, error) {
	if credential == nil {
		return nil, fmt.Errorf("%w: nil Anthropic credential", agentkit.ErrInvalidConfig)
	}
	configuration := config{baseURL: defaultBaseURL}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil Anthropic option", agentkit.ErrInvalidConfig)
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	if _, baking := credential.(interface{ bakesTransport() }); baking && configuration.hasBaseOverride {
		return nil, fmt.Errorf("%w: Anthropic OAuth and WithBaseURL are mutually exclusive", agentkit.ErrInvalidConfig)
	}
	return agentkit.NewKnownWireConversation(agentkit.KnownWireAnthropic, configuration.baseURL, authAdapter{credential})
}
