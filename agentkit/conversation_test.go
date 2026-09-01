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

func drainStream(stream *Stream) []Event {
	var events []Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events
}

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
		event := ToolCall{Use: ToolUse{Name: p.wire.name + ":" + p.endpoint.name}}
		if !yield(event, nil) {
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

func TestEndpointConversationExecutesWithDefaultHTTPClient(t *testing.T) {
	// R-YKSS-NDBC
	originalDefault := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = originalDefault })

	defaultCalls := 0
	defaultClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		defaultCalls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	http.DefaultClient = defaultClient
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	defaultEndpoint, err := NewEndpoint("https://default.test/messages", auth)
	if err != nil {
		t.Fatal(err)
	}
	if defaultEndpoint.config.client != http.DefaultClient {
		t.Fatal("endpoint did not retain http.DefaultClient as its default")
	}
	defaultConversation := newEndpointConversation(&testWire{}, defaultEndpoint, Identity{Model: "default-model"})
	drainStream(defaultConversation.Send(context.Background(), Text{Text: "hello"}))
	if defaultCalls != 1 {
		t.Fatalf("default client calls = %d, want 1", defaultCalls)
	}
}

func TestEndpointConversationExecutesWithOverrideHTTPClient(t *testing.T) {
	// R-YKSS-NDBC
	overrideCalls := 0
	overrideClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		overrideCalls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	overrideEndpoint, err := NewEndpoint("https://override.test/messages", auth, WithHTTPClient(overrideClient))
	if err != nil {
		t.Fatal(err)
	}
	if overrideEndpoint.config.client != overrideClient {
		t.Fatal("WithHTTPClient did not retain the selected client")
	}
	overrideConversation := newEndpointConversation(&testWire{}, overrideEndpoint, Identity{Model: "override-model"})
	drainStream(overrideConversation.Send(context.Background(), Text{Text: "hello"}))
	if overrideCalls != 1 {
		t.Fatalf("override client calls = %d, want 1", overrideCalls)
	}
}

type conversationAxesCapture struct {
	body []byte
	err  error
}

func newConversationAxesFixture(t *testing.T) (*Conversation, *fixtureProvider, string, *conversationAxesCapture) {
	t.Helper()
	capture := &conversationAxesCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capture.body, capture.err = io.ReadAll(request.Body)
		writer.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	unknownModel := "released-today/unknown model β"
	conversation, provider := genericFixture(
		fixtureWire{name: "messages"},
		fixtureEndpoint{name: "vendor", url: server.URL},
		unknownModel,
		server.Client(),
	)
	return conversation, provider, unknownModel, capture
}

func TestConversationExposesOnlyDeferredAndSendAsExportedMethods(t *testing.T) {
	// R-1POH-Q9DL
	conversation, _, _, _ := newConversationAxesFixture(t)
	conversationType := reflect.TypeOf(conversation)
	want := []string{"Deferred", "Send"}
	if conversationType.NumMethod() != len(want) {
		t.Fatalf("Conversation has %d exported methods, want exactly %d", conversationType.NumMethod(), len(want))
	}
	for index, name := range want {
		if got := conversationType.Method(index).Name; got != name {
			t.Fatalf("exported Conversation method %d = %q, want %q", index, got, name)
		}
	}
}

func TestConversationSendsModelVerbatim(t *testing.T) {
	// R-1S4A-HSUZ
	conversation, provider, unknownModel, capture := newConversationAxesFixture(t)
	stream := conversation.Send(context.Background(), Text{Text: "text"})
	drainStream(stream)
	if capture.err != nil {
		t.Fatal(capture.err)
	}
	if string(capture.body) != "messages" {
		t.Fatalf("wire body = %q, want messages", capture.body)
	}
	if len(provider.states) != 1 || provider.states[0].Model != unknownModel {
		t.Fatalf("provider states = %#v; model was not transmitted verbatim", provider.states)
	}
	if stream.err == nil || stream.err.Error() != "unknown: status 400 (status 400)" {
		t.Fatalf("stream error = %v, want classified vendor error status 400", stream.err)
	}
}

func TestConversationIdentityRemainsStable(t *testing.T) {
	// R-1S4A-HSUZ
	_, provider, unknownModel, _ := newConversationAxesFixture(t)
	if got := provider.Identity(); got != (Identity{Endpoint: "vendor", AuthMode: "fixture", Model: unknownModel}) {
		t.Fatalf("identity changed or fused: %#v", got)
	}
}

func TestSendValidationFailsBeforeConfiguredProviderBoundaries(t *testing.T) {
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
			drainStream(stream)
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
	drainStream(stream)
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
	drainStream(stream)
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
	events := drainStream(stream)
	wantEvent := ToolCall{Use: ToolUse{Name: "messages:vendor"}}
	if len(events) != 1 || !reflect.DeepEqual(events[0], wantEvent) {
		t.Fatalf("events before terminal error = %#v", events)
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

	drainStream(conversation.Send(context.Background(), Text{Text: "text"}, ToolUse{ID: "call", Name: "image"}))
	if len(provider.states) != 1 || len(provider.states[0].History) != 1 || len(provider.states[0].History[0].Blocks) != 2 {
		t.Fatalf("Send did not carry both block variants: %#v", provider.states)
	}
	conversationType := reflect.TypeOf(conversation)
	if got := conversationType.NumMethod(); got != 2 || conversationType.Method(0).Name != "Deferred" || conversationType.Method(1).Name != "Send" {
		t.Fatalf("Conversation exported methods changed: %v", conversationType)
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
	vendorEvents := drainStream(vendorStream)
	genericEvents := drainStream(genericStream)
	if !reflect.DeepEqual(vendorProvider.states, genericProvider.states) {
		t.Fatalf("construction routes produced different states: vendor=%#v generic=%#v", vendorProvider.states, genericProvider.states)
	}
	if !reflect.DeepEqual(vendorEvents, genericEvents) || !reflect.DeepEqual(vendorStream.Err(), genericStream.Err()) {
		t.Fatalf("construction routes produced different streams: vendor=%#v generic=%#v", vendorEvents, genericEvents)
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
	drainStream(stream)
	if stream.err != nil {
		t.Fatal(stream.err)
	}
	want := Message{Role: RoleUser, Blocks: []Block{Text{Text: "hello", Provider: payload}}}
	if !reflect.DeepEqual(provider.states[0].History, []Message{want}) {
		t.Fatalf("provider snapshot = %#v, want %#v", provider.states[0].History, []Message{want})
	}
	gotPayload := provider.states[0].History[0].Blocks[0].(Text).Provider
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("provider payload = %q, want byte-identical %q", gotPayload, payload)
	}
	if !reflect.DeepEqual(conversation.history, History{want}) {
		t.Fatalf("committed history = %#v, want one complete user turn %#v", conversation.history, History{want})
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
			drainStream(stream)
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

func TestUnsupportedSettingsFailAtStartOfSendWithoutMutation(t *testing.T) {
	// R-3S2D-29LY
	// R-3UI5-TT3C
	tests := []struct {
		name         string
		context      string
		capabilities wireCapabilities
		settings     Settings
	}{
		{
			name:         "reasoning budget",
			context:      "reasoning mode budget",
			capabilities: wireCapabilities{name: "controlled grammar", reasoning: reasoningShapeEffort, toolChoice: toolChoiceShapeTool},
			settings:     Settings{Reasoning: ReasoningConfig{Mode: ReasoningBudget, Budget: 4096}},
		},
		{
			name:         "named tool",
			context:      "tool choice tool",
			capabilities: wireCapabilities{name: "controlled grammar", reasoning: reasoningShapeBudget, toolChoice: toolChoiceShapeRequired},
			settings:     Settings{ToolChoice: ToolChoice{Mode: ToolChoiceTool, Name: "lookup"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encodeCalls := 0
			decodeCalls := 0
			classifyCalls := 0
			transportCalls := 0
			wire := boundaryWire(test.capabilities, func(RequestState) {
				encodeCalls++
			}, func() {
				decodeCalls++
			})
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				transportCalls++
				return nil, errors.New("transport must not run")
			})}
			endpoint, err := NewEndpoint(
				"https://provider.invalid/generate",
				authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
				WithHTTPClient(client),
				WithClassifier(func(int, http.Header, []byte) error {
					classifyCalls++
					return errors.New("classifier must not run")
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: "opaque-model"})
			conversation.settings = cloneSettings(test.settings)
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
			beforeHistory, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			beforeSettings := cloneSettings(conversation.settings)

			stream := conversation.Send(context.Background(), Text{Text: "not appended"})
			drainStream(stream)
			if !errors.Is(stream.err, ErrInvalidConfig) {
				t.Fatalf("Send error = %v, want ErrInvalidConfig", stream.err)
			}
			if !strings.Contains(stream.err.Error(), "controlled grammar") || !strings.Contains(stream.err.Error(), test.context) {
				t.Fatalf("Send error lacks setting/wire context: %v", stream.err)
			}
			if encodeCalls != 0 || decodeCalls != 0 || classifyCalls != 0 || transportCalls != 0 {
				t.Fatalf("boundary calls after invalid setting: encode/build=%d decode=%d classify=%d transport=%d", encodeCalls, decodeCalls, classifyCalls, transportCalls)
			}
			afterHistory, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterHistory, beforeHistory) {
				t.Fatalf("History changed: before=%s after=%s", beforeHistory, afterHistory)
			}
			if !reflect.DeepEqual(conversation.settings, beforeSettings) {
				t.Fatalf("directive was substituted or dropped: before=%#v after=%#v", beforeSettings, conversation.settings)
			}
		})
	}
}

func TestWireCapabilityDecisionIgnoresOpaqueModel(t *testing.T) {
	// R-3VQ2-7KU1
	settings := Settings{Reasoning: ReasoningConfig{Mode: ReasoningOn}}
	models := []string{"old-looking-model", "released-today/unknown:model-beta"}
	for _, model := range models {
		wire := boundaryWire(
			wireCapabilities{name: "effort-only grammar", reasoning: reasoningShapeEffort},
			func(RequestState) { t.Fatalf("model %q reached EncodeRequest after wire rejection", model) },
			func() { t.Fatalf("model %q reached Decode after wire rejection", model) },
		)
		endpoint, err := NewEndpoint("https://provider.invalid", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
		if err != nil {
			t.Fatal(err)
		}
		conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: model})
		conversation.settings = settings
		stream := conversation.Send(context.Background(), Text{Text: "hello"})
		drainStream(stream)
		if !errors.Is(stream.err, ErrInvalidConfig) {
			t.Errorf("model %q capability result = %v, want identical ErrInvalidConfig", model, stream.err)
		}
	}
}

func TestUnknownModelReachesVendorAndClassifier(t *testing.T) {
	// R-3WXY-LCKQ
	unknownModel := "released-after-agentkit/opaque:model-beta"
	settings := Settings{Reasoning: ReasoningConfig{Mode: ReasoningBudget, Budget: 2048}}
	var encodedState RequestState
	transportCalls := 0
	responseBody := []byte(`{"error":"unsupported model"}`)
	classified := &Error{Category: CategoryInvalidRequest, Status: http.StatusBadRequest, Code: "model_not_found", Message: "unsupported model"}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"X-Vendor": []string{"exact"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
		}, nil
	})}
	classifierCalls := 0
	endpoint, err := NewEndpoint(
		"https://provider.invalid/generate",
		authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
		WithHTTPClient(client),
		WithClassifier(func(status int, header http.Header, body []byte) error {
			classifierCalls++
			if status != http.StatusBadRequest || header.Get("X-Vendor") != "exact" || !bytes.Equal(body, responseBody) {
				t.Fatalf("classifier inputs = (%d, %#v, %q)", status, header, body)
			}
			return classified
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	wire := boundaryWire(
		wireCapabilities{name: "budget grammar", reasoning: reasoningShapeBudget},
		func(state RequestState) { encodedState = state },
		func() { t.Fatal("non-2xx response must not be decoded") },
	)
	conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: unknownModel})
	conversation.settings = settings

	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	drainStream(stream)
	if encodedState.Model != unknownModel || !reflect.DeepEqual(encodedState.Settings, settings) {
		t.Fatalf("wire received state %#v, want opaque model and unchanged settings %#v", encodedState, settings)
	}
	if transportCalls != 1 || classifierCalls != 1 {
		t.Fatalf("vendor boundary calls: transport=%d classifier=%d, want one each", transportCalls, classifierCalls)
	}
	if reflect.ValueOf(stream.err).Pointer() != reflect.ValueOf(classified).Pointer() {
		t.Fatalf("Send error = %#v, want classifier result unchanged %#v", stream.err, classified)
	}
}

