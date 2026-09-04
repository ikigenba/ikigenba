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

func useDefaultHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()
	original := http.DefaultClient
	http.DefaultClient = client
	t.Cleanup(func() { http.DefaultClient = original })
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
	states             []requestState
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

func (p *fixtureProvider) BuildRequest(ctx context.Context, state requestState) (*http.Request, error) {
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
	return newConversation(provider, client, Config{}), provider
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
	// R-OFIQ-BSPA
	wantConstructor := reflect.TypeOf(func(string, Authenticator) (Endpoint, error) { return Endpoint{}, nil })
	if got := reflect.TypeOf(NewEndpoint); got != wantConstructor || got.IsVariadic() {
		t.Fatalf("endpoint construction exposes transport hooks: %s variadic=%t", got, got.IsVariadic())
	}
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
	defaultConversation := newEndpointConversation(&testWire{}, defaultEndpoint, Identity{Model: "default-model"}, Config{})
	drainStream(defaultConversation.Send(context.Background(), Text{Text: "hello"}))
	if defaultCalls != 1 {
		t.Fatalf("default client calls = %d, want 1", defaultCalls)
	}
}

func phase10TestWire() *testWire {
	return &testWire{classifier: func(status int, _ http.Header, _ []byte) error {
		if status >= http.StatusOK && status < http.StatusMultipleChoices {
			return nil
		}
		return &Error{Category: classifyStatus(status), Status: status, Message: http.StatusText(status)}
	}}
}

// countingRefreshSource records Refresh calls; tokenSourceStub itself keeps no
// such tally.
type countingRefreshSource struct {
	*tokenSourceStub
	refreshes int
}

func (s *countingRefreshSource) Refresh(ctx context.Context) (Token, error) {
	s.refreshes++
	return s.tokenSourceStub.Refresh(ctx)
}

// R-04K9-MWUT
func TestConversationSurfacesOAuthRefreshFailureUnchanged(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	refreshErr := &Error{Category: CategoryAuth, Status: http.StatusBadGateway, Message: "refresh rejected"}
	source := &countingRefreshSource{tokenSourceStub: &tokenSourceStub{token: Token{Bearer: "stale"}, refreshErr: refreshErr}}
	endpoint, err := NewEndpoint(server.URL, oauthApplier{provider: OfferingAnthropicMessages, source: source})
	if err != nil {
		t.Fatal(err)
	}
	conversation := newEndpointConversation(phase10TestWire(), endpoint, Identity{Model: "m"}, Config{})
	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	if events := drainStream(stream); len(events) != 0 {
		t.Fatalf("failed refresh emitted events %#v", events)
	}
	var providerErr *Error
	if !errors.As(stream.Err(), &providerErr) || providerErr != refreshErr {
		t.Fatalf("Send error = %v (%p), want exact refresh error %v (%p)", stream.Err(), providerErr, refreshErr, refreshErr)
	}
	if requests != 1 || source.refreshes != 1 {
		t.Fatalf("requests = %d, Refresh calls = %d; want 1 and 1", requests, source.refreshes)
	}
}

// R-KNWJ-HBYA
func TestConversationOAuth401RefreshesAndReissuesOnce(t *testing.T) {
	t.Run("successful refresh", func(t *testing.T) {
		requests := 0
		var authorizations []string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requests++
			authorizations = append(authorizations, request.Header.Get("Authorization"))
			if requests == 1 {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			writer.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(writer, "data: refreshed\n\n")
		}))
		defer server.Close()

		source := &countingRefreshSource{tokenSourceStub: &tokenSourceStub{
			token:        Token{Bearer: "stale"},
			refreshToken: Token{Bearer: "fresh"},
		}}
		offering := Offering{ID: OfferingAnthropicMessages, AuthModes: []AuthMode{AuthModeOAuth}}
		authenticator, err := offering.Authenticator(OAuth(source))
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := NewEndpoint(server.URL, authenticator)
		if err != nil {
			t.Fatal(err)
		}
		conversation := newEndpointConversation(phase10TestWire(), endpoint, Identity{Model: "m"}, Config{})
		stream := conversation.Send(context.Background(), Text{Text: "hello"})
		events := drainStream(stream)
		wantEvents := []Event{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "refreshed"}}}}}
		if stream.Err() != nil {
			t.Fatalf("Send error = %v, want nil", stream.Err())
		}
		if !reflect.DeepEqual(events, wantEvents) {
			t.Fatalf("events = %#v, want the single plain-200 event %#v", events, wantEvents)
		}
		if requests != 2 || source.refreshes != 1 {
			t.Fatalf("requests = %d, Refresh calls = %d; want 2 and 1", requests, source.refreshes)
		}
		if !reflect.DeepEqual(authorizations, []string{"Bearer stale", "Bearer fresh"}) {
			t.Fatalf("Authorization headers = %q, want stale then refreshed bearer", authorizations)
		}
	})

	t.Run("second 401 surfaces without another refresh", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			requests++
			writer.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		source := &countingRefreshSource{tokenSourceStub: &tokenSourceStub{
			token:        Token{Bearer: "stale"},
			refreshToken: Token{Bearer: "fresh"},
		}}
		offering := Offering{ID: OfferingAnthropicMessages, AuthModes: []AuthMode{AuthModeOAuth}}
		authenticator, err := offering.Authenticator(OAuth(source))
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := NewEndpoint(server.URL, authenticator)
		if err != nil {
			t.Fatal(err)
		}
		conversation := newEndpointConversation(phase10TestWire(), endpoint, Identity{Model: "m"}, Config{})
		stream := conversation.Send(context.Background(), Text{Text: "hello"})
		if events := drainStream(stream); len(events) != 0 {
			t.Fatalf("401 exchange emitted events %#v", events)
		}
		var providerErr *Error
		if !errors.As(stream.Err(), &providerErr) || providerErr.Status != http.StatusUnauthorized {
			t.Fatalf("Send error = %v, want classified *Error with status 401", stream.Err())
		}
		if requests != 2 || source.refreshes != 1 {
			t.Fatalf("requests = %d, Refresh calls = %d; want 2 and 1", requests, source.refreshes)
		}
	})

	t.Run("non-401 does not refresh", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			requests++
			writer.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		source := &countingRefreshSource{tokenSourceStub: &tokenSourceStub{token: Token{Bearer: "current"}}}
		offering := Offering{ID: OfferingAnthropicMessages, AuthModes: []AuthMode{AuthModeOAuth}}
		authenticator, err := offering.Authenticator(OAuth(source))
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := NewEndpoint(server.URL, authenticator)
		if err != nil {
			t.Fatal(err)
		}
		conversation := newEndpointConversation(phase10TestWire(), endpoint, Identity{Model: "m"}, Config{})
		stream := conversation.Send(context.Background(), Text{Text: "hello"})
		drainStream(stream)
		var providerErr *Error
		if !errors.As(stream.Err(), &providerErr) || providerErr.Status != http.StatusInternalServerError {
			t.Fatalf("Send error = %v, want classified *Error with status 500", stream.Err())
		}
		if requests != 1 || source.refreshes != 0 {
			t.Fatalf("requests = %d, Refresh calls = %d; want 1 and 0", requests, source.refreshes)
		}
	})
}

