// Package gemini constructs conversations using Gemini credentials.
package gemini

import (
	"context"
	"net/http"
)

// Credential is the sealed set of Gemini credentials.
type Credential interface {
	apply(ctx context.Context, req *http.Request, body []byte) error
	isGeminiCredential()
}

type apiKeyCredential struct{ key string }

// APIKey creates a static Gemini API-key credential.
func APIKey(key string) Credential { return apiKeyCredential{key: key} }

func (credential apiKeyCredential) apply(_ context.Context, request *http.Request, _ []byte) error {
	query := request.URL.Query()
	query.Set("key", credential.key)
	request.URL.RawQuery = query.Encode()
	return nil
}

func (apiKeyCredential) isGeminiCredential() {}

type authAdapter struct{ credential Credential }

func (authAdapter) EndpointIdentity() string { return "gemini" }

func (adapter authAdapter) AuthMode() string {
	if _, oauth := adapter.credential.(interface{ bakesTransport() }); oauth {
		return "oauth"
	}
	return "api_key"
}

func (adapter authAdapter) Apply(ctx context.Context, request *http.Request, body []byte) error {
	return adapter.credential.apply(ctx, request, body)
}