func boundaryWire(capabilities wireCapabilities, encoded func(RequestState), decoded func()) WireFormat {
	return &boundaryTestWire{wireCodec: wireCodec{
		capabilities: capabilities,
		encode: func(state RequestState) ([]byte, error) {
			encoded(state)
			return []byte(`{}`), nil
		},
		decoder: func() frameDecoder {
			decoded()
			return func([]byte) (*Message, usageFragment, bool, error) {
				return nil, usageFragment{}, false, nil
			}
		},
	}}
}

type boundaryTestWire struct{ wireCodec }

func (*boundaryTestWire) RenderTools([]Tool) (json.RawMessage, error) { return nil, nil }

type phase15Provider struct {
	model           string
	states          []RequestState
	responses       [][]Event
	decodeErrors    []error
	decodeCalls     int
	classifyCalls   int
	classified      error
	accounting      []providerAccounting
	accountingCalls int
}

type phase15TransportStep struct {
	response *http.Response
	err      error
}

type phase15Transport struct {
	steps []phase15TransportStep
	calls int
}

func (t *phase15Transport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	if len(t.steps) == 0 {
		return nil, errors.New("unexpected transport call")
	}
	step := t.steps[0]
	t.steps = t.steps[1:]
	return step.response, step.err
}

func (p *phase15Provider) BuildRequest(ctx context.Context, state RequestState) (*http.Request, error) {
	p.states = append(p.states, cloneRequestState(state))
	if state.Options != nil {
		state.Options["mutated"] = json.RawMessage(`true`)
		for key := range state.Options {
			state.Options[key] = json.RawMessage(`null`)
			break
		}
	}
	if len(state.History) > 0 {
		state.History[0].Blocks = nil
	}
	if len(state.Tools) > 0 {
		state.Tools[0] = nil
	}
	return http.NewRequestWithContext(ctx, http.MethodPost, "https://phase15.invalid", nil)
}

func (p *phase15Provider) Decode(_ context.Context, _ *http.Response) iter.Seq2[Event, error] {
	index := p.decodeCalls
	p.decodeCalls++
	return func(yield func(Event, error) bool) {
		if index < len(p.responses) {
			for _, event := range p.responses[index] {
				if !yield(event, nil) {
					return
				}
			}
		}
		if index < len(p.decodeErrors) && p.decodeErrors[index] != nil {
			yield(nil, p.decodeErrors[index])
		}
	}
}

func (p *phase15Provider) Classify(int, http.Header, []byte) error {
	p.classifyCalls++
	return p.classified
}

func (p *phase15Provider) Identity() Identity {
	return Identity{Endpoint: "phase15", AuthMode: "fixture", Model: p.model}
}

func (p *phase15Provider) turnAccounting() providerAccounting {
	p.accountingCalls++
	index := p.decodeCalls - 1
	if index >= 0 && index < len(p.accounting) {
		return p.accounting[index]
	}
	return providerAccounting{}
}

func cloneRequestState(state RequestState) RequestState {
	return RequestState{
		Model:    state.Model,
		History:  cloneHistory(state.History),
		Settings: cloneSettings(state.Settings),
		Options:  cloneProviderOptions(state.Options),
		Tools:    cloneTools(state.Tools),
	}
}

func successfulPhase15Client(transportCalls *int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		*transportCalls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
}

type captureEventSink struct {
	records []eventRecord
}

func TestDurableLogMirrorsMultiRoundStreamAtMessageGranularity(t *testing.T) {
	// R-5ELJ-F97A
	call := ToolUse{ID: "logged-call", Name: "weather", Input: json.RawMessage(`{"city":"Oslo"}`)}
	first := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "checking"}, call}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "sunny"}}}
	provider := &phase15Provider{model: "gpt-4.1-mini", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	var output bytes.Buffer
	log := NewLog(&output, func() time.Time { return time.Date(2033, 1, 1, 0, 0, 0, 0, time.UTC) })
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.eventSink = log
	conversation.tools = []Tool{MustTool("weather", "", func(context.Context, phase15Input) (string, error) { return "sunny", nil })}

	events := drainStream(conversation.Send(context.Background(), Text{Text: "forecast"}))
	if len(events) != 4 {
		t.Fatalf("live event count = %d, want 4", len(events))
	}
	records := decodeLogRecords(t, output.Bytes())
	assertSelectedLogPayloads(t, records)
	var projected []Event
	for _, record := range records {
		switch record.Type {
		case RecordMessage:
			projected = append(projected, MessageDone{Message: *record.Message})
		case RecordToolUse:
			projected = append(projected, ToolCall{Use: *record.ToolUse})
		case RecordToolResult:
			projected = append(projected, ToolReturn{Result: *record.ToolResult})
		}
	}
	if !reflect.DeepEqual(projected, events) {
		t.Fatalf("log projection = %#v, want exact live event order %#v", projected, events)
	}
	if bytes.Contains(output.Bytes(), []byte("delta")) || len(records) != len(events)+3 || records[0].Type != RecordTurnStart || records[len(records)-2].Type != RecordUsage || records[len(records)-1].Type != RecordTurnEnd {
		t.Fatalf("log is not one message-granular line per protocol/lifecycle event: %s", output.Bytes())
	}
}

