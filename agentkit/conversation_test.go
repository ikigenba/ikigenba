package agentkit

import (
	"context"
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
	wire     fixtureWire
	endpoint fixtureEndpoint
	model    string
	states   []RequestState
}

func (p *fixtureProvider) BuildRequest(ctx context.Context, state RequestState) (*http.Request, error) {
	p.states = append(p.states, state)
	return http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.url, strings.NewReader(p.wire.name))
}

func (p *fixtureProvider) Decode(context.Context, *http.Response) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		yield(p.wire.name+":"+p.endpoint.name, nil)
	}
}

func (*fixtureProvider) Classify(status int, _ http.Header, _ []byte) error {
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
	stream := conversation.Send(context.Background(), "text")
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
	type imageBlock struct{ URL string }
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	conversation, provider := vendorFixture(server.URL, "model", server.Client())

	conversation.Send(context.Background(), "text", imageBlock{URL: "image.png"})
	if len(provider.states) != 1 || len(provider.states[0].Blocks) != 2 {
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

	vendorStream := vendorConversation.Send(context.Background(), "hello")
	genericStream := genericConversation.Send(context.Background(), "hello")
	if !reflect.DeepEqual(vendorProvider.states, genericProvider.states) {
		t.Fatalf("construction routes produced different states: vendor=%#v generic=%#v", vendorProvider.states, genericProvider.states)
	}
	if !reflect.DeepEqual(vendorStream, genericStream) {
		t.Fatalf("construction routes produced different streams: vendor=%#v generic=%#v", vendorStream, genericStream)
	}
}
