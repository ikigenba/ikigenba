// Package repos provides the loopback HTTP client for the repositories version
// plane. HTTP details stop here; callers receive script-domain errors.
package repos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"scripts/internal/script"
)

const requestTimeout = 30 * time.Second

// Client implements script.VersionPlane over repos' loopback HTTP surface.
type Client struct {
	base string
	hc   *http.Client
}

// New constructs a client. The supplied HTTP client is cloned so applying the
// version-plane timeout cannot change another caller's shared configuration.
func New(base string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	configured := *hc
	configured.Timeout = requestTimeout
	return &Client{base: strings.TrimRight(base, "/"), hc: &configured}
}

// BaseURL reports the configured endpoint. It is primarily useful to verify
// composition without exposing the client's HTTP machinery.
func (c *Client) BaseURL() string { return c.base }

func (c *Client) Create(ctx context.Context, key, clientID string) error {
	return c.doJSON(ctx, http.MethodPost, "/repositories/create", clientID, map[string]any{"key": key}, nil)
}

func (c *Client) Commit(ctx context.Context, key string, files map[string]string, message, clientID string) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/repositories/commit", clientID, map[string]any{
		"key": key, "files": files, "message": message,
	}, &out)
	return out.SHA, err
}

func (c *Client) Head(ctx context.Context, key, branch string) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/repositories/head", "", map[string]any{"key": key, "branch": branch}, &out)
	return out.SHA, err
}

func (c *Client) ReadFile(ctx context.Context, key, ref, path string) ([]byte, error) {
	query := make(url.Values)
	query.Set("key", key)
	query.Set("ref", ref)
	query.Set("path", path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/repositories/file?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("repos: build read-file request: %w", err)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, unavailable(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, unavailable(err)
	}
	if err := statusError(resp.StatusCode, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) Rename(ctx context.Context, oldKey, newKey, clientID string) error {
	return c.doJSON(ctx, http.MethodPost, "/repositories/rename", clientID, map[string]any{"old_key": oldKey, "new_key": newKey}, nil)
}

func (c *Client) Delete(ctx context.Context, key, clientID string) error {
	return c.doJSON(ctx, http.MethodPost, "/repositories/delete", clientID, map[string]any{"key": key}, nil)
}

func (c *Client) RunToken(ctx context.Context, key string) (string, string, error) {
	var out struct {
		Token    string `json:"token"`
		CloneURL string `json:"clone_url"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/repositories/run-token", "", map[string]any{"key": key}, &out)
	return out.Token, out.CloneURL, err
}

func (c *Client) doJSON(ctx context.Context, method, path, clientID string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("repos: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("repos: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if clientID != "" {
		req.Header.Set("X-Client-Id", clientID)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return unavailable(err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return unavailable(err)
	}
	if err := statusError(resp.StatusCode, responseBody); err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(responseBody, out); err != nil {
			return fmt.Errorf("repos: decode response: %w", err)
		}
	}
	return nil
}

func statusError(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = http.StatusText(status)
	}
	var sentinel error
	switch status {
	case http.StatusNotFound:
		sentinel = script.ErrNotFound
	case http.StatusConflict:
		sentinel = script.ErrConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		sentinel = script.ErrValidation
	default:
		return fmt.Errorf("repos: status %d: %s", status, detail)
	}
	return fmt.Errorf("%w: %s", sentinel, detail)
}

func unavailable(err error) error {
	return fmt.Errorf("%w: %v", script.ErrSourceUnavailable, err)
}

var _ script.VersionPlane = (*Client)(nil)
