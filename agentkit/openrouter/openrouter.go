package openrouter

import (
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultChatURL = "https://openrouter.ai/api/v1/chat/completions"

// API selects an OpenRouter API surface.
type API int

const (
	// ChatCompletions selects Chat Completions and is the default.
	ChatCompletions API = iota
	// Responses selects the Responses API.
	Responses
)

type config struct {
	baseURL         string
	hasBaseOverride bool
	api             API
}

// Option configures an OpenRouter conversation.
type Option func(*config) error

// WithBaseURL overrides the OpenRouter endpoint.
func WithBaseURL(raw string) Option {
	return func(configuration *config) error {
		configuration.baseURL = raw
		configuration.hasBaseOverride = true
		return nil
	}
}

// WithAPI selects the OpenRouter API surface.
func WithAPI(api API) Option {
	return func(configuration *config) error {
		configuration.api = api
		return nil
	}
}

// New constructs an OpenRouter conversation from an OpenRouter credential and model.
func New(credential Credential, model string, options ...Option) (*agentkit.Conversation, error) {
	if credential == nil {
		return nil, fmt.Errorf("%w: nil OpenRouter credential", agentkit.ErrInvalidConfig)
	}
	configuration := config{baseURL: defaultChatURL}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil OpenRouter option", agentkit.ErrInvalidConfig)
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	wire := agentkit.KnownWireOpenAIChat
	switch configuration.api {
	case ChatCompletions:
	case Responses:
		wire = agentkit.KnownWireOpenAIResponses
		if !configuration.hasBaseOverride {
			configuration.baseURL = "https://openrouter.ai/api/v1/responses"
		}
	default:
		return nil, fmt.Errorf("%w: unsupported OpenRouter API %d", agentkit.ErrInvalidConfig, configuration.api)
	}
	endpoint, err := agentkit.NewEndpoint(configuration.baseURL, authAdapter{credential})
	if err != nil {
		return nil, err
	}
	return agentkit.NewForWire(wire, endpoint, model)
}
