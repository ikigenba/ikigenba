package xai

import (
	"bytes"
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
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	cfg := agentkit.Config{Settings: agentkit.Settings{StopSequences: []string{"phase-four"}}}
	vendor, err := New(APIKey("vendor-key"), "parity-model", WithConfig(agentkit.Config{}), WithBaseURL(server.URL+"/v1/chat/completions"), WithAPI(ChatCompletions), WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := agentkit.NewEndpoint(server.URL+"/v1/chat/completions", parityAuth{})
	if err != nil {
		t.Fatal(err)
	}
	generic, err := agentkit.NewForWire(agentkit.KnownWireOpenAIChat, endpoint, "parity-model", cfg)
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

func TestAPISelectsResponsesByDefaultAndChatAsAlternate(t *testing.T) {
	// R-YWZS-H2QA
	if Responses != 0 || ChatCompletions != 1 {
		t.Fatalf("API values = %d, %d", Responses, ChatCompletions)
	}
	for _, check := range []struct {
		api API
		key string
	}{{Responses, "input"}, {ChatCompletions, "messages"}} {
		seen := make(chan map[string]json.RawMessage, 1)
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			var object map[string]json.RawMessage
			_ = json.Unmarshal(body, &object)
			seen <- object
			writer.Header().Set("Content-Type", "text/event-stream")
		}))
		conversation, err := New(APIKey("key"), "model", WithBaseURL(server.URL), WithAPI(check.api))
		if err != nil {
			t.Fatal(err)
		}
		for event := range conversation.Send(context.Background(), agentkit.Text{Text: "hello"}).Events() {
			_ = event
		}
		if _, exists := (<-seen)[check.key]; !exists {
			t.Fatalf("selected wire body lacks %q", check.key)
		}
		server.Close()
	}
}

func TestOAuthAndBaseURLConflictAtConstruction(t *testing.T) {
	// R-4XIY-2GTK
	calls := 0
	conversation, err := New(OAuth(tokenSourceFunc(func(context.Context) (string, error) {
		calls++
		return "token", nil
	})), "model", WithBaseURL("https://example.test"))
	if conversation != nil || !errors.Is(err, agentkit.ErrInvalidConfig) {
		t.Fatalf("New = (%v, %v), want nil ErrInvalidConfig", conversation, err)
	}
	if calls != 0 {
		t.Fatalf("token source called %d times during rejected construction", calls)
	}
}

func TestNewNamesXAIEndpointAndUsesCatalogPricing(t *testing.T) {
	// R-EKRG-9Y2W
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}}\n\n")
	}))
	defer server.Close()

	var output bytes.Buffer
	conversation, err := New(APIKey("key"), "grok-4.5",
		WithBaseURL(server.URL),
		WithConfig(agentkit.Config{Log: agentkit.NewLog(&output, nil)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, streamErr := collectParityStream(conversation.Send(context.Background(), agentkit.Text{Text: "hello"}))
	if streamErr != nil {
		t.Fatal(streamErr)
	}
	const wantCost = agentkit.Cost(2*2_000 + 3*6_000)
	assertXAITurnIdentityAndCost(t, output.Bytes(), agentkit.ProviderXAI, wantCost)
}

func assertXAITurnIdentityAndCost(t *testing.T, data []byte, provider agentkit.ProviderID, want agentkit.Cost) {
	t.Helper()
	var identity *agentkit.Identity
	var cost *agentkit.Cost
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var record agentkit.LogRecord
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record.Type == agentkit.RecordTurnStart {
			identity = record.Identity
		}
		if record.Type == agentkit.RecordUsage {
			cost = record.Cost
		}
	}
	if identity == nil || identity.Endpoint != string(provider) {
		t.Fatalf("turn identity = %+v, want endpoint %q", identity, provider)
	}
	if cost == nil || *cost != want {
		t.Fatalf("turn cost = %v, want catalog amount %d", cost, want)
	}
}