func TestLogFailureDoesNotAlterSuccessOrTerminalStreamSemantics(t *testing.T) {
	// R-5LWX-PVNG
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "kept"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.eventSink = NewLog(failingLogWriter{}, func() time.Time { return time.Time{} })
	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	if events := drainStream(stream); !reflect.DeepEqual(events, []Event{MessageDone{Message: message}}) || stream.Err() != nil {
		t.Fatalf("failing log changed successful stream: events=%#v err=%v", events, stream.Err())
	}
	if len(conversation.history) != 2 {
		t.Fatalf("failing log changed committed history: %#v", conversation.history)
	}

	terminal := errors.New("decode failed")
	provider = &phase15Provider{model: "model", decodeErrors: []error{terminal}}
	transportCalls = 0
	var output bytes.Buffer
	conversation = NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.eventSink = NewLog(&output, func() time.Time { return time.Time{} })
	stream = conversation.Send(context.Background(), Text{Text: "fail"})
	drainStream(stream)
	if !errors.Is(stream.Err(), terminal) || stream.Err().Error() != "unknown: provider response decoding ended before completion: decode failed (status 200)" || len(conversation.history) != 0 {
		t.Fatalf("terminal semantics changed: err=%v history=%#v", stream.Err(), conversation.history)
	}
	records := decodeLogRecords(t, output.Bytes())
	assertSelectedLogPayloads(t, records)
	foundError := false
	for _, record := range records {
		if record.Type == RecordError && record.Err != nil && record.Err.Message == "provider response decoding ended before completion: decode failed" {
			foundError = true
		}
	}
	if !foundError {
		t.Fatalf("terminal error record missing from %#v", records)
	}

	provider = &phase15Provider{model: "model"}
	transportCalls = 0
	output.Reset()
	cancelClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++
		return nil, context.Canceled
	})}
	conversation = NewConversation(provider, cancelClient)
	conversation.eventSink = NewLog(&output, func() time.Time { return time.Time{} })
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	stream = conversation.Send(canceled, Text{Text: "cancelled"})
	drainStream(stream)
	var providerError *Error
	if !errors.Is(stream.Err(), context.Canceled) || !errors.As(stream.Err(), &providerError) || providerError.Category != CategoryTransport {
		t.Fatalf("already-cancelled stream err = %v, want transport error wrapping context.Canceled", stream.Err())
	}
	if len(provider.states) != 1 || transportCalls != 1 || len(conversation.history) != 0 {
		t.Fatalf("already-cancelled BuildRequest/transport/history = %d/%d/%#v, want prior behavior 1/1/empty", len(provider.states), transportCalls, conversation.history)
	}
	assertSelectedLogPayloads(t, decodeLogRecords(t, output.Bytes()))
}

func TestConversationCloseSummarizesResolvedCostsAndRejectsLaterSend(t *testing.T) {
	// R-5N4U-3NE5
	// R-5OCQ-HF4U
	knownPricing := map[string]Pricing{"priced": {InputPerToken: 10, OutputPerToken: 20}}
	provider := &phase15Provider{
		model: "priced",
		responses: [][]Event{
			{MessageDone{Message: Message{Role: RoleAssistant}}},
			{MessageDone{Message: Message{Role: RoleAssistant}}},
		},
		accounting: []providerAccounting{
			{usage: Usage{InputTokens: 2, OutputTokens: 3}, pricing: knownPricing},
			{usage: Usage{CachedTokens: 5}},
		},
	}
	transportCalls := 0
	var output bytes.Buffer
	log := NewLog(&output, func() time.Time { return time.Time{} })
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.eventSink = log
	for range 2 {
		stream := conversation.Send(context.Background(), Text{Text: "turn"})
		drainStream(stream)
		if stream.Err() != nil {
			t.Fatal(stream.Err())
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	closedOutput := output.String()
	if err := log.Close(); err != nil || output.String() != closedOutput {
		t.Fatalf("idempotent Close = %v, output changed=%t", err, output.String() != closedOutput)
	}
	stream := conversation.Send(context.Background(), Text{Text: "after close"})
	drainStream(stream)
	if !errors.Is(stream.Err(), ErrClosed) || transportCalls != 2 {
		t.Fatalf("Send after Close err/calls = %v/%d, want ErrClosed/2", stream.Err(), transportCalls)
	}
	records := decodeLogRecords(t, output.Bytes())
	var usageRecords []LogRecord
	for _, record := range records {
		if record.Type == RecordUsage {
			usageRecords = append(usageRecords, record)
		}
	}
	if len(usageRecords) != 2 || usageRecords[0].Cost == nil || *usageRecords[0].Cost != (Cost{Amount: 80, Known: true}) || usageRecords[1].Cost == nil || usageRecords[1].Cost.Known {
		t.Fatalf("resolved per-turn usage costs = %#v", usageRecords)
	}
	if provider.accountingCalls != 2 {
		t.Fatalf("provider accounting calls = %d, want exactly 2", provider.accountingCalls)
	}
	summary := records[len(records)-1]
	wantUsage := Usage{InputTokens: 2, CachedTokens: 5, OutputTokens: 3}
	if summary.Type != RecordSummary || summary.Usage == nil || *summary.Usage != wantUsage || summary.Cost == nil || *summary.Cost != (Cost{Amount: 80, Known: false}) {
		t.Fatalf("cumulative summary = %#v, want usage %+v unknown cost amount 80", summary, wantUsage)
	}
}

type failingLogWriter struct{}

func (failingLogWriter) Write([]byte) (int, error) { return 0, errors.New("log disk full") }

func assertSelectedLogPayloads(t *testing.T, records []LogRecord) {
	t.Helper()
	for index, record := range records {
		present := map[string]bool{
			"identity":    record.Identity != nil,
			"message":     record.Message != nil,
			"tool_use":    record.ToolUse != nil,
			"tool_result": record.ToolResult != nil,
			"usage":       record.Usage != nil,
			"cost":        record.Cost != nil,
			"error":       record.Err != nil,
			"retry":       record.Retry != nil,
		}
		var want map[string]bool
		switch record.Type {
		case RecordTurnStart:
			want = map[string]bool{"identity": true}
		case RecordMessage:
			want = map[string]bool{"message": true}
		case RecordToolUse:
			want = map[string]bool{"tool_use": true}
		case RecordToolResult:
			want = map[string]bool{"tool_result": true}
		case RecordUsage, RecordSummary:
			want = map[string]bool{"usage": true, "cost": true}
		case RecordError:
			want = map[string]bool{"error": true}
		case RecordRetry:
			want = map[string]bool{"retry": true}
		case RecordTurnEnd:
			want = map[string]bool{}
		default:
			t.Fatalf("record %d has unknown type %q", index, record.Type)
		}
		if !reflect.DeepEqual(present, payloadPresence(want)) {
			t.Fatalf("record %d type %q payload presence = %v, want only %v", index, record.Type, present, want)
		}
	}
}

func payloadPresence(selected map[string]bool) map[string]bool {
	all := map[string]bool{
		"identity": false, "message": false, "tool_use": false,
		"tool_result": false, "usage": false, "cost": false,
		"error": false, "retry": false,
	}
	for name := range selected {
		all[name] = true
	}
	return all
}

func (sink *captureEventSink) record(record eventRecord) {
	sink.records = append(sink.records, record)
}

func TestStreamConsumptionDrivesLiveEventsInRoundTripOrder(t *testing.T) {
	// R-4ZYQ-U0AY
	// R-516N-7S1N
	// R-53MF-ZBJ1
	call := ToolUse{ID: "live-call", Name: "weather", Input: json.RawMessage(`{"city":"Oslo"}`)}
	first := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "checking"}, call}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "sunny"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	toolCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	tool := &phase17CountingTool{
		name:   "weather",
		schema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		call: func(context.Context, json.RawMessage) (string, error) {
			toolCalls++
			return "sunny", nil
		},
	}
	conversation.tools = []Tool{tool}

	stream := conversation.Send(context.Background(), Text{Text: "forecast"})
	if transportCalls != 0 || provider.decodeCalls != 0 || toolCalls != 0 || tool.schemaCalls != 0 {
		t.Fatalf("work happened before consumption: transport=%d decode=%d tool=%d schema=%d", transportCalls, provider.decodeCalls, toolCalls, tool.schemaCalls)
	}
	var events []Event
	for event := range stream.Events() {
		events = append(events, event)
		if len(events) == 1 && (transportCalls != 1 || provider.decodeCalls != 1 || toolCalls != 0) {
			t.Fatalf("first round was not delivered live: transport=%d decode=%d tool=%d", transportCalls, provider.decodeCalls, toolCalls)
		}
	}
	wantResult := ToolResult{ToolUseID: call.ID, Content: "sunny"}
	want := []Event{
		MessageDone{Message: first},
		ToolCall{Use: call},
		ToolReturn{Result: wantResult},
		MessageDone{Message: final},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want exact assistant/call/return/assistant order %#v", events, want)
	}
	if stream.Err() != nil {
		t.Fatalf("clean Stream.Err() = %v", stream.Err())
	}
}

