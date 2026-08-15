// Package repos provides the loopback HTTP client for the repositories version
// plane. HTTP details stop here; callers receive script-domain errors.
package repos

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"scripts/internal/script"
	"sort"
	"strings"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	repositoryKind = "scripts"
)

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

func (c *Client) Create(ctx context.Context, nameKey string, owner script.Owner, clientID string) error {
	return c.mcpVerb(ctx, "create", nameKey, "", owner, clientID)
}

func (c *Client) Rename(ctx context.Context, oldNameKey, newNameKey string, owner script.Owner, clientID string) error {
	return c.mcpVerb(ctx, "rename", oldNameKey, newNameKey, owner, clientID)
}

func (c *Client) Delete(ctx context.Context, nameKey string, owner script.Owner, clientID string) error {
	return c.mcpVerb(ctx, "delete", nameKey, "", owner, clientID)
}

func (c *Client) mcpVerb(ctx context.Context, verb, nameKey, to string, owner script.Owner, clientID string) error {
	arguments := map[string]string{"kind": repositoryKind, "name": nameKey}
	if to != "" {
		arguments["to"] = to
	}
	in := map[string]any{
		"jsonrpc": "2.0",
		"id":      verb,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      verb,
			"arguments": arguments,
		},
	}
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("repos: encode %s request: %w", verb, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/mcp", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("repos: build %s request: %w", verb, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner-Id", owner.ID)
	req.Header.Set("X-Owner-Email", owner.Email)
	req.Header.Set("X-Client-Id", clientID)
	responseBody, err := c.send(req)
	if err != nil {
		return err
	}
	var envelope struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("repos: decode %s response: %w", verb, err)
	}
	if !envelope.Result.IsError {
		return nil
	}
	detail := envelope.Result.StructuredContent.Message
	if detail == "" {
		detail = envelope.Result.StructuredContent.Code
	}
	var sentinel error
	switch envelope.Result.StructuredContent.Code {
	case "not_found":
		sentinel = script.ErrNotFound
	case "conflict":
		sentinel = script.ErrConflict
	case "validation":
		sentinel = script.ErrValidation
	default:
		return fmt.Errorf("repos: %s: %s", verb, detail)
	}
	return fmt.Errorf("%w: %s", sentinel, detail)
}

func (c *Client) Commit(ctx context.Context, nameKey string, files map[string]string, message, clientID string) (string, error) {
	type change struct {
		Op         string `json:"op"`
		Path       string `json:"path"`
		ContentB64 string `json:"content_b64"`
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	changes := make([]change, 0, len(paths))
	for _, path := range paths {
		changes = append(changes, change{Op: "put", Path: path, ContentB64: base64.StdEncoding.EncodeToString([]byte(files[path]))})
	}
	var out struct {
		Rev string `json:"rev"`
	}
	err := c.plumbingJSON(ctx, http.MethodPost, "/commit", map[string]any{
		"kind": repositoryKind, "name": nameKey, "message": message, "actor": clientID, "changes": changes,
	}, &out)
	return out.Rev, err
}

func (c *Client) Head(ctx context.Context, nameKey, ref string) (string, error) {
	query := make(url.Values)
	query.Set("kind", repositoryKind)
	query.Set("name", nameKey)
	query.Set("ref", ref)
	var out struct {
		Rev string `json:"rev"`
	}
	if err := c.plumbingJSON(ctx, http.MethodGet, "/list?"+query.Encode(), nil, &out); err != nil {
		return "", err
	}
	if out.Rev == "" {
		return "", fmt.Errorf("%w: repository has no commits", script.ErrNotFound)
	}
	return out.Rev, nil
}

func (c *Client) ReadFile(ctx context.Context, nameKey, ref, path string) ([]byte, error) {
	query := make(url.Values)
	query.Set("kind", repositoryKind)
	query.Set("name", nameKey)
	query.Set("ref", ref)
	query.Set("path", path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/content?"+query.Encode(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("repos: build read-file request: %w", err)
	}
	return c.send(req)
}

func (c *Client) RunToken(ctx context.Context, nameKey string, ttl time.Duration) (string, string, error) {
	var out struct {
		Token    string `json:"token"`
		CloneURL string `json:"clone_url"`
	}
	err := c.plumbingJSON(ctx, http.MethodPost, "/run-token", map[string]string{
		"kind": repositoryKind, "name": nameKey, "ttl": ttl.String(),
	}, &out)
	return out.Token, out.CloneURL, err
}

func (c *Client) plumbingJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("repos: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return fmt.Errorf("repos: build request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	responseBody, err := c.send(req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("repos: decode response: %w", err)
	}
	return nil
}

func (c *Client) send(req *http.Request) ([]byte, error) {
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
