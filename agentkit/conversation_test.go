package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fixtureWire struct {
	name string
}

type fixtureEndpoint struct {
	name string
	url  string
}

type fixtureProvider struct {
	wire               fixtureWire
	endpoint           fixtureEndpoint
	model              string
	states             []RequestState
	buildErr           error
	decodeErr          error
	classifyErr        error
	decodeCalls        int
	classifyCalls      int
	classifiedStatus   int
	classifiedHeader   http.Header
	classifiedBody     []byte
	decodeClassifyBody []byte
	classify           func(int, http.Header, []byte) error
}

func (p *fixtureProvider) BuildRequest(ctx context.Context, state RequestState) (*http.Request, error) {
	p.states = append(p.states, state)
	if p.buildErr != nil {
		return nil, p.buildErr
	}
	return http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.url, strings.NewReader(p.wire.name))
}

func (p *fixtureProvider) Decode(_ context.Context, response *http.Response) iter.Seq2[Event, error] {
	p.decodeCalls++
	return func(yield func(Event, error) bool) {
		if !yield(p.wire.name+":"+p.endpoint.name, nil) {
			return
		}
		if p.decodeClassifyBody != nil {
			yield(nil, p.Classify(response.StatusCode, nil, p.decodeClassifyBody))

			return
		}
		if p.decodeErr != nil {
			yield(nil, p.decodeErr)
		}
	}
}

func (p *fixtureProvider) Classify(status int, header http.Header, body []byte) error {
	p.classifyCalls++
	p.classifiedStatus = status
	p.classifiedHeader = header.Clone()
	p.classifiedBody = append([]byte(nil), body...)
	if p.classify != nil {
		return p.classify(status, header, body)
	}
	if p.classifyErr != nil {
		return p.classifyErr
	}
	return &Error{Category: CategoryUnknown, Status: status, Message: fmt.Sprintf("status %d", status)}
}

func (p *fixtureProvider) Identity() Identity {
	return Identity{Endpoint: p.endpoint.name, AuthMode: "fixture", Model: p.model}
}

func genericFixture(wire fixtureWire, endpoint fixtureEndpoint, model string, client *http.Client) (*Conversation, *fixtureProvider) {
	provider := &fixtureProvider{wire: wire, endpoint: endpoint, model: model}
	return NewConversation(provider, client), provider
}

func vendorFixture(endpointURL, model string, client *http.Client) (*Conversation, *fixtureProvider) {
	return genericFixture(
		fixtureWire{name: "messages"},
		fixtureEndpoint{name: "vendor", url: endpointURL},
		model,
		client,
	)
}

func TestConversationAxesAreStableAndModelIsVerbatim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		if string(body) != "messages" {
			t.Errorf("wire body = %q, want messages", body)
		}
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	unknownModel := "released-today/unknown model β"
	conversation, provider := genericFixture(
		fixtureWire{name: "messages"},
		fixtureEndpoint{name: "vendor", url: server.URL},
		unknownModel,
		server.Client(),
	)

	// R-1POH-Q9DL
	conversationType := reflect.TypeOf(conversation)
	for index := range conversationType.NumMethod() {
		if conversationType.Method(index).Name != "Send" {
			t.Fatalf("unexpected exported reassignment/API method %q", conversationType.Method(index).Name)
		}
	}

	// R-1S4A-HSUZ
	stream := conversation.Send(context.Background(), Text{Text: "text"})
	if len(provider.states) != 1 || provider.states[0].Model != unknownModel {
		t.Fatalf("provider states = %#v; model was not transmitted verbatim", provider.states)
	}
	if stream.err == nil || stream.err.Error() != "unknown: status 400 (status 400)" {
		t.Fatalf("stream error = %v, want classified vendor error status 400", stream.err)
	}
	if got := provider.Identity(); got != (Identity{Endpoint: "vendor", AuthMode: "fixture", Model: unknownModel}) {
		t.Fatalf("identity changed or fused: %#v", got)
	}
}