func TestStreamEarlyStopHaltsTurnAndCannotReplay(t *testing.T) {
	first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "stop-call", Name: "weather", Input: json.RawMessage(`{"city":"Oslo"}`)}}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "must not arrive"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	toolCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.tools = []Tool{MustTool("weather", "", func(context.Context, phase15Input) (string, error) {
		toolCalls++
		return "unused", nil
	})}

	stream := conversation.Send(context.Background(), Text{Text: "stop"})
	seen := 0
	for range stream.Events() {
		seen++
		break
	}
	if replay := drainStream(stream); len(replay) != 0 {
		t.Fatalf("single-use stream replayed %#v", replay)
	}
	if seen != 1 || transportCalls != 1 || provider.decodeCalls != 1 || toolCalls != 0 {
		t.Fatalf("early stop work: seen=%d transport=%d decode=%d tool=%d", seen, transportCalls, provider.decodeCalls, toolCalls)
	}
	if len(conversation.history) != 0 {
		t.Fatalf("abandoned turn committed history %#v", conversation.history)
	}
}

func TestStreamEventsAndPrivateLogBridgeHaveExactParity(t *testing.T) {
	// R-54UC-D39Q
	call := ToolUse{ID: "parity-call", Name: "weather", Input: json.RawMessage(`{"city":"Oslo"}`)}
	first := Message{Role: RoleAssistant, Blocks: []Block{call}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	sink := &captureEventSink{}
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.eventSink = sink
	conversation.tools = []Tool{MustTool("weather", "", func(context.Context, phase15Input) (string, error) { return "sunny", nil })}

	events := drainStream(conversation.Send(context.Background(), Text{Text: "forecast"}))
	result := ToolResult{ToolUseID: call.ID, Content: "sunny"}
	wantEvents := []Event{MessageDone{Message: first}, ToolCall{Use: call}, ToolReturn{Result: result}, MessageDone{Message: final}}
	wantRecords := []eventRecord{
		{kind: eventRecordMessage, value: first},
		{kind: eventRecordToolUse, value: call},
		{kind: eventRecordToolResult, value: result},
		{kind: eventRecordMessage, value: final},
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("live events = %#v, want %#v", events, wantEvents)
	}
	if !reflect.DeepEqual(sink.records, wantRecords) {
		t.Fatalf("bridge records = %#v, want independent protocol records %#v", sink.records, wantRecords)
	}
}

type phase15Input struct {
	City string `json:"city" jsonschema:"required,minLength=2"`
}

func TestSendCompletesToolRoundTripsWithFixedClonedConfigAndOneCommit(t *testing.T) {
	// R-4NRR-0AW0
	// R-4OZN-E2MP
	// R-4Q7J-RUDE
	// R-4TV8-X5LH
	callID := "vendor::call/β-0099-not-a-local-id"
	first := Message{Role: RoleAssistant, Blocks: []Block{
		Text{Text: "checking"},
		ToolUse{ID: callID, Name: "weather", Input: json.RawMessage(`{"city":"Oslo"}`)},
	}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "clear skies"}}}
	provider := &phase15Provider{model: "fixed/model β", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	temperature := 0.25
	conversation.settings = Settings{Temperature: &temperature, StopSequences: []string{"END"}}
	conversation.options = ProviderOptions{"vendor_flag": json.RawMessage(`{"mode":"exact"}`)}
	toolCalls := 0
	weather := MustTool("weather", "look up weather", func(_ context.Context, input phase15Input) (string, error) {
		toolCalls++
		return "weather for " + input.City, nil
	})
	conversation.tools = []Tool{weather}
	prior := Message{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}
	conversation.history = History{prior}
	user := Message{Role: RoleUser, Blocks: []Block{Text{Text: "forecast"}}}
	result := Message{Role: RoleTool, Blocks: []Block{ToolResult{ToolUseID: callID, Content: "weather for Oslo"}}}

	stream := conversation.Send(context.Background(), user.Blocks...)
	events := drainStream(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if transportCalls != 2 || provider.decodeCalls != 2 || toolCalls != 1 {
		t.Fatalf("calls: transport=%d decode=%d tool=%d, want 2, 2, 1", transportCalls, provider.decodeCalls, toolCalls)
	}
	wantHistories := []History{{prior, user}, {prior, user, first, result}}
	if len(provider.states) != 2 {
		t.Fatalf("round-trip state count = %d, want 2", len(provider.states))
	}
	for index, state := range provider.states {
		if state.Model != provider.model || !reflect.DeepEqual(state.History, []Message(wantHistories[index])) ||
			!reflect.DeepEqual(state.Settings, conversation.settings) || !reflect.DeepEqual(state.Options, conversation.options) ||
			len(state.Tools) != 1 || state.Tools[0].Name() != weather.Name() || !bytes.Equal(state.Tools[0].Schema(), weather.Schema()) {
			t.Fatalf("round-trip state %d = %#v, want fixed cloned config and history %#v", index, state, wantHistories[index])
		}
	}
	wantEvents := []Event{
		MessageDone{Message: first},
		ToolCall{Use: first.Blocks[1].(ToolUse)},
		ToolReturn{Result: result.Blocks[0].(ToolResult)},
		MessageDone{Message: final},
	}
	if got := events; !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("stream events = %#v, want assistant/tool/assistant order", got)
	}
	if got := conversation.history; !reflect.DeepEqual(got, History{prior, user, first, result, final}) {
		t.Fatalf("history = %#v, want one complete turn splice", got)
	}
	if result.Blocks[0].(ToolResult).ToolUseID != callID || provider.states[1].History[3].Blocks[0].(ToolResult).ToolUseID != callID {
		t.Fatal("tool result did not preserve the vendor call id byte-for-byte")
	}
	conversationType := reflect.TypeOf(conversation)
	if got := conversationType.NumMethod(); got != 2 || conversationType.Method(0).Name != "Deferred" || conversationType.Method(1).Name != "Send" {
		t.Fatalf("Conversation exported methods changed: %v", conversationType)
	}
	if _, mutated := conversation.options["mutated"]; mutated || !bytes.Equal(conversation.options["vendor_flag"], []byte(`{"mode":"exact"}`)) {
		t.Fatalf("provider snapshot mutated fixed options: %#v", conversation.options)
	}
	if conversation.tools[0] == nil || conversation.settings.StopSequences[0] != "END" || conversation.history[0].Blocks == nil {
		t.Fatal("provider snapshot mutated fixed config or prior history")
	}
}

