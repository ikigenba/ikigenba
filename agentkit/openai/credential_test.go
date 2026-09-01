package openai

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

type tokenSourceFunc func(context.Context) (string, string, error)

type credentialContextKey struct{}

func (source tokenSourceFunc) Token(ctx context.Context) (string, string, error) { return source(ctx) }

func TestCredentialsApplyOpenAIHeadersAndPropagateTokenErrors(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := APIKey("api-key").apply(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer api-key" {
		t.Fatalf("Authorization = %q", got)
	}

	want := errors.New("subscription unavailable")
	credential := Subscription(tokenSourceFunc(func(context.Context) (string, string, error) { return "", "", want }))
	if err := credential.apply(context.Background(), request, nil); !errors.Is(err, want) {
		t.Fatalf("subscription error = %v, want %v", err, want)
	}
}

func TestSubscriptionUsesBearerAndAccountTokenSource(t *testing.T) {
	// R-3OEN-WYDV
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	credential := Subscription(tokenSourceFunc(func(context.Context) (string, string, error) {
		return "subscription-token", "account-42", nil
	}))
	if err := credential.apply(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer subscription-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := request.Header.Get("ChatGPT-Account-Id"); got != "account-42" {
		t.Fatalf("ChatGPT-Account-Id = %q", got)
	}
}

func TestCredentialsUseSharedAuthApplierRuntimeSeam(t *testing.T) {
	// R-3KQY-RN5S
	ctx := context.WithValue(context.Background(), credentialContextKey{}, "credential context")
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}

	var apiKeyAuth agentkit.AuthApplier = authAdapter{credential: APIKey("api-key")}
	if err := apiKeyAuth.Apply(ctx, request, []byte(`{"input":[]}`)); err != nil {
		t.Fatal(err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer api-key" {
		t.Fatalf("Authorization through AuthApplier = %q", got)
	}

	tokenSourceCalled := false
	subscription := Subscription(tokenSourceFunc(func(got context.Context) (string, string, error) {
		tokenSourceCalled = true
		if got != ctx {
			t.Errorf("token source context differs from AuthApplier context")
		}
		return "subscription-token", "account-42", nil
	}))
	var subscriptionAuth agentkit.AuthApplier = authAdapter{credential: subscription}
	if err := subscriptionAuth.Apply(ctx, request, []byte(`{"input":[]}`)); err != nil {
		t.Fatal(err)
	}
	if !tokenSourceCalled {
		t.Fatal("subscription credential was not reached through AuthApplier")
	}
	if got := request.Header.Get("Authorization"); got != "Bearer subscription-token" {
		t.Fatalf("Authorization through AuthApplier = %q", got)
	}
	if got := request.Header.Get("ChatGPT-Account-Id"); got != "account-42" {
		t.Fatalf("ChatGPT-Account-Id through AuthApplier = %q", got)
	}
}
