// Package githubidp is the narrow seam between the dashboard and GitHub's
// OAuth and REST APIs. It reports identity facts; callers enforce admission
// policy such as requiring active organization membership.
package githubidp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	issuer      = "https://github.com"
	httpTimeout = 10 * time.Second
)

// Credentials configures the live provider.
type Credentials struct {
	ClientID     string
	ClientSecret string
	Org          string
}

// Identity contains the GitHub facts learned during an OAuth callback.
type Identity struct {
	Iss           string
	Sub           string
	Login         string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	OrgMembership string
}

// Provider is the seam used by the dashboard's GitHub login flow.
type Provider interface {
	AuthorizeURL(state, redirectURI string) string
	ExchangeCode(ctx context.Context, code, redirectURI string) (Identity, error)
}

type github struct {
	credentials Credentials
	webBase     string
	apiBase     string
	httpClient  *http.Client
}

// New returns a Provider backed by GitHub's live endpoints.
func New(credentials Credentials) Provider {
	return &github{
		credentials: credentials,
		webBase:     "https://github.com",
		apiBase:     "https://api.github.com",
		httpClient:  &http.Client{Timeout: httpTimeout},
	}
}

// Stub is a configurable, network-free Provider for server tests.
type Stub struct {
	Identity Identity
	Err      error
}

// NewStub returns a stub with a valid canned identity. Callers may replace its
// Identity or Err fields to drive callback outcomes without network access.
func NewStub() *Stub {
	return &Stub{Identity: Identity{
		Iss:           issuer,
		Sub:           "583231",
		Login:         "octocat",
		Email:         "octocat@github.invalid",
		EmailVerified: true,
		Name:          "The Octocat",
		OrgMembership: "active",
	}}
}

// AuthorizeURL returns a deterministic stand-in URL for tests.
func (s *Stub) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{}
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	return "https://github.stub.invalid/authorize?" + q.Encode()
}

// ExchangeCode returns the configured identity and error.
func (s *Stub) ExchangeCode(context.Context, string, string) (Identity, error) {
	return s.Identity, s.Err
}

// AuthorizeURL builds the GitHub authorization URL. GitHub Apps do not request
// OAuth scopes here; their user-to-server access comes from App permissions.
func (g *github) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", g.credentials.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	return strings.TrimRight(g.webBase, "/") + "/login/oauth/authorize?" + q.Encode()
}

// ExchangeCode trades a one-time code for a short-lived user token, reads the
// user's profile, primary email, and organization membership, then discards the
// token. Every request gets one retry when its response is a 5xx.
func (g *github) ExchangeCode(ctx context.Context, code, redirectURI string) (Identity, error) {
	token, err := g.exchangeToken(ctx, code, redirectURI)
	if err != nil {
		return Identity{}, err
	}

	profile, err := g.fetchProfile(ctx, token)
	if err != nil {
		return Identity{}, err
	}
	email, verified, err := g.fetchPrimaryEmail(ctx, token)
	if err != nil {
		return Identity{}, err
	}
	membership, err := g.fetchMembership(ctx, token)
	if err != nil {
		return Identity{}, err
	}

	return Identity{
		Iss:           issuer,
		Sub:           strconv.FormatInt(profile.ID, 10),
		Login:         profile.Login,
		Email:         email,
		EmailVerified: verified,
		Name:          profile.Name,
		Picture:       profile.AvatarURL,
		OrgMembership: membership,
	}, nil
}

func (g *github) exchangeToken(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", g.credentials.ClientID)
	form.Set("client_secret", g.credentials.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	resp, err := g.doWithRetry(func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.webBase, "/")+"/login/oauth/access_token", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	})
	if err != nil {
		return "", fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", responseError("token endpoint", resp)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if payload.Error != "" {
		return "", fmt.Errorf("token endpoint: %s: %s", payload.Error, payload.Description)
	}
	if payload.AccessToken == "" {
		return "", errors.New("token response missing access_token")
	}
	return payload.AccessToken, nil
}

type profile struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (g *github) fetchProfile(ctx context.Context, token string) (profile, error) {
	resp, err := g.apiGet(ctx, token, "/user")
	if err != nil {
		return profile{}, fmt.Errorf("user endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return profile{}, responseError("user endpoint", resp)
	}
	var value profile
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return profile{}, fmt.Errorf("decode user response: %w", err)
	}
	return value, nil
}

func (g *github) fetchPrimaryEmail(ctx context.Context, token string) (string, bool, error) {
	resp, err := g.apiGet(ctx, token, "/user/emails")
	if err != nil {
		return "", false, fmt.Errorf("email endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, responseError("email endpoint", resp)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", false, fmt.Errorf("decode email response: %w", err)
	}
	for _, email := range emails {
		if email.Primary {
			return email.Email, email.Verified, nil
		}
	}
	return "", false, errors.New("email response has no primary address")
}

func (g *github) fetchMembership(ctx context.Context, token string) (string, error) {
	path := "/user/memberships/orgs/" + url.PathEscape(g.credentials.Org)
	resp, err := g.apiGet(ctx, token, path)
	if err != nil {
		return "", fmt.Errorf("membership endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", responseError("membership endpoint", resp)
	}
	var membership struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&membership); err != nil {
		return "", fmt.Errorf("decode membership response: %w", err)
	}
	return membership.State, nil
}

func (g *github) apiGet(ctx context.Context, token, path string) (*http.Response, error) {
	return g.doWithRetry(func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(g.apiBase, "/")+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	})
}

func (g *github) doWithRetry(buildRequest func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := buildRequest()
		if err != nil {
			return nil, err
		}
		resp, err := g.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 != 5 {
			return resp, nil
		}
		if attempt == 1 {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	panic("unreachable")
}

func responseError(endpoint string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return fmt.Errorf("%s: %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
}
