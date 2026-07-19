package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/agentkit"
	"github.com/ikigenba/agentkit/toolkit"
)

const (
	nameBash       = "Bash"
	nameRead       = "Read"
	nameWrite      = "Write"
	nameEdit       = "Edit"
	nameGlob       = "Glob"
	nameGrep       = "Grep"
	nameFetch      = "Fetch"
	nameFileList   = "FileList"
	nameFileGet    = "FileGet"
	nameFilePut    = "FilePut"
	nameFileDelete = "FileDelete"
	nameFileMove   = "FileMove"
	nameFileMkdir  = "FileMkdir"
)

// ShareConfig locates the account file share's loopback filesystem API.
// BaseURL is the share service's loopback base, and ClientID is sent as
// X-Client-Id on every mutating call.
type ShareConfig struct {
	BaseURL  string
	ClientID string
}

// All returns the thirteen built-in tools confined to sandboxRoot: the six
// standard toolkit tools followed by Fetch and the six file-share tools.
func All(sandboxRoot string, sourcePortAllowed func(port int) bool, share ShareConfig) []agentkit.Tool {
	tools := toolkit.All(sandboxRoot)
	return append(tools,
		agentkit.NewTool(nameFetch, "Fetch a suite content URL into the sandbox.", func(ctx context.Context, in fetchInput) (string, error) {
			return fetchFile(ctx, sandboxRoot, sourcePortAllowed, in)
		}),
		agentkit.NewTool(nameFileList, "List entries in the account's file share.", func(ctx context.Context, in fileListInput) (string, error) {
			return fileList(ctx, share, in)
		}),
		agentkit.NewTool(nameFileGet, "Copy a file from the account's file share into the sandbox.", func(ctx context.Context, in fileGetInput) (string, error) {
			return fileGet(ctx, sandboxRoot, share, in)
		}),
		agentkit.NewTool(nameFilePut, "Copy a sandbox file to the account's file share.", func(ctx context.Context, in filePutInput) (string, error) {
			return filePut(ctx, sandboxRoot, share, in)
		}),
		agentkit.NewTool(nameFileDelete, "Delete a file or folder in the account's file share.", func(ctx context.Context, in fileDeleteInput) (string, error) {
			return fileDelete(ctx, share, in)
		}),
		agentkit.NewTool(nameFileMove, "Move or rename an entry in the account's file share.", func(ctx context.Context, in fileMoveInput) (string, error) {
			return fileMove(ctx, share, in)
		}),
		agentkit.NewTool(nameFileMkdir, "Create a folder in the account's file share.", func(ctx context.Context, in fileMkdirInput) (string, error) {
			return fileMkdir(ctx, share, in)
		}),
	)
}

type fetchInput struct {
	ContentURL string `json:"content_url" jsonschema:"required,description=A suite loopback content URL (e.g. from an event payload or a tool result)"`
	DestPath   string `json:"dest_path" jsonschema:"required,description=Destination file path, relative to the sandbox root"`
}

type fileGetInput struct {
	SharePath string `json:"share_path" jsonschema:"required,description=Path of the share file to copy"`
	DestPath  string `json:"dest_path" jsonschema:"required,description=Destination file path, relative to the sandbox root"`
}

type filePutInput struct {
	SourcePath string `json:"source_path" jsonschema:"required,description=Sandbox file to copy, relative to the sandbox root"`
	SharePath  string `json:"share_path" jsonschema:"required,description=Destination path in the share (overwrites)"`
}

type fileListInput struct {
	Path   string `json:"path,omitempty" jsonschema:"description=Share folder to list (default: the share root)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=Continuation cursor from a previous FileList result"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Maximum entries to return"`
}

type fileDeleteInput struct {
	SharePath string `json:"share_path" jsonschema:"required,description=Share file or folder to delete"`
}

type fileMoveInput struct {
	From string `json:"from" jsonschema:"required,description=Current share path"`
	To   string `json:"to" jsonschema:"required,description=New share path"`
}

type fileMkdirInput struct {
	SharePath string `json:"share_path" jsonschema:"required,description=Share folder to create"`
}

