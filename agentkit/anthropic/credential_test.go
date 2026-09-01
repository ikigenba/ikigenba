package anthropic

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

type tokenSourceFunc func(context.Context) (string, error)

type credentialContextKey struct{}

func (source tokenSourceFunc) Token(ctx context.Context) (string, error) { return source(ctx) }

func TestCredentialsApplyAnthropicHeadersAndPropagateTokenErrors(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := APIKey("key-value").apply(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("x-api-key"); got != "key-value" {
		t.Fatalf("x-api-key = %q", got)
	}

	want := errors.New("token unavailable")
	credential := OAuth(tokenSourceFunc(func(context.Context) (string, error) { return "", want }))
	if err := credential.apply(context.Background(), request, nil); !errors.Is(err, want) {
		t.Fatalf("OAuth error = %v, want %v", err, want)
	}
}

func TestOAuthUsesAnthropicBearerOnlyTokenSource(t *testing.T) {
	// R-3OEN-WYDV
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	credential := OAuth(tokenSourceFunc(func(context.Context) (string, error) { return "oauth-token", nil }))
	if err := credential.apply(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer oauth-token" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestCredentialsUseSharedAuthApplierRuntimeSeam(t *testing.T) {
	// R-3KQY-RN5S
	ctx := context.WithValue(context.Background(), credentialContextKey{}, "credential context")
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}

	var apiKeyAuth agentkit.AuthApplier = authAdapter{credential: APIKey("key-value")}
	if err := apiKeyAuth.Apply(ctx, request, []byte(`{"messages":[]}`)); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("x-api-key"); got != "key-value" {
		t.Fatalf("x-api-key through AuthApplier = %q", got)
	}

	tokenSourceCalled := false
	oauth := OAuth(tokenSourceFunc(func(got context.Context) (string, error) {
		tokenSourceCalled = true
		if got != ctx {
			t.Errorf("token source context differs from AuthApplier context")
		}
		return "oauth-token", nil
	}))
	var oauthAuth agentkit.AuthApplier = authAdapter{credential: oauth}
	if err := oauthAuth.Apply(ctx, request, []byte(`{"messages":[]}`)); err != nil {
		t.Fatal(err)
	}
	if !tokenSourceCalled {
		t.Fatal("OAuth credential was not reached through AuthApplier")
	}
	if got := request.Header.Get("Authorization"); got != "Bearer oauth-token" {
		t.Fatalf("Authorization through AuthApplier = %q", got)
	}
}
