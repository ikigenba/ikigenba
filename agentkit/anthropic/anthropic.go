// Package anthropic constructs conversations using Anthropic credentials.
package anthropic

import (
	"fmt"

	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultBaseURL = "https://api.anthropic.com/v1/messages"

// API selects an Anthropic API surface.
type API int

const (
	// Messages selects the Anthropic Messages API and is the default.
	Messages API = iota
	// TextCompletions names the legacy API for which no built-in wire ships.
	TextCompletions
)

type config struct {
	baseURL         string
	hasBaseOverride bool
	api             API
	conversation    agentkit.Config
}

// WithAPI selects the Anthropic API surface.
func WithAPI(api API) Option {
	return func(configuration *config) error {
		configuration.api = api
		return nil
	}
}

// Option configures an Anthropic conversation.
type Option func(*config) error

// WithConfig supplies the construction-time conversation configuration.
func WithConfig(cfg agentkit.Config) Option {
	return func(configuration *config) error {
		configuration.conversation = cfg
		return nil
	}
}

// WithBaseURL overrides the Anthropic API-key endpoint. raw must be a valid
// absolute HTTP(S) URL.
func WithBaseURL(raw string) Option {
	return func(configuration *config) error {
		configuration.baseURL = raw
		configuration.hasBaseOverride = true
		return nil
	}
}

// New constructs an Anthropic conversation from an Anthropic credential.
func New(credential Credential, model string, options ...Option) (*agentkit.Conversation, error) {
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
	if configuration.api != Messages {
		return nil, fmt.Errorf("%w: unsupported Anthropic API %d", agentkit.ErrInvalidConfig, configuration.api)
	}
	endpoint, err := agentkit.NewEndpoint(configuration.baseURL, authAdapter{credential})
	if err != nil {
		return nil, err
	}
	return agentkit.NewForWire(agentkit.KnownWireAnthropicMessages, endpoint, model, configuration.conversation)
}