func fileList(ctx context.Context, share ShareConfig, in fileListInput) (string, error) {
	resp, err := shareRouteRequest(ctx, share, http.MethodGet, "/list", url.Values{
		"path":   {in.Path},
		"cursor": {in.Cursor},
		"limit":  {strconv.Itoa(in.Limit)},
	}, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := shareStatusError(resp); err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("source_unavailable: read file share response: %w", err)
	}
	return string(body), nil
}

func fileDelete(ctx context.Context, share ShareConfig, in fileDeleteInput) (string, error) {
	resp, err := shareRouteRequest(ctx, share, http.MethodDelete, "/content", url.Values{"path": {in.SharePath}}, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := shareStatusError(resp); err != nil {
		return "", err
	}
	return smallResult("deleted", in.SharePath)
}

func fileMove(ctx context.Context, share ShareConfig, in fileMoveInput) (string, error) {
	resp, err := shareRouteRequest(ctx, share, http.MethodPost, "/move", url.Values{"from": {in.From}, "to": {in.To}}, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := shareStatusError(resp); err != nil {
		return "", err
	}
	result, err := json.Marshal(map[string]string{"moved": in.From, "to": in.To})
	return string(result), err
}

func fileMkdir(ctx context.Context, share ShareConfig, in fileMkdirInput) (string, error) {
	resp, err := shareRouteRequest(ctx, share, http.MethodPost, "/mkdir", url.Values{"path": {in.SharePath}}, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := shareStatusError(resp); err != nil {
		return "", err
	}
	return smallResult("created", in.SharePath)
}

func smallResult(key, value string) (string, error) {
	result, err := json.Marshal(map[string]string{key: value})
	return string(result), err
}

func fileGet(ctx context.Context, root string, share ShareConfig, in fileGetInput) (string, error) {
	dest, err := confinePath(root, in.DestPath)
	if err != nil {
		return "", fmt.Errorf("validation: destination path must stay inside the sandbox: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("source_unavailable: prepare destination: %w", err)
	}
	base, err := sandboxRoot(root)
	if err != nil {
		return "", fmt.Errorf("source_unavailable: resolve sandbox: %w", err)
	}
	tmp, err := os.CreateTemp(base, ".fileget-*")
	if err != nil {
		return "", fmt.Errorf("source_unavailable: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	resp, err := shareRequest(ctx, share, http.MethodGet, in.SharePath, nil)
	if err != nil {
		_ = tmp.Close()
		return "", err
	}
	defer resp.Body.Close()
	if err := shareStatusError(resp); err != nil {
		_ = tmp.Close()
		return "", err
	}
	hash := sha256.New()
	size, err := io.Copy(tmp, io.TeeReader(resp.Body, hash))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("source_unavailable: stream file share content: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", fmt.Errorf("source_unavailable: finalize file share content: %w", err)
	}
	result, err := json.Marshal(struct {
		Path        string `json:"path"`
		Size        int64  `json:"size"`
		ContentHash string `json:"content_hash"`
	}{in.DestPath, size, fmt.Sprintf("%x", hash.Sum(nil))})
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func filePut(ctx context.Context, root string, share ShareConfig, in filePutInput) (string, error) {
	source, err := confinePath(root, in.SourcePath)
	if err != nil {
		return "", fmt.Errorf("validation: source path must stay inside the sandbox: %w", err)
	}
	file, err := os.Open(source)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("not_found: sandbox source path is absent: %s", in.SourcePath)
	}
	if err != nil {
		return "", fmt.Errorf("source_unavailable: open sandbox source: %w", err)
	}
	defer file.Close()
	resp, err := shareRequest(ctx, share, http.MethodPut, in.SharePath, file)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := shareStatusError(resp); err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("source_unavailable: read file share response: %w", err)
	}
	return string(body), nil
}

func shareRequest(ctx context.Context, share ShareConfig, method, sharePath string, body io.Reader) (*http.Response, error) {
	return shareRouteRequest(ctx, share, method, "/content", url.Values{"path": {sharePath}}, body)
}

func shareRouteRequest(ctx context.Context, share ShareConfig, method, route string, query url.Values, body io.Reader) (*http.Response, error) {
	u, err := url.Parse(strings.TrimRight(share.BaseURL, "/") + route)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("source_unavailable: invalid file share base URL")
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("source_unavailable: create file share request: %w", err)
	}
	req.Header.Set("X-Client-Id", share.ClientID)
	client := &http.Client{Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		ResponseHeaderTimeout: 10 * time.Second,
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source_unavailable: file share request: %w", err)
	}
	return resp, nil
}

func shareStatusError(resp *http.Response) error {
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	detail, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(detail))
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return fmt.Errorf("validation: %s", text)
	case http.StatusNotFound:
		return fmt.Errorf("not_found: the path is stale or absent — re-derive it, e.g. with FileList: %s", text)
	case http.StatusConflict:
		return fmt.Errorf("conflict: %s", text)
	default:
		return fmt.Errorf("source_unavailable: file share returned HTTP %d: %s", resp.StatusCode, text)
	}
}

func fetchFile(ctx context.Context, root string, sourcePortAllowed func(int) bool, in fetchInput) (string, error) {
	dest, err := confinePath(root, in.DestPath)
	if err != nil {
		return "", fmt.Errorf("validation: destination path must stay inside the sandbox: %w", err)
	}
	u, err := validateContentURL(in.ContentURL, sourcePortAllowed)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("source_unavailable: prepare destination: %w", err)
	}
	base, err := sandboxRoot(root)
	if err != nil {
		return "", fmt.Errorf("source_unavailable: resolve sandbox: %w", err)
	}
	tmp, err := os.CreateTemp(base, ".fetch-*")
	if err != nil {
		return "", fmt.Errorf("source_unavailable: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	client := &http.Client{
		Timeout: 0,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("validation: invalid content URL: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("source_unavailable: fetch content URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("not_found: the reference is stale or absent — re-derive it from the holder")
	}
	if resp.StatusCode == http.StatusConflict {
		return "", fmt.Errorf("conflict: the reference revision moved — re-derive it from the holder")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("source_unavailable: source returned HTTP %d", resp.StatusCode)
	}

	hash := sha256.New()
	size, err := io.Copy(tmp, io.TeeReader(resp.Body, hash))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("source_unavailable: stream content: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", fmt.Errorf("source_unavailable: finalize content: %w", err)
	}
	result, err := json.Marshal(struct {
		Path        string `json:"path"`
		Size        int64  `json:"size"`
		ContentHash string `json:"content_hash"`
	}{in.DestPath, size, fmt.Sprintf("%x", hash.Sum(nil))})
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func validateContentURL(raw string, sourcePortAllowed func(int) bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return nil, fmt.Errorf("validation: content URL must use the http scheme")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("validation: content URL host must be literal 127.0.0.1 or ::1")
	}
	portText := u.Port()
	port, err := strconv.Atoi(portText)
	if err != nil || portText == "" || port < 1 || port > 65535 {
		return nil, fmt.Errorf("validation: content URL must carry an explicit valid port")
	}
	if sourcePortAllowed == nil || !sourcePortAllowed(port) {
		return nil, fmt.Errorf("validation: content URL port %d is not registered", port)
	}
	return u, nil
}

func sandboxRoot(root string) (string, error) {
	if root == "" {
		return os.Getwd()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func confinePath(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path is required")
	}
	base, err := sandboxRoot(root)
	if err != nil {
		return "", err
	}
	path := p
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	realBase := resolveExisting(base)
	resolved := resolveExisting(path)
	rel, err := filepath.Rel(realBase, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes sandbox: %q", p)
	}
	return path, nil
}

func resolveExisting(path string) string {
	existing := filepath.Clean(path)
	var remainder string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return filepath.Clean(path)
		}
		remainder = filepath.Join(filepath.Base(existing), remainder)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		resolved = existing
	}
	if remainder == "" {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(filepath.Join(resolved, remainder))
}