// R-KP4F-V3OZ
func TestConversationAPIKey401DoesNotReissue(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	offering := Offering{ID: OfferingAnthropicMessages, AuthModes: []AuthMode{AuthModeAPIKey}}
	authenticator, err := offering.Authenticator(APIKey("secret"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := NewEndpoint(server.URL, authenticator)
	if err != nil {
		t.Fatal(err)
	}
	conversation := newEndpointConversation(phase10TestWire(), endpoint, Identity{Model: "m"}, Config{})
	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	if events := drainStream(stream); len(events) != 0 {
		t.Fatalf("401 exchange emitted events %#v", events)
	}
	var providerErr *Error
	if !errors.As(stream.Err(), &providerErr) || providerErr.Status != http.StatusUnauthorized {
		t.Fatalf("Send error = %v, want classified *Error with status 401", stream.Err())
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
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

func TestBuiltInWireClassifiesNonSuccessResponse(t *testing.T) {
	// R-OGQM-PKFZ
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusPaymentRequired)
		_, _ = writer.Write([]byte("quota exhausted"))
	}))
	t.Cleanup(server.Close)

	endpoint, err := NewEndpoint(server.URL, authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	useDefaultHTTPClient(t, server.Client())
	conversation, err := New(OpenAIChatWire(), endpoint, "model", Config{})
	if err != nil {
		t.Fatal(err)
	}

	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	drainStream(stream)
	var providerError *Error
	if !errors.As(stream.err, &providerError) {
		t.Fatalf("Send error type = %T, want *Error", stream.err)
	}
	if providerError.Status != http.StatusPaymentRequired || providerError.Category != CategoryInsufficientQuota {
		t.Fatalf("Send error = %#v, want status %d and CategoryInsufficientQuota", providerError, http.StatusPaymentRequired)
	}
}

func TestBuiltInWireLiftsRetryAfterHeaderIntoError(t *testing.T) {
	// R-1JWR-1RWS
	tests := []struct {
		name  string
		set   bool
		value string
		want  time.Duration
	}{
		{name: "delta-seconds", set: true, value: "30", want: 30 * time.Second},
		{name: "absent", set: false, want: 0},
		{name: "http-date", set: true, value: "Wed, 21 Oct 2026 07:28:00 GMT", want: 0},
		{name: "negative", set: true, value: "-5", want: 0},
		{name: "non-integer", set: true, value: "3.5", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.set {
					writer.Header().Set("Retry-After", test.value)
				}
				writer.WriteHeader(http.StatusTooManyRequests)
				_, _ = writer.Write([]byte("slow down"))
			}))
			t.Cleanup(server.Close)

			endpoint, err := NewEndpoint(server.URL, authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
			if err != nil {
				t.Fatal(err)
			}
			useDefaultHTTPClient(t, server.Client())
			conversation, err := New(OpenAIChatWire(), endpoint, "model", Config{})
			if err != nil {
				t.Fatal(err)
			}

			stream := conversation.Send(context.Background(), Text{Text: "hello"})
			drainStream(stream)
			var providerError *Error
			if !errors.As(stream.err, &providerError) {
				t.Fatalf("Send error type = %T, want *Error", stream.err)
			}
			if providerError.RetryAfter != test.want {
				t.Fatalf("RetryAfter = %v, want %v", providerError.RetryAfter, test.want)
			}
		})
	}
}

func TestTransportFailureIsWrappedWithStableIdentity(t *testing.T) {
	// R-2K5Z-AIWY
	cause := errors.New("connection refused")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}
	conversation, provider := vendorFixture("http://provider.invalid", "original-model", client)
	wantIdentity := Identity{Endpoint: "vendor", AuthMode: "fixture", Model: "original-model"}
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
	if got := conversationType.NumMethod(); got != 1 || conversationType.Method(0).Name != "Send" {
		t.Fatalf("Conversation exported methods changed: %v", conversationType)
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
	// R-NYLV-1K50
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
			settings:     Settings{Options: Options{"thinking_budget": "4096"}},
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
			wire := boundaryWire(test.capabilities, func(requestState) {
				encodeCalls++
			}, func() {
				decodeCalls++
			})
			endpoint, err := NewEndpoint("https://provider.invalid/generate", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
			if err != nil {
				t.Fatal(err)
			}
			conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: "opaque-model"}, Config{Settings: test.settings})
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
			if encodeCalls != 0 || decodeCalls != 0 {
				t.Fatalf("boundary calls after invalid setting: encode/build=%d decode=%d", encodeCalls, decodeCalls)
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

func TestReasoningTermMismatchAndConflictFailAtStartOfSendWithoutMutation(t *testing.T) {
	// R-W3V7-6GSB
	// R-W533-K8J0
	tests := []struct {
		name     string
		options  Options
		wantKeys []string
	}{
		{name: "term mismatch", options: Options{"thinking": "high"}, wantKeys: []string{"thinking"}},
		{name: "conflict", options: Options{"effort": "high", "thinking": "on"}, wantKeys: []string{"effort", "thinking"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encodeCalls := 0
			decodeCalls := 0
			wire := boundaryWire(
				wireCapabilities{
					name:      "controlled grammar",
					reasoning: reasoningShapeOff | reasoningShapeOn | reasoningShapeEffort | reasoningShapeBudget,
				},
				func(requestState) { encodeCalls++ },
				func() { decodeCalls++ },
			)
			endpoint, err := NewEndpoint("https://provider.invalid/generate", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
			if err != nil {
				t.Fatal(err)
			}
			conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: "opaque-model"}, Config{Settings: Settings{Options: test.options}})
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
			for _, key := range test.wantKeys {
				if !strings.Contains(stream.err.Error(), key) {
					t.Errorf("Send error = %v, want reasoning key %q", stream.err, key)
				}
			}
			if encodeCalls != 0 || decodeCalls != 0 {
				t.Fatalf("boundary calls after invalid reasoning: encode/build=%d decode=%d", encodeCalls, decodeCalls)
			}
			afterHistory, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterHistory, beforeHistory) {
				t.Fatalf("History changed: before=%s after=%s", beforeHistory, afterHistory)
			}
			if !reflect.DeepEqual(conversation.settings, beforeSettings) {
				t.Fatalf("reasoning option was substituted or dropped: before=%#v after=%#v", beforeSettings, conversation.settings)
			}
		})
	}
}

func TestUnknownOrUnparsableOptionFailsAtStartOfSendWithoutMutation(t *testing.T) {
	// R-OI49-5W04
	// R-OJC5-JNQT
	tests := []struct {
		name    string
		options Options
		wantKey string
	}{
		{name: "unknown option", options: Options{"not_a_real_option": "x"}, wantKey: "not_a_real_option"},
		{name: "unparsable option", options: Options{"temperature": "not-a-number"}, wantKey: "temperature"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encodeCalls := 0
			decodeCalls := 0
			wire := boundaryWire(
				wireCapabilities{name: "controlled grammar"},
				func(requestState) { encodeCalls++ },
				func() { decodeCalls++ },
			)
			endpoint, err := NewEndpoint("https://provider.invalid/generate", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
			if err != nil {
				t.Fatal(err)
			}
			conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: "opaque-model"}, Config{Settings: Settings{Options: test.options}})
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
			if !strings.Contains(stream.err.Error(), test.wantKey) {
				t.Fatalf("Send error = %v, want offending key %q", stream.err, test.wantKey)
			}
			if encodeCalls != 0 || decodeCalls != 0 {
				t.Fatalf("boundary calls after invalid option: encode/build=%d decode=%d", encodeCalls, decodeCalls)
			}
			afterHistory, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterHistory, beforeHistory) {
				t.Fatalf("History changed: before=%s after=%s", beforeHistory, afterHistory)
			}
			if !reflect.DeepEqual(conversation.settings, beforeSettings) {
				t.Fatalf("option was substituted or dropped: before=%#v after=%#v", beforeSettings, conversation.settings)
			}
		})
	}
}

