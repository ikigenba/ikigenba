package sites

import (
	"appkit"
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
)

// Commit identifies one commit on a repository's main branch.
type Commit struct {
	Sha string
}

// FileChange is one path-level change in a commit.
type FileChange struct {
	Path   string
	Data   []byte
	Delete bool
}

// TreeEntry is one plain file in an exported tree.
type TreeEntry struct {
	Path string
	Data []byte
}

// Owner is the site's recorded owner asserted to the version plane.
type Owner struct {
	ID    string
	Email string
}

// VersionClient is the complete version-plane seam used by sites.
type VersionClient interface {
	Create(ctx context.Context, slug string, owner Owner) error
	Commit(ctx context.Context, slug, message string, changes []FileChange) (Commit, error)
	Export(ctx context.Context, slug string) ([]TreeEntry, Commit, error)
	Rename(ctx context.Context, oldSlug, newSlug string, owner Owner) error
	Delete(ctx context.Context, slug string, owner Owner) error
}

// ErrVersionUnavailable identifies transport and server failures from repos.
var ErrVersionUnavailable = errors.New("version plane unavailable")

type versionHTTPClient struct {
	base string
	hc   *http.Client
}

type versionActorContextKey struct{}

// WithVersionActor carries the acting client attribution without changing the
// VersionClient interface shared by service and background callers.
func WithVersionActor(ctx context.Context, actor string) context.Context {
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, versionActorContextKey{}, actor)
}

func versionActor(ctx context.Context, slug string) string {
	if actor, ok := ctx.Value(versionActorContextKey{}).(string); ok && actor != "" {
		return actor
	}
	if identity, ok := appkit.IdentityFrom(ctx); ok && identity.ClientID != "" {
		return identity.ClientID
	}
	return clientID(slug)
}

// NewVersionClient constructs the sole production version-plane client.
func NewVersionClient(base string, hc *http.Client) VersionClient {
	if hc == nil {
		panic("sites: NewVersionClient requires an HTTP client")
	}
	return &versionHTTPClient{base: strings.TrimRight(base, "/"), hc: hc}
}

func (c *versionHTTPClient) Create(ctx context.Context, slug string, owner Owner) error {
	err := c.callTool(ctx, slug, owner, "create", map[string]string{"kind": "sites", "name": slug})
	if isVersionCode(err, "conflict") {
		return nil
	}
	return err
}

func (c *versionHTTPClient) Rename(ctx context.Context, oldSlug, newSlug string, owner Owner) error {
	return c.callTool(ctx, oldSlug, owner, "rename", map[string]string{"kind": "sites", "name": oldSlug, "to": newSlug})
}

func (c *versionHTTPClient) Delete(ctx context.Context, slug string, owner Owner) error {
	err := c.callTool(ctx, slug, owner, "delete", map[string]string{"kind": "sites", "name": slug})
	if isVersionCode(err, "not_found") {
		return nil
	}
	return err
}

func (c *versionHTTPClient) Commit(ctx context.Context, slug, message string, changes []FileChange) (Commit, error) {
	if len(changes) == 0 {
		_, tip, err := c.Export(ctx, slug)
		return tip, err
	}
	type wireChange struct {
		Op         string `json:"op"`
		Path       string `json:"path"`
		ContentB64 string `json:"content_b64,omitempty"`
	}
	wireChanges := make([]wireChange, 0, len(changes))
	for _, change := range changes {
		item := wireChange{Op: "put", Path: change.Path}
		if change.Delete {
			item.Op = "delete"
		} else {
			item.ContentB64 = base64.StdEncoding.EncodeToString(change.Data)
		}
		wireChanges = append(wireChanges, item)
	}
	body, err := json.Marshal(map[string]any{
		"kind": "sites", "name": slug, "message": message,
		"actor": versionActor(ctx, slug), "changes": wireChanges,
	})
	if err != nil {
		return Commit{}, fmt.Errorf("encode version commit: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/commit", bytes.NewReader(body))
	if err != nil {
		return Commit{}, fmt.Errorf("build version commit request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Client-Id", versionActor(ctx, slug))
	response, err := c.do(request)
	if err != nil {
		return Commit{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return Commit{}, responseError("commit", response)
	}
	var result struct {
		Rev string `json:"rev"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return Commit{}, fmt.Errorf("decode version commit: %w", err)
	}
	return Commit{Sha: result.Rev}, nil
}

func (c *versionHTTPClient) Export(ctx context.Context, slug string) ([]TreeEntry, Commit, error) {
	query := url.Values{"kind": {"sites"}, "name": {slug}, "ref": {"main"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/archive?"+query.Encode(), http.NoBody)
	if err != nil {
		return nil, Commit{}, fmt.Errorf("build version export request: %w", err)
	}
	response, err := c.do(request)
	if err != nil {
		return nil, Commit{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		body, readErr := io.ReadAll(response.Body)
		if readErr == nil && strings.Contains(string(body), "no ref") {
			return []TreeEntry{}, Commit{}, nil
		}
		return nil, Commit{}, fmt.Errorf("version export: status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if response.StatusCode >= 400 {
		return nil, Commit{}, responseError("export", response)
	}
	entries, err := readTree(response.Body)
	if err != nil {
		return nil, Commit{}, fmt.Errorf("decode version export: %w", err)
	}
	return entries, Commit{Sha: response.Header.Get("X-Repos-Rev")}, nil
}

func (c *versionHTTPClient) callTool(ctx context.Context, slug string, owner Owner, tool string, arguments map[string]string) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": tool, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": arguments},
	})
	if err != nil {
		return fmt.Errorf("encode repos %s call: %w", tool, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/mcp", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build repos %s request: %w", tool, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Owner-Id", owner.ID)
	request.Header.Set("X-Owner-Email", owner.Email)
	request.Header.Set("X-Client-Id", versionActor(ctx, slug))
	response, err := c.do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return responseError(tool, response)
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
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode repos %s response: %w", tool, err)
	}
	if envelope.Result.IsError {
		return &versionCodeError{code: envelope.Result.StructuredContent.Code, message: envelope.Result.StructuredContent.Message}
	}
	return nil
}

func (c *versionHTTPClient) do(request *http.Request) (*http.Response, error) {
	response, err := c.hc.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVersionUnavailable, err)
	}
	if response.StatusCode >= 500 {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("%w: status %d: %s", ErrVersionUnavailable, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return response, nil
}

func clientID(slug string) string { return "sites:" + slug }

type versionCodeError struct {
	code    string
	message string
}

func (e *versionCodeError) Error() string { return "repos " + e.code + ": " + e.message }

func isVersionCode(err error, code string) bool {
	var coded *versionCodeError
	return errors.As(err, &coded) && coded.code == code
}

func responseError(operation string, response *http.Response) error {
	body, _ := io.ReadAll(response.Body)
	return fmt.Errorf("version %s: status %d: %s", operation, response.StatusCode, strings.TrimSpace(string(body)))
}

func readTree(reader io.Reader) ([]TreeEntry, error) {
	tarReader := tar.NewReader(reader)
	entries := []TreeEntry{}
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, err
		}
		entries = append(entries, TreeEntry{Path: header.Name, Data: data})
	}
}
