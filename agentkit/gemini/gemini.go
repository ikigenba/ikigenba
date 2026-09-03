package gemini

import (
	"fmt"
	"net/url"

	"github.com/ikigenba/ikigenba/agentkit"
)

type config struct {
	baseURL      string
	conversation agentkit.Config
}

// Option configures a Gemini conversation.
type Option func(*config) error

// WithConfig supplies the construction-time conversation configuration.
func WithConfig(cfg agentkit.Config) Option {
	return func(configuration *config) error {
		configuration.conversation = cfg
		return nil
	}
}

// WithBaseURL overrides the Gemini endpoint. raw must be a valid absolute
// HTTP(S) URL.
func WithBaseURL(raw string) Option {
	return func(configuration *config) error {
		configuration.baseURL = raw
		return nil
	}
}

// New constructs a Gemini conversation from a Gemini credential and model.
func New(credential Credential, model string, options ...Option) (*agentkit.Conversation, error) {
	if credential == nil {
		return nil, fmt.Errorf("%w: nil Gemini credential", agentkit.ErrInvalidConfig)
	}
	configuration := config{baseURL: "https://generativelanguage.googleapis.com/v1beta/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse"}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil Gemini option", agentkit.ErrInvalidConfig)
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	endpoint, err := agentkit.NewEndpoint(configuration.baseURL, authAdapter{credential})
	if err != nil {
		return nil, err
	}
	return agentkit.NewForWire(agentkit.KnownWireGemini, endpoint, model, configuration.conversation)
}
