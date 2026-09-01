package gemini

import (
	"context"
	"net/http"
	"testing"
)

func TestAPIKeyAppliesQueryCredential(t *testing.T) {
	// R-Z1VE-05P2
	request, _ := http.NewRequest(http.MethodPost, "https://example.test/path?alt=sse", nil)
	credential := APIKey("gemini-key")
	if err := credential.apply(context.Background(), request, nil); err != nil || request.URL.Query().Get("key") != "gemini-key" {
		t.Fatalf("apply = %v, query %q", err, request.URL.RawQuery)
	}
}
