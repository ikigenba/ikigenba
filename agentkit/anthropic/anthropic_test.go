package anthropic

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
