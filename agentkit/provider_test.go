package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type authFunc func(context.Context, *http.Request, []byte) error

func (function authFunc) Authenticate(ctx context.Context, request *http.Request, body []byte) error {
	return function(ctx, request, body)
}

type testWire struct {
	encode     func(requestState) ([]byte, error)
	decode     func(iter.Seq2[[]byte, error]) iter.Seq2[Event, error]
	classifier errorClassifier
}

func (wire *testWire) EncodeRequest(state requestState) ([]byte, error) {
	if wire.encode != nil {
		return wire.encode(state)
	}
	return []byte(state.Model), nil
}

func (wire *testWire) DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error] {
	if wire.decode != nil {
		return wire.decode(frames)
	}
	return wire.defaultDecodeStream(frames)
}

func (wire *testWire) defaultDecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for frame, err := range frames {
			if err != nil {
				yield(nil, err)
				return
			}
			if wire.classifier != nil {
				if classifyErr := wire.classifier(http.StatusOK, nil, frame); classifyErr != nil {
					yield(nil, classifyErr)
					return
				}
			}
			event := MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: string(frame)}}}}
			if !yield(event, nil) {
				return
			}
		}
	}
}

func (*testWire) RenderTools([]Tool) (json.RawMessage, error) { return nil, nil }
func (*testWire) OptionSpecs() []OptionSpec                   { return nil }
func (wire *testWire) classifyResponse(status int, header http.Header, body []byte) error {
	if wire.classifier == nil {
		return nil
	}
	return wire.classifier(status, header, body)
}

func TestComposedProviderPropagatesAssemblyFailures(t *testing.T) {
	encodeFailure := errors.New("encode failed")
	authFailure := errors.New("auth failed")
	checks := []struct {
		wire *testWire
		auth Authenticator
		want error
	}{
		{wire: &testWire{encode: func(requestState) ([]byte, error) { return nil, encodeFailure }}, want: encodeFailure},
		{wire: &testWire{}, auth: authFunc(func(context.Context, *http.Request, []byte) error { return authFailure }), want: authFailure},
	}
	for _, check := range checks {
		auth := check.auth
		if auth == nil {
			auth = authFunc(func(context.Context, *http.Request, []byte) error { return nil })
		}
		endpoint, err := NewEndpoint("https://example.test", auth)
		if err != nil {
			t.Fatal(err)
		}
		_, err = newComposedProvider(check.wire, endpoint, Identity{}).BuildRequest(context.Background(), requestState{})
		if !errors.Is(err, check.want) {
			t.Fatalf("BuildRequest error = %v, want %v", err, check.want)
		}
	}
}

// R-U1DK-UGQI
func TestComposedProviderAuthenticatesRequestBodyBytes(t *testing.T) {
	encodedBody := []byte(`{"model":"body-signing-model","stream":true}`)
	wire := &testWire{encode: func(requestState) ([]byte, error) {
		return encodedBody, nil
	}}
	var authenticatedBody []byte
	endpoint, err := NewEndpoint("https://example.test", authFunc(func(_ context.Context, _ *http.Request, body []byte) error {
		authenticatedBody = body
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	request, err := newComposedProvider(wire, endpoint, Identity{}).BuildRequest(
		context.Background(),
		requestState{Model: "body-signing-model"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(authenticatedBody, encodedBody) {
		t.Fatalf("authenticated body = %q, want %q", authenticatedBody, encodedBody)
	}
	if request.GetBody == nil {
		t.Fatal("request.GetBody is nil")
	}
	requestBody, err := request.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	gotBody, readErr := io.ReadAll(requestBody)
	closeErr := requestBody.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if !bytes.Equal(gotBody, encodedBody) {
		t.Fatalf("request body = %q, want %q", gotBody, encodedBody)
	}
}

// R-K4E5-D036
func TestAnthropicWireSetsRequiredProtocolHeader(t *testing.T) {
	endpoint, err := NewEndpoint("https://example.test", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		wire wireFormat
		want string
	}{
		{name: "anthropic", wire: AnthropicMessagesWire(), want: "2023-06-01"},
		{name: "other wire", wire: ChatWire()},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			request, buildErr := newComposedProvider(check.wire, endpoint, Identity{}).BuildRequest(context.Background(), requestState{})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if got := request.Header.Get("anthropic-version"); got != check.want {
				t.Fatalf("anthropic-version = %q, want %q", got, check.want)
			}
		})
	}
}

func TestComposedProviderUsesSSEFramingAndMessageDecode(t *testing.T) {
	endpoint, err := NewEndpoint("https://example.test", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	provider := newComposedProvider(&testWire{}, endpoint, Identity{})
	response := &http.Response{Body: io.NopCloser(strings.NewReader("data: first\n\ndata: second\n\n"))}
	var events []Event
	for event, decodeErr := range provider.Decode(context.Background(), response) {
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		events = append(events, event)
	}
	want := []Event{
		MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "first"}}}},
		MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "second"}}}},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v", events)
	}
}