func TestWireCapabilityDecisionIgnoresOpaqueModel(t *testing.T) {
	// R-3VQ2-7KU1
	settings := Settings{Options: Options{"thinking": "on"}}
	models := []string{"old-looking-model", "released-today/unknown:model-beta"}
	for _, model := range models {
		wire := boundaryWire(
			wireCapabilities{name: "effort-only grammar", reasoning: reasoningShapeEffort},
			func(requestState) { t.Fatalf("model %q reached EncodeRequest after wire rejection", model) },
			func() { t.Fatalf("model %q reached Decode after wire rejection", model) },
		)
		endpoint, err := NewEndpoint("https://provider.invalid", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
		if err != nil {
			t.Fatal(err)
		}
		conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: model}, Config{})
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
	settings := Settings{Options: Options{"thinking_budget": "2048"}}
	var encodedState requestState
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
	useDefaultHTTPClient(t, client)
	classifierCalls := 0
	endpoint, err := NewEndpoint("https://provider.invalid/generate", authFunc(func(context.Context, *http.Request, []byte) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	wire := boundaryWire(
		wireCapabilities{name: "budget grammar", reasoning: reasoningShapeBudget},
		func(state requestState) { encodedState = state },
		func() { t.Fatal("non-2xx response must not be decoded") },
	)
	wire.classifier = func(status int, header http.Header, body []byte) error {
		classifierCalls++
		if status != http.StatusBadRequest || header.Get("X-Vendor") != "exact" || !bytes.Equal(body, responseBody) {
			t.Fatalf("classifier inputs = (%d, %#v, %q)", status, header, body)
		}
		return classified
	}
	conversation := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: unknownModel}, Config{})
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

func boundaryWire(capabilities wireCapabilities, encoded func(requestState), decoded func()) *boundaryTestWire {
	return &boundaryTestWire{wireCodec: wireCodec{
		capabilities: capabilities,
		optionSpecs:  wireOptionSpecsWithStop,
		encode: func(state requestState) ([]byte, error) {
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
	endpoint        string
	model           string
	states          []requestState
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

func (p *phase15Provider) BuildRequest(ctx context.Context, state requestState) (*http.Request, error) {
	p.states = append(p.states, cloneRequestState(state))
	if state.Output != nil {
		state.Output.MaxAttempts = 999
		if len(state.Output.Schema) > 0 {
			state.Output.Schema[0] = '!'
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
	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = "phase15"
	}
	return Identity{Endpoint: endpoint, AuthMode: "fixture", Model: p.model}
}

func (p *phase15Provider) turnAccounting() providerAccounting {
	p.accountingCalls++
	index := p.decodeCalls - 1
	if index >= 0 && index < len(p.accounting) {
		return p.accounting[index]
	}
	return providerAccounting{}
}

func cloneRequestState(state requestState) requestState {
	return requestState{
		Model:    state.Model,
		History:  cloneHistory(state.History),
		Settings: cloneSettings(state.Settings),
		Tools:    cloneTools(state.Tools),
		Output:   cloneOutputContract(state.Output),
	}
}

func successfulPhase15Client(transportCalls *int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		*transportCalls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
}

func TestOffCatalogConversationConstructsSendsAndPricesToZero(t *testing.T) {
	// R-NRNO-TPO5
	// R-JYAN-G5DP
	const model = "released-today-uncataloged"
	if _, err := Lookup(model, "", ""); err == nil {
		t.Fatalf("test model %q unexpectedly resolved in catalog", model)
	}
	provider := &phase15Provider{
		model: model,
		responses: [][]Event{{MessageDone{Message: Message{
			Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}},
		}}}},
		accounting: []providerAccounting{{usage: Usage{InputTokens: 2, OutputTokens: 3}}},
	}
	transportCalls := 0
	var logOutput bytes.Buffer
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Log: NewLog(&logOutput, func() time.Time { return time.Time{} }),
	})
	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	drainStream(stream)
	if stream.Err() != nil || transportCalls != 1 || len(provider.states) != 1 {
		t.Fatalf("off-catalog Send error=%v transport=%d states=%d, want normal completed request", stream.Err(), transportCalls, len(provider.states))
	}
	if provider.states[0].Model != model {
		t.Fatalf("sent model = %q, want %q verbatim", provider.states[0].Model, model)
	}
	for _, record := range decodeLogRecords(t, logOutput.Bytes()) {
		if record.Type == RecordUsage {
			if record.Cost == nil || *record.Cost != 0 {
				t.Fatalf("off-catalog usage cost = %v, want zero", record.Cost)
			}
			return
		}
	}
	t.Fatal("off-catalog Send emitted no usage record")
}

func TestCatalogPricingUsesMergedUsageAcrossRounds(t *testing.T) {
	// R-NP7W-266R
	call := ToolUse{ID: "catalog-price-call", Name: "lookup", Input: json.RawMessage(`{"key":"value"}`)}
	provider := &phase15Provider{
		endpoint: string(OfferingOpenAIResponses),
		model:    "gpt-5.4",
		responses: [][]Event{
			{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{call}}}},
			{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}}},
		},
		accounting: []providerAccounting{
			{usage: Usage{InputTokens: 100_000, CachedTokens: 20_000, OutputTokens: 3, ReasoningTokens: 5}},
			{usage: Usage{InputTokens: 100_000, CachedTokens: 60_001, OutputTokens: 7, ReasoningTokens: 11}},
		},
	}
	transportCalls := 0
	var logOutput bytes.Buffer
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Log: NewLog(&logOutput, func() time.Time { return time.Time{} }),
		Tools: []Tool{MustTool("lookup", "", func(context.Context, phase15Input) (string, error) {
			return "found", nil
		})},
	})
	stream := conversation.Send(context.Background(), Text{Text: "price both rounds"})
	drainStream(stream)
	if stream.Err() != nil || transportCalls != 2 || provider.accountingCalls != 2 {
		t.Fatalf("multi-round Send error=%v transport=%d accounting=%d, want successful two-round turn", stream.Err(), transportCalls, provider.accountingCalls)
	}

	const wantCost = Cost(200_000*5_000 + 80_001*500 + (3+5+7+11)*22_500)
	for _, record := range decodeLogRecords(t, logOutput.Bytes()) {
		if record.Type == RecordUsage {
			if record.Usage == nil || *record.Usage != (Usage{InputTokens: 200_000, CachedTokens: 80_001, OutputTokens: 10, ReasoningTokens: 16}) {
				t.Fatalf("logged merged usage = %#v, want both rounds summed", record.Usage)
			}
			if record.Cost == nil || *record.Cost != wantCost {
				t.Fatalf("merged catalog cost = %v, want exact second-tier amount %d", record.Cost, wantCost)
			}
			return
		}
	}
	t.Fatal("multi-round Send emitted no usage record")
}

func TestZeroConfigLeavesEveryOptionalRequestAxisEmpty(t *testing.T) {
	// R-SQPK-3AUV
	provider := &phase15Provider{
		model: "zero-config-model",
		responses: [][]Event{{MessageDone{Message: Message{
			Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}},
		}}}},
	}
	transportCalls := 0
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	events := drainStream(stream)

	if stream.Err() != nil || transportCalls != 1 || len(provider.states) != 1 {
		t.Fatalf("zero-config Send = %v, transport=%d states=%d", stream.Err(), transportCalls, len(provider.states))
	}
	state := provider.states[0]
	if !reflect.DeepEqual(state.Settings, Settings{}) || len(state.Tools) != 0 {
		t.Fatalf("zero-config request axes = settings %#v, tools %#v", state.Settings, state.Tools)
	}
	if state.Output != nil {
		t.Fatal("zero Config declared structured output through requestState")
	}
	// R-UEGM-U26W
	if len(events) != 1 || reflect.TypeOf(events[0]) != reflect.TypeFor[MessageDone]() {
		t.Fatalf("no-contract events = %#v, want one MessageDone and no OutputDone", events)
	}
	if conversation.eventSink != nil {
		t.Fatalf("nil Config.Log installed event sink %#v", conversation.eventSink)
	}
}

