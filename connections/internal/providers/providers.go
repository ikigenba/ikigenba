// Package providers implements the OAuth calls for the provider catalog.
package providers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider describes the provider OAuth contract.
type Provider struct {
	Name, DisplayName, AuthorizeURL, TokenURL, RevokeURL string
	Scopes                                               []string
	Granted                                              map[string]string
	Extra                                                url.Values
	ClientIDVar, ClientSecretVar                         string
	Account                                              func(idToken string) (Account, error)
}

// Account describes the provider OAuth contract.
type Account struct{ Email, Subject string }

// Grant describes the provider OAuth contract.
type Grant struct {
	AccessToken, RefreshToken string
	Expires                   time.Time
	Scopes                    []string
	Account                   Account
}

// Client describes the provider OAuth contract.
type Client struct {
	Provider   Provider
	ID, Secret string
	HTTP       *http.Client
	Now        func() time.Time
}

// Errors distinguish provider refusal, failure, and account extraction.
var (
	ErrRefused = errors.New("provider refused request")
	ErrFailed  = errors.New("provider request failed")
	ErrAccount = errors.New("provider account unavailable")
)

type statusError struct {
	kind   error
	status int
}

// Error implements the provider OAuth operation.
func (e *statusError) Error() string { return e.kind.Error() }

// Unwrap implements the provider OAuth operation.
func (e *statusError) Unwrap() error { return e.kind }

// HTTPStatus implements the provider OAuth operation.
func HTTPStatus(err error) int {
	var e *statusError
	if errors.As(err, &e) {
		return e.status
	}
	return 0
}

const googleSecretVariable = "GOOGLE_CLIENT_SECRET"

// Table implements the provider OAuth operation.
func Table() []Provider {
	var p Provider
	p.Name = "google"
	p.DisplayName = "Google"
	p.AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	p.TokenURL = "https://oauth2.googleapis.com/token"
	p.RevokeURL = "https://oauth2.googleapis.com/revoke"
	p.Scopes = []string{"openid", "email", "https://www.googleapis.com/auth/drive", "https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.compose"}
	p.Granted = map[string]string{"email": "https://www.googleapis.com/auth/userinfo.email"}
	p.Extra = url.Values{"access_type": {"offline"}, "prompt": {"consent"}}
	p.ClientIDVar = "GOOGLE_CLIENT_ID"
	p.Account = IDTokenAccount
	p.ClientSecretVar = googleSecretVariable
	return []Provider{p}
}

// GrantedName implements the provider OAuth operation.
func (p Provider) GrantedName(scope string) string {
	if s, ok := p.Granted[scope]; ok {
		return s
	}
	return scope
}

