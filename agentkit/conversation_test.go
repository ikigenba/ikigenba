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
)

type fixtureWire struct {
	name string
}

type fixtureEndpoint struct {
	name string
	url  string
}

type fixtureProvider struct {
	wire        fixtureWire
	endpoint    fixtureEndpoint
	model       string
	states      []RequestState
	buildErr    error
	decodeErr   error
	classifyErr error
}

func (p *fixtureProvider) BuildRequest(ctx context.Context, state RequestState) (*http.Request, error) {
	p.states = append(p.states, state)
	if p.buildErr != nil {
		return nil, p.buildErr
	}
	return http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.url, strings.NewReader(p.wire.name))
}

func (p *fixtureProvider) Decode(context.Context, *http.Response) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		if !yield(p.wire.name+":"+p.endpoint.name, nil) {
			return
		}
		if p.decodeErr != nil {
			yield(nil, p.decodeErr)
		}
	}
}

func (p *fixtureProvider) Classify(status int, _ http.Header, _ []byte) error {
	if p.classifyErr != nil {
		return p.classifyErr
	}
	return fmt.Errorf("status %d", status)
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
	if stream.err == nil || stream.err.Error() != "status 400" {
		t.Fatalf("stream error = %v, want classified vendor error status 400", stream.err)
	}
	if got := provider.Identity(); got != (Identity{Endpoint: "vendor", AuthMode: "fixture", Model: unknownModel}) {
		t.Fatalf("identity changed or fused: %#v", got)
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