func TestConfiguredToolsSettingsAndOptionsPersistAcrossTurns(t *testing.T) {
	// R-ST5C-UUC9
	// R-NXDY-NSEB
	callbackCalls := 0
	tool := MustTool("configured_tool", "", func(context.Context, phase15Input) (string, error) {
		callbackCalls++
		return "called", nil
	})
	settings := Settings{Options: Options{"temperature": "0.4", "stop": `["END"]`}}
	call := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{
		ID: "configured-call", Name: tool.Name(), Input: json.RawMessage(`{"city":"Oslo"}`),
	}}}
	doneOne := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "first done"}}}
	doneTwo := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "second done"}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{
		{MessageDone{Message: call}}, {MessageDone{Message: doneOne}}, {MessageDone{Message: doneTwo}},
	}}
	transportCalls := 0
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Tools: []Tool{tool}, Settings: settings,
	})
	first := conversation.Send(context.Background(), Text{Text: "turn one"})
	drainStream(first)
	second := conversation.Send(context.Background(), Text{Text: "turn two"})
	drainStream(second)

	if first.Err() != nil || second.Err() != nil || callbackCalls != 1 || transportCalls != 3 || len(provider.states) != 3 {
		t.Fatalf("configured turns = errors %v/%v callback=%d transport=%d states=%d", first.Err(), second.Err(), callbackCalls, transportCalls, len(provider.states))
	}
	for index, state := range provider.states {
		if got := toolNames(state.Tools); !reflect.DeepEqual(got, []string{"configured_tool"}) {
			t.Errorf("state %d eager tools = %v", index, got)
		}
		if !reflect.DeepEqual(state.Settings, settings) {
			t.Errorf("state %d fixed config = settings %#v", index, state.Settings)
		}
	}
	result := provider.states[1].History[len(provider.states[1].History)-1].Blocks[0].(ToolResult)
	if result.ToolUseID != "configured-call" || result.Content != "called" || result.IsError {
		t.Fatalf("configured eager dispatch result = %#v", result)
	}
}

func TestConfiguredLogReceivesEveryConversationTurn(t *testing.T) {
	// R-SY0Y-DXB1
	done := MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{done}, {done}}}
	transportCalls := 0
	var output bytes.Buffer
	log := NewLog(&output, func() time.Time { return time.Date(2034, 2, 3, 4, 5, 6, 0, time.UTC) })
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Log: log})
	for _, prompt := range []string{"first", "second"} {
		stream := conversation.Send(context.Background(), Text{Text: prompt})
		drainStream(stream)
		if stream.Err() != nil {
			t.Fatal(stream.Err())
		}
	}
	records := decodeLogRecords(t, output.Bytes())
	counts := make(map[RecordType]int)
	for _, record := range records {
		counts[record.Type]++
	}
	if counts[RecordTurnStart] != 2 || counts[RecordMessage] != 2 || counts[RecordTurnEnd] != 2 {
		t.Fatalf("configured log record counts = %#v; records=%#v", counts, records)
	}

	nilLogConversation := newConversation(&phase15Provider{model: "model", responses: [][]Event{{done}}}, successfulPhase15Client(new(int)), Config{})
	nilLogStream := nilLogConversation.Send(context.Background(), Text{Text: "nil log"})
	drainStream(nilLogStream)
	if nilLogStream.Err() != nil || nilLogConversation.eventSink != nil {
		t.Fatalf("nil Config.Log = err %v sink %#v", nilLogStream.Err(), nilLogConversation.eventSink)
	}
}

func TestOutputContractValidationAtSendBoundary(t *testing.T) {
	validSchema := json.RawMessage(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`)
	invalidSchema := json.RawMessage(`{"type":"object","oneOf":[]}`)
	if err := ValidateOutputSchema(invalidSchema); err == nil {
		t.Fatal("invalid-schema fixture unexpectedly passed ValidateOutputSchema")
	}

	invalid := []struct {
		name       string
		contract   *OutputContract
		diagnostic string
	}{
		{name: "schema outside output subset", contract: &OutputContract{Schema: invalidSchema}, diagnostic: "oneOf"},
		{name: "negative attempt limit", contract: &OutputContract{Schema: validSchema, MaxAttempts: -1}, diagnostic: "MaxAttempts"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			provider := &phase15Provider{model: "model"}
			transportCalls := 0
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: test.contract})
			if conversation == nil {
				t.Fatal("construction rejected output config before Send")
			}
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "unchanged"}}}}
			before, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}

			stream := conversation.Send(context.Background(), Text{Text: "not committed"})
			if stream.Err() != nil || len(provider.states) != 0 || transportCalls != 0 {
				t.Fatalf("unconsumed Stream crossed validation boundary: err=%v build=%d transport=%d", stream.Err(), len(provider.states), transportCalls)
			}
			events := drainStream(stream)
			// R-U5XC-5O01
			if !errors.Is(stream.Err(), ErrInvalidConfig) || !strings.Contains(stream.Err().Error(), test.diagnostic) {
				t.Fatalf("Send error = %v, want diagnostic ErrInvalidConfig containing %q", stream.Err(), test.diagnostic)
			}
			if len(events) != 0 || len(provider.states) != 0 || provider.decodeCalls != 0 || provider.classifyCalls != 0 || transportCalls != 0 {
				t.Fatalf("invalid output config effects: events=%#v build=%d decode=%d classify=%d transport=%d", events, len(provider.states), provider.decodeCalls, provider.classifyCalls, transportCalls)
			}
			after, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("history changed: before=%s after=%s", before, after)
			}

			encodeCalls, authCalls := 0, 0
			decodeCalls := 0
			wire := boundaryWire(wireCapabilities{name: "output boundary"}, func(requestState) {
				encodeCalls++
			}, func() {
				decodeCalls++
			})
			endpoint, err := NewEndpoint("https://provider.invalid/generate", authFunc(func(context.Context, *http.Request, []byte) error { authCalls++; return nil }))
			if err != nil {
				t.Fatal(err)
			}
			composed := newEndpointConversation(wire, endpoint, Identity{Endpoint: "controlled", Model: "model"}, Config{Output: test.contract})
			composed.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "unchanged"}}}}
			composedBefore, err := json.Marshal(composed.history)
			if err != nil {
				t.Fatal(err)
			}
			composedStream := composed.Send(context.Background(), Text{Text: "not committed"})
			composedEvents := drainStream(composedStream)
			composedAfter, err := json.Marshal(composed.history)
			if err != nil {
				t.Fatal(err)
			}
			// R-U5XC-5O01
			if !errors.Is(composedStream.Err(), ErrInvalidConfig) || len(composedEvents) != 0 ||
				encodeCalls != 0 || authCalls != 0 || decodeCalls != 0 ||
				!bytes.Equal(composedAfter, composedBefore) {
				t.Fatalf("composed boundary effects: err=%v events=%#v encode=%d auth=%d decode=%d history=%s",
					composedStream.Err(), composedEvents, encodeCalls, authCalls, decodeCalls, composedAfter)
			}
		})
	}

	accepted := []struct {
		name     string
		contract *OutputContract
	}{
		{name: "nil contract"},
		{name: "zero attempt limit", contract: &OutputContract{Schema: validSchema}},
		{name: "positive attempt limit", contract: &OutputContract{Schema: validSchema, MaxAttempts: 2}},
	}
	for _, test := range accepted {
		t.Run(test.name, func(t *testing.T) {
			provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: Message{
				Role: RoleAssistant, Blocks: []Block{Text{Text: `{}`}},
			}}}}}
			transportCalls := 0
			stream := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: test.contract}).Send(context.Background(), Text{Text: "accepted"})
			drainStream(stream)
			// R-U5XC-5O01
			if stream.Err() != nil || len(provider.states) != 1 || transportCalls != 1 {
				t.Fatalf("accepted output boundary: err=%v build=%d transport=%d", stream.Err(), len(provider.states), transportCalls)
			}
		})
	}
}

func TestOutputContractIsOwnedByConversation(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`)
	contract := &OutputContract{Schema: schema, MaxAttempts: 1}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: Message{
		Role: RoleAssistant, Blocks: []Block{Text{Text: `{}`}},
	}}}}}
	transportCalls := 0
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: contract})

	copy(schema[9:15], "broken")
	contract.Schema = json.RawMessage(`{"type":"object","oneOf":[]}`)
	contract.MaxAttempts = -1
	stream := conversation.Send(context.Background(), Text{Text: "uses construction-time copy"})
	drainStream(stream)

	// R-U5XC-5O01
	if stream.Err() != nil || len(provider.states) != 1 || transportCalls != 1 {
		t.Fatalf("caller mutation altered retained output contract: err=%v build=%d transport=%d", stream.Err(), len(provider.states), transportCalls)
	}
}

