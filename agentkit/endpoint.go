package agentkit

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Endpoint is an opaque transport description: base URL plus auth applier.
// Construct it with NewEndpoint; its fields are unexported.
type Endpoint struct {
	config endpointConfig
}

// AuthApplier carries a credential onto a fully assembled request.
type AuthApplier interface {
	Apply(ctx context.Context, req *http.Request, body []byte) error
}

// ErrorClassifier classifies a provider response from its complete transport
// inputs.
type ErrorClassifier func(status int, header http.Header, body []byte) error

type endpointConfig struct {
	baseURL *url.URL
	auth    AuthApplier
}

// NewEndpoint builds an Endpoint from its required base URL and auth applier.
func NewEndpoint(baseURL string, auth AuthApplier) (Endpoint, error) {
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Endpoint{}, fmt.Errorf("%w: invalid endpoint base URL %q", ErrInvalidConfig, baseURL)
	}
	if auth == nil {
		return Endpoint{}, fmt.Errorf("%w: nil auth applier", ErrInvalidConfig)
	}
	return Endpoint{config: endpointConfig{baseURL: parsed, auth: auth}}, nil
}
