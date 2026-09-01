package openai

import (
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultBaseURL = "https://api.openai.com/v1/responses"

type config struct {
	baseURL         string
	hasBaseOverride bool
}

// Option configures an OpenAI conversation.
type Option func(*config) error

// WithBaseURL overrides the OpenAI API-key endpoint.
func WithBaseURL(raw string) Option {
	return func(configuration *config) error {
		configuration.baseURL = raw
		configuration.hasBaseOverride = true
		return nil
	}
}

// New constructs an OpenAI conversation from an OpenAI credential.
func New(credential Credential, options ...Option) (*agentkit.Conversation, error) {
	if credential == nil {
		return nil, fmt.Errorf("%w: nil OpenAI credential", agentkit.ErrInvalidConfig)
	}
	configuration := config{baseURL: defaultBaseURL}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil OpenAI option", agentkit.ErrInvalidConfig)
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	if _, baking := credential.(interface{ bakesTransport() }); baking && configuration.hasBaseOverride {
		return nil, fmt.Errorf("%w: OpenAI subscription and WithBaseURL are mutually exclusive", agentkit.ErrInvalidConfig)
	}
	return agentkit.NewKnownWireConversation(agentkit.KnownWireOpenAIResponses, configuration.baseURL, authAdapter{credential})
}