func TestSendValidationFailsBeforeEveryProviderBoundary(t *testing.T) {
	// R-2TX6-COUI
	// R-2V52-QGL7
	tests := []struct {
		name  string
		cause error
	}{
		{name: "reserved provider option", cause: errors.New("reserved provider option")},
		{name: "unsupported forced tool choice", cause: errors.New("wire cannot express forced tool choice")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transportCalls := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				transportCalls++
				return nil, errors.New("unexpected transport call")
			})}
			conversation, provider := vendorFixture("http://provider.invalid", "stable-model", client)
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "unchanged"}}}}
			before, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			conversation.validate = func() error { return test.cause }

			stream := conversation.Send(context.Background(), Text{Text: "must not be appended"})
			var providerError *Error
			if !errors.As(stream.err, &providerError) || providerError.Category != CategoryInvalidRequest {
				t.Fatalf("Send error = %#v, want CategoryInvalidRequest *Error", stream.err)
			}
			if !errors.Is(stream.err, ErrInvalidConfig) {
				t.Fatalf("Send error = %v, want errors.Is ErrInvalidConfig", stream.err)
			}
			if len(provider.states) != 0 || provider.decodeCalls != 0 || provider.classifyCalls != 0 || transportCalls != 0 {
				t.Fatalf("calls after invalid config: build=%d decode=%d classify=%d transport=%d", len(provider.states), provider.decodeCalls, provider.classifyCalls, transportCalls)
			}
			after, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("history changed: before=%s after=%s", before, after)
			}
		})
	}
}

func TestNonSuccessResponseUsesClassifierInputsAndResult(t *testing.T) {
	// R-2LDV-OANN
	body := []byte(`{"error":{"code":"throttled","message":"slow down"}}`)
	header := http.Header{"Retry-After": []string{"1750ms"}, "X-Fixture": []string{"exact"}}
	classified := &Error{
		Category:   CategoryRateLimit,
		Status:     http.StatusTooManyRequests,
		Code:       "throttled",
		Message:    "slow down",
		RetryAfter: 1750 * time.Millisecond,
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     header,
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	})}
	conversation, provider := vendorFixture("http://provider.invalid", "model", client)
	provider.classify = func(int, http.Header, []byte) error { return classified }

	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	if reflect.ValueOf(stream.err).Pointer() != reflect.ValueOf(classified).Pointer() {
		t.Fatalf("terminal error = %#v, want classifier result %#v unchanged", stream.err, classified)
	}
	if provider.classifiedStatus != http.StatusTooManyRequests || !reflect.DeepEqual(provider.classifiedHeader, header) || !bytes.Equal(provider.classifiedBody, body) {
		t.Fatalf("classifier inputs = (%d, %#v, %q), want (%d, %#v, %q)", provider.classifiedStatus, provider.classifiedHeader, provider.classifiedBody, http.StatusTooManyRequests, header, body)
	}
}

func TestTransportFailureIsWrappedWithStableIdentity(t *testing.T) {
	// R-2K5Z-AIWY
	cause := errors.New("connection refused")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}
	conversation, provider := vendorFixture("http://provider.invalid", "original-model", client)
	wantIdentity := provider.Identity()
	provider.model = "changed-after-construction"

	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	var providerError *Error
	if reflect.TypeOf(stream.err) != reflect.TypeOf(providerError) || !errors.As(stream.err, &providerError) {
		t.Fatalf("transport error type = %T, want *Error", stream.err)
	}
	if providerError.Category != CategoryTransport || providerError.Status != 0 || providerError.Endpoint != wantIdentity {
		t.Fatalf("transport error = %#v, want transport/status 0/identity %#v", providerError, wantIdentity)
	}
	if !errors.Is(providerError, cause) {
		t.Fatalf("transport error does not wrap original cause: %v", providerError)
	}
}

func TestDecodeCanUseClassifierForInBandErrorAfterHTTP200(t *testing.T) {
	// R-2P1K-TLVQ
	frame := []byte(`{"error":{"code":"busy","message":"try later"}}`)
	classified := &Error{
		Category: CategoryOverloaded,
		Status:   http.StatusOK,
		Code:     "busy",
		Message:  "try later",
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("stream bytes")),
		}, nil
	})}
	conversation, provider := vendorFixture("http://provider.invalid", "model", client)
	provider.decodeClassifyBody = frame
	provider.classify = func(int, http.Header, []byte) error { return classified }

	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	if len(stream.events) != 1 || stream.events[0] != "messages:vendor" {
		t.Fatalf("events before terminal error = %#v", stream.events)
	}
	if reflect.ValueOf(stream.err).Pointer() != reflect.ValueOf(classified).Pointer() {
		t.Fatalf("terminal error = %#v, want in-band classifier result %#v", stream.err, classified)
	}
	if provider.classifyCalls != 1 || provider.classifiedStatus != http.StatusOK || provider.classifiedHeader != nil || !bytes.Equal(provider.classifiedBody, frame) {
		t.Fatalf("in-band classifier inputs/calls = %d, (%d, %#v, %q)", provider.classifyCalls, provider.classifiedStatus, provider.classifiedHeader, provider.classifiedBody)
	}
}

