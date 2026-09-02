package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

type parityAuth struct{}

func (parityAuth) Apply(context.Context, *http.Request, []byte) error { return nil }

type parityRequest struct {
	path string
	body []byte
}

func collectParityStream(stream *agentkit.Stream) ([]agentkit.Event, error) {
	var events []agentkit.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events, stream.Err()
}

func TestWithConfigMatchesGenericConstructor(t *testing.T) {
	// R-SZ8U-RP1Q
	requests := make(chan parityRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests <- parityRequest{path: request.URL.Path, body: body}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	cfg := agentkit.Config{Settings: agentkit.Settings{StopSequences: []string{"phase-four"}}}
	vendor, err := New(APIKey("vendor-key"), "parity-model", WithConfig(agentkit.Config{}), WithBaseURL(server.URL+"/v1/messages"), WithAPI(Messages), WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := agentkit.NewEndpoint(server.URL+"/v1/messages", parityAuth{})
	if err != nil {
		t.Fatal(err)
	}
	generic, err := agentkit.NewForWire(agentkit.KnownWireAnthropicMessages, endpoint, "parity-model", cfg)
	if err != nil {
		t.Fatal(err)
	}

	vendorEvents, vendorErr := collectParityStream(vendor.Send(context.Background(), agentkit.Text{Text: "hello"}))
	genericEvents, genericErr := collectParityStream(generic.Send(context.Background(), agentkit.Text{Text: "hello"}))
	vendorRequest, genericRequest := <-requests, <-requests
	if !reflect.DeepEqual(vendorRequest, genericRequest) {
		t.Fatalf("requests differ: vendor=%+v generic=%+v", vendorRequest, genericRequest)
	}
	if !reflect.DeepEqual(vendorEvents, genericEvents) || len(vendorEvents) != 1 {
		t.Fatalf("events differ: vendor=%#v generic=%#v", vendorEvents, genericEvents)
	}
	if vendorErr != nil || genericErr != nil {
		t.Fatalf("stream errors differ: vendor=%v generic=%v", vendorErr, genericErr)
	}
}

func TestOAuthAndBaseURLConflictAtConstruction(t *testing.T) {
	// R-3N6R-J6N6
	calls := 0
	source := tokenSourceFunc(func(context.Context) (string, error) {
		calls++
		return "token", nil
	})
	conversation, err := New(OAuth(source), "model", WithBaseURL("https://example.test/messages"))
	if conversation != nil || !errors.Is(err, agentkit.ErrInvalidConfig) {
		t.Fatalf("New = (%v, %v), want nil ErrInvalidConfig", conversation, err)
	}
	if calls != 0 {
		t.Fatalf("token source called %d times during rejected construction", calls)
	}
}

func TestAPIKeyAllowsBaseURL(t *testing.T) {
	conversation, err := New(APIKey("key"), "model", WithBaseURL("https://example.test/messages"))
	if err != nil || conversation == nil {
		t.Fatalf("New = (%v, %v)", conversation, err)
	}
}

func TestAPIDeclarationDefaultsToMessagesAndRejectsUnshippedTextCodec(t *testing.T) {
	// R-YUJZ-PJ8W
	if Messages != 0 || TextCompletions != 1 {
		t.Fatalf("API values = %d, %d", Messages, TextCompletions)
	}
	conversation, err := New(APIKey("key"), "model")
	if err != nil || conversation == nil {
		t.Fatalf("zero API New = (%v, %v)", conversation, err)
	}
	conversation, err = New(APIKey("key"), "model", WithAPI(TextCompletions))
	if conversation != nil || !errors.Is(err, agentkit.ErrInvalidConfig) {
		t.Fatalf("TextCompletions New = (%v, %v), want nil ErrInvalidConfig without an unshipped codec", conversation, err)
	}
}

func TestNewSelectsAnthropicMessagesWireAndEndpoint(t *testing.T) {
	// R-YPOE-6GA4
	type observedRequest struct {
		path   string
		apiKey string
		body   map[string]json.RawMessage
	}
	seen := make(chan observedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var object map[string]json.RawMessage
		_ = json.Unmarshal(body, &object)
		seen <- observedRequest{
			path:   request.URL.Path,
			apiKey: request.Header.Get("X-Api-Key"),
			body:   object,
		}
		writer.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	conversation, err := New(APIKey("secret"), "verbatim-model", WithBaseURL(server.URL+"/v1/messages"))
	if err != nil {
		t.Fatal(err)
	}
	for event := range conversation.Send(context.Background(), agentkit.Text{Text: "hello"}).Events() {
		_ = event
	}

	request := <-seen
	if request.path != "/v1/messages" || request.apiKey != "secret" {
		t.Fatalf("endpoint request path=%q api-key=%q", request.path, request.apiKey)
	}
	if _, exists := request.body["messages"]; !exists {
		t.Fatalf("selected wire body lacks messages: %v", request.body)
	}
}