func TestStructuredOutputValidTerminationPreservesBytesAndOrder(t *testing.T) {
	text := "  {\n  \"score\": 0.30, \"label\": \"ok\"\n}  "
	message := Message{Role: RoleAssistant, Blocks: []Block{Reasoning{Text: "ignore me"}, Text{Text: text}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"score":{"type":"number","minimum":0,"multipleOf":0.1},"label":{"type":"string","pattern":"^[a-z]+$"}},"required":["score","label"]}`)
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: &OutputContract{Schema: schema, MaxAttempts: 1}})
	user := Text{Text: "answer"}
	stream := conversation.Send(context.Background(), user)
	events := drainStream(stream)

	// R-U758-JFQQ
	if len(events) != 2 || !reflect.DeepEqual(events[0], MessageDone{Message: message}) || transportCalls != 1 || stream.Err() != nil {
		t.Fatalf("structured turn events = %#v, transport=%d err=%v", events, transportCalls, stream.Err())
	}
	// R-UEGM-U26W
	output, ok := events[1].(OutputDone)
	if !ok || !bytes.Equal(output.Value, []byte(text)) || provider.states[0].Output == nil {
		t.Fatalf("final output = %#v, state output=%#v", events[1], provider.states[0].Output)
	}
	if got := conversation.history; !reflect.DeepEqual(got, History{{Role: RoleUser, Blocks: []Block{user}}, message}) {
		t.Fatalf("history = %#v, want messages only", got)
	}
	if provider.states[0].Output == conversation.output || !bytes.Equal(conversation.output.Schema, schema) {
		t.Fatal("request did not own a defensive output-contract snapshot")
	}
}

func TestOutputDrivesStructuredStreamOnceAndRetainsResult(t *testing.T) {
	type result struct {
		Answer string `json:"answer"`
	}
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"answer":"yes"}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"answer":{"type":"string"}},"required":["answer"]}`)
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Output: &OutputContract{Schema: schema, MaxAttempts: 1},
	})
	stream := conversation.Send(context.Background(), Text{Text: "answer"})

	first, err := Output[result](stream)
	second, secondErr := Output[result](stream)
	// R-UFOJ-7TXL
	if err != nil || secondErr != nil || first != (result{Answer: "yes"}) || second != first ||
		transportCalls != 1 || len(provider.states) != 1 || len(drainStream(stream)) != 0 {
		t.Fatalf("Output calls = (%#v, %v), (%#v, %v); transport=%d states=%d",
			first, err, second, secondErr, transportCalls, len(provider.states))
	}
}

func TestOutputDecodesNormallyDrainedStructuredStream(t *testing.T) {
	type result struct {
		Count int `json:"count"`
	}
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"count":4}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer"}},"required":["count"]}`)
	stream := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Output: &OutputContract{Schema: schema, MaxAttempts: 1},
	}).Send(context.Background(), Text{Text: "count"})
	events := drainStream(stream)

	got, err := Output[result](stream)
	// R-UFOJ-7TXL
	if err != nil || got != (result{Count: 4}) || len(events) != 2 || transportCalls != 1 || len(provider.states) != 1 {
		t.Fatalf("drained Output = (%#v, %v), events=%#v transport=%d states=%d",
			got, err, events, transportCalls, len(provider.states))
	}
}

func TestOutputReturnsExactTerminalStreamError(t *testing.T) {
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"value":-1}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"value":{"type":"integer","minimum":0}},"required":["value"]}`)
	stream := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Output: &OutputContract{Schema: schema, MaxAttempts: 1},
	}).Send(context.Background(), Text{Text: "answer"})

	got, err := Output[map[string]int](stream)
	var outputTerminal, streamTerminal *Error
	errors.As(err, &outputTerminal)
	errors.As(stream.Err(), &streamTerminal)
	// R-UFOJ-7TXL
	if err == nil || outputTerminal == nil || outputTerminal != streamTerminal || got != nil ||
		transportCalls != 1 || len(provider.states) != 1 {
		t.Fatalf("terminal Output = (%#v, %v), stream err=%v transport=%d states=%d",
			got, err, stream.Err(), transportCalls, len(provider.states))
	}
}

func TestOutputDoesNotResumeAbandonedStructuredStream(t *testing.T) {
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"answer":"yes"}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"answer":{"type":"string"}},"required":["answer"]}`)
	stream := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Output: &OutputContract{Schema: schema, MaxAttempts: 1},
	}).Send(context.Background(), Text{Text: "answer"})
	for range stream.Events() {
		break
	}

	got, err := Output[map[string]string](stream)
	// R-UFOJ-7TXL
	if err == nil || got != nil || !strings.Contains(err.Error(), "without completed output") ||
		transportCalls != 1 || len(provider.states) != 1 {
		t.Fatalf("abandoned Output = (%#v, %v), transport=%d states=%d", got, err, transportCalls, len(provider.states))
	}
}

func TestStructuredOutputRejectsEmptyCompletedResponseWithoutCommit(t *testing.T) {
	prior := History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`)
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: &OutputContract{Schema: schema, MaxAttempts: 1}})
	conversation.history = cloneHistory(prior)

	stream := conversation.Send(context.Background(), Text{Text: "answer"})
	events := drainStream(stream)

	// R-U758-JFQQ
	if !errors.Is(stream.Err(), ErrInvalidOutput) || transportCalls != 1 || len(events) != 0 {
		t.Fatalf("empty structured response: err=%v transport=%d events=%#v", stream.Err(), transportCalls, events)
	}
	if !reflect.DeepEqual(conversation.history, prior) {
		t.Fatalf("empty structured response committed history: got %#v, want %#v", conversation.history, prior)
	}
}

func TestStructuredOutputToolRoundTripSkipsTextValidation(t *testing.T) {
	call := ToolUse{ID: "call", Name: "weather", Input: json.RawMessage(`{"city":"Oslo"}`)}
	first := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "not json"}, call}}
	finalText := `{"answer":"sunny"}`
	final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: finalText}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{
		{MessageDone{Message: first}},
		{MessageDone{Message: final}},
	}}
	transportCalls := 0
	toolCalls := 0
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"answer":{"type":"string"}},"required":["answer"]}`)
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: &OutputContract{Schema: schema, MaxAttempts: 1}})
	conversation.tools = []Tool{MustTool("weather", "", func(context.Context, phase15Input) (string, error) {
		toolCalls++
		return "sunny", nil
	})}
	user := Message{Role: RoleUser, Blocks: []Block{Text{Text: "forecast"}}}
	stream := conversation.Send(context.Background(), user.Blocks...)
	events := drainStream(stream)
	result := ToolResult{ToolUseID: call.ID, Content: "sunny"}
	wantEvents := []Event{
		MessageDone{Message: first},
		ToolCall{Use: call},
		ToolReturn{Result: result},
		MessageDone{Message: final},
		OutputDone{Value: json.RawMessage(finalText)},
	}

	// R-U8D4-X7HF
	// R-UC0U-2IPI
	if stream.Err() != nil || transportCalls != 2 || toolCalls != 1 || len(provider.states) != 2 || !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("tool output turn: err=%v transport=%d tool=%d states=%d events=%#v, want %#v",
			stream.Err(), transportCalls, toolCalls, len(provider.states), events, wantEvents)
	}
	wantHistory := History{user, first, {Role: RoleTool, Blocks: []Block{result}}, final}
	if !reflect.DeepEqual(conversation.history, wantHistory) {
		t.Fatalf("committed tool output history = %#v, want %#v", conversation.history, wantHistory)
	}
	if provider.states[0].Output == nil || provider.states[1].Output == nil ||
		provider.states[0].Output == provider.states[1].Output ||
		!bytes.Equal(provider.states[1].Output.Schema, schema) || provider.states[1].Output.MaxAttempts != 1 {
		t.Fatalf("output snapshots = %#v, %#v", provider.states[0].Output, provider.states[1].Output)
	}
	last := provider.states[1].History[len(provider.states[1].History)-1]
	if last.Role != RoleTool || len(last.Blocks) != 1 {
		t.Fatalf("second request history = %#v", provider.states[1].History)
	}
}

