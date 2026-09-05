package agentkit

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
)

// New constructs a conversation from a built-in wire, caller-built endpoint,
// and verbatim model name.
func New(wire WireFormat, endpoint Endpoint, model string, cfg Config) (*Conversation, error) {
	if wire == nil {
		return nil, fmt.Errorf("%w: nil wire", ErrInvalidConfig)
	}

	endpointIdentity := endpoint.config.baseURL.String()
	authMode := "custom"
	if named, ok := endpoint.config.auth.(interface{ EndpointIdentity() string }); ok {
		endpointIdentity = named.EndpointIdentity()
	}
	if moded, ok := endpoint.config.auth.(interface{ AuthMode() string }); ok {
		authMode = moded.AuthMode()
	}
	identity := Identity{Endpoint: endpointIdentity, AuthMode: authMode, Model: model}
	return newEndpointConversation(wire, endpoint, identity, cfg), nil
}

// wireProvider is the composed wire-format and endpoint adapter driven for one
// round-trip.
type wireProvider interface {
	BuildRequest(ctx context.Context, state requestState) (*http.Request, error)
	Decode(ctx context.Context, resp *http.Response) iter.Seq2[Event, error]
	Classify(status int, header http.Header, body []byte) error
	Identity() Identity
}

type composedProvider struct {
	wire     wireFormat
	endpoint Endpoint
	identity Identity
}

func newComposedProvider(wire wireFormat, endpoint Endpoint, identity Identity) *composedProvider {
	return &composedProvider{wire: wire, endpoint: endpoint, identity: identity}
}

func newEndpointConversation(wire wireFormat, endpoint Endpoint, identity Identity, cfg Config) *Conversation {
	provider := newComposedProvider(wire, endpoint, identity)
	conversation := newConversation(provider, http.DefaultClient, cfg)
	conversation.validate = func() error {
		return provider.validateSettings(conversation.settings)
	}
	return conversation
}

func (provider *composedProvider) BuildRequest(ctx context.Context, state requestState) (*http.Request, error) {
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
	request.Header.Set("Content-Type", "application/json")
	if setter, ok := provider.wire.(interface{ setProtocolHeaders(*http.Request) }); ok {
		setter.setProtocolHeaders(request)
	}
	if err := provider.endpoint.config.auth.Authenticate(ctx, request, body); err != nil {
		return nil, err
	}
	return request, nil
}

// refreshHook exposes the endpoint's auth applier's 401 hook, if it has one
// (D22): only an OAuth applier implements oauthRefreshHook.
func (provider *composedProvider) refreshHook() (oauthRefreshHook, bool) {
	hook, ok := provider.endpoint.config.auth.(oauthRefreshHook)
	return hook, ok
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
// accounting without widening wireFormat or adding an accounting method to the
// codec seam.
func takeBuiltInWireUsage(wire wireFormat) Usage {
	var codec *wireCodec
	switch wire := wire.(type) {
	case *anthropicWire:
		codec = &wire.wireCodec
	case *openAIResponsesWire:
		codec = &wire.wireCodec
	case *responsesWire:
		codec = &wire.wireCodec
	case *openAIChatWire:
		codec = &wire.wireCodec
	case *chatWire:
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

func (provider *composedProvider) validateSettings(settings Settings) error {
	validator, ok := provider.wire.(interface{ validateSettings(Settings) error })
	if !ok {
		if settingsAreZero(settings) {
			return nil
		}
		return fmt.Errorf("%w: selected wire does not declare settings capabilities", ErrInvalidConfig)
	}
	if err := validator.validateSettings(settings); err != nil {
		return err
	}
	if extra, ok := provider.wire.(interface {
		validateMaxTokens(Identity, Settings) error
	}); ok {
		return extra.validateMaxTokens(provider.identity, settings)
	}
	return nil
}

func synchronizeRequestBody(request *http.Request, body []byte) {
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	bodyCopy := append([]byte(nil), body...)
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyCopy)), nil
	}
}
