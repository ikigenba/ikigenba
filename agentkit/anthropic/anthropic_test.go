package anthropic

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

func TestNewNamesAnthropicEndpointAndUsesCatalogPricing(t *testing.T) {
	// R-EKRG-9Y2W
	// R-OP9X-DYMU
	// R-OQHT-RQDJ
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"usage\":{\"output_tokens\":3}}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	var output bytes.Buffer
	conversation, err := New(APIKey("key"), "claude-haiku-4-5",
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

	var identity *agentkit.Identity
	var cost *agentkit.Cost
	decoder := json.NewDecoder(&output)
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
	if identity == nil || identity.Endpoint != string(agentkit.ProviderAnthropic) {
		t.Fatalf("turn identity = %+v, want endpoint %q", identity, agentkit.ProviderAnthropic)
	}
	const wantCost = agentkit.Cost(2*1_000 + 3*5_000)
	if cost == nil || *cost != wantCost {
		t.Fatalf("turn cost = %v, want 2 input at 1000 plus 3 output at 5000 = %d", cost, wantCost)
	}
}
