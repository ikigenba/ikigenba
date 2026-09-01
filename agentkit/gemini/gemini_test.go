package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

func TestNewSelectsGeminiWire(t *testing.T) {
	// R-YPOE-6GA4
	seen := make(chan map[string]json.RawMessage, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var object map[string]json.RawMessage
		_ = json.Unmarshal(body, &object)
		seen <- object
		writer.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()
	conversation, err := New(APIKey("key"), "verbatim-model", WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	for event := range conversation.Send(context.Background(), agentkit.Text{Text: "hello"}).Events() {
		_ = event
	}
	if _, exists := (<-seen)["contents"]; !exists {
		t.Fatal("Gemini constructor did not select the Gemini codec")
	}
}
