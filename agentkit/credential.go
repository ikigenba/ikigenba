package agentkit

import (
	"context"
	"fmt"
	"net/http"
)

// AuthMode names how a credential is presented.
type AuthMode string

const (
	// AuthModeAPIKey presents a credential as an API key.
	AuthModeAPIKey AuthMode = "api_key"
	// AuthModeOAuth presents a credential as an OAuth bearer token.
	AuthModeOAuth AuthMode = "oauth"
)

// Token is one OAuth grant. AccountID is optional.
type Token struct {
	Bearer    string
	AccountID string
}

// Authenticator turns a rotator into the authenticator for this offering: it
// holds the rotator, remembers the endpoint spec that serves its mode (so an
// OAuth rotation knows where to post), and hands each resolved token to
// o.WireFormat's placement. It returns ErrInvalidConfig for a nil r or a mode
// o.Endpoints does not list.
func (o Offering) Authenticator(r Rotator) (Authenticator, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: nil rotator", ErrInvalidConfig)
	}
	matched, found := o.endpointForAuthMode(r.AuthMode())
	if !found {
		return nil, fmt.Errorf("%w: %s does not accept auth mode %q", ErrInvalidConfig, o.ID, r.AuthMode())
	}
	switch r.AuthMode() {
	case AuthModeAPIKey:
		return apiKeyApplier{provider: o.ID, wire: o.WireFormat, rotator: r, baseURL: matched.BaseURL}, nil
	case AuthModeOAuth:
		return oauthApplier{provider: o.ID, wire: o.WireFormat, rotator: r, rotation: matched.Rotation, baseURL: matched.BaseURL}, nil
	default:
		return nil, fmt.Errorf("%w: unrecognized auth mode %q", ErrInvalidConfig, r.AuthMode())
	}
}

func (o Offering) endpointForAuthMode(mode AuthMode) (EndpointSpec, bool) {
	for _, endpoint := range o.Endpoints {
		if endpoint.AuthMode == mode {
			return endpoint, true
		}
	}
	return EndpointSpec{}, false
}

type apiKeyApplier struct {
	provider OfferingID
	wire     WireFormat
	rotator  Rotator
	baseURL  string
}

func (a apiKeyApplier) EndpointIdentity() string { return string(a.provider) }
func (a apiKeyApplier) AuthMode() string         { return string(AuthModeAPIKey) }
func (a apiKeyApplier) defaultBaseURL() string   { return a.baseURL }

func (a apiKeyApplier) Authenticate(ctx context.Context, req *http.Request, _ []byte) error {
	token, err := a.rotator.Token(ctx)
	if err != nil {
		return err
	}
	switch a.wire.(type) {
	case *anthropicWire:
		req.Header.Set("x-api-key", token.Bearer)
	case *geminiWire:
		query := req.URL.Query()
		query.Set("key", token.Bearer)
		req.URL.RawQuery = query.Encode()
	default:
		req.Header.Set("Authorization", "Bearer "+token.Bearer)
	}
	return nil
}

type oauthApplier struct {
	provider OfferingID
	wire     WireFormat
	rotator  Rotator
	rotation Rotation
	baseURL  string
}

// oauthRefreshHook lets Conversation ask an applier to re-mint credentials
// after a vendor 401, without Conversation knowing about OAuth or
// Rotator (D22). Only oauthApplier implements it; apiKeyApplier does
// not, so a 401 under an API-key credential never attempts a refresh.
type oauthRefreshHook interface {
	refreshOn401(ctx context.Context) error
}

func (a oauthApplier) EndpointIdentity() string { return string(a.provider) }
func (a oauthApplier) AuthMode() string         { return string(AuthModeOAuth) }
func (a oauthApplier) defaultBaseURL() string   { return a.baseURL }

func (a oauthApplier) Authenticate(ctx context.Context, req *http.Request, _ []byte) error {
	token, err := a.rotator.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.Bearer)
	switch a.wire.(type) {
	case *openAIResponsesWire, *openAIChatWire:
		if token.AccountID == "" {
			return fmt.Errorf("%w: empty OAuth AccountID for %s", ErrInvalidConfig, a.provider)
		}
		req.Header.Set("ChatGPT-Account-Id", token.AccountID)
	}
	return nil
}

func (a oauthApplier) refreshOn401(ctx context.Context) error {
	_, err := a.rotator.Rotate(ctx, a.rotation)
	return err
}
