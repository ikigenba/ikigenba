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

	r.token = Token{
		Bearer:    accessToken,
		AccountID: openAIAccountID(accessToken),
	}
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

	r.mu.Lock()
	raw := append([]byte(nil), r.raw...)
	r.mu.Unlock()
	if len(raw) == 0 {
		var err error
		raw, err = r.store.Read(ctx)
		if err != nil {
			return Token{}, fmt.Errorf("read OAuth refresh token: %w", ErrInvalidConfig)
		}
	}

	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		return Token{}, fmt.Errorf("decode OAuth refresh token: %w", ErrInvalidConfig)
	}
	refreshToken, ok := stored["refresh_token"].(string)
	if !ok || refreshToken == "" {
		return Token{}, fmt.Errorf("OAuth token has no refresh_token: %w", ErrInvalidConfig)
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {rotation.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rotation.RefreshURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("build OAuth refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, &Error{Category: CategoryTransport}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Token{}, &Error{Category: CategoryTransport, Status: resp.StatusCode}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var oauthError struct {
			Code    string `json:"error"`
			Message string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &oauthError)
		return Token{}, &Error{
			Category: CategoryAuth,
			Status:   resp.StatusCode,
			Code:     oauthError.Code,
			Message:  oauthError.Message,
		}
	}

	accessToken, response, ok, err := oauthAccessToken(body)
	if err != nil || !ok {
		return Token{}, fmt.Errorf("OAuth token endpoint has no access_token: %w", ErrInvalidConfig)
	}

	updated := body
	if _, ok := response["refresh_token"]; !ok {
		response["refresh_token"], _ = json.Marshal(refreshToken)
		updated, _ = json.Marshal(response)
	}
	if err := r.store.Write(ctx, updated); err != nil {
		return Token{}, err
	}

	token := Token{
		Bearer:    accessToken,
		AccountID: openAIAccountID(accessToken),
	}
	r.mu.Lock()
	r.token = token
	r.raw = append(r.raw[:0], updated...)
	r.cached = true
	r.mu.Unlock()

	return token, nil
}

func openAIAccountID(accessToken string) string {
	segments := strings.Split(accessToken, ".")
	if len(segments) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return ""
	}
	var claims struct {
		OpenAIAuth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.OpenAIAuth.AccountID
}
