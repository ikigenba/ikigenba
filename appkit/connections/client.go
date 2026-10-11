// Package connections retrieves a caller's provider tokens over the broker's socket.
package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// ClientConfig supplies the broker and the caller's telemetry writer.
type ClientConfig struct {
	Service   services.Entry
	Telemetry *telemetry.Writer
}

// Client speaks the connections token route and is safe for concurrent use.
type Client struct {
	name string
	http *http.Client
}

// Token is the broker's current access token and account metadata.
type Token struct {
	AccessToken string
	Expires     time.Time
	Account     string
	Scopes      []string
}

// Broker refusals have distinct identities for errors.Is.
var (
	ErrUnknownProvider = errors.New("connections: unknown provider")
	ErrNotConnected    = errors.New("connections: not connected")
	ErrProvider        = errors.New("connections: provider failure")
	ErrUnavailable     = errors.New("connections: unavailable")
)

// ReconnectError names the page the person must open to restore a grant.
type ReconnectError struct{ URL string }

func (e *ReconnectError) Error() string { return "connections: reconnect at " + e.URL }

// HTTPError retains an unexpected response's status and bounded body prefix.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("connections: unexpected HTTP response (status %d)", e.StatusCode)
}

// NewClient constructs a client without contacting the broker.
func NewClient(cfg ClientConfig) *Client {
	client := telemetry.SiblingClient(cfg.Telemetry, cfg.Service.Name, telemetry.SocketTransport(cfg.Service.Socket))
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{name: cfg.Service.Name, http: client}
}

// Token fetches one token, forwarding the caller carried by ctx.
func (c *Client) Token(ctx context.Context, provider string) (Token, error) {
	u := &url.URL{Scheme: "http", Host: c.name, Path: "/token/" + provider}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Token{}, fmt.Errorf("connections: request: %w", err)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Token{}, fmt.Errorf("connections: request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusOK {
		fields, err := readObject(response.Body)
		if err != nil {
			return Token{}, fmt.Errorf("connections: invalid token response: %w", err)
		}
		token, err := decodeToken(fields)
		if err != nil {
			return Token{}, err
		}
		return token, nil
	}
	prefix := &bodyPrefix{}
	switch response.StatusCode {
	case http.StatusNotFound, http.StatusConflict, http.StatusBadGateway, http.StatusServiceUnavailable:
		fields, err := readObject(io.TeeReader(response.Body, prefix))
		if err == nil {
			if refusal := decodeRefusal(response.StatusCode, fields); refusal != nil {
				return Token{}, refusal
			}
		}
		// A body read can fail; retain any remaining bytes of the bounded prefix.
		_, _ = io.Copy(prefix, io.LimitReader(response.Body, int64(4096-len(prefix.body))))
	default:
		_, _ = io.Copy(prefix, io.LimitReader(response.Body, 4096))
	}
	return Token{}, &HTTPError{StatusCode: response.StatusCode, Body: string(prefix.body)}
}

type bodyPrefix struct{ body []byte }

func (p *bodyPrefix) Write(data []byte) (int, error) {
	n := len(data)
	remaining := 4096 - len(p.body)
	if len(data) > remaining {
		data = data[:remaining]
	}
	p.body = append(p.body, data...)
	return n, nil
}

func readObject(reader io.Reader) (map[string]json.RawMessage, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, errors.New("invalid JSON encoding")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("expected an object")
	}
	return fields, nil
}

func stringMember(fields map[string]json.RawMessage, key string) (string, bool) {
	var value any
	if json.Unmarshal(fields[key], &value) != nil {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func decodeToken(fields map[string]json.RawMessage) (Token, error) {
	access, accessOK := stringMember(fields, "access_token")
	expires, expiresOK := stringMember(fields, "expires")
	account, accountOK := stringMember(fields, "account")
	expiry, err := time.Parse(time.RFC3339, expires)
	var items []json.RawMessage
	if !accessOK || !expiresOK || !accountOK || err != nil || json.Unmarshal(fields["scopes"], &items) != nil || items == nil {
		return Token{}, errors.New("connections: invalid token response")
	}
	scopes := make([]string, len(items))
	for i, item := range items {
		var value any
		if json.Unmarshal(item, &value) != nil {
			return Token{}, errors.New("connections: invalid token scopes")
		}
		text, ok := value.(string)
		if !ok {
			return Token{}, errors.New("connections: invalid token scopes")
		}
		scopes[i] = text
	}
	return Token{access, expiry, account, scopes}, nil
}

func decodeRefusal(status int, fields map[string]json.RawMessage) error {
	word, _ := stringMember(fields, "error")
	switch {
	case status == http.StatusNotFound && word == "unknown_provider":
		return ErrUnknownProvider
	case status == http.StatusNotFound && word == "not_connected":
		return ErrNotConnected
	case status == http.StatusBadGateway && word == "provider":
		return ErrProvider
	case status == http.StatusServiceUnavailable && word == "unavailable":
		return ErrUnavailable
	case status == http.StatusConflict && word == "reconnect":
		if location, ok := stringMember(fields, "url"); ok {
			return &ReconnectError{location}
		}
	}
	return nil
}