func TestToolDispatchFailuresAreInBandAndRecoverable(t *testing.T) {
	// R-4RFG-5M43
	// R-52EJ-LJSC
	tests := []struct {
		name              string
		toolName          string
		input             json.RawMessage
		tools             func(*int) []Tool
		wantCallbackCalls int
	}{
		{name: "unknown tool", toolName: "missing", input: json.RawMessage(`{}`), tools: func(*int) []Tool { return nil }},
		{name: "invalid arguments", toolName: "weather", input: json.RawMessage(`{"city":"x"}`), tools: func(calls *int) []Tool {
			return []Tool{MustTool("weather", "", func(context.Context, phase15Input) (string, error) {
				*calls++
				return "must not run", nil
			})}
		}},
		{name: "callback error", toolName: "weather", input: json.RawMessage(`{"city":"Oslo"}`), wantCallbackCalls: 1, tools: func(calls *int) []Tool {
			return []Tool{MustTool("weather", "", func(context.Context, phase15Input) (string, error) {
				*calls++
				return "", errors.New("tool backend unavailable")
			})}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callbackCalls := 0
			callID := "vendor-id-for-" + test.name
			first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: callID, Name: test.toolName, Input: test.input}}}
			final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "recovered"}}}
			provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
			transportCalls := 0
			conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
			conversation.tools = test.tools(&callbackCalls)

			stream := conversation.Send(context.Background(), Text{Text: "go"})
			events := drainStream(stream)
			if stream.Err() != nil || provider.decodeCalls != 2 || transportCalls != 2 {
				t.Fatalf("turn = err %v, decode %d, transport %d; want clean two-round recovery", stream.Err(), provider.decodeCalls, transportCalls)
			}
			if callbackCalls != test.wantCallbackCalls {
				t.Fatalf("callback calls = %d, want %d", callbackCalls, test.wantCallbackCalls)
			}
			returned, ok := events[2].(ToolReturn)
			if !ok || !returned.Result.IsError || returned.Result.ToolUseID != callID {
				t.Fatalf("stream tool failure = %#v, want correlated in-band ToolReturn", events)
			}
			toolMessage := provider.states[1].History[len(provider.states[1].History)-1]
			result, ok := toolMessage.Blocks[0].(ToolResult)
			if !ok || !result.IsError || result.ToolUseID != callID || result.Content == "" {
				t.Fatalf("in-band result = %#v, want IsError with exact id and content", toolMessage.Blocks[0])
			}
			if !reflect.DeepEqual(events[len(events)-1], MessageDone{Message: final}) {
				t.Fatalf("final recovery event = %#v, want %#v", events, final)
			}
		})
	}
}

type phase17CountingTool struct {
	name        string
	schema      json.RawMessage
	schemaCalls int
	call        func(context.Context, json.RawMessage) (string, error)
}

func (t *phase17CountingTool) Name() string            { return t.name }
func (t *phase17CountingTool) Description() string     { return "phase 17 fixture" }
func (t *phase17CountingTool) Schema() json.RawMessage { t.schemaCalls++; return t.schema }
func (t *phase17CountingTool) isTool()                 {}
func (t *phase17CountingTool) Call(ctx context.Context, input json.RawMessage) (string, error) {
	return t.call(ctx, input)
}

func phase17Tool(name string) Tool {
	return concreteTool{
		name:   name,
		schema: json.RawMessage(`{"type":"object","properties":{}}`),
		call:   func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	}
}

func toolNames(tools []Tool) []string {
	names := make([]string, len(tools))
	for index, tool := range tools {
		names[index] = tool.Name()
	}
	return names
}

func TestDeferredToolsAreOwnedValidatedAndWithheldUntilLoaded(t *testing.T) {
	// R-5PKM-V6VJ
	deferred := Tool(&concreteTool{
		name:   "records_lookup",
		schema: json.RawMessage(`{"type":"object","properties":{}}`),
		call:   func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	})
	registered := []Tool{deferred}
	load := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "load", Name: loadToolsName, Input: json.RawMessage(`{"names":["records"]}`)}}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: load}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.Deferred(DeferredGroup{Name: "records", Blurb: "Record operations", Tools: registered})
	registered[0] = phase17Tool("caller_mutation")

	stream := conversation.Send(context.Background(), Text{Text: "go"})
	drainStream(stream)
	if stream.Err() != nil || transportCalls != 2 {
		t.Fatalf("Send = %v, calls %d", stream.Err(), transportCalls)
	}
	if got := toolNames(provider.states[0].Tools); !reflect.DeepEqual(got, []string{loadToolsName}) {
		t.Fatalf("initial tools = %v, want only loader", got)
	}
	if got := toolNames(provider.states[1].Tools); !reflect.DeepEqual(got, []string{loadToolsName, "records_lookup"}) {
		t.Fatalf("loaded tools = %v", got)
	}
	if provider.states[1].Tools[1] != deferred {
		t.Fatal("loaded deferred member is not the registered ordinary Tool value")
	}
	for _, placement := range []string{"eager", "deferred"} {
		t.Run("invalid "+placement, func(t *testing.T) {
			provider := &phase15Provider{model: "model"}
			calls := 0
			conversation := NewConversation(provider, successfulPhase15Client(&calls))
			invalid := concreteTool{name: "bad", schema: json.RawMessage(`{"type":"array"}`)}
			if placement == "eager" {
				conversation.tools = []Tool{invalid}
			} else {
				conversation.Deferred(DeferredGroup{Name: "bad_group", Tools: []Tool{invalid}})
			}
			failed := conversation.Send(context.Background(), Text{Text: "blocked"})
			drainStream(failed)
			if !errors.Is(failed.Err(), ErrInvalidConfig) || calls != 0 || len(provider.states) != 0 {
				t.Fatalf("%s validation = %v, transport=%d provider=%d", placement, failed.Err(), calls, len(provider.states))
			}
		})
	}
}

func TestDeferredGroupsConditionallySynthesizeExactlyOneLoader(t *testing.T) {
	// R-5QSJ-8YM8
	for _, test := range []struct {
		name   string
		groups []DeferredGroup
		want   []string
	}{
		{name: "zero", want: []string{}},
		{name: "one", groups: []DeferredGroup{{Name: "one", Tools: []Tool{phase17Tool("a")}}}, want: []string{loadToolsName}},
		{name: "several", groups: []DeferredGroup{{Name: "one", Tools: []Tool{phase17Tool("a")}}, {Name: "two", Tools: []Tool{phase17Tool("b")}}}, want: []string{loadToolsName}},
	} {
		t.Run(test.name, func(t *testing.T) {
			conversation := NewConversation(&phase15Provider{model: "model"}, http.DefaultClient)
			conversation.Deferred(test.groups...)
			o, err := conversation.prepareOrchestrator()
			if err != nil {
				t.Fatal(err)
			}
			if got := toolNames(o.advertisedSnapshot()); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("advertised = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLoadToolsCatalogContainsOnlyGroupBlurbsAndBareNames(t *testing.T) {
	// R-5S0F-MQCX
	secretDescription := "DISTINCTIVE TOOL DESCRIPTION MUST STAY HIDDEN"
	distinctiveSchemaMarker := "schema_only_secret"
	tool := concreteTool{
		name:        "tool_token_93",
		description: secretDescription,
		schema:      json.RawMessage(`{"type":"object","properties":{"schema_only_secret":{"type":"string"}}}`),
	}
	conversation := NewConversation(&phase15Provider{model: "model"}, http.DefaultClient)
	conversation.Deferred(
		DeferredGroup{Name: "group_token_71", Blurb: "blurb token 82", Tools: []Tool{tool}},
		DeferredGroup{Name: "group_token_64", Blurb: "blurb token 55", Tools: []Tool{phase17Tool("tool_token_46")}},
	)
	o, err := conversation.prepareOrchestrator()
	if err != nil {
		t.Fatal(err)
	}
	description := o.advertisedSnapshot()[0].Description()
	for _, want := range []string{"group_token_71", "blurb token 82", "tool_token_93", "group_token_64", "blurb token 55", "tool_token_46"} {
		if !strings.Contains(description, want) {
			t.Errorf("catalog %q omits %q", description, want)
		}
	}
	for _, forbidden := range []string{secretDescription, distinctiveSchemaMarker, `"properties"`} {
		if strings.Contains(description, forbidden) {
			t.Errorf("catalog leaked %q: %q", forbidden, description)
		}
	}
}

func TestLoadToolsBatchesGroupsAndToolsWithInBandUnknownRecovery(t *testing.T) {
	// R-5T8C-0I3M
	a1 := concreteTool{name: "a1", schema: json.RawMessage(`{"type":"object","properties":{"alpha_marker":{"type":"string"}}}`)}
	a2 := phase17Tool("a2")
	solo := phase17Tool("solo")
	b2 := phase17Tool("b2")
	firstLoad := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "load-1", Name: loadToolsName, Input: json.RawMessage(`{"names":["group_a","solo","missing"]}`)}}}
	secondLoad := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "load-2", Name: loadToolsName, Input: json.RawMessage(`{"names":["a1","group_b"]}`)}}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: firstLoad}}, {MessageDone{Message: secondLoad}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.Deferred(
		DeferredGroup{Name: "group_a", Blurb: "A", Tools: []Tool{a1, a2}},
		DeferredGroup{Name: "group_b", Blurb: "B", Tools: []Tool{solo, b2}},
	)
	stream := conversation.Send(context.Background(), Text{Text: "go"})
	drainStream(stream)
	if stream.Err() != nil || transportCalls != 3 {
		t.Fatalf("turn = %v, calls %d", stream.Err(), transportCalls)
	}
	if got := toolNames(provider.states[1].Tools); !reflect.DeepEqual(got, []string{loadToolsName, "a1", "a2", "solo"}) {
		t.Fatalf("first loaded snapshot = %v", got)
	}
	if !bytes.Equal(provider.states[1].Tools[1].Schema(), a1.Schema()) {
		t.Fatal("loaded tool did not expose its full schema on the immediate next round-trip")
	}
	firstResult := provider.states[1].History[len(provider.states[1].History)-1].Blocks[0].(ToolResult)
	if firstResult.IsError || !strings.Contains(firstResult.Content, "missing") {
		t.Fatalf("unknown-name result = %#v", firstResult)
	}
	if got := toolNames(provider.states[2].Tools); !reflect.DeepEqual(got, []string{loadToolsName, "a1", "a2", "solo", "b2"}) {
		t.Fatalf("second loaded snapshot = %v; repeated names must not duplicate", got)
	}
	secondResult := provider.states[2].History[len(provider.states[2].History)-1].Blocks[0].(ToolResult)
	if secondResult.IsError {
		t.Fatalf("already-loaded name became terminal: %#v", secondResult)
	}
}

