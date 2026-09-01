package agentkit

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Endpoint is a public, opaque, option-built transport description. Construct
// it with NewEndpoint; its fields are unexported.
type Endpoint struct {
	config endpointConfig
}

// EndpointOption configures an Endpoint at construction. Options may fail.
type EndpointOption func(*endpointConfig) error

// AuthApplier carries a credential onto a fully assembled request.
type AuthApplier interface {
	Apply(ctx context.Context, req *http.Request, body []byte) error
}

// RequestMutator rewrites the assembled request and its body before auth.
type RequestMutator func(req *http.Request, body *[]byte) error

// ErrorClassifier classifies a provider response from its complete transport
// inputs.
type ErrorClassifier func(status int, header http.Header, body []byte) error

type modelPlacement uint8

const (
	modelInBody modelPlacement = iota
	modelInPath
)

type endpointConfig struct {
	baseURL        *url.URL
	headers        http.Header
	framer         Framer
	classifier     ErrorClassifier
	mutator        RequestMutator
	auth           AuthApplier
	modelPlacement modelPlacement
	client         *http.Client
}

// WithHeader adds a static request header.
func WithHeader(name, value string) EndpointOption {
	return func(config *endpointConfig) error {
		if !validHeaderName(name) || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%w: invalid endpoint header", ErrInvalidConfig)
		}
		config.headers.Add(name, value)
		return nil
	}
}

// WithFramer replaces the endpoint's default SSE response framing.
func WithFramer(f Framer) EndpointOption {
	return func(config *endpointConfig) error {
		if f == nil {
			return fmt.Errorf("%w: nil endpoint framer", ErrInvalidConfig)
		}
		config.framer = f
		return nil
	}
}

// WithClassifier installs the endpoint's authoritative error classifier.
func WithClassifier(classifier ErrorClassifier) EndpointOption {
	return func(config *endpointConfig) error {
		if classifier == nil {
			return fmt.Errorf("%w: nil endpoint classifier", ErrInvalidConfig)
		}
		config.classifier = classifier
		return nil
	}
}

// WithMutator installs the endpoint's single request-mutation hook.
func WithMutator(mutator RequestMutator) EndpointOption {
	return func(config *endpointConfig) error {
		if mutator == nil {
			return fmt.Errorf("%w: nil endpoint mutator", ErrInvalidConfig)
		}
		config.mutator = mutator
		return nil
	}
}

// WithHTTPClient selects the client used to execute requests for this endpoint.
func WithHTTPClient(client *http.Client) EndpointOption {
	return func(config *endpointConfig) error {
		if client == nil {
			return fmt.Errorf("%w: nil endpoint HTTP client", ErrInvalidConfig)
		}
		config.client = client
		return nil
	}
}

// NewEndpoint builds an Endpoint from its required base URL and auth applier.
func NewEndpoint(baseURL string, auth AuthApplier, options ...EndpointOption) (Endpoint, error) {
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Endpoint{}, fmt.Errorf("%w: invalid endpoint base URL %q", ErrInvalidConfig, baseURL)
	}
	if auth == nil {
		return Endpoint{}, fmt.Errorf("%w: nil auth applier", ErrInvalidConfig)
	}
	config := endpointConfig{
		baseURL:    parsed,
		headers:    make(http.Header),
		framer:     SSEFrames,
		classifier: func(int, http.Header, []byte) error { return nil },
		mutator:    func(*http.Request, *[]byte) error { return nil },
		auth:       auth,
		client:     http.DefaultClient,
	}
	for _, option := range options {
		if option == nil {
			return Endpoint{}, fmt.Errorf("%w: nil endpoint option", ErrInvalidConfig)
		}
		if err := option(&config); err != nil {
			return Endpoint{}, err
		}
	}
	return Endpoint{config: config}, nil
}

func withModelPlacement(placement modelPlacement) EndpointOption {
	return func(config *endpointConfig) error {
		if placement != modelInBody && placement != modelInPath {
			return fmt.Errorf("%w: invalid model placement", ErrInvalidConfig)
		}
		config.modelPlacement = placement
		return nil
	}
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	const separators = "()<>@,;:\\\"/[]?={} \t"
	for _, character := range name {
		if character < 33 || character > 126 || strings.ContainsRune(separators, character) {
			return false
		}
	}
	return true
}