func TestSendIsSoleVerbAndAcceptsDifferentBlockVariants(t *testing.T) {
	// R-1TC6-VKLO
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	conversation, provider := vendorFixture(server.URL, "model", server.Client())

	conversation.Send(context.Background(), Text{Text: "text"}, ToolUse{ID: "call", Name: "image"})
	if len(provider.states) != 1 || len(provider.states[0].History) != 1 || len(provider.states[0].History[0].Blocks) != 2 {
		t.Fatalf("Send did not carry both block variants: %#v", provider.states)
	}
	if got := reflect.TypeOf(conversation).NumMethod(); got != 1 {
		t.Fatalf("Conversation has %d exported methods, want only Send", got)
	}
}

func TestVendorAndGenericRoutesAreEquivalent(t *testing.T) {
	// R-1UK3-9CCD
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	model := "any-new-model"
	vendorConversation, vendorProvider := vendorFixture(server.URL, model, server.Client())
	genericConversation, genericProvider := genericFixture(
		fixtureWire{name: "messages"},
		fixtureEndpoint{name: "vendor", url: server.URL},
		model,
		server.Client(),
	)

	vendorStream := vendorConversation.Send(context.Background(), Text{Text: "hello"})
	genericStream := genericConversation.Send(context.Background(), Text{Text: "hello"})
	if !reflect.DeepEqual(vendorProvider.states, genericProvider.states) {
		t.Fatalf("construction routes produced different states: vendor=%#v generic=%#v", vendorProvider.states, genericProvider.states)
	}
	if !reflect.DeepEqual(vendorStream, genericStream) {
		t.Fatalf("construction routes produced different streams: vendor=%#v generic=%#v", vendorStream, genericStream)
	}
}

func TestSendSnapshotPreservesPayloadAndCommitsCompleteUserTurn(t *testing.T) {
	// R-1ZFO-SFB5
	// R-25J6-PA0M
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	conversation, provider := vendorFixture(server.URL, "model", server.Client())
	payload := json.RawMessage(` {"signature":"bytes stay opaque"} `)

	stream := conversation.Send(context.Background(), Text{Text: "hello", Provider: payload})
	if stream.err != nil {
		t.Fatal(stream.err)
	}
	want := History{{Role: RoleUser, Blocks: []Block{Text{Text: "hello", Provider: payload}}}}
	if !reflect.DeepEqual(provider.states[0].History, want) {
		t.Fatalf("provider snapshot = %#v, want %#v", provider.states[0].History, want)
	}
	gotPayload := provider.states[0].History[0].Blocks[0].(Text).Provider
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("provider payload = %q, want byte-identical %q", gotPayload, payload)
	}
	if !reflect.DeepEqual(conversation.history, want) {
		t.Fatalf("committed history = %#v, want one complete user turn %#v", conversation.history, want)
	}
}

func TestSendFailuresLeaveHistoryUnchanged(t *testing.T) {
	// R-25J6-PA0M
	type failureCase struct {
		name   string
		status int
		setup  func(*fixtureProvider, *http.Client)
	}
	cases := []failureCase{
		{name: "build", status: http.StatusOK, setup: func(provider *fixtureProvider, _ *http.Client) {
			provider.buildErr = errors.New("build failed")
		}},
		{name: "transport", status: http.StatusOK, setup: func(_ *fixtureProvider, client *http.Client) {
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport failed") })
		}},
		{name: "classification", status: http.StatusBadRequest, setup: func(provider *fixtureProvider, _ *http.Client) {
			provider.classifyErr = errors.New("classification failed")
		}},
		{name: "decode", status: http.StatusOK, setup: func(provider *fixtureProvider, _ *http.Client) {
			provider.decodeErr = errors.New("decode failed")
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			client := server.Client()
			conversation, provider := vendorFixture(server.URL, "model", client)
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
			before, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			test.setup(provider, client)

			stream := conversation.Send(context.Background(), Text{Text: "do not commit"})
			if stream.err == nil {
				t.Fatal("Send error = nil, want terminal failure")
			}
			after, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("history changed on failure: before=%s after=%s", before, after)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
