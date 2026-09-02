package xai

import (
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultResponsesURL = "https://api.x.ai/v1/responses"

// API selects an xAI API surface.
type API int

const (
	// Responses selects the xAI Responses API and is the default.
	Responses API = iota
	// ChatCompletions selects the xAI Chat Completions API.
	ChatCompletions
)

type config struct {
	baseURL         string
	hasBaseOverride bool
	api             API
	conversation    agentkit.Config
}

// Option configures an xAI conversation.
type Option func(*config) error

// WithConfig supplies the construction-time conversation configuration.
func WithConfig(cfg agentkit.Config) Option {
	return func(configuration *config) error {
		configuration.conversation = cfg
		return nil
	}
}

// WithBaseURL overrides the xAI API-key endpoint. raw must be a valid absolute
// HTTP(S) URL.
func WithBaseURL(raw string) Option {
	return func(configuration *config) error {
		configuration.baseURL = raw
		configuration.hasBaseOverride = true
		return nil
	}
}

// WithAPI selects the xAI API surface.
func WithAPI(api API) Option {
	return func(configuration *config) error {
		configuration.api = api
		return nil
	}
}

// New constructs an xAI conversation from an xAI credential and model.
func New(credential Credential, model string, options ...Option) (*agentkit.Conversation, error) {
	if credential == nil {
		return nil, fmt.Errorf("%w: nil xAI credential", agentkit.ErrInvalidConfig)
	}
	configuration, err := buildConfig(options)
	if err != nil {
		return nil, err
	}
	if err := validateConfig(credential, configuration); err != nil {
		return nil, err
	}
	wire, baseURL, err := selectWire(configuration)
	if err != nil {
		return nil, err
	}
	endpoint, err := agentkit.NewEndpoint(baseURL, authAdapter{credential})
	if err != nil {
		return nil, err
	}
	return agentkit.NewForWire(wire, endpoint, model, configuration.conversation)
}

func buildConfig(options []Option) (config, error) {
	configuration := config{baseURL: defaultResponsesURL}
	for _, option := range options {
		if option == nil {
			return config{}, fmt.Errorf("%w: nil xAI option", agentkit.ErrInvalidConfig)
		}
		if err := option(&configuration); err != nil {
			return config{}, err
		}
	}
	return configuration, nil
}

func validateConfig(credential Credential, configuration config) error {
	if _, baking := credential.(interface{ bakesTransport() }); baking && configuration.hasBaseOverride {
		return fmt.Errorf("%w: xAI OAuth and WithBaseURL are mutually exclusive", agentkit.ErrInvalidConfig)
	}
	return nil
}

func selectWire(configuration config) (agentkit.KnownWire, string, error) {
	switch configuration.api {
	case Responses:
		return agentkit.KnownWireOpenAIResponses, configuration.baseURL, nil
	case ChatCompletions:
		if !configuration.hasBaseOverride {
			configuration.baseURL = "https://api.x.ai/v1/chat/completions"
		}
		return agentkit.KnownWireOpenAIChat, configuration.baseURL, nil
	default:
		return 0, "", fmt.Errorf("%w: unsupported xAI API %d", agentkit.ErrInvalidConfig, configuration.api)
	}
}
