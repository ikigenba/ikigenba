package oauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MaxErrorBody bounds how much of a non-2xx token response body appears in the
// returned error.
const MaxErrorBody = 4096

var reservedTokenParams = [...]string{
	"grant_type",
	"code",
	"code_verifier",
	"redirect_uri",
	"client_id",
	"client_secret",
}

// Exchange POSTs the authorization code to the token endpoint and returns the
// response body verbatim.
func (c Client) Exchange(
	ctx context.Context,
	hc *http.Client,
	s Session,
	code string,
	extra, headers []Param,
) ([]byte, error) {
	req, err := c.tokenRequest(ctx, s, code, extra, headers)
	if err != nil {
		return nil, err
	}

	response, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send token request: %w", err)
	}

	return readTokenResponse(response)
}

func (c Client) tokenRequest(
	ctx context.Context,
	s Session,
	code string,
	extra, headers []Param,
) (*http.Request, error) {
	var body strings.Builder
	appendFormParam := func(key, value string) {
		if body.Len() != 0 {
			body.WriteByte('&')
		}
		body.WriteString(url.QueryEscape(key))
		body.WriteByte('=')
		body.WriteString(url.QueryEscape(value))
	}

	appendFormParam("grant_type", "authorization_code")
	appendFormParam("code", code)
	appendFormParam("code_verifier", s.CodeVerifier)
	appendFormParam("redirect_uri", c.RedirectURI)
	appendFormParam("client_id", c.ClientID)
	if c.ClientSecret != "" {
		appendFormParam("client_secret", c.ClientSecret)
	}
	for _, param := range extra {
		appendFormParam(param.Key, param.Value)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL.String(), strings.NewReader(body.String()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, header := range headers {
		req.Header.Add(header.Key, header.Value)
	}

	return req, nil
}

func readTokenResponse(response *http.Response) ([]byte, error) {
	success := response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	reader := io.Reader(response.Body)
	if !success {
		reader = io.LimitReader(response.Body, MaxErrorBody)
	}
	responseBody, readErr := io.ReadAll(reader)
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read token response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close token response: %w", closeErr)
	}
	if !success {
		return nil, fmt.Errorf("token endpoint returned status %q with body %s", response.Status, strconv.Quote(string(responseBody)))
	}

	return responseBody, nil
}

// ReservedTokenParam reports whether key is written by Exchange.
func ReservedTokenParam(key string) bool {
	for _, reserved := range reservedTokenParams {
		if key == reserved {
			return true
		}
	}

	return false
}