func TestDeferredLoadingIsMonotonicAcrossConversationSends(t *testing.T) {
	// R-5UG8-E9UB
	loadSecond := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "second", Name: loadToolsName, Input: json.RawMessage(`{"names":["second"]}`)}}}
	loadFirst := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "first", Name: loadToolsName, Input: json.RawMessage(`{"names":["first"]}`)}}}
	done := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: loadSecond}}, {MessageDone{Message: done}}, {MessageDone{Message: loadFirst}}, {MessageDone{Message: done}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.Deferred(DeferredGroup{Name: "all", Blurb: "All", Tools: []Tool{phase17Tool("first"), phase17Tool("second")}})
	firstStream := conversation.Send(context.Background(), Text{Text: "turn one"})
	drainStream(firstStream)
	secondStream := conversation.Send(context.Background(), Text{Text: "turn two"})
	drainStream(secondStream)
	if firstStream.Err() != nil || secondStream.Err() != nil || len(provider.states) != 4 {
		t.Fatalf("sends = %v / %v, states %d", firstStream.Err(), secondStream.Err(), len(provider.states))
	}
	if got := toolNames(provider.states[1].Tools); !reflect.DeepEqual(got, []string{loadToolsName, "second"}) {
		t.Fatalf("first turn loaded tools = %v", got)
	}
	if got := toolNames(provider.states[2].Tools); !reflect.DeepEqual(got, []string{loadToolsName, "second"}) {
		t.Fatalf("second turn forgot loaded tail: %v", got)
	}
	if got := toolNames(provider.states[3].Tools); !reflect.DeepEqual(got, []string{loadToolsName, "second", "first"}) {
		t.Fatalf("monotonic conversation order = %v", got)
	}
	conversationType := reflect.TypeFor[*Conversation]()
	if _, exists := conversationType.MethodByName("Unload"); exists {
		t.Fatal("Conversation unexpectedly exposes an unload operation")
	}
}

func TestAdvertisedToolsUseSortedBaseAndTailOnlyLoadOrder(t *testing.T) {
	// R-5WW1-5TBP
	load := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "order", Name: loadToolsName, Input: json.RawMessage(`{"names":["tail_z","tail_a"]}`)}}}
	done := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: load}}, {MessageDone{Message: done}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.tools = []Tool{phase17Tool("z_eager"), phase17Tool("a_eager")}
	conversation.Deferred(DeferredGroup{Name: "tails", Blurb: "Tails", Tools: []Tool{phase17Tool("tail_a"), phase17Tool("tail_z")}})
	drainStream(conversation.Send(context.Background(), Text{Text: "go"}))
	wantBase := []string{"a_eager", loadToolsName, "z_eager"}
	if got := toolNames(provider.states[0].Tools); !reflect.DeepEqual(got, wantBase) {
		t.Fatalf("base = %v, want %v", got, wantBase)
	}
	wantExtended := append(append([]string(nil), wantBase...), "tail_z", "tail_a")
	if got := toolNames(provider.states[1].Tools); !reflect.DeepEqual(got, wantExtended) {
		t.Fatalf("extended = %v, want %v", got, wantExtended)
	}
}

func TestDeferredRegistrationSynthesizesExactLoadToolsSchema(t *testing.T) {
	// R-0S9U-CR7T
	conversation := NewConversation(&phase15Provider{model: "model"}, http.DefaultClient)
	conversation.Deferred(DeferredGroup{
		Name:  "search",
		Blurb: "Search stored records",
		Tools: []Tool{phase17Tool("search_records")},
	})
	orchestrator, err := conversation.prepareOrchestrator()
	if err != nil {
		t.Fatal(err)
	}
	advertised := orchestrator.advertisedSnapshot()
	if len(advertised) != 1 || advertised[0].Name() != "load_tools" {
		t.Fatalf("advertised tools = %#v, want exactly load_tools", advertised)
	}

	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(advertised[0].Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Type != "object" || len(schema.Properties) != 1 || !reflect.DeepEqual(schema.Required, []string{"names"}) {
		t.Fatalf("load_tools root schema = %#v, want object with sole required property names", schema)
	}
	namesSchema, exists := schema.Properties["names"]
	if !exists {
		t.Fatal("load_tools schema has no names property")
	}
	var names struct {
		Type  string `json:"type"`
		Items struct {
			Type string `json:"type"`
		} `json:"items"`
	}
	if err := json.Unmarshal(namesSchema, &names); err != nil {
		t.Fatal(err)
	}
	if names.Type != "array" || names.Items.Type != "string" {
		t.Fatalf("load_tools names schema = %#v, want array of strings", names)
	}
}

func TestSendGatesTheCompleteLiveToolSetOnceBeforeAllBoundaries(t *testing.T) {
	// R-4F8G-BWP5
	// R-4GGC-POFU
	// R-5Y3X-JL2E
	tests := []struct {
		name     string
		eager    []Tool
		deferred []DeferredGroup
	}{
		{name: "invalid eager schema", eager: []Tool{concreteTool{name: "bad", schema: json.RawMessage(`{"type":"array"}`)}}},
		{name: "invalid deferred schema", deferred: []DeferredGroup{{Tools: []Tool{concreteTool{name: "bad", schema: json.RawMessage(`{"type":"array"}`)}}}}},
		{name: "duplicate eager eager", eager: []Tool{phase17Tool("same"), phase17Tool("same")}},
		{name: "duplicate eager deferred", eager: []Tool{phase17Tool("same")}, deferred: []DeferredGroup{{Tools: []Tool{phase17Tool("same")}}}},
		{name: "duplicate deferred deferred", deferred: []DeferredGroup{{Tools: []Tool{phase17Tool("same")}}, {Tools: []Tool{phase17Tool("same")}}}},
		{name: "consumer collides with load tools", eager: []Tool{phase17Tool(loadToolsName)}, deferred: []DeferredGroup{{Tools: []Tool{phase17Tool("later")}}}},
		{name: "deferred collides with load tools", deferred: []DeferredGroup{{Tools: []Tool{phase17Tool(loadToolsName)}}}},
	}

	ordinaryLoadToolsCall := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "ordinary-loader-name", Name: loadToolsName, Input: json.RawMessage(`{}`)}}}
	providerWithoutGroups := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: ordinaryLoadToolsCall}}, {MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "ok"}}}}}}}
	transportWithoutGroups := 0
	ordinaryCalls := 0
	conversationWithoutGroups := NewConversation(providerWithoutGroups, successfulPhase15Client(&transportWithoutGroups))
	conversationWithoutGroups.tools = []Tool{concreteTool{
		name:   loadToolsName,
		schema: json.RawMessage(`{"type":"object","properties":{}}`),
		call: func(context.Context, json.RawMessage) (string, error) {
			ordinaryCalls++
			return "ordinary consumer tool", nil
		},
	}}
	streamWithoutGroups := conversationWithoutGroups.Send(context.Background(), Text{Text: "allowed"})
	drainStream(streamWithoutGroups)
	if streamWithoutGroups.Err() != nil || transportWithoutGroups != 2 || ordinaryCalls != 1 {
		t.Fatalf("absent synthetic loader created false collision: err=%v transport=%d ordinary=%d", streamWithoutGroups.Err(), transportWithoutGroups, ordinaryCalls)
	}

	eagerCount := &phase17CountingTool{name: "eager_count", schema: json.RawMessage(`{"type":"object","properties":{}}`), call: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	deferredCount := &phase17CountingTool{name: "deferred_count", schema: json.RawMessage(`{"type":"object","properties":{}}`), call: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	gateProvider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "ok"}}}}}}}
	gateCalls := 0
	gateConversation := NewConversation(gateProvider, successfulPhase15Client(&gateCalls))
	gateConversation.tools = []Tool{eagerCount}
	gateConversation.Deferred(DeferredGroup{Name: "counted", Tools: []Tool{deferredCount}})
	gateStream := gateConversation.Send(context.Background(), Text{Text: "validate union"})
	drainStream(gateStream)
	if gateStream.Err() != nil || eagerCount.schemaCalls != 1 || deferredCount.schemaCalls != 1 {
		t.Fatalf("union gate: err=%v eager schemas=%d deferred schemas=%d", gateStream.Err(), eagerCount.schemaCalls, deferredCount.schemaCalls)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &phase15Provider{model: "model"}
			transportCalls := 0
			conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
			conversation.tools = test.eager
			conversation.deferred = test.deferred
			callbackCalls := 0
			conversation.validate = func() error { callbackCalls++; return nil }
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "unchanged"}}}}
			before, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}

			stream := conversation.Send(context.Background(), Text{Text: "never sent"})
			drainStream(stream)
			after, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !errors.Is(stream.Err(), ErrInvalidConfig) {
				t.Fatalf("Stream.Err() = %v, want ErrInvalidConfig", stream.Err())
			}
			if transportCalls != 0 || len(provider.states) != 0 || provider.decodeCalls != 0 || provider.classifyCalls != 0 || callbackCalls != 0 {
				t.Fatalf("boundary calls: transport=%d build=%d decode=%d classify=%d callback=%d", transportCalls, len(provider.states), provider.decodeCalls, provider.classifyCalls, callbackCalls)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("history changed: before=%s after=%s", before, after)
			}
		})
	}

	counting := &phase17CountingTool{
		name:   "counted",
		schema: json.RawMessage(`{"type":"object","properties":{}}`),
		call:   func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	}
	first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "count-call", Name: "counted", Input: json.RawMessage(`{}`)}}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	conversation.tools = []Tool{counting}
	stream := conversation.Send(context.Background(), Text{Text: "go"})
	drainStream(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if counting.schemaCalls != 2 {
		t.Fatalf("Schema calls = %d, want one gate plus one dispatch validation (not a second-round gate)", counting.schemaCalls)
	}
}