// Challenge implements the provider OAuth operation.
func Challenge(verifier string) string {
	b := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// AuthorizeURL implements the provider OAuth operation.
func (c Client) AuthorizeURL(state, verifier, redirectURI string) string {
	q := url.Values{}
	for k, v := range c.Provider.Extra {
		q[k] = append([]string{}, v...)
	}
	q.Set("response_type", "code")
	q.Set("client_id", c.ID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(c.Provider.Scopes, " "))
	q.Set("state", state)
	q.Set("code_challenge", Challenge(verifier))
	q.Set("code_challenge_method", "S256")
	return c.Provider.AuthorizeURL + "?" + q.Encode()
}
func (c Client) request(ctx context.Context, endpoint string, form url.Values) ([]byte, int, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if e != nil {
		return nil, 0, &statusError{ErrFailed, 0}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	copyClient := *hc
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, e := copyClient.Do(req)
	if e != nil {
		return nil, 0, &statusError{ErrFailed, 0}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		kind := ErrFailed
		if res.StatusCode == 400 {
			kind = ErrRefused
		}
		return nil, res.StatusCode, &statusError{kind, res.StatusCode}
	}
	if endpoint == c.Provider.RevokeURL && len(form) == 1 {
		return nil, res.StatusCode, nil
	}
	b, e := io.ReadAll(res.Body)
	if e != nil {
		return nil, res.StatusCode, &statusError{ErrFailed, res.StatusCode}
	}
	return b, res.StatusCode, nil
}
func (p Provider) order(scope string) []string {
	pieces := []string{}
	seen := map[string]bool{}
	for _, s := range strings.Split(scope, " ") {
		if s != "" && !seen[s] {
			pieces = append(pieces, s)
			seen[s] = true
		}
	}
	out := []string{}
	for _, s := range p.Scopes {
		g := p.GrantedName(s)
		if seen[g] {
			out = append(out, g)
			delete(seen, g)
		}
	}
	for _, s := range pieces {
		if seen[s] {
			out = append(out, s)
			delete(seen, s)
		}
	}
	return out
}

type tokenResponse struct {
	access, refresh, scope, id string
	seconds                    int64
	hasID                      bool
}

func decodeToken(body []byte, exchange bool) (tokenResponse, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil || m == nil {
		return tokenResponse{}, false
	}
	var t tokenResponse
	for _, v := range []struct {
		key  string
		dest *string
	}{{"access_token", &t.access}, {"refresh_token", &t.refresh}, {"scope", &t.scope}, {"id_token", &t.id}} {
		if b, ok := m[v.key]; ok {
			if len(b) == 0 || b[0] != '"' || json.Unmarshal(b, v.dest) != nil {
				return tokenResponse{}, false
			}
		}
	}
	t.hasID = m["id_token"] != nil
	b, ok := m["expires_in"]
	if !ok || len(b) == 0 || b[0] == '"' {
		return tokenResponse{}, false
	}
	var number json.Number
	if json.Unmarshal(b, &number) != nil {
		return tokenResponse{}, false
	}
	r, ok := new(big.Rat).SetString(number.String())
	if !ok || !r.IsInt() || r.Sign() <= 0 || r.Num().Cmp(big.NewInt(31536000)) > 0 {
		return tokenResponse{}, false
	}
	t.seconds = r.Num().Int64()
	return t, t.access != "" && (!exchange || t.refresh != "")
}
func (c Client) tokens(ctx context.Context, form url.Values, exchange bool) (Grant, error) {
	body, status, e := c.request(ctx, c.Provider.TokenURL, form)
	if e != nil {
		return Grant{}, e
	}
	t, ok := decodeToken(body, exchange)
	if !ok {
		return Grant{}, &statusError{ErrFailed, status}
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}
	g := Grant{AccessToken: t.access, RefreshToken: t.refresh, Expires: addSeconds(now(), t.seconds)}
	if strings.Trim(t.scope, " ") != "" {
		g.Scopes = c.Provider.order(t.scope)
	} else if exchange {
		g.Scopes = []string{}
		for _, s := range c.Provider.Scopes {
			g.Scopes = append(g.Scopes, c.Provider.GrantedName(s))
		}
	}
	if exchange {
		account := c.Provider.Account
		if account == nil {
			account = IDTokenAccount
		}
		if !t.hasID {
			return Grant{}, ErrAccount
		}
		g.Account, e = account(t.id)
		if e != nil {
			return Grant{}, ErrAccount
		}
	}
	return g, nil
}

// Exchange implements the provider OAuth operation.
func (c Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (Grant, error) {
	return c.tokens(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirectURI}, "client_id": {c.ID}, "client_secret": {c.Secret}}, true)
}

// Refresh implements the provider OAuth operation.
func (c Client) Refresh(ctx context.Context, refreshToken string) (Grant, error) {
	return c.tokens(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {c.ID}, "client_secret": {c.Secret}}, false)
}

// Revoke implements the provider OAuth operation.
func (c Client) Revoke(ctx context.Context, token string) error {
	_, _, e := c.request(ctx, c.Provider.RevokeURL, url.Values{"token": {token}})
	return e
}

// IDTokenAccount implements the provider OAuth operation.
func IDTokenAccount(idToken string) (Account, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Account{}, ErrAccount
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return Account{}, ErrAccount
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil || m == nil {
		return Account{}, ErrAccount
	}
	var a Account
	for _, v := range []struct {
		key  string
		dest *string
	}{{"email", &a.Email}, {"sub", &a.Subject}} {
		b := m[v.key]
		if len(b) == 0 || b[0] != '"' || json.Unmarshal(b, v.dest) != nil || *v.dest == "" {
			return Account{}, ErrAccount
		}
	}
	return a, nil
}

func addSeconds(now time.Time, seconds int64) time.Time {
	now = now.UTC().Truncate(time.Second)
	return time.Unix(now.Unix()+seconds, 0).UTC()
}
