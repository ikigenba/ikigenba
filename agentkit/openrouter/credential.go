// Package openrouter constructs conversations using OpenRouter credentials.
package openrouter

import (
	"context"
	"net/http"
)

// Credential is the sealed set of OpenRouter credentials.
type Credential interface {
	apply(ctx context.Context, req *http.Request, body []byte) error
	isOpenRouterCredential()
}

type apiKeyCredential struct{ key string }

// APIKey creates a static Bearer API-key credential.
func APIKey(key string) Credential { return apiKeyCredential{key: key} }

func (credential apiKeyCredential) apply(_ context.Context, request *http.Request, _ []byte) error {
	request.Header.Set("Authorization", "Bearer "+credential.key)
	return nil
}

func (apiKeyCredential) isOpenRouterCredential() {}

type authAdapter struct{ credential Credential }

func (authAdapter) EndpointIdentity() string { return "openrouter" }

func (adapter authAdapter) Apply(ctx context.Context, request *http.Request, body []byte) error {
	return adapter.credential.apply(ctx, request, body)
}