func TestOrchestratorValidatesEveryCallBeforeInvokingTool(t *testing.T) {
	// R-4HO9-3G6J
	// R-4K41-UZNX
	schema := json.RawMessage(`{"type":"object","properties":{"place":{"type":"object","properties":{"city":{"type":"string","minLength":2,"pattern":"^[A-Z]"}},"required":["city"]}},"required":["place"]}`)
	tests := []struct {
		name  string
		input json.RawMessage
		valid bool
	}{
		{name: "malformed JSON", input: json.RawMessage(`{"place":`)},
		{name: "trailing JSON", input: json.RawMessage(`{"place":{"city":"Oslo"}} {}`)},
		{name: "wrong type", input: json.RawMessage(`{"place":{"city":7}}`)},
		{name: "missing required", input: json.RawMessage(`{"place":{}}`)},
		{name: "nested constraint", input: json.RawMessage(`{"place":{"city":"oslo"}}`)},
		{name: "valid exact bytes", input: json.RawMessage(" {\n\t\"place\": {\"city\":\"Oslo\"}} "), valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callbackCalls := 0
			var callbackInput json.RawMessage
			tool, err := NewToolFromSchema("lookup", "", schema, func(_ context.Context, input json.RawMessage) (string, error) {
				callbackCalls++
				callbackInput = append(json.RawMessage(nil), input...)
				return "accepted", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "validation-id", Name: "lookup", Input: test.input}}}
			final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "recovered"}}}
			provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
			transportCalls := 0
			conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
			conversation.tools = []Tool{tool}

			stream := conversation.Send(context.Background(), Text{Text: "go"})
			drainStream(stream)
			if stream.Err() != nil || transportCalls != 2 || provider.decodeCalls != 2 {
				t.Fatalf("recovering turn: err=%v transport=%d decode=%d", stream.Err(), transportCalls, provider.decodeCalls)
			}
			result := provider.states[1].History[len(provider.states[1].History)-1].Blocks[0].(ToolResult)
			if test.valid {
				if callbackCalls != 1 || !bytes.Equal(callbackInput, test.input) || result.IsError || result.Content != "accepted" {
					t.Fatalf("valid dispatch: calls=%d input=%q result=%#v", callbackCalls, callbackInput, result)
				}
			} else if callbackCalls != 0 || !result.IsError || !strings.Contains(result.Content, "invalid arguments") {
				t.Fatalf("invalid dispatch: calls=%d result=%#v", callbackCalls, result)
			}
		})
	}
}

func TestUnknownAndDeferredDirectCallsRecoverWithoutGuessedExecution(t *testing.T) {
	// R-4IW5-H7X8
	// R-5VO4-S1L0
	for _, test := range []struct {
		name          string
		deferred      bool
		wantNextTools []string
	}{
		{name: "wholly unknown", wantNextTools: nil},
		{name: "known deferred unloaded", deferred: true, wantNextTools: []string{loadToolsName, "secret"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			callName := "missing"
			callbackCalls := 0
			first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "unknown-id", Name: callName, Input: json.RawMessage(`{"guessed":true}`)}}}
			final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "recovered"}}}
			provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
			transportCalls := 0
			conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
			if test.deferred {
				callName = "secret"
				first.Blocks[0] = ToolUse{ID: "unknown-id", Name: callName, Input: json.RawMessage(`{"guessed":true}`)}
				provider.responses[0] = []Event{MessageDone{Message: first}}
				secret := concreteTool{name: callName, schema: json.RawMessage(`{"type":"object","properties":{}}`), call: func(context.Context, json.RawMessage) (string, error) {
					callbackCalls++
					return "must not execute", nil
				}}
				conversation.deferred = []DeferredGroup{{Tools: []Tool{secret}}}
			}

			stream := conversation.Send(context.Background(), Text{Text: "go"})
			drainStream(stream)
			if stream.Err() != nil || callbackCalls != 0 || len(provider.states) != 2 {
				t.Fatalf("turn err=%v callback=%d states=%d", stream.Err(), callbackCalls, len(provider.states))
			}
			result := provider.states[1].History[len(provider.states[1].History)-1].Blocks[0].(ToolResult)
			if !result.IsError || result.ToolUseID != "unknown-id" || !strings.Contains(result.Content, "unknown tool") {
				t.Fatalf("unknown result = %#v", result)
			}
			if test.deferred && (!strings.Contains(result.Content, callName) || !strings.Contains(result.Content, loadToolsName)) {
				t.Fatalf("deferred recovery result does not name tool and loader: %#v", result)
			}
			var names []string
			for _, tool := range provider.states[1].Tools {
				names = append(names, tool.Name())
			}
			if !reflect.DeepEqual(names, test.wantNextTools) {
				t.Fatalf("next tools = %v, want %v", names, test.wantNextTools)
			}
			if test.deferred && !bytes.Equal(provider.states[1].Tools[1].Schema(), json.RawMessage(`{"type":"object","properties":{}}`)) {
				t.Fatalf("next round did not advertise full deferred schema: %s", provider.states[1].Tools[1].Schema())
			}
		})
	}
}

func TestToolCallbackErrorIsCorrelatedDeliveredAndRecoverable(t *testing.T) {
	// R-4LBY-8REM
	// R-67V4-LQZY
	distinctive := "dead MCP server mid-turn"
	first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "callback-id", Name: "boom", Input: json.RawMessage(`{}`)}}}
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "recovered"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
	transportCalls := 0
	conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
	runtimeTool, err := NewToolFromSchema("boom", "remote tool", json.RawMessage(`{"type":"object","properties":{}}`), func(context.Context, json.RawMessage) (string, error) {
		return "discarded", errors.New(distinctive)
	})
	if err != nil {
		t.Fatal(err)
	}
	conversation.tools = []Tool{runtimeTool}

	stream := conversation.Send(context.Background(), Text{Text: "go"})
	events := drainStream(stream)
	if stream.Err() != nil || transportCalls != 2 || provider.decodeCalls != 2 {
		t.Fatalf("turn err=%v transport=%d decode=%d", stream.Err(), transportCalls, provider.decodeCalls)
	}
	result := provider.states[1].History[len(provider.states[1].History)-1].Blocks[0].(ToolResult)
	if !result.IsError || result.ToolUseID != "callback-id" || result.Content != distinctive {
		t.Fatalf("callback result = %#v", result)
	}
	if !reflect.DeepEqual(events[len(events)-1], MessageDone{Message: final}) {
		t.Fatalf("turn did not complete after callback error: %#v", events)
	}
}

