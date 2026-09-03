// Package xai constructs conversations using xAI credentials.
package xai

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Credential is the sealed set of xAI credentials.
type Credential interface {
	apply(ctx context.Context, req *http.Request, body []byte) error
	isXAICredential()
}

// TokenSource obtains an xAI OAuth bearer token.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type apiKeyCredential struct{ key string }

// APIKey creates a static Bearer API-key credential.
func APIKey(key string) Credential { return apiKeyCredential{key: key} }

func (credential apiKeyCredential) apply(_ context.Context, request *http.Request, _ []byte) error {
	request.Header.Set("Authorization", "Bearer "+credential.key)
	return nil
}

func (apiKeyCredential) isXAICredential() {}

type oauthCredential struct{ source TokenSource }

// OAuth creates a transport-baking OAuth credential.
func OAuth(source TokenSource) Credential { return oauthCredential{source: source} }

func (credential oauthCredential) apply(ctx context.Context, request *http.Request, _ []byte) error {
	if credential.source == nil {
		return fmt.Errorf("%w: nil xAI token source", agentkit.ErrInvalidConfig)
	}
	token, err := credential.source.Token(ctx)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func (oauthCredential) isXAICredential() {}
func (oauthCredential) bakesTransport()  {}

type authAdapter struct{ credential Credential }

func (authAdapter) EndpointIdentity() string { return "xai" }

func (adapter authAdapter) AuthMode() string {
	if _, oauth := adapter.credential.(interface{ bakesTransport() }); oauth {
		return "oauth"
	}
	return "api_key"
}

func (adapter authAdapter) Apply(ctx context.Context, request *http.Request, body []byte) error {
	return adapter.credential.apply(ctx, request, body)
}
