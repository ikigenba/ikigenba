package google

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	googleIssuer       = "https://accounts.google.com"
	googleIssuerLegacy = "accounts.google.com"
)

// Claims are the identity claims auth consumes from a verified Google ID token.
type Claims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	HostedDomain  string
}

// Client provides the Google OAuth 2.0 and OIDC operations used by auth.
type Client struct {
	clientID     string
	clientSecret string
	workspace    string
	issuer       string

	mu       sync.Mutex
	provider *oidc.Provider
}

// NewClient records Google configuration without contacting the issuer.
func NewClient(clientID, clientSecret, workspaceDomain, issuer string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		workspace:    workspaceDomain,
		issuer:       issuer,
	}
}

// AuthCodeURL builds an authorization URL using request-specific callback data.
// Discovery of the issuer happens here, not at construction, and a failed
// discovery is not remembered.
func (c *Client) AuthCodeURL(state, verifier, redirectURI string) (string, error) {
	config, err := c.oauthConfig(context.Background(), redirectURI)
	if err != nil {
		return "", err
	}

	return config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("hd", c.workspace),
		oauth2.S256ChallengeOption(verifier),
	), nil
}

// Exchange exchanges a code and verifies the returned Google ID token.
// Discovery of the issuer happens here, not at construction, and a failed
// discovery is not remembered.
func (c *Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (Claims, error) {
	config, err := c.oauthConfig(ctx, redirectURI)
	if err != nil {
		return Claims{}, err
	}

	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Claims{}, fmt.Errorf("exchange authorization code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Claims{}, errors.New("exchange authorization code: response has no ID token")
	}

	provider, err := c.discovered(ctx)
	if err != nil {
		return Claims{}, err
	}
	idToken, err := provider.Verifier(&oidc.Config{
		ClientID:             c.clientID,
		SkipIssuerCheck:      true,
		SupportedSigningAlgs: []string{oidc.RS256},
	}).Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, fmt.Errorf("verify ID token: %w", err)
	}

	var claims struct {
		Issuer        string `json:"iss"`
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HostedDomain  string `json:"hd"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Claims{}, fmt.Errorf("decode ID token claims: %w", err)
	}
	if claims.Issuer != googleIssuer && claims.Issuer != googleIssuerLegacy {
		return Claims{}, fmt.Errorf("verify ID token: unexpected issuer %q", claims.Issuer)
	}

	return Claims{
		Issuer:        claims.Issuer,
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		HostedDomain:  claims.HostedDomain,
	}, nil
}

// oauthConfig discovers the issuer's endpoints on demand and returns an
// oauth2 config whose redirect URL is the one for this request.
func (c *Client) oauthConfig(ctx context.Context, redirectURI string) (oauth2.Config, error) {
	provider, err := c.discovered(ctx)
	if err != nil {
		return oauth2.Config{}, err
	}
	return oauth2.Config{
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}, nil
}

// discovered returns the issuer's OpenID provider, fetching it when none is
// cached. A failed fetch is not stored, so the next sign-in retries it.
func (c *Client) discovered(ctx context.Context) (*oidc.Provider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.provider != nil {
		return c.provider, nil
	}
	provider, err := oidc.NewProvider(ctx, c.issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	c.provider = provider
	return provider, nil
}
