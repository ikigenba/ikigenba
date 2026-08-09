// Package version contains prompts' complete client boundary to the version
// plane.
package version

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
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

// Owner is the identity a version-plane domain verb is scoped to.
type Owner struct {
	ID    string
	Email string
}

// File is one path in a batch write. Delete removes the path instead of
// writing Data.
type File struct {
	Path   string
	Data   []byte
	Delete bool
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
	Create(ctx context.Context, nameKey string, owner Owner, actor string) error
	Commit(ctx context.Context, nameKey string, files []File, message, actor string) (sha string, err error)
	Head(ctx context.Context, nameKey string) (sha string, err error)
	Read(ctx context.Context, nameKey, ref string) (Definition, error)
	Rename(ctx context.Context, fromKey, toKey string, owner Owner, actor string) error
	Archive(ctx context.Context, nameKey string, owner Owner, actor string) error
	RunToken(ctx context.Context, nameKey, runID string, ttl time.Duration) (Credential, error)
}

// HTTPClient implements Client over repos' loopback HTTP surface. promptID
// resolves a repository name key to the prompt id used for attribution when a
// caller does not supply an actor.
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

func (c *HTTPClient) Create(ctx context.Context, nameKey string, owner Owner, actor string) error {
	return c.doMCP(ctx, "create", nameKey, owner, actor, map[string]any{
		"kind": repositoryKind,
		"name": nameKey,
	})
}

func (c *HTTPClient) Commit(ctx context.Context, nameKey string, files []File, message, actor string) (string, error) {
	type change struct {
		Op         string  `json:"op"`
		Path       string  `json:"path"`
		ContentB64 *string `json:"content_b64,omitempty"`
	}
	changes := make([]change, 0, len(files))
	for _, file := range files {
		encoded := base64.StdEncoding.EncodeToString(file.Data)
		entry := change{Op: "put", Path: file.Path, ContentB64: &encoded}
		if file.Delete {
			entry.Op = "delete"
			entry.ContentB64 = nil
		}
		changes = append(changes, entry)
	}
	var response struct {
		Rev string `json:"rev"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/commit", nil, map[string]any{
		"kind":    repositoryKind,
		"name":    nameKey,
		"message": message,
		"actor":   actor,
		"changes": changes,
	}, &response)
	return response.Rev, err
}

func (c *HTTPClient) Head(ctx context.Context, nameKey string) (string, error) {
	query := make(url.Values)
	query.Set("kind", repositoryKind)
	query.Set("name", nameKey)
	query.Set("ref", "main")
	var response struct {
		Rev string `json:"rev"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/list?"+query.Encode(), nil, nil, &response); err != nil {
		return "", err
	}
	if response.Rev == "" {
		return "", ErrNotFound
	}
	return response.Rev, nil
}

func (c *HTTPClient) Read(ctx context.Context, nameKey, ref string) (Definition, error) {
	query := make(url.Values)
	query.Set("kind", repositoryKind)
	query.Set("name", nameKey)
	query.Set("ref", ref)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/archive?"+query.Encode(), nil)
	if err != nil {
		return Definition{}, unavailable(fmt.Errorf("build request: %w", err))
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Definition{}, unavailable(err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return Definition{}, unavailable(readErr)
		}
		return Definition{}, statusError(response.StatusCode, body)
	}

	files := make(map[string][]byte, 3)
	archive := tar.NewReader(response.Body)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Definition{}, unavailable(fmt.Errorf("decode archive: %w", err))
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name != "prompt.md" && name != "system.md" && name != "config.json" {
			continue
		}
		data, err := io.ReadAll(archive)
		if err != nil {
			return Definition{}, unavailable(fmt.Errorf("read %s from archive: %w", name, err))
		}
		files[name] = data
	}
	for _, required := range []string{"prompt.md", "config.json"} {
		if _, ok := files[required]; !ok {
			return Definition{}, fmt.Errorf("version: archive missing %s", required)
		}
	}
	return Definition{
		UserPrompt:   string(files["prompt.md"]),
		SystemPrompt: string(files["system.md"]),
		ConfigJSON:   files["config.json"],
	}, nil
}

func (c *HTTPClient) Rename(ctx context.Context, fromKey, toKey string, owner Owner, actor string) error {
	return c.doMCP(ctx, "rename", fromKey, owner, actor, map[string]any{
		"kind": repositoryKind,
		"name": fromKey,
		"to":   toKey,
	})
}

func (c *HTTPClient) Archive(ctx context.Context, nameKey string, owner Owner, actor string) error {
	return c.doMCP(ctx, "delete", nameKey, owner, actor, map[string]any{
		"kind": repositoryKind,
		"name": nameKey,
	})
}

func (c *HTTPClient) RunToken(ctx context.Context, nameKey, _ string, ttl time.Duration) (Credential, error) {
	var response struct {
		CloneURL  string `json:"clone_url"`
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/run-token", nil, map[string]any{
		"kind": repositoryKind,
		"name": nameKey,
		"ttl":  ttl.String(),
	}, &response)
	if err != nil {
		return Credential{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, response.ExpiresAt)
	if err != nil {
		return Credential{}, unavailable(fmt.Errorf("decode expires_at: %w", err))
	}
	return Credential{
		CloneURL: response.CloneURL, Username: "run", Password: response.Token, ExpiresAt: expiresAt,
	}, nil
}

func (c *HTTPClient) doMCP(ctx context.Context, tool, nameKey string, owner Owner, actor string, arguments map[string]any) error {
	input := map[string]any{
		"jsonrpc": "2.0",
		"id":      tool,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": arguments,
		},
	}
	headers := make(http.Header)
	headers.Set("X-Owner-Id", owner.ID)
	headers.Set("X-Owner-Email", owner.Email)
	if actor == "" {
		promptID := nameKey
		if c.promptID != nil {
			promptID = c.promptID(nameKey)
		}
		actor = repositoryKind + ":" + promptID
	}
	headers.Set("X-Client-Id", actor)
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/mcp", headers, input, &response); err != nil {
		return err
	}
	if !response.Result.IsError {
		return nil
	}
	var sentinel error
	switch response.Result.StructuredContent.Code {
	case "not_found":
		sentinel = ErrNotFound
	case "conflict":
		sentinel = ErrConflict
	case "validation":
		sentinel = ErrValidation
	default:
		sentinel = ErrUnavailable
	}
	detail := response.Result.StructuredContent.Message
	if detail == "" {
		detail = response.Result.StructuredContent.Code
	}
	return fmt.Errorf("%w: %s", sentinel, detail)
}

func (c *HTTPClient) doJSON(ctx context.Context, method, path string, headers http.Header, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return unavailable(fmt.Errorf("encode request: %w", err))
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return unavailable(fmt.Errorf("build request: %w", err))
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
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
