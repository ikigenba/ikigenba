package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
)

// Client holds the provider-independent OAuth client configuration.
type Client struct {
	AuthURL, TokenURL      *url.URL
	ClientID, ClientSecret string
	RedirectURI, Scope     string
}

// Param is a key/value parameter or header supplied by the caller.
type Param struct{ Key, Value string }

// Challenge returns the unpadded base64url-encoded SHA-256 digest of verifier.
func Challenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// AuthorizeURL constructs the browser authorization URL.
func (c Client) AuthorizeURL(session Session, _ []Param) string {
	authorizeURL := *c.AuthURL
	query := make(url.Values, 7)
	query.Set("response_type", "code")
	query.Set("client_id", c.ClientID)
	query.Set("redirect_uri", c.RedirectURI)
	query.Set("state", session.State)
	query.Set("code_challenge", Challenge(session.CodeVerifier))
	query.Set("code_challenge_method", "S256")
	if c.Scope != "" {
		query.Set("scope", c.Scope)
	}
	authorizeURL.RawQuery = query.Encode()

	return authorizeURL.String()
}
