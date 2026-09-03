package openai

import (
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultBaseURL = "https://api.openai.com/v1/responses"

// API selects an OpenAI API surface.
type API int

const (
	// Responses selects the OpenAI Responses API and is the default.
	Responses API = iota
	// ChatCompletions selects the OpenAI Chat Completions API.
	ChatCompletions
)

type config struct {
	baseURL         string
	hasBaseOverride bool
	api             API
	conversation    agentkit.Config
}

// WithAPI selects the OpenAI API surface.
func WithAPI(api API) Option {
	return func(configuration *config) error {
		configuration.api = api
		return nil
	}
}

// Option configures an OpenAI conversation.
type Option func(*config) error

// WithConfig supplies the construction-time conversation configuration.
func WithConfig(cfg agentkit.Config) Option {
	return func(configuration *config) error {
		configuration.conversation = cfg
		return nil
	}
}

// WithBaseURL overrides the OpenAI API-key endpoint. raw must be a valid
// absolute HTTP(S) URL.
func WithBaseURL(raw string) Option {
	return func(configuration *config) error {
		configuration.baseURL = raw
		configuration.hasBaseOverride = true
		return nil
	}
}

// New constructs an OpenAI conversation from an OpenAI credential.
func New(credential Credential, model string, options ...Option) (*agentkit.Conversation, error) {
	if credential == nil {
		return nil, fmt.Errorf("%w: nil OpenAI credential", agentkit.ErrInvalidConfig)
	}
	configuration, err := buildConfig(credential, options)
	if err != nil {
		return nil, err
	}
	wire, err := selectWire(&configuration)
	if err != nil {
		return nil, err
	}
	endpoint, err := agentkit.NewEndpoint(configuration.baseURL, authAdapter{credential}, agentkit.WithName(string(agentkit.ProviderOpenAI)))
	if err != nil {
		return nil, err
	}
	return agentkit.NewForWire(wire, endpoint, model, configuration.conversation)
}

func buildConfig(credential Credential, options []Option) (config, error) {
	configuration := config{baseURL: defaultBaseURL}
	for _, option := range options {
		if option == nil {
			return config{}, fmt.Errorf("%w: nil OpenAI option", agentkit.ErrInvalidConfig)
		}
		if err := option(&configuration); err != nil {
			return config{}, err
		}
	}
	if _, baking := credential.(interface{ bakesTransport() }); baking && configuration.hasBaseOverride {
		return config{}, fmt.Errorf("%w: OpenAI OAuth and WithBaseURL are mutually exclusive", agentkit.ErrInvalidConfig)
	}
	return configuration, nil
}

func selectWire(configuration *config) (agentkit.KnownWire, error) {
	wire := agentkit.KnownWireOpenAIResponses
	switch configuration.api {
	case Responses:
	case ChatCompletions:
		wire = agentkit.KnownWireOpenAIChat
		if !configuration.hasBaseOverride {
			configuration.baseURL = "https://api.openai.com/v1/chat/completions"
		}
	default:
		return 0, fmt.Errorf("%w: unsupported OpenAI API %d", agentkit.ErrInvalidConfig, configuration.api)
	}
	return wire, nil
}
