// Package version contains prompts' complete client boundary to the version
// plane.
package version

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	repositoryKind = "prompts"
	requestTimeout = 30 * time.Second
)

var (
	ErrNotFound    = errors.New("version: not found")
	ErrConflict    = errors.New("version: conflict")
	ErrValidation  = errors.New("version: invalid")
	ErrUnavailable = errors.New("version: unavailable")
)

// File is one path in a batch write. Delete removes the path instead of
// writing Data.
type File struct {
	Path   string `json:"path"`
	Data   []byte `json:"data,omitempty"`
	Delete bool   `json:"delete,omitempty"`
}

// Definition is the three addressable files of a prompt tree read at one ref.
type Definition struct {
	UserPrompt   string
	SystemPrompt string
	ConfigJSON   []byte
}

// Credential is a short-lived credential for one prompt repository.
type Credential struct {
	CloneURL  string
	Username  string
	Password  string
	ExpiresAt time.Time
}

// Client is the version plane as prompts sees it.
type Client interface {
	Create(ctx context.Context, nameKey, actor string) error
	Commit(ctx context.Context, nameKey string, files []File, message, actor string) (sha string, err error)
	Head(ctx context.Context, nameKey string) (sha string, err error)
	Read(ctx context.Context, nameKey, ref string) (Definition, error)
	Rename(ctx context.Context, fromKey, toKey string) error
	Archive(ctx context.Context, nameKey string) error
	RunToken(ctx context.Context, nameKey, runID string, ttl time.Duration) (Credential, error)
}

// HTTPClient implements Client over repos' loopback HTTP surface. promptID
// resolves a repository name key to the prompt id used for attribution.
type HTTPClient struct {
	base     string
	promptID func(string) string
	http     *http.Client
}

// New constructs a version-plane client from its injected base URL and prompt
// id resolver.
func New(baseURL string, promptID func(string) string) *HTTPClient {
	configured := *http.DefaultClient
	configured.Timeout = requestTimeout
	return &HTTPClient{
		base:     strings.TrimRight(baseURL, "/"),
		promptID: promptID,
		http:     &configured,
	}
}

// BaseURL reports the injected endpoint without exposing HTTP details.
func (c *HTTPClient) BaseURL() string { return c.base }

func (c *HTTPClient) Create(ctx context.Context, nameKey, actor string) error {
	return c.doJSON(ctx, http.MethodPost, "/repositories/create", nameKey, actor, map[string]any{
		"key": repositoryKey(nameKey),
	}, nil)
}

func (c *HTTPClient) Commit(ctx context.Context, nameKey string, files []File, message, actor string) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/repositories/commit", nameKey, actor, map[string]any{
		"key":     repositoryKey(nameKey),
		"files":   files,
		"message": message,
	}, &response)
	return response.SHA, err
}

func (c *HTTPClient) Head(ctx context.Context, nameKey string) (string, error) {
	var response struct {
		SHA string `json:"sha"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/repositories/head", nameKey, "", map[string]any{
		"key":    repositoryKey(nameKey),
		"branch": "main",
	}, &response)
	return response.SHA, err
}

func (c *HTTPClient) Read(ctx context.Context, nameKey, ref string) (Definition, error) {
	userPrompt, err := c.readFile(ctx, nameKey, ref, "prompt.md")
	if err != nil {
		return Definition{}, err
	}
	systemPrompt, err := c.readFile(ctx, nameKey, ref, "system.md")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Definition{}, err
	}
	configJSON, err := c.readFile(ctx, nameKey, ref, "config.json")
	if err != nil {
		return Definition{}, err
	}
	return Definition{
		UserPrompt:   string(userPrompt),
		SystemPrompt: string(systemPrompt),
		ConfigJSON:   configJSON,
	}, nil
}

func (c *HTTPClient) Rename(ctx context.Context, fromKey, toKey string) error {
	return c.doJSON(ctx, http.MethodPost, "/repositories/rename", fromKey, "", map[string]any{
		"old_key": repositoryKey(fromKey),
		"new_key": repositoryKey(toKey),
	}, nil)
}

func (c *HTTPClient) Archive(ctx context.Context, nameKey string) error {
	return c.doJSON(ctx, http.MethodPost, "/repositories/delete", nameKey, "", map[string]any{
		"key": repositoryKey(nameKey),
	}, nil)
}

func (c *HTTPClient) RunToken(ctx context.Context, nameKey, runID string, ttl time.Duration) (Credential, error) {
	var response struct {
		CloneURL  string `json:"clone_url"`
		Username  string `json:"username"`
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/repositories/run-token", nameKey, "", map[string]any{
		"key":    repositoryKey(nameKey),
		"run_id": runID,
		"ttl":    ttl.String(),
	}, &response)
	if err != nil {
		return Credential{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, response.ExpiresAt)
	if err != nil {
		return Credential{}, unavailable(fmt.Errorf("decode expires_at: %w", err))
	}
	username := response.Username
	if username == "" {
		username = "run"
	}
	return Credential{
		CloneURL: response.CloneURL, Username: username, Password: response.Token, ExpiresAt: expiresAt,
	}, nil
}

func (c *HTTPClient) readFile(ctx context.Context, nameKey, ref, path string) ([]byte, error) {
	query := make(url.Values)
	query.Set("key", repositoryKey(nameKey))
	query.Set("ref", ref)
	query.Set("path", path)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/repositories/file?"+query.Encode(), nil)
	if err != nil {
		return nil, unavailable(err)
	}
	c.setClientID(request, nameKey, "")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, unavailable(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, unavailable(err)
	}
	if err := statusError(response.StatusCode, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *HTTPClient) doJSON(ctx context.Context, method, path, nameKey, actor string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return unavailable(fmt.Errorf("encode request: %w", err))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return unavailable(fmt.Errorf("build request: %w", err))
	}
	request.Header.Set("Content-Type", "application/json")
	c.setClientID(request, nameKey, actor)
	response, err := c.http.Do(request)
	if err != nil {
		return unavailable(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return unavailable(err)
	}
	if err := statusError(response.StatusCode, responseBody); err != nil {
		return err
	}
	if output != nil {
		if err := json.Unmarshal(responseBody, output); err != nil {
			return unavailable(fmt.Errorf("decode response: %w", err))
		}
	}
	return nil
}

func (c *HTTPClient) setClientID(request *http.Request, nameKey, actor string) {
	clientID := actor
	if clientID == "" {
		promptID := nameKey
		if c.promptID != nil {
			promptID = c.promptID(nameKey)
		}
		clientID = repositoryKind + ":" + promptID
	}
	request.Header.Set("X-Client-Id", clientID)
}

func repositoryKey(nameKey string) string {
	return repositoryKind + "/" + url.PathEscape(nameKey)
}

func statusError(status int, body []byte) error {
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return nil
	}
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = http.StatusText(status)
	}
	var sentinel error
	switch status {
	case http.StatusNotFound:
		sentinel = ErrNotFound
	case http.StatusConflict:
		sentinel = ErrConflict
	case http.StatusBadRequest:
		sentinel = ErrValidation
	default:
		sentinel = ErrUnavailable
	}
	return fmt.Errorf("%w: %s", sentinel, detail)
}

func unavailable(err error) error {
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

var _ Client = (*HTTPClient)(nil)
