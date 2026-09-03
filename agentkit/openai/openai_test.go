package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

func collectParityStream(stream *agentkit.Stream) ([]agentkit.Event, error) {
	var events []agentkit.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	return events, stream.Err()
}

func TestOAuthAndBaseURLConflictAtConstruction(t *testing.T) {
	// R-3N6R-J6N6
	// R-4XIY-2GTK
	calls := 0
	source := tokenSourceFunc(func(context.Context) (string, string, error) {
		calls++
		return "token", "account", nil
	})
	conversation, err := New(OAuth(source), "model", WithBaseURL("https://example.test/responses"))
	if conversation != nil || !errors.Is(err, agentkit.ErrInvalidConfig) {
		t.Fatalf("New = (%v, %v), want nil ErrInvalidConfig", conversation, err)
	}
	if calls != 0 {
		t.Fatalf("token source called %d times during rejected construction", calls)
	}
}

func TestAPISelectsResponsesByDefaultAndChatAsAlternate(t *testing.T) {
	// R-YVRW-3AZL
	if Responses != 0 || ChatCompletions != 1 {
		t.Fatalf("API values = %d, %d", Responses, ChatCompletions)
	}
	for _, check := range []struct {
		name string
		api  API
		key  string
	}{{"default responses", Responses, "input"}, {"alternate chat", ChatCompletions, "messages"}} {
		t.Run(check.name, func(t *testing.T) {
			seen := make(chan map[string]json.RawMessage, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				var object map[string]json.RawMessage
				_ = json.Unmarshal(body, &object)
				seen <- object
				writer.Header().Set("Content-Type", "text/event-stream")
			}))
			defer server.Close()
			conversation, err := New(APIKey("key"), "verbatim-model", WithBaseURL(server.URL), WithAPI(check.api))
			if err != nil {
				t.Fatal(err)
			}
			for event := range conversation.Send(context.Background(), agentkit.Text{Text: "hello"}).Events() {
				_ = event
			}
			if _, exists := (<-seen)[check.key]; !exists {
				t.Fatalf("selected wire body lacks %q", check.key)
			}
		})
	}
}

func TestAPIKeyAllowsBaseURL(t *testing.T) {
	conversation, err := New(APIKey("key"), "model", WithBaseURL("https://example.test/responses"))
	if err != nil || conversation == nil {
		t.Fatalf("New = (%v, %v)", conversation, err)
	}
}

func TestNewReportsAuthModeFromCredential(t *testing.T) {
	// R-UFIH-AUGX
	// OAuth credentials reject WithBaseURL (mutually exclusive), so the oauth
	// case builds against the default endpoint and cancels the context before
	// the request goes out; RecordTurnStart carries the AuthMode regardless of
	// whether the send itself reaches a server.
	for _, testCase := range []struct {
		name         string
		credential   Credential
		overrideURL  bool
		wantAuthMode string
	}{
		{"api key", APIKey("key"), true, "api_key"},
		{"oauth", OAuth(tokenSourceFunc(func(context.Context) (string, string, error) { return "token", "account", nil })), false, "oauth"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}}\n\n")
			}))
			defer server.Close()

			var output bytes.Buffer
			options := []Option{WithConfig(agentkit.Config{Log: agentkit.NewLog(&output, nil)})}
			ctx := context.Background()
			if testCase.overrideURL {
				options = append(options, WithBaseURL(server.URL))
			} else {
				cancelled, cancel := context.WithCancel(context.Background())
				cancel()
				ctx = cancelled
			}
			conversation, err := New(testCase.credential, "model", options...)
			if err != nil {
				t.Fatal(err)
			}
			for event := range conversation.Send(ctx, agentkit.Text{Text: "hello"}).Events() {
				_ = event
			}
			assertOpenAITurnAuthMode(t, output.Bytes(), testCase.wantAuthMode)
		})
	}
}

func assertOpenAITurnAuthMode(t *testing.T, data []byte, wantAuthMode string) {
	t.Helper()
	var identity *agentkit.Identity
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
	}
	if identity == nil || identity.AuthMode != wantAuthMode {
		t.Fatalf("turn identity = %+v, want AuthMode %q", identity, wantAuthMode)
	}
}

func TestNewNamesOpenAIEndpointAndUsesCatalogPricing(t *testing.T) {
	// R-OP9X-DYMU
	// R-OQHT-RQDJ
	// R-UEAK-X2Q8
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}}\n\n")
	}))
	defer server.Close()

	var output bytes.Buffer
	conversation, err := New(APIKey("key"), "gpt-5.4-nano",
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
	const wantCost = agentkit.Cost(2*200 + 3*1_250)
	assertOpenAITurnIdentityAndCost(t, output.Bytes(), "openai", wantCost)
}

func assertOpenAITurnIdentityAndCost(t *testing.T, data []byte, wantEndpoint string, want agentkit.Cost) {
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
	if identity == nil || identity.Endpoint != wantEndpoint {
		t.Fatalf("turn identity = %+v, want endpoint %q", identity, wantEndpoint)
	}
	if cost == nil || *cost != want {
		t.Fatalf("turn cost = %v, want catalog amount %d", cost, want)
	}
}