func TestStructuredOutputRejectsLocalConstraintWithoutCommit(t *testing.T) {
	prior := History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"rows":[{"line":-3}]}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"rows":{"type":"array","uniqueItems":true,"items":{"type":"object","additionalProperties":false,"properties":{"line":{"type":"number","minimum":0}},"required":["line"]}}},"required":["rows"]}`)
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: &OutputContract{Schema: schema, MaxAttempts: 1}})
	conversation.history = cloneHistory(prior)
	stream := conversation.Send(context.Background(), Text{Text: "answer"})
	events := drainStream(stream)

	// R-UJC8-D55O
	if !errors.Is(stream.Err(), ErrInvalidOutput) || transportCalls != 1 {
		t.Fatalf("local constraint result: err=%v transport=%d", stream.Err(), transportCalls)
	}
	if len(events) != 1 || !reflect.DeepEqual(events[0], MessageDone{Message: message}) || !reflect.DeepEqual(conversation.history, prior) {
		t.Fatalf("rejected effects: events=%#v history=%#v", events, conversation.history)
	}
}

func TestStructuredOutputFormatGateAndLocalMeaning(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"when":{"type":"string","format":"date"}},"required":["when"]}`)
	for _, test := range []struct {
		name string
		text string
		ok   bool
	}{{"valid", `{"when":"2024-02-29"}`, true}, {"invalid", `{"when":"2023-02-29"}`, false}} {
		t.Run(test.name, func(t *testing.T) {
			provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: test.text}}}}}}}
			transportCalls := 0
			stream := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: &OutputContract{Schema: schema, MaxAttempts: 1}}).Send(context.Background(), Text{Text: "date"})
			events := drainStream(stream)
			// R-UKK4-QWWD
			if (stream.Err() == nil) != test.ok || transportCalls != 1 || (len(events) == 2) != test.ok {
				t.Fatalf("format turn: err=%v events=%#v transport=%d", stream.Err(), events, transportCalls)
			}
		})
	}

	provider := &phase15Provider{model: "model"}
	transportCalls := 0
	unsupported := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","format":"future"}},"required":["x"]}`)
	stream := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: &OutputContract{Schema: unsupported}}).Send(context.Background(), Text{Text: "x"})
	drainStream(stream)
	if !errors.Is(stream.Err(), ErrInvalidConfig) || transportCalls != 0 || len(provider.states) != 0 {
		t.Fatalf("unsupported format crossed provider gate: err=%v transport=%d", stream.Err(), transportCalls)
	}
}

func TestStructuredOutputEarlyStopDoesNotCommit(t *testing.T) {
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	schema := json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Output: &OutputContract{Schema: schema}})
	stream := conversation.Send(context.Background(), Text{Text: "answer"})
	seen := 0
	for range stream.Events() {
		seen++
		if seen == 2 {
			break
		}
	}
	// R-UEGM-U26W
	if seen != 2 || len(conversation.history) != 0 || len(drainStream(stream)) != 0 {
		t.Fatalf("early output stop: seen=%d history=%#v", seen, conversation.history)
	}
}

func TestStructuredOutputEarlyStopOnCorrectionDoesNotRedrive(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"value":{"type":"integer"}},"required":["value"]}`)
	rejected := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"wrong":true}`}}}
	accepted := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"value":1}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{
		{MessageDone{Message: rejected}}, {MessageDone{Message: accepted}},
	}}
	transportCalls := 0
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Output: &OutputContract{Schema: schema, MaxAttempts: 2},
	})
	prior := History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
	conversation.history = cloneHistory(prior)
	stream := conversation.Send(context.Background(), Text{Text: "answer"})
	var seen []Event
	for event := range stream.Events() {
		seen = append(seen, event)
		if len(seen) == 2 {
			break
		}
	}
	// R-UASX-OQYT
	if len(seen) != 2 || seen[1].(MessageDone).Message.Role != RoleUser || transportCalls != 1 ||
		len(provider.states) != 1 || stream.Err() != nil || !reflect.DeepEqual(conversation.history, prior) {
		t.Fatalf("correction early stop: events=%#v transport=%d states=%d err=%v history=%#v",
			seen, transportCalls, len(provider.states), stream.Err(), conversation.history)
	}
}

