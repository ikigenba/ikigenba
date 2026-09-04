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
}

// NewEndpoint builds an Endpoint from its required base URL and authenticator.
func NewEndpoint(baseURL string, auth Authenticator) (Endpoint, error) {
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Endpoint{}, fmt.Errorf("%w: invalid endpoint base URL %q", ErrInvalidConfig, baseURL)
	}
	if auth == nil {
		return Endpoint{}, fmt.Errorf("%w: nil auth applier", ErrInvalidConfig)
	}
	return Endpoint{config: endpointConfig{baseURL: parsed, auth: auth}}, nil
}
