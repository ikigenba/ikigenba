package agentkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type offeringTokenSource struct {
	mu       sync.Mutex
	offering Offering
	store    TokenStore
	stored   []byte
	token    Token

	refreshCall *refreshCall
}

type refreshCall struct {
	done  chan struct{}
	token Token
	err   error
}

// TokenSource builds the concrete OAuth source for this offering over store.
// It reads the store once, here, and fails fast on an unusable store.
func (o Offering) TokenSource(store TokenStore) (TokenSource, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: nil token store", ErrInvalidConfig)
	}
	if !slices.Contains(o.AuthModes, AuthModeOAuth) {
		return nil, fmt.Errorf("%w: %s does not accept OAuth", ErrInvalidConfig, o.ID)
	}

	data, err := store.Read(context.Background())
	if err != nil {
		return nil, err
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(data, &stored); err != nil || stored == nil {
		return nil, fmt.Errorf("%w: token store has no access_token", ErrInvalidConfig)
	}
	var accessToken string
	if err := json.Unmarshal(stored["access_token"], &accessToken); err != nil || accessToken == "" {
		return nil, fmt.Errorf("%w: token store has no access_token", ErrInvalidConfig)
	}

	return &offeringTokenSource{
		offering: o,
		store:    store,
		stored:   bytes.Clone(data),
		token:    o.token(accessToken),
	}, nil
}

func (s *offeringTokenSource) Token(_ context.Context) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.token, nil
}

func (s *offeringTokenSource) Refresh(ctx context.Context) (Token, error) {
	s.mu.Lock()
	if call := s.refreshCall; call != nil {
		s.mu.Unlock()
		<-call.done
		return call.token, call.err
	}
	call := &refreshCall{done: make(chan struct{})}
	s.refreshCall = call
	stored := bytes.Clone(s.stored)
	s.mu.Unlock()

	token, updated, err := s.refresh(ctx, stored)

	s.mu.Lock()
	call.token = token
	call.err = err
	if err == nil {
		s.token = token
		s.stored = updated
	}
	s.refreshCall = nil
	close(call.done)
	s.mu.Unlock()

	return token, err
}

func (s *offeringTokenSource) refresh(ctx context.Context, stored []byte) (Token, []byte, error) {
	refreshToken, err := storedRefreshToken(stored)
	if err != nil {
		return Token{}, nil, err
	}

	form := url.Values{
		"client_id":     {s.offering.OAuth.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.offering.OAuth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, nil, &Error{Category: CategoryTransport}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, nil, &Error{Category: CategoryTransport}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Token{}, nil, &Error{Category: CategoryTransport, Status: resp.StatusCode}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var oauthError struct {
			Code    string `json:"error"`
			Message string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &oauthError)
		return Token{}, nil, &Error{
			Category: CategoryAuth,
			Status:   resp.StatusCode,
			Code:     oauthError.Code,
			Message:  oauthError.Message,
		}
	}

	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil || response == nil {
		return Token{}, nil, fmt.Errorf("%w: token endpoint has no access_token", ErrInvalidConfig)
	}
	var accessToken string
	if err := json.Unmarshal(response["access_token"], &accessToken); err != nil || accessToken == "" {
		return Token{}, nil, fmt.Errorf("%w: token endpoint has no access_token", ErrInvalidConfig)
	}

	updated := body
	if _, ok := response["refresh_token"]; !ok {
		response["refresh_token"] = json.RawMessage(strconv.Quote(refreshToken))
		updated, _ = json.Marshal(response)
	}
	if err := s.store.Write(ctx, updated); err != nil {
		return Token{}, nil, err
	}
	return s.offering.token(accessToken), bytes.Clone(updated), nil
}

func storedRefreshToken(stored []byte) (string, error) {
	var value struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(stored, &value); err != nil || value.RefreshToken == "" {
		return "", fmt.Errorf("%w: token store has no refresh_token", ErrInvalidConfig)
	}
	return value.RefreshToken, nil
}

func (o Offering) token(accessToken string) Token {
	token := Token{Bearer: accessToken}
	if o.ID == OfferingOpenAIResponses || o.ID == OfferingOpenAIChat {
		token.AccountID = openAIAccountID(accessToken)
	}
	return token
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