func TestStructuredOutputCorrectionRetriesAndCommitsFullTranscript(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object","additionalProperties":false,
		"properties":{
			"label":{"type":"string","minLength":2,"pattern":"^[a-z]+$"},
			"rows":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"line":{"type":"number","minimum":0}},"required":["line"]}},
			"missing":{"type":"boolean"}
		},"required":["label","rows","missing"]}`)
	rejectedText := `{"label":"","rows":[{"line":-3.0000000000000000001},{"line":-4}],"extra":"bad\"value"}`
	acceptedText := `{"label":"ok","rows":[{"line":0}],"missing":true}`
	rejected := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: rejectedText}}}
	accepted := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: acceptedText}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{
		{MessageDone{Message: rejected}},
		{MessageDone{Message: accepted}},
	}}
	transportCalls := 0
	var logOutput bytes.Buffer
	log := NewLog(&logOutput, func() time.Time { return time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC) })
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Output: &OutputContract{Schema: schema, MaxAttempts: 2}, Log: log,
	})
	user := Message{Role: RoleUser, Blocks: []Block{Text{Text: "answer"}}}
	events := drainStream(conversation.Send(context.Background(), user.Blocks...))

	if len(events) != 4 {
		t.Fatalf("corrected event count = %d, want 4: %#v", len(events), events)
	}
	correctiveEvent, ok := events[1].(MessageDone)
	if !ok || correctiveEvent.Message.Role != RoleUser || len(correctiveEvent.Message.Blocks) != 1 {
		t.Fatalf("corrective event = %#v", events[1])
	}
	correctiveText := correctiveEvent.Message.Blocks[0].(Text).Text
	for _, exact := range []string{
		`$.missing: is required; offending value: missing`,
		`$.extra: property must be declared by the schema; offending value: "bad\"value"`,
		`$.label: length must be at least 2; offending value: ""`,
		`$.label: must match pattern "^[a-z]+$"; offending value: ""`,
		`$.rows[0].line: must be >= 0; offending value: -3.0000000000000000001`,
		`$.rows[1].line: must be >= 0; offending value: -4`,
	} {
		// R-UASX-OQYT
		if !strings.Contains(correctiveText, exact) {
			t.Errorf("corrective text missing %q:\n%s", exact, correctiveText)
		}
	}
	corrective := correctiveEvent.Message
	wantEvents := []Event{
		MessageDone{Message: rejected}, MessageDone{Message: corrective},
		MessageDone{Message: accepted}, OutputDone{Value: json.RawMessage(acceptedText)},
	}
	if transportCalls != 2 || len(provider.states) != 2 || !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("corrected turn: transport=%d states=%d events=%#v, want %#v", transportCalls, len(provider.states), events, wantEvents)
	}
	if got, want := History(provider.states[1].History), (History{user, rejected, corrective}); !reflect.DeepEqual(got, want) {
		t.Fatalf("second request history = %#v, want %#v", got, want)
	}
	if provider.states[1].Output == nil || provider.states[1].Output == conversation.output ||
		!bytes.Equal(provider.states[1].Output.Schema, conversation.output.Schema) {
		t.Fatalf("second request output snapshot = %#v, retained %#v", provider.states[1].Output, conversation.output)
	}
	// R-UD8Q-GAG7
	if got, want := conversation.history, (History{user, rejected, corrective, accepted}); !reflect.DeepEqual(got, want) {
		t.Fatalf("corrected history = %#v, want %#v", got, want)
	}
	var loggedMessages History
	var projected []Event
	outputCount := 0
	outputPosition := -1
	records := decodeLogRecords(t, logOutput.Bytes())
	for index, record := range records {
		switch record.Type {
		case RecordMessage:
			loggedMessages = append(loggedMessages, *record.Message)
			projected = append(projected, MessageDone{Message: *record.Message})
		case RecordOutput:
			outputCount++
			outputPosition = index
			projected = append(projected, OutputDone{Value: record.Output})
		}
	}
	if !reflect.DeepEqual(loggedMessages, History{rejected, corrective, accepted}) {
		t.Fatalf("logged corrected messages = %#v", loggedMessages)
	}
	// R-UI4B-ZDEZ
	if len(records) != 7 || outputCount != 1 || outputPosition != 4 || !bytes.Equal(records[outputPosition].Output, []byte(acceptedText)) ||
		!reflect.DeepEqual(projected, events) || records[5].Type != RecordUsage || records[6].Type != RecordTurnEnd {
		t.Fatalf("structured log does not mirror live events exactly once in order: records=%#v projected=%#v events=%#v", records, projected, events)
	}
}

func TestStructuredOutputAttemptLimitsExhaustWithoutCommit(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"value":{"type":"integer","minimum":0}},"required":["value"]}`)
	prior := History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
	for _, test := range []struct {
		name         string
		maxAttempts  int
		wantAttempts int
	}{{name: "default", wantAttempts: 3}, {name: "one", maxAttempts: 1, wantAttempts: 1}} {
		t.Run(test.name, func(t *testing.T) {
			responses := make([][]Event, test.wantAttempts)
			for index := range responses {
				message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: fmt.Sprintf(`{"value":-%d}`, index+1)}}}
				responses[index] = []Event{MessageDone{Message: message}}
			}
			provider := &phase15Provider{model: "model", responses: responses}
			transportCalls := 0
			var logOutput bytes.Buffer
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
				Output: &OutputContract{Schema: schema, MaxAttempts: test.maxAttempts},
				Log:    NewLog(&logOutput, func() time.Time { return time.Time{} }),
			})
			conversation.history = cloneHistory(prior)
			stream := conversation.Send(context.Background(), Text{Text: "answer"})
			events := drainStream(stream)

			var terminal *Error
			wantIdentity := Identity{Endpoint: "phase15", AuthMode: "fixture", Model: "model"}
			// R-UC0U-2IPI
			if !errors.As(stream.Err(), &terminal) || !errors.Is(stream.Err(), ErrInvalidOutput) ||
				terminal.Category != CategoryUnknown || terminal.Endpoint != wantIdentity || Retryable(stream.Err()) {
				t.Fatalf("terminal error = %#v, want non-retryable CategoryUnknown wrapping ErrInvalidOutput for %#v", stream.Err(), wantIdentity)
			}
			if transportCalls != test.wantAttempts || len(provider.states) != test.wantAttempts {
				t.Fatalf("attempts = transport %d states %d, want %d", transportCalls, len(provider.states), test.wantAttempts)
			}
			if !reflect.DeepEqual(conversation.history, prior) {
				t.Fatalf("exhausted history = %#v, want unchanged %#v", conversation.history, prior)
			}
			wantEvents := test.wantAttempts*2 - 1
			if len(events) != wantEvents {
				t.Fatalf("exhausted events = %d, want %d: %#v", len(events), wantEvents, events)
			}
			for _, event := range events {
				if _, ok := event.(OutputDone); ok {
					t.Fatalf("exhausted turn emitted OutputDone: %#v", events)
				}
			}
			for _, record := range decodeLogRecords(t, logOutput.Bytes()) {
				if record.Type == RecordOutput {
					t.Fatalf("exhausted turn logged output without OutputDone: %#v", record)
				}
			}
		})
	}
}

func TestNoContractTurnDoesNotLogOutput(t *testing.T) {
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: `{"value":1}`}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	var logOutput bytes.Buffer
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Log: NewLog(&logOutput, func() time.Time { return time.Time{} }),
	})
	events := drainStream(conversation.Send(context.Background(), Text{Text: "answer"}))
	if !reflect.DeepEqual(events, []Event{MessageDone{Message: message}}) {
		t.Fatalf("no-contract events = %#v", events)
	}
	for _, record := range decodeLogRecords(t, logOutput.Bytes()) {
		if record.Type == RecordOutput {
			t.Fatalf("no-contract turn logged output: %#v", record)
		}
	}
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
		case RecordOutput:
			projected = append(projected, OutputDone{Value: record.Output})
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
	text := `{"value":1}`
	message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: text}}}
	provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: message}}}}
	transportCalls := 0
	failure := &failingLogWriter{}
	writer := &scriptedLogWriter{steps: []io.Writer{io.Discard, io.Discard, failure}}
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Output: &OutputContract{Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"]}`)},
		Log:    NewLog(writer, func() time.Time { return time.Time{} }),
	})
	stream := conversation.Send(context.Background(), Text{Text: "hello"})
	wantEvents := []Event{MessageDone{Message: message}, OutputDone{Value: json.RawMessage(text)}}
	if events := drainStream(stream); !reflect.DeepEqual(events, wantEvents) || stream.Err() != nil {
		t.Fatalf("failing log changed successful stream: events=%#v err=%v", events, stream.Err())
	}
	if len(conversation.history) != 2 || !failure.called {
		t.Fatalf("output log failure changed history or did not occur at output: history=%#v failure=%t", conversation.history, failure.called)
	}

	terminal := errors.New("decode failed")
	provider = &phase15Provider{model: "model", decodeErrors: []error{terminal}}
	transportCalls = 0
	var output bytes.Buffer
	conversation = newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
	conversation = newConversation(provider, cancelClient, Config{})
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

