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
	defaultConversation.Send(context.Background(), Text{Text: "hello"})
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
	overrideConversation.Send(context.Background(), Text{Text: "hello"})
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

func TestConversationExposesOnlySendAsExportedMethod(t *testing.T) {
	// R-1POH-Q9DL
	conversation, _, _, _ := newConversationAxesFixture(t)
	conversationType := reflect.TypeOf(conversation)
	for index := range conversationType.NumMethod() {
		if conversationType.Method(index).Name != "Send" {
			t.Fatalf("unexpected exported reassignment/API method %q", conversationType.Method(index).Name)
		}
	}
}

func TestConversationSendsModelVerbatim(t *testing.T) {
	// R-1S4A-HSUZ
	conversation, provider, unknownModel, capture := newConversationAxesFixture(t)
	stream := conversation.Send(context.Background(), Text{Text: "text"})
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
		if stream := conversation.Send(context.Background(), Text{Text: "hello"}); !errors.Is(stream.err, ErrInvalidConfig) {
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
	model         string
	states        []RequestState
	responses     [][]Event
	decodeErrors  []error
	decodeCalls   int
	classifyCalls int
	classified    error
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
	provider := &phase15Provider{model: "fixed/model β", responses: [][]Event{{first}, {final}}}
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
	if got := stream.events; !reflect.DeepEqual(got, []Event{first, result, final}) {
		t.Fatalf("stream events = %#v, want assistant/tool/assistant order", got)
	}
	if got := conversation.history; !reflect.DeepEqual(got, History{prior, user, first, result, final}) {
		t.Fatalf("history = %#v, want one complete turn splice", got)
	}
	if result.Blocks[0].(ToolResult).ToolUseID != callID || provider.states[1].History[3].Blocks[0].(ToolResult).ToolUseID != callID {
		t.Fatal("tool result did not preserve the vendor call id byte-for-byte")
	}
	if got := reflect.TypeOf(conversation).NumMethod(); got != 1 || reflect.TypeOf(conversation).Method(0).Name != "Send" {
		t.Fatalf("Conversation exported methods changed: %v", reflect.TypeOf(conversation))
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
			provider := &phase15Provider{model: "model", responses: [][]Event{{first}, {final}}}
			transportCalls := 0
			conversation := NewConversation(provider, successfulPhase15Client(&transportCalls))
			conversation.tools = test.tools(&callbackCalls)

			stream := conversation.Send(context.Background(), Text{Text: "go"})
			if stream.Err() != nil || provider.decodeCalls != 2 || transportCalls != 2 {
				t.Fatalf("turn = err %v, decode %d, transport %d; want clean two-round recovery", stream.Err(), provider.decodeCalls, transportCalls)
			}
			if callbackCalls != test.wantCallbackCalls {
				t.Fatalf("callback calls = %d, want %d", callbackCalls, test.wantCallbackCalls)
			}
			toolMessage := provider.states[1].History[len(provider.states[1].History)-1]
			result, ok := toolMessage.Blocks[0].(ToolResult)
			if !ok || !result.IsError || result.ToolUseID != callID || result.Content == "" {
				t.Fatalf("in-band result = %#v, want IsError with exact id and content", toolMessage.Blocks[0])
			}
			if !reflect.DeepEqual(stream.events[len(stream.events)-1], final) {
				t.Fatalf("final recovery event = %#v, want %#v", stream.events, final)
			}
		})
	}
}

func TestTerminalFailuresAfterCompletedRoundTripPreserveEventsAndAtomicHistory(t *testing.T) {
	// R-4Q7J-RUDE
	// R-4SNC-JDUS
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
			provider := &phase15Provider{model: "model", responses: [][]Event{{first}, nil}}
			if test.name == "decode" {
				provider.responses[1] = []Event{partial}
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
			wantEvents := 2
			if test.name == "decode" {
				wantEvents = 3
			}
			if len(stream.events) != wantEvents || !reflect.DeepEqual(stream.events[0], first) {
				t.Fatalf("completed first-round events were lost: %#v", stream.events)
			}
			if test.name == "decode" && !reflect.DeepEqual(stream.events[len(stream.events)-1], partial) {
				t.Fatalf("message before decode failure was lost: %#v", stream.events)
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
	if stream := conversation.Send(context.Background(), Text{Text: "allowed"}); stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if wire.encodeCalls != 1 || !bytes.Equal(wire.state.Options["safe"], []byte(`{"verbatim":true}`)) {
		t.Fatalf("noncolliding options snapshot = %#v, encode calls %d", wire.state.Options, wire.encodeCalls)
	}
	if !bytes.Equal(conversation.options["safe"], []byte(`{"verbatim":true}`)) {
		t.Fatalf("provider mutated conversation options: %#v", conversation.options)
	}
}
