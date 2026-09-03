// Package openai constructs conversations using OpenAI credentials.
package openai

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Credential is the sealed set of OpenAI credentials.
type Credential interface {
	apply(ctx context.Context, req *http.Request, body []byte) error
	isOpenAICredential()
}

// TokenSource obtains OpenAI subscription authentication and account data.
type TokenSource interface {
	Token(ctx context.Context) (bearer, accountID string, err error)
}

type apiKeyCredential struct{ key string }

// APIKey creates a static Bearer API-key credential.
func APIKey(key string) Credential { return apiKeyCredential{key: key} }

func (credential apiKeyCredential) apply(_ context.Context, request *http.Request, _ []byte) error {
	request.Header.Set("Authorization", "Bearer "+credential.key)
	return nil
}

func (apiKeyCredential) isOpenAICredential() {}

type oauthCredential struct{ source TokenSource }

// OAuth creates a transport-baking OAuth credential.
func OAuth(source TokenSource) Credential { return oauthCredential{source: source} }

func (credential oauthCredential) apply(ctx context.Context, request *http.Request, _ []byte) error {
	if credential.source == nil {
		return fmt.Errorf("%w: nil OpenAI token source", agentkit.ErrInvalidConfig)
	}
	token, accountID, err := credential.source.Token(ctx)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("ChatGPT-Account-Id", accountID)
	return nil
}

func (oauthCredential) isOpenAICredential() {}
func (oauthCredential) bakesTransport()     {}

type authAdapter struct{ credential Credential }

func (authAdapter) EndpointIdentity() string { return "openai" }

func (adapter authAdapter) Apply(ctx context.Context, request *http.Request, body []byte) error {
	return adapter.credential.apply(ctx, request, body)
}