func TestConversationCloseIsIdempotentAndRejectsLaterSend(t *testing.T) {
	// R-5N4U-3NE5
	provider := &phase15Provider{model: "model"}
	transportCalls := 0
	var output bytes.Buffer
	log := NewLog(&output, func() time.Time { return time.Time{} })
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
	conversation.eventSink = log
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	closedOutput := output.String()
	if err := log.Close(); err != nil || output.String() != closedOutput {
		t.Fatalf("idempotent Close = %v, output changed=%t", err, output.String() != closedOutput)
	}
	stream := conversation.Send(context.Background(), Text{Text: "after close"})
	drainStream(stream)
	if !errors.Is(stream.Err(), ErrClosed) || transportCalls != 0 {
		t.Fatalf("Send after Close err/calls = %v/%d, want ErrClosed/0", stream.Err(), transportCalls)
	}
	records := decodeLogRecords(t, output.Bytes())
	summaryCount := 0
	for _, record := range records {
		if record.Type == RecordSummary {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Fatalf("summary records = %d, want exactly 1: %#v", summaryCount, records)
	}
}

type scriptedLogWriter struct {
	steps []io.Writer
}

func (w *scriptedLogWriter) Write(data []byte) (int, error) {
	if len(w.steps) == 0 {
		return 0, errors.New("unexpected log write")
	}
	step := w.steps[0]
	w.steps = w.steps[1:]
	return step.Write(data)
}

type failingLogWriter struct {
	called bool
}

func (w *failingLogWriter) Write([]byte) (int, error) {
	w.called = true
	return 0, errors.New("log disk full")
}

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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
	conversation.settings = Settings{Options: Options{"temperature": "0.25", "stop": `["END"]`}}
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
			!reflect.DeepEqual(state.Settings, conversation.settings) ||
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
	if got := conversationType.NumMethod(); got != 1 || conversationType.Method(0).Name != "Send" {
		t.Fatalf("Conversation exported methods changed: %v", conversationType)
	}
	if conversation.tools[0] == nil || conversation.settings.Options["stop"] != `["END"]` || conversation.history[0].Blocks == nil {
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
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
	// R-UUBB-T2TX
	// R-NW62-A0NM
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Deferred: []DeferredGroup{{Name: "records", Blurb: "Record operations", Tools: registered}}})
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
	if !bytes.Equal(provider.states[1].Tools[1].Schema(), deferred.Schema()) {
		t.Fatal("loaded deferred member did not retain its full ordinary Tool schema")
	}
	for _, placement := range []string{"eager", "deferred"} {
		t.Run("invalid "+placement, func(t *testing.T) {
			provider := &phase15Provider{model: "model"}
			calls := 0
			invalid := concreteTool{name: "bad", schema: json.RawMessage(`{"type":"array"}`)}
			cfg := Config{}
			if placement == "eager" {
				cfg.Tools = []Tool{invalid}
			} else {
				cfg.Deferred = []DeferredGroup{{Name: "bad_group", Tools: []Tool{invalid}}}
			}
			conversation := newConversation(provider, successfulPhase15Client(&calls), cfg)
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "unchanged"}}}}
			before := cloneHistory(conversation.history)
			failed := conversation.Send(context.Background(), Text{Text: "blocked"})
			drainStream(failed)
			if !errors.Is(failed.Err(), ErrInvalidConfig) || calls != 0 || len(provider.states) != 0 || !reflect.DeepEqual(conversation.history, before) {
				t.Fatalf("%s validation = %v, transport=%d provider=%d history=%#v", placement, failed.Err(), calls, len(provider.states), conversation.history)
			}
		})
	}
}

func TestDeferredGroupsConditionallySynthesizeExactlyOneLoader(t *testing.T) {
	// R-5QSJ-8YM8
	// R-SUD9-8M2Y
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
			conversation := newConversation(&phase15Provider{model: "model"}, http.DefaultClient, Config{Deferred: test.groups})
			o, err := conversation.prepareOrchestrator()
			if err != nil {
				t.Fatal(err)
			}
			if got := toolNames(o.advertisedSnapshot()); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("advertised = %v, want %v", got, test.want)
			}
		})
	}
	conversationType := reflect.TypeFor[*Conversation]()
	if conversationType.NumMethod() != 1 || conversationType.Method(0).Name != "Send" {
		t.Fatalf("post-construction method set = %v, want only Send", conversationType)
	}
	if _, exists := conversationType.MethodByName("Deferred"); exists {
		t.Fatal("post-construction Deferred registration still exists")
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
	conversation := newConversation(&phase15Provider{model: "model"}, http.DefaultClient, Config{Deferred: []DeferredGroup{
		{Name: "group_token_71", Blurb: "blurb token 82", Tools: []Tool{tool}},
		{Name: "group_token_64", Blurb: "blurb token 55", Tools: []Tool{phase17Tool("tool_token_46")}},
	}})
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Deferred: []DeferredGroup{
		{Name: "group_a", Blurb: "A", Tools: []Tool{a1, a2}},
		{Name: "group_b", Blurb: "B", Tools: []Tool{solo, b2}},
	}})
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Deferred: []DeferredGroup{{Name: "all", Blurb: "All", Tools: []Tool{phase17Tool("first"), phase17Tool("second")}}}})
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Tools:    []Tool{phase17Tool("z_eager"), phase17Tool("a_eager")},
		Deferred: []DeferredGroup{{Name: "tails", Blurb: "Tails", Tools: []Tool{phase17Tool("tail_a"), phase17Tool("tail_z")}}},
	})
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
	conversation := newConversation(&phase15Provider{model: "model"}, http.DefaultClient, Config{Deferred: []DeferredGroup{{
		Name:  "search",
		Blurb: "Search stored records",
		Tools: []Tool{phase17Tool("search_records")},
	}}})
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
	// R-NYLV-1K50
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
	conversationWithoutGroups := newConversation(providerWithoutGroups, successfulPhase15Client(&transportWithoutGroups), Config{Tools: []Tool{concreteTool{
		name:   loadToolsName,
		schema: json.RawMessage(`{"type":"object","properties":{}}`),
		call: func(context.Context, json.RawMessage) (string, error) {
			ordinaryCalls++
			return "ordinary consumer tool", nil
		},
	}}})
	streamWithoutGroups := conversationWithoutGroups.Send(context.Background(), Text{Text: "allowed"})
	drainStream(streamWithoutGroups)
	if streamWithoutGroups.Err() != nil || transportWithoutGroups != 2 || ordinaryCalls != 1 {
		t.Fatalf("absent synthetic loader created false collision: err=%v transport=%d ordinary=%d", streamWithoutGroups.Err(), transportWithoutGroups, ordinaryCalls)
	}

	eagerCount := &phase17CountingTool{name: "eager_count", schema: json.RawMessage(`{"type":"object","properties":{}}`), call: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	deferredCount := &phase17CountingTool{name: "deferred_count", schema: json.RawMessage(`{"type":"object","properties":{}}`), call: func(context.Context, json.RawMessage) (string, error) { return "", nil }}
	gateProvider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "ok"}}}}}}}
	gateCalls := 0
	gateConversation := newConversation(gateProvider, successfulPhase15Client(&gateCalls), Config{
		Tools:    []Tool{eagerCount},
		Deferred: []DeferredGroup{{Name: "counted", Tools: []Tool{deferredCount}}},
	})
	gateStream := gateConversation.Send(context.Background(), Text{Text: "validate union"})
	drainStream(gateStream)
	if gateStream.Err() != nil || eagerCount.schemaCalls != 1 || deferredCount.schemaCalls != 1 {
		t.Fatalf("union gate: err=%v eager schemas=%d deferred schemas=%d", gateStream.Err(), eagerCount.schemaCalls, deferredCount.schemaCalls)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &phase15Provider{model: "model"}
			transportCalls := 0
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Tools: test.eager, Deferred: test.deferred})
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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Tools: []Tool{counting}})
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
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
			cfg := Config{}
			if test.deferred {
				callName = "secret"
				first.Blocks[0] = ToolUse{ID: "unknown-id", Name: callName, Input: json.RawMessage(`{"guessed":true}`)}
				secret := concreteTool{name: callName, schema: json.RawMessage(`{"type":"object","properties":{}}`), call: func(context.Context, json.RawMessage) (string, error) {
					callbackCalls++
					return "must not execute", nil
				}}
				cfg.Deferred = []DeferredGroup{{Tools: []Tool{secret}}}
			}
			provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: final}}}}
			transportCalls := 0
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), cfg)

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
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{})
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
			conversation := newConversation(provider, client, Config{})
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
