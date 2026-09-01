package xai

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type tokenSourceFunc func(context.Context) (string, error)

func (source tokenSourceFunc) Token(ctx context.Context) (string, error) { return source(ctx) }

func TestCredentialsApplyBearerHeadersAndPropagateOAuthErrors(t *testing.T) {
	// R-YTC3-BRI7
	// R-YZFL-8M7O
	request, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err := APIKey("api-key").apply(context.Background(), request, nil); err != nil || request.Header.Get("Authorization") != "Bearer api-key" {
		t.Fatalf("APIKey apply = %v, header %q", err, request.Header.Get("Authorization"))
	}
	want := errors.New("token unavailable")
	credential := OAuth(tokenSourceFunc(func(context.Context) (string, error) { return "", want }))
	if err := credential.apply(context.Background(), request, nil); !errors.Is(err, want) {
		t.Fatalf("OAuth error = %v, want %v", err, want)
	}
}