func TestSiblingRuntimeSchemaAndRootEagerToolsShareTheSendTimeGate(t *testing.T) {
	// R-66N8-7Z99
	invalidSchema := json.RawMessage(`{"type":"object","additionalProperties":true}`)
	for _, test := range []struct {
		name string
		tool func(*int) Tool
	}{
		{name: "sibling runtime schema fixture", tool: func(callbackCalls *int) Tool {
			return &phase17CountingTool{name: "remote", schema: invalidSchema, call: func(context.Context, json.RawMessage) (string, error) {
				*callbackCalls++
				return "must not run", nil
			}}
		}},
		{name: "root eager fixture", tool: func(callbackCalls *int) Tool {
			return concreteTool{name: "eager", schema: invalidSchema, call: func(context.Context, json.RawMessage) (string, error) {
				*callbackCalls++
				return "must not run", nil
			}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &phase15Provider{model: "model"}
			transportCalls := 0
			callbackCalls := 0
			conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
			conversation.tools = []Tool{test.tool(&callbackCalls)}

			stream := conversation.Send(context.Background(), Text{Text: "blocked"})
			events := drainStream(stream)
			if !errors.Is(stream.Err(), ErrInvalidConfig) {
				t.Fatalf("Send error = %v, want ErrInvalidConfig", stream.Err())
			}
			if len(events) != 0 || transportCalls != 0 || len(provider.states) != 0 || provider.decodeCalls != 0 || provider.classifyCalls != 0 || callbackCalls != 0 {
				t.Fatalf("invalid tool crossed boundary: events=%d transport=%d build=%d decode=%d classify=%d callback=%d", len(events), transportCalls, len(provider.states), provider.decodeCalls, provider.classifyCalls, callbackCalls)
			}
		})
	}
}

func TestTerminalFailuresAfterCompletedRoundTripPreserveEventsAndAtomicHistory(t *testing.T) {
	// R-4Q7J-RUDE
	// R-4SNC-JDUS
	// R-53MF-ZBJ1
	tests := []struct {
		name      string
		decodeErr error
		wantErr   error
	}{
		{name: "transport", wantErr: errors.New("second transport failed")},
		{name: "classified vendor", wantErr: &Error{Category: CategoryRateLimit, Status: http.StatusTooManyRequests, Message: "slow down"}},
		{name: "decode", decodeErr: errors.New("broken terminal frame"), wantErr: errors.New("broken terminal frame")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callID := "vendor-terminal-id"
			first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: callID, Name: "weather", Input: json.RawMessage(`{"city":"Oslo"}`)}}}
			partial := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "observable before decode failure"}}}
			provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, nil}}
			if test.name == "decode" {
				provider.responses[1] = []Event{MessageDone{Message: partial}}
				provider.decodeErrors = []error{nil, test.decodeErr}
			}
			if test.name == "classified vendor" {
				provider.classified = test.wantErr
			}
			success := func() *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}
			}
			second := phase15TransportStep{response: success()}
			switch test.name {
			case "transport":
				second = phase15TransportStep{err: test.wantErr}
			case "classified vendor":
				second = phase15TransportStep{response: &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("rate limited"))}}
			}
			transport := &phase15Transport{steps: []phase15TransportStep{{response: success()}, second}}
			client := &http.Client{Transport: transport}
			toolCalls := 0
			conversation := NewConversation(provider, client)
			conversation.tools = []Tool{MustTool("weather", "", func(context.Context, phase15Input) (string, error) {
				toolCalls++
				return "sunny", nil
			})}
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
			before, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}

			stream := conversation.Send(context.Background(), Text{Text: "do not commit"})
			events := drainStream(stream)
			expected := test.wantErr
			if test.name == "decode" {
				expected = test.decodeErr
			}
			if stream.Err() == nil || !errors.Is(stream.Err(), expected) {
				t.Fatalf("Stream.Err() = %v, want terminal error wrapping %v", stream.Err(), expected)
			}
			after, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("history changed: before=%s after=%s", before, after)
			}
			wantClassifyCalls := 0
			if test.name == "classified vendor" {
				wantClassifyCalls = 1
			}
			if toolCalls != 1 || transport.calls != 2 || len(provider.states) != 2 || provider.classifyCalls != wantClassifyCalls {
				t.Fatalf("calls after terminal error: tool=%d transport=%d build=%d classify=%d", toolCalls, transport.calls, len(provider.states), provider.classifyCalls)
			}
			wantEvents := 3
			if test.name == "decode" {
				wantEvents = 4
			}
			if len(events) != wantEvents || !reflect.DeepEqual(events[0], MessageDone{Message: first}) {
				t.Fatalf("completed first-round events were lost: %#v", events)
			}
			if test.name == "decode" && !reflect.DeepEqual(events[len(events)-1], MessageDone{Message: partial}) {
				t.Fatalf("message before decode failure was lost: %#v", events)
			}
		})
	}
}

type phase15Wire struct {
	reserved    []string
	encodeCalls int
	decodeCalls int
	state       RequestState
}

func (w *phase15Wire) EncodeRequest(state RequestState) ([]byte, error) {
	w.encodeCalls++
	w.state = cloneRequestState(state)
	state.Options["safe"] = json.RawMessage(`null`)
	return []byte(`{}`), nil
}

func (w *phase15Wire) DecodeStream(iter.Seq2[[]byte, error]) iter.Seq2[Event, error] {
	w.decodeCalls++
	return func(func(Event, error) bool) {}
}

func (*phase15Wire) RenderTools([]Tool) (json.RawMessage, error) { return nil, nil }
func (w *phase15Wire) ReservedKeys() []string                    { return append([]string(nil), w.reserved...) }

func TestProviderOptionsReservedCollisionFailsBeforeProviderBoundaries(t *testing.T) {
	// R-4V35-AXC6
	authCalls := 0
	transportCalls := 0
	classifyCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	endpoint, err := NewEndpoint(
		"https://phase15.invalid",
		authFunc(func(context.Context, *http.Request, []byte) error { authCalls++; return nil }),
		WithHTTPClient(client),
		WithClassifier(func(int, http.Header, []byte) error { classifyCalls++; return errors.New("unused") }),
	)
	if err != nil {
		t.Fatal(err)
	}
	wire := &phase15Wire{reserved: []string{"model"}}
	conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "phase15", Model: "model"})
	conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
	conversation.options = ProviderOptions{"model": json.RawMessage(`"override"`)}
	before, _ := json.Marshal(conversation.history)

	stream := conversation.Send(context.Background(), Text{Text: "blocked"})
	drainStream(stream)
	if !errors.Is(stream.Err(), ErrInvalidConfig) {
		t.Fatalf("Stream.Err() = %v, want ErrInvalidConfig", stream.Err())
	}
	after, _ := json.Marshal(conversation.history)
	if wire.encodeCalls != 0 || wire.decodeCalls != 0 || authCalls != 0 || transportCalls != 0 || classifyCalls != 0 {
		t.Fatalf("calls after collision: encode=%d decode=%d auth=%d transport=%d classify=%d", wire.encodeCalls, wire.decodeCalls, authCalls, transportCalls, classifyCalls)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("history changed: before=%s after=%s", before, after)
	}
}

func TestProviderOptionsNoncollidingSnapshotIsCloned(t *testing.T) {
	// R-4V35-AXC6
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	endpoint, err := NewEndpoint(
		"https://phase15.invalid",
		authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
		WithHTTPClient(client),
	)
	if err != nil {
		t.Fatal(err)
	}
	wire := &phase15Wire{reserved: []string{"model"}}
	conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "phase15", Model: "model"})
	conversation.options = ProviderOptions{"safe": json.RawMessage(`{"verbatim":true}`)}
	stream := conversation.Send(context.Background(), Text{Text: "allowed"})
	drainStream(stream)
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if wire.encodeCalls != 1 || !bytes.Equal(wire.state.Options["safe"], []byte(`{"verbatim":true}`)) {
		t.Fatalf("noncolliding options snapshot = %#v, encode calls %d", wire.state.Options, wire.encodeCalls)
	}
	if !bytes.Equal(conversation.options["safe"], []byte(`{"verbatim":true}`)) {
		t.Fatalf("provider mutated conversation options: %#v", conversation.options)
	}
}
