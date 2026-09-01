package openrouter

import (
	"context"
	"net/http"
	"testing"
)

func TestAPIKeyAppliesBearerHeader(t *testing.T) {
	// R-Z0NH-MDYD
	request, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
	credential := APIKey("router-key")
	if err := credential.apply(context.Background(), request, nil); err != nil || request.Header.Get("Authorization") != "Bearer router-key" {
		t.Fatalf("apply = %v, header %q", err, request.Header.Get("Authorization"))
	}
}
