package agentkit

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
)

// KnownWire identifies a built-in wire format available to the generic route.
type KnownWire int

// Known wire names accepted by NewKnownWireConversation.
const (
	KnownWireAnthropicMessages KnownWire = iota
	KnownWireOpenAIResponses
	KnownWireOpenAIChat
	KnownWireGemini
)

// NewKnownWireConversation constructs the generic custom-endpoint route. Its
// authentication input is deliberately the runtime AuthApplier seam rather
// than either vendor package's sealed credential type.
func NewKnownWireConversation(wireName KnownWire, baseURL string, auth AuthApplier) (*Conversation, error) {
	return newKnownWireConversation(wireName, baseURL, "", auth)
}

// NewKnownWireModelConversation constructs a conversation from an established
// wire while retaining the vendor constructor's required model.
func NewKnownWireModelConversation(wireName KnownWire, baseURL, model string, auth AuthApplier) (*Conversation, error) {
	return newKnownWireConversation(wireName, baseURL, model, auth)
}

func newKnownWireConversation(wireName KnownWire, baseURL, model string, auth AuthApplier) (*Conversation, error) {
	var wire WireFormat
	switch wireName {
	case KnownWireAnthropicMessages:
		wire = newAnthropicWire(nil)
	case KnownWireOpenAIResponses:
		wire = newOpenAIResponsesWire(nil)
	case KnownWireOpenAIChat:
		wire = newOpenAIChatWire(nil)
	case KnownWireGemini:
		wire = newGeminiWire(nil)
	default:
		return nil, fmt.Errorf("%w: unknown wire format %d", ErrInvalidConfig, wireName)
	}

	endpoint, err := NewEndpoint(baseURL, auth)
	if err != nil {
		return nil, err
	}
	identity := Identity{Endpoint: baseURL, AuthMode: "custom", Model: model}
	return newEndpointConversation(wire, endpoint, identity), nil
}

// Provider is the composed wire-format and endpoint adapter driven for one
// round-trip.
type Provider interface {
	BuildRequest(ctx context.Context, state RequestState) (*http.Request, error)
	Decode(ctx context.Context, resp *http.Response) iter.Seq2[Event, error]
	Classify(status int, header http.Header, body []byte) error
	Identity() Identity
}

type composedProvider struct {
	wire     WireFormat
	endpoint Endpoint
	identity Identity
}

func newComposedProvider(wire WireFormat, endpoint Endpoint, identity Identity) Provider {
	if classifiable, ok := wire.(interface {
		withClassifier(wireClassifier) WireFormat
	}); ok {
		wire = classifiable.withClassifier(endpoint.config.classifier)
	}
	return &composedProvider{wire: wire, endpoint: endpoint, identity: identity}
}

func newEndpointConversation(wire WireFormat, endpoint Endpoint, identity Identity) *Conversation {
	return NewConversation(newComposedProvider(wire, endpoint, identity), endpoint.config.client)
}

func (provider *composedProvider) BuildRequest(ctx context.Context, state RequestState) (*http.Request, error) {
	body, err := provider.wire.EncodeRequest(state)
	if err != nil {
		return nil, err
	}
	body = append([]byte(nil), body...)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint.config.baseURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header = provider.endpoint.config.headers.Clone()
	if err := provider.endpoint.config.mutator(request, &body); err != nil {
		return nil, err
	}
	if body == nil {
		body = []byte{}
	}
	synchronizeRequestBody(request, body)
	if err := provider.endpoint.config.auth.Apply(ctx, request, body); err != nil {
		return nil, err
	}
	return request, nil
}

func (provider *composedProvider) Decode(_ context.Context, response *http.Response) iter.Seq2[Event, error] {
	if response == nil || response.Body == nil {
		return func(yield func(Event, error) bool) {
			yield(nil, fmt.Errorf("agentkit: response body is required"))
		}
	}
	frames := provider.endpoint.config.framer(response.Body)
	return provider.wire.DecodeStream(frames)
}

func (provider *composedProvider) Classify(status int, header http.Header, body []byte) error {
	return provider.endpoint.config.classifier(status, header, body)
}

func (provider *composedProvider) Identity() Identity { return provider.identity }

func synchronizeRequestBody(request *http.Request, body []byte) {
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	bodyCopy := append([]byte(nil), body...)
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyCopy)), nil
	}
}
