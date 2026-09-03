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

// Known wire names accepted by NewForWire.
const (
	KnownWireAnthropicMessages KnownWire = iota
	KnownWireOpenAIResponses
	KnownWireOpenAIChat
	KnownWireGemini
)

// NewForWire constructs a conversation from a known wire, caller-built
// endpoint, and verbatim model name.
func NewForWire(wireName KnownWire, endpoint Endpoint, model string, cfg Config) (*Conversation, error) {
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

	endpointIdentity := endpoint.config.baseURL.String()
	if named, ok := endpoint.config.auth.(interface{ EndpointIdentity() string }); ok {
		endpointIdentity = named.EndpointIdentity()
	}
	identity := Identity{Endpoint: endpointIdentity, AuthMode: "custom", Model: model}
	return newEndpointConversation(wire, endpoint, identity, cfg), nil
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

func newComposedProvider(wire WireFormat, endpoint Endpoint, identity Identity) *composedProvider {
	return &composedProvider{wire: wire, endpoint: endpoint, identity: identity}
}

func newEndpointConversation(wire WireFormat, endpoint Endpoint, identity Identity, cfg Config) *Conversation {
	provider := newComposedProvider(wire, endpoint, identity)
	conversation := NewConversation(provider, http.DefaultClient, cfg)
	conversation.validate = func() error {
		return provider.validateSettings(conversation.settings)
	}
	return conversation
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
	frames := SSEFrames(response.Body)
	return provider.wire.DecodeStream(frames)
}

func (provider *composedProvider) Classify(status int, header http.Header, body []byte) error {
	if classifier, ok := provider.wire.(interface {
		classifyResponse(int, http.Header, []byte) error
	}); ok {
		return classifier.classifyResponse(status, header, body)
	}
	return nil
}

func (provider *composedProvider) Identity() Identity { return provider.identity }

func (provider *composedProvider) turnAccounting() providerAccounting {
	return providerAccounting{usage: takeBuiltInWireUsage(provider.wire)}
}

// takeBuiltInWireUsage adapts the package's concrete codecs to conversation
// accounting without widening WireFormat or adding an accounting method to the
// codec seam.
func takeBuiltInWireUsage(wire WireFormat) Usage {
	var codec *wireCodec
	switch wire := wire.(type) {
	case *anthropicWire:
		codec = &wire.wireCodec
	case *openAIResponsesWire:
		codec = &wire.wireCodec
	case *openAIChatWire:
		codec = &wire.wireCodec
	case *geminiWire:
		codec = &wire.wireCodec
	default:
		return Usage{}
	}
	usage := codec.lastUsage
	codec.lastUsage = Usage{}
	return usage
}

func (provider *composedProvider) reservedKeys() []string {
	return provider.wire.ReservedKeys()
}

func (provider *composedProvider) validateSettings(settings Settings) error {
	validator, ok := provider.wire.(interface{ validateSettings(Settings) error })
	if !ok {
		if settingsAreZero(settings) {
			return nil
		}
		return fmt.Errorf("%w: selected wire does not declare settings capabilities", ErrInvalidConfig)
	}
	return validator.validateSettings(settings)
}

func synchronizeRequestBody(request *http.Request, body []byte) {
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	bodyCopy := append([]byte(nil), body...)
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyCopy)), nil
	}
}
