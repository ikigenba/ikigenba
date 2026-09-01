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
	encode     func(RequestState) ([]byte, error)
	decode     func(iter.Seq2[[]byte, error]) iter.Seq2[Event, error]
	classifier wireClassifier
}

func (wire *testWire) EncodeRequest(state RequestState) ([]byte, error) {
	if wire.encode != nil {
		return wire.encode(state)
	}
	return []byte(state.Model), nil
}

func (wire *testWire) DecodeStream(frames iter.Seq2[[]byte, error]) iter.Seq2[Event, error] {
	if wire.decode != nil {
		return wire.decode(frames)
	}
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
			if !yield(string(frame), nil) {
				return
			}
		}
	}
}

func (*testWire) RenderTools([]Tool) (json.RawMessage, error) { return nil, nil }
func (*testWire) ReservedKeys() []string                      { return nil }
func (wire *testWire) withClassifier(classifier wireClassifier) WireFormat {
	clone := *wire
	clone.classifier = classifier
	return &clone
}

func TestComposedProviderMutatesBeforeAuthWithFinalBodyState(t *testing.T) {
	// R-3DFK-H0PM
	// R-3ENG-USGB
	// R-0VXJ-I2FW
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "original-context")
	order := make([]string, 0, 2)
	endpoint, err := NewEndpoint(
		"https://original.test/v1",
		authFunc(func(authCtx context.Context, request *http.Request, body []byte) error {
			order = append(order, "auth")
			if authCtx.Value(contextKey{}) != "original-context" || request.Context() != ctx {
				t.Fatal("auth did not receive the original request context")
			}
			if string(body) != "mutated-final-body" || request.URL.String() != "http://redirected.test/models/moved-model" || request.Header.Get("X-Redirected") != "yes" {
				t.Fatalf("auth saw request %s and body %q before mutation", request.URL, body)
			}
			request.Header.Set("Authorization", "signed-final-body")
			return nil
		}),
		WithHeader("X-Static", "present"),
		WithMutator(func(request *http.Request, body *[]byte) error {
			order = append(order, "mutate")
			request.URL.Scheme = "http"
			request.URL.Host = "redirected.test"
			request.URL.Path = "/models/moved-model"
			request.Header.Set("X-Redirected", "yes")
			*body = []byte("mutated-final-body")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	provider := newComposedProvider(&testWire{}, endpoint, Identity{Endpoint: "fixture", Model: "moved-model"})
	request, err := provider.BuildRequest(ctx, RequestState{Model: "encoded-original"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"mutate", "auth"}) {
		t.Fatalf("hook order = %v", order)
	}
	if request.Header.Get("X-Static") != "present" || request.Header.Get("Authorization") != "signed-final-body" {
		t.Fatalf("assembled headers = %v", request.Header)
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
	if string(body) != "mutated-final-body" || !bytes.Equal(body, replayBody) || request.ContentLength != int64(len(body)) {
		t.Fatalf("body=%q replay=%q length=%d", body, replayBody, request.ContentLength)
	}
}

func TestComposedProviderPropagatesAssemblyFailures(t *testing.T) {
	encodeFailure := errors.New("encode failed")
	mutateFailure := errors.New("mutate failed")
	authFailure := errors.New("auth failed")
	checks := []struct {
		wire    *testWire
		auth    AuthApplier
		options []EndpointOption
		want    error
	}{
		{wire: &testWire{encode: func(RequestState) ([]byte, error) { return nil, encodeFailure }}, want: encodeFailure},
		{wire: &testWire{}, options: []EndpointOption{WithMutator(func(*http.Request, *[]byte) error { return mutateFailure })}, want: mutateFailure},
		{wire: &testWire{}, auth: authFunc(func(context.Context, *http.Request, []byte) error { return authFailure }), want: authFailure},
	}
	for _, check := range checks {
		auth := check.auth
		if auth == nil {
			auth = authFunc(func(context.Context, *http.Request, []byte) error { return nil })
		}
		endpoint, err := NewEndpoint("https://example.test", auth, check.options...)
		if err != nil {
			t.Fatal(err)
		}
		_, err = newComposedProvider(check.wire, endpoint, Identity{}).BuildRequest(context.Background(), RequestState{})
		if !errors.Is(err, check.want) {
			t.Fatalf("BuildRequest error = %v, want %v", err, check.want)
		}
	}
}

func TestComposedProviderUsesEndpointFramingAndMessageDecode(t *testing.T) {
	framerCalled := false
	endpoint, err := NewEndpoint(
		"https://example.test",
		authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
		WithFramer(func(reader io.Reader) iter.Seq2[[]byte, error] {
			framerCalled = true
			payload, readErr := io.ReadAll(reader)
			return func(yield func([]byte, error) bool) {
				if readErr != nil {
					yield(nil, readErr)
					return
				}
				for _, frame := range bytes.Split(payload, []byte("|")) {
					if !yield(frame, nil) {
						return
					}
				}
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	provider := newComposedProvider(&testWire{}, endpoint, Identity{})
	response := &http.Response{Body: io.NopCloser(strings.NewReader("first|second"))}
	var events []Event
	for event, decodeErr := range provider.Decode(context.Background(), response) {
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		events = append(events, event)
	}
	if !framerCalled || !reflect.DeepEqual(events, []Event{"first", "second"}) {
		t.Fatalf("framerCalled=%v events=%v", framerCalled, events)
	}
}

func TestComposedProviderClassificationIsExactAndTyped(t *testing.T) {
	// R-3FVD-8K70
	typed := &Error{Category: CategoryRateLimit, Status: http.StatusTooManyRequests, Message: "slow down"}
	headers := http.Header{"Retry-After": []string{"7"}}
	body := []byte("body-only-code")
	var gotStatus int
	var gotHeaders http.Header
	var gotBody []byte
	endpoint, err := NewEndpoint(
		"https://example.test",
		authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
		WithClassifier(func(status int, header http.Header, classifiedBody []byte) error {
			gotStatus, gotHeaders, gotBody = status, header, classifiedBody
			return typed
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	provider := newComposedProvider(&testWire{}, endpoint, Identity{})
	if classified := provider.Classify(http.StatusTooManyRequests, headers, body); !errors.Is(classified, typed) {
		t.Fatalf("Classify returned %p, want authoritative %p", classified, typed)
	}
	if gotStatus != http.StatusTooManyRequests || gotHeaders.Get("Retry-After") != "7" || !bytes.Equal(gotBody, body) {
		t.Fatalf("classifier inputs = %d %v %q", gotStatus, gotHeaders, gotBody)
	}

	response := &http.Response{Body: io.NopCloser(strings.NewReader("data: error-frame\n\n"))}
	for _, decodeErr := range provider.Decode(context.Background(), response) {
		if !errors.Is(decodeErr, typed) {
			t.Fatalf("in-band error = %v, want typed error", decodeErr)
		}
		return
	}
	t.Fatal("in-band 2xx frame did not reach endpoint classifier")
}
