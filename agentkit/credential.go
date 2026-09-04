package agentkit

import (
	"context"
	"fmt"
	"net/http"
	"slices"
)

// AuthMode names how a credential is presented.
type AuthMode string

const (
	// AuthModeAPIKey presents a credential as an API key.
	AuthModeAPIKey AuthMode = "api_key"
	// AuthModeOAuth presents a credential as an OAuth bearer token.
	AuthModeOAuth AuthMode = "oauth"
)

// Credential is the sealed set of consumer credentials.
type Credential interface {
	mode() AuthMode
	isCredential()
}

type apiKeyCredential struct {
	key string
}

func (apiKeyCredential) mode() AuthMode { return AuthModeAPIKey }
func (apiKeyCredential) isCredential()  {}

// APIKey constructs an API-key credential.
func APIKey(key string) Credential {
	return apiKeyCredential{key: key}
}

type oauthCredential struct {
	source TokenSource
}

func (oauthCredential) mode() AuthMode { return AuthModeOAuth }
func (oauthCredential) isCredential()  {}

// OAuth constructs an OAuth credential backed by source.
func OAuth(source TokenSource) Credential {
	return oauthCredential{source: source}
}

// Token is one OAuth grant. AccountID is optional.
type Token struct {
	Bearer    string
	AccountID string
}

// TokenSource yields the current token and can mint a new one. Token is
// called before every request; Refresh is called by the conversation when a
// request comes back 401 (D22). The root ships one concrete source,
// Offering.TokenSource (D22); the interface stays public so tests can fake it.
type TokenSource interface {
	Token(ctx context.Context) (Token, error)
	Refresh(ctx context.Context) (Token, error)
}

// Authenticator turns a credential into the authenticator for this offering:
// it holds the credential and hands each resolved value to o.WireFormat's
// placement. It returns ErrInvalidConfig for a nil cred or a mode o.AuthModes
// does not list.
func (o Offering) Authenticator(cred Credential) (Authenticator, error) {
	if cred == nil {
		return nil, fmt.Errorf("%w: nil credential", ErrInvalidConfig)
	}
	if !slices.Contains(o.AuthModes, cred.mode()) {
		return nil, fmt.Errorf("%w: %s does not accept credential mode %q", ErrInvalidConfig, o.ID, cred.mode())
	}
	switch credential := cred.(type) {
	case apiKeyCredential:
		return apiKeyApplier{provider: o.ID, wire: o.WireFormat, key: credential.key}, nil
	case oauthCredential:
		return oauthApplier{provider: o.ID, wire: o.WireFormat, source: credential.source}, nil
	default:
		return nil, fmt.Errorf("%w: unrecognized credential type", ErrInvalidConfig)
	}
}

type apiKeyApplier struct {
	provider OfferingID
	wire     WireFormat
	key      string
}

func (a apiKeyApplier) EndpointIdentity() string { return string(a.provider) }
func (a apiKeyApplier) AuthMode() string         { return string(AuthModeAPIKey) }

func (a apiKeyApplier) Authenticate(_ context.Context, req *http.Request, _ []byte) error {
	switch a.wire.(type) {
	case *anthropicWire:
		req.Header.Set("x-api-key", a.key)
	case *geminiWire:
		query := req.URL.Query()
		query.Set("key", a.key)
		req.URL.RawQuery = query.Encode()
	default:
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	return nil
}

type oauthApplier struct {
	provider OfferingID
	wire     WireFormat
	source   TokenSource
}

// oauthRefreshHook lets Conversation ask an applier to re-mint credentials
// after a vendor 401, without Conversation knowing about OAuth or
// TokenSource (D22). Only oauthApplier implements it; apiKeyApplier does
// not, so a 401 under an API-key credential never attempts a refresh.
type oauthRefreshHook interface {
	refreshOn401(ctx context.Context) error
}

func (a oauthApplier) EndpointIdentity() string { return string(a.provider) }
func (a oauthApplier) AuthMode() string         { return string(AuthModeOAuth) }

func (a oauthApplier) Authenticate(ctx context.Context, req *http.Request, _ []byte) error {
	source, err := a.checkedSource()
	if err != nil {
		return err
	}
	token, err := source.Token(ctx)
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
	source, err := a.checkedSource()
	if err != nil {
		return err
	}
	_, err = source.Refresh(ctx)
	return err
}

func (a oauthApplier) checkedSource() (TokenSource, error) {
	if a.source == nil {
		return nil, fmt.Errorf("%w: nil OAuth token source", ErrInvalidConfig)
	}
	return a.source, nil
}
