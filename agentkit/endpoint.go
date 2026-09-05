package agentkit

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Endpoint is an opaque transport description: base URL plus authenticator.
// Construct it with NewEndpoint; its fields are unexported.
type Endpoint struct {
	config endpointConfig
}

// Authenticator authenticates a fully assembled request. It sees the final
// body so a body-signing scheme can sign it; it takes a context so an OAuth
// refresh can run and fail. Obtained from Offering.Authenticator.
type Authenticator interface {
	Authenticate(ctx context.Context, req *http.Request, body []byte) error
}

// errorClassifier classifies a provider response from its complete transport
// inputs.
type errorClassifier func(status int, header http.Header, body []byte) error

type endpointConfig struct {
	baseURL *url.URL
	auth    Authenticator

	// overrideBaseURL/overrideSet hold a WithBaseURL argument between option
	// application and final URL resolution; they never survive into the
	// constructed Endpoint's config.
	overrideBaseURL string
	overrideSet     bool
}

// EndpointOption adjusts NewEndpoint. WithBaseURL is the only one.
type EndpointOption func(*endpointConfig)

// NewEndpoint builds an Endpoint from an authenticator obtained from
// Offering.Authenticator. Without WithBaseURL the URL is the BaseURL of the
// offering's EndpointSpec for the authenticator's AuthMode.
func NewEndpoint(auth Authenticator, opts ...EndpointOption) (Endpoint, error) {
	if auth == nil {
		return Endpoint{}, fmt.Errorf("%w: nil auth applier", ErrInvalidConfig)
	}

	var cfg endpointConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	rawURL := cfg.overrideBaseURL
	if !cfg.overrideSet {
		if defaulter, ok := auth.(endpointDefaultBaseURL); ok {
			rawURL = defaulter.defaultBaseURL()
		}
	}

	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Endpoint{}, fmt.Errorf("%w: invalid endpoint base URL %q", ErrInvalidConfig, rawURL)
	}
	return Endpoint{config: endpointConfig{baseURL: parsed, auth: auth}}, nil
}

// WithBaseURL replaces the URL the authenticator's offering would supply.
// When given more than once, the last call wins.
func WithBaseURL(rawURL string) EndpointOption {
	return func(cfg *endpointConfig) {
		cfg.overrideBaseURL = rawURL
		cfg.overrideSet = true
	}
}

// endpointDefaultBaseURL is implemented by the authenticators
// Offering.Authenticator returns; it names the BaseURL of the EndpointSpec
// that matched the authenticator's rotator mode.
type endpointDefaultBaseURL interface {
	defaultBaseURL() string
}
