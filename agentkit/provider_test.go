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

func (function authFunc) Apply(ctx context.Context, request *http.Request, body []byte) error {
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
func (*testWire) ReservedKeys() []string                      { return nil }
func (wire *testWire) classifyResponse(status int, header http.Header, body []byte) error {
	if wire.classifier == nil {
		return nil
	}
	return wire.classifier(status, header, body)
}

func TestComposedProviderAuthenticatesFinalBodyState(t *testing.T) {
	// R-0VXJ-I2FW
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "original-context")
	authCalls := 0
	endpoint, err := NewEndpoint(
		"https://original.test/v1",
		authFunc(func(authCtx context.Context, request *http.Request, body []byte) error {
			authCalls++
			if authCtx.Value(contextKey{}) != "original-context" || request.Context() != ctx {
				t.Fatal("auth did not receive the original request context")
			}
			if string(body) != "encoded-final-body" || request.URL.String() != "https://original.test/v1" {
				t.Fatalf("auth saw request %q and body %q before final assembly", request.URL, body)
			}
			request.Header.Set("Authorization", "signed-final-body")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	provider := newComposedProvider(&testWire{}, endpoint, Identity{Endpoint: "fixture", Model: "model"})
	request, err := provider.BuildRequest(ctx, requestState{Model: "encoded-final-body"})
	if err != nil {
		t.Fatal(err)
	}
	if authCalls != 1 || request.Header.Get("Authorization") != "signed-final-body" {
		t.Fatalf("assembled headers = %q", request.Header)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := request.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	replayBody, _ := io.ReadAll(replay)
	if string(body) != "encoded-final-body" || !bytes.Equal(body, replayBody) || request.ContentLength != int64(len(body)) {
		t.Fatalf("body=%q replay=%q length=%d", body, replayBody, request.ContentLength)
	}
}

func TestComposedProviderPropagatesAssemblyFailures(t *testing.T) {
	encodeFailure := errors.New("encode failed")
	authFailure := errors.New("auth failed")
	checks := []struct {
		wire *testWire
		auth AuthApplier
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
