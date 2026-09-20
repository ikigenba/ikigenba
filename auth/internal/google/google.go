package google

import (
	"context"
	"errors"
	"fmt"

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
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	workspace    string
}

// NewClient discovers the provider at issuer and constructs a Google client.
func NewClient(clientID, clientSecret, workspaceDomain, issuer string) (*Client, error) {
	provider, err := oidc.NewProvider(context.Background(), issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}

	return &Client{
		oauth2Config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{
			ClientID:             clientID,
			SkipIssuerCheck:      true,
			SupportedSigningAlgs: []string{oidc.RS256},
		}),
		workspace: workspaceDomain,
	}, nil
}

// AuthCodeURL builds an authorization URL using request-specific callback data.
func (c *Client) AuthCodeURL(state, verifier, redirectURI string) string {
	config := c.oauth2Config
	config.RedirectURL = redirectURI

	return config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("hd", c.workspace),
		oauth2.S256ChallengeOption(verifier),
	)
}

// Exchange exchanges a code and verifies the returned Google ID token.
func (c *Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (Claims, error) {
	config := c.oauth2Config
	config.RedirectURL = redirectURI

	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Claims{}, fmt.Errorf("exchange authorization code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Claims{}, errors.New("exchange authorization code: response has no ID token")
	}

	idToken, err := c.verifier.Verify(ctx, rawIDToken)
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
