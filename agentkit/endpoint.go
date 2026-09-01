package agentkit

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Endpoint is an opaque description of the transport surrounding a wire
// format. Endpoints are assembled from EndpointOption values by provider
// constructors.
type Endpoint struct {
	config endpointConfig
}

// EndpointOption configures an Endpoint.
type EndpointOption func(*endpointConfig) error

// AuthApplier applies authentication to a fully assembled request.
type AuthApplier interface {
	Apply(ctx context.Context, req *http.Request, body []byte) error
}

// RequestMutator reshapes a request and, when necessary, its encoded body.
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
}

type noAuth struct{}

func (noAuth) Apply(context.Context, *http.Request, []byte) error { return nil }

// WithBaseURL sets the endpoint base URL, including any fixed path.
func WithBaseURL(raw string) EndpointOption {
	return func(config *endpointConfig) error {
		parsed, err := url.ParseRequestURI(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%w: invalid endpoint base URL %q", ErrInvalidConfig, raw)
		}
		config.baseURL = parsed
		return nil
	}
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

func newEndpoint(options ...EndpointOption) (Endpoint, error) {
	config := endpointConfig{
		headers:    make(http.Header),
		framer:     SSEFrames,
		classifier: func(int, http.Header, []byte) error { return nil },
		mutator:    func(*http.Request, *[]byte) error { return nil },
		auth:       noAuth{},
	}
	for _, option := range options {
		if option == nil {
			return Endpoint{}, fmt.Errorf("%w: nil endpoint option", ErrInvalidConfig)
		}
		if err := option(&config); err != nil {
			return Endpoint{}, err
		}
	}
	if config.baseURL == nil {
		return Endpoint{}, fmt.Errorf("%w: endpoint base URL is required", ErrInvalidConfig)
	}
	return Endpoint{config: config}, nil
}

func withAuth(auth AuthApplier) EndpointOption {
	return func(config *endpointConfig) error {
		if auth == nil {
			return fmt.Errorf("%w: nil auth applier", ErrInvalidConfig)
		}
		config.auth = auth
		return nil
	}
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
