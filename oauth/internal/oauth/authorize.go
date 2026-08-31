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

type authorizeParamSpec struct {
	key       string
	value     func(Client, Session) string
	omitEmpty bool
}

var authorizeParamSpecs = [...]authorizeParamSpec{
	{key: "response_type", value: func(Client, Session) string { return "code" }},
	{key: "client_id", value: func(client Client, _ Session) string { return client.ClientID }},
	{key: "redirect_uri", value: func(client Client, _ Session) string { return client.RedirectURI }},
	{key: "state", value: func(_ Client, session Session) string { return session.State }},
	{key: "code_challenge", value: func(_ Client, session Session) string { return Challenge(session.CodeVerifier) }},
	{key: "code_challenge_method", value: func(Client, Session) string { return "S256" }},
	{key: "scope", value: func(client Client, _ Session) string { return client.Scope }, omitEmpty: true},
}

// Challenge returns the unpadded base64url-encoded SHA-256 digest of verifier.
func Challenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// AuthorizeURL constructs the browser authorization URL.
func (c Client) AuthorizeURL(session Session, extra []Param) string {
	authorizeURL := *c.AuthURL
	query := authorizeURL.Query()
	for _, spec := range authorizeParamSpecs {
		value := spec.value(c, session)
		if spec.omitEmpty && value == "" {
			continue
		}
		query.Add(spec.key, value)
	}
	authorizeURL.RawQuery = query.Encode()
	for _, param := range extra {
		if authorizeURL.RawQuery != "" {
			authorizeURL.RawQuery += "&"
		}
		authorizeURL.RawQuery += url.QueryEscape(param.Key) + "=" + url.QueryEscape(param.Value)
	}

	return authorizeURL.String()
}

// ReservedAuthorizeParam reports whether key is written by AuthorizeURL.
func ReservedAuthorizeParam(key string) bool {
	for _, spec := range authorizeParamSpecs {
		if key == spec.key {
			return true
		}
	}

	return false
}
