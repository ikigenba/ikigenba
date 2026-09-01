package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

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
