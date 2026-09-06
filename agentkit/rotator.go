package agentkit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Rotator presents the current secret and can rotate it.
type Rotator interface {
	AuthMode() AuthMode
	Token(ctx context.Context) (Token, error)
	Rotate(ctx context.Context, rotation Rotation) (Token, error)
}

type apiKeyRotator struct {
	key string
}

// APIKeyRotator returns a rotator for a non-rotating API key.
func APIKeyRotator(key string) Rotator {
	return apiKeyRotator{key: key}
}

func (r apiKeyRotator) AuthMode() AuthMode {
	return AuthModeAPIKey
}

func (r apiKeyRotator) Token(context.Context) (Token, error) {
	return Token{Bearer: r.key}, nil
}

func (apiKeyRotator) Rotate(context.Context, Rotation) (Token, error) {
	return Token{}, fmt.Errorf("rotate API key: %w", ErrInvalidConfig)
}

type oauthRotator struct {
	mu         sync.Mutex
	store      TokenStore
	token      Token
	raw        []byte
	cached     bool
	rotateCall *rotateCall
}

type rotateCall struct {
	done  chan struct{}
	token Token
	err   error
}

// OAuthRotator returns a rotator backed by a token store.
func OAuthRotator(store TokenStore) Rotator {
	return &oauthRotator{store: store}
}

func (*oauthRotator) AuthMode() AuthMode {
	return AuthModeOAuth
}

func (r *oauthRotator) Token(ctx context.Context) (Token, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cached {
		return r.token, nil
	}

	raw, err := r.store.Read(ctx)
	if err != nil {
		return Token{}, err
	}

	accessToken, _, ok, err := oauthAccessToken(raw)
	if err != nil {
		return Token{}, fmt.Errorf("decode OAuth token: %w", ErrInvalidConfig)
	}
	if !ok {
		return Token{}, fmt.Errorf("OAuth token has no access_token: %w", ErrInvalidConfig)
	}

	r.token = tokenFromAccessToken(accessToken)
	r.raw = append(r.raw[:0], raw...)
	r.cached = true
	return r.token, nil
}

func oauthAccessToken(raw []byte) (string, map[string]json.RawMessage, bool, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", nil, false, err
	}
	var accessToken string
	if err := json.Unmarshal(response["access_token"], &accessToken); err != nil || accessToken == "" {
		return "", response, false, nil
	}

	return accessToken, response, true, nil
}

func (r *oauthRotator) Rotate(ctx context.Context, rotation Rotation) (Token, error) {
	r.mu.Lock()
	if call := r.rotateCall; call != nil {
		r.mu.Unlock()
		<-call.done
		return call.token, call.err
	}
	call := &rotateCall{done: make(chan struct{})}
	r.rotateCall = call
	r.mu.Unlock()

	token, err := r.rotateOnce(ctx, rotation)

	r.mu.Lock()
	call.token = token
	call.err = err
	r.rotateCall = nil
	close(call.done)
	r.mu.Unlock()

	return token, err
}

func (r *oauthRotator) rotateOnce(ctx context.Context, rotation Rotation) (Token, error) {
	if rotation.RefreshURL == "" {
		return Token{}, fmt.Errorf("OAuth rotation has no refresh URL: %w", ErrInvalidConfig)
	}

	refreshToken, err := r.refreshToken(ctx)
	if err != nil {
		return Token{}, err
	}
	req, err := oauthRefreshRequest(ctx, rotation, refreshToken)
	if err != nil {
		return Token{}, err
	}
	body, err := executeOAuthRefresh(req)
	if err != nil {
		return Token{}, err
	}
	accessToken, updated, err := mergeOAuthRefresh(body, refreshToken)
	if err != nil {
		return Token{}, err
	}
	if err := r.store.Write(ctx, updated); err != nil {
		return Token{}, err
	}

	token := tokenFromAccessToken(accessToken)
	r.mu.Lock()
	r.token = token
	r.raw = append(r.raw[:0], updated...)
	r.cached = true
	r.mu.Unlock()

	return token, nil
}

func (r *oauthRotator) refreshToken(ctx context.Context) (string, error) {
	r.mu.Lock()
	raw := append([]byte(nil), r.raw...)
	r.mu.Unlock()
	if len(raw) == 0 {
		var err error
		raw, err = r.store.Read(ctx)
		if err != nil {
			return "", fmt.Errorf("read OAuth refresh token: %w", ErrInvalidConfig)
		}
	}

	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		return "", fmt.Errorf("decode OAuth refresh token: %w", ErrInvalidConfig)
	}
	refreshToken, ok := stored["refresh_token"].(string)
	if !ok || refreshToken == "" {
		return "", fmt.Errorf("OAuth token has no refresh_token: %w", ErrInvalidConfig)
	}
	return refreshToken, nil
}

func oauthRefreshRequest(ctx context.Context, rotation Rotation, refreshToken string) (*http.Request, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {rotation.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rotation.RefreshURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build OAuth refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func executeOAuthRefresh(req *http.Request) ([]byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &Error{Category: CategoryTransport}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{Category: CategoryTransport, Status: resp.StatusCode}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var oauthError struct {
			Code    string `json:"error"`
			Message string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &oauthError)
		return nil, &Error{
			Category: CategoryAuth,
			Status:   resp.StatusCode,
			Code:     oauthError.Code,
			Message:  oauthError.Message,
		}
	}
	return body, nil
}

func mergeOAuthRefresh(body []byte, refreshToken string) (string, []byte, error) {
	accessToken, response, ok, err := oauthAccessToken(body)
	if err != nil || !ok {
		return "", nil, fmt.Errorf("OAuth token endpoint has no access_token: %w", ErrInvalidConfig)
	}

	updated := body
	if _, ok := response["refresh_token"]; !ok {
		response["refresh_token"], _ = json.Marshal(refreshToken)
		updated, _ = json.Marshal(response)
	}
	return accessToken, updated, nil
}

func tokenFromAccessToken(accessToken string) Token {
	claims := jwtPayloadClaims(accessToken)
	return Token{
		Bearer:    accessToken,
		AccountID: openAIAccountID(claims),
		ExpiresAt: accessTokenExpiry(claims),
	}
}

func jwtPayloadClaims(accessToken string) map[string]json.RawMessage {
	segments := strings.Split(accessToken, ".")
	if len(segments) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return nil
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func openAIAccountID(claims map[string]json.RawMessage) string {
	var openAIAuth struct {
		AccountID string `json:"chatgpt_account_id"`
	}
	if err := json.Unmarshal(claims["https://api.openai.com/auth"], &openAIAuth); err != nil {
		return ""
	}
	return openAIAuth.AccountID
}

func accessTokenExpiry(claims map[string]json.RawMessage) time.Time {
	var claim any
	if err := json.Unmarshal(claims["exp"], &claim); err != nil {
		return time.Time{}
	}
	expiry, numeric := claim.(float64)
	if !numeric {
		return time.Time{}
	}
	return time.Unix(int64(expiry), 0)
}
