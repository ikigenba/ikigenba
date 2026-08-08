package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"sites/internal/sites"
)

type correlationKey struct{}

type recordedCommitRequest struct {
	clientID    string
	correlation any
	body        map[string]any
}

type commitRecordingTransport struct {
	mu     sync.Mutex
	calls  []recordedCommitRequest
	status int
	err    error
}

func (r *commitRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, readErr := io.ReadAll(request.Body)
	if readErr != nil {
		return nil, readErr
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.calls = append(r.calls, recordedCommitRequest{
		clientID: request.Header.Get("X-Client-Id"), correlation: request.Context().Value(correlationKey{}), body: decoded,
	})
	r.mu.Unlock()
	_, isCommit := decoded["changes"]
	if isCommit && r.err != nil {
		return nil, r.err
	}
	status := 0
	if isCommit {
		status = r.status
	}
	if status == 0 {
		status = http.StatusOK
	}
	responseBody := `{"rev":"write-sha"}`
	if status >= 400 {
		responseBody = `{"error":"rejected"}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
		Request:    request,
	}, nil
}

func (r *commitRecordingTransport) snapshot() []recordedCommitRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedCommitRequest(nil), r.calls...)
}

func (r *commitRecordingTransport) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

func newWritePathHandler(t *testing.T, transport http.RoundTripper) (*testHandler, string, *commitRecordingTransport) {
	t.Helper()
	recorder, ok := transport.(*commitRecordingTransport)
	if !ok {
		recorder = nil
	}
	version := sites.NewVersionClient("http://repos.test", &http.Client{Transport: transport})
	h, root := newTestHandlerWithVersion(t, version)
	createTestSite(t, h, "demo")
	if recorder != nil {
		recorder.reset()
	}
	return h, root, recorder
}

func createTestSite(t *testing.T, h http.Handler, slug string) {
	t.Helper()
	result := call(t, h, "create", map[string]any{"name": slug, "slug": slug, "visibility": "public"})
	if result.IsError {
		t.Fatalf("create %q: %#v", slug, result.StructuredContent)
	}
}

func seedTestSite(t *testing.T, h *testHandler, slug string) {
	t.Helper()
	if _, err := h.store.Create(context.Background(), slug, slug, testOwnerID, testOwner, sites.Public); err != nil {
		t.Fatalf("seed site %q: %v", slug, err)
	}
	if err := os.MkdirAll(h.layout.SiteDir(sites.Public, slug), 0o755); err != nil {
		t.Fatalf("seed site directory %q: %v", slug, err)
	}
}

func commitChange(t *testing.T, call recordedCommitRequest) (string, []byte) {
	t.Helper()
	changes, ok := call.body["changes"].([]any)
	if !ok || len(changes) != 1 {
		t.Fatalf("commit changes = %#v, want exactly one", call.body["changes"])
	}
	change, ok := changes[0].(map[string]any)
	if !ok {
		t.Fatalf("commit change = %#v", changes[0])
	}
	encoded, _ := change["content_b64"].(string)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode commit content: %v", err)
	}
	path, _ := change["path"].(string)
	return path, data
}

func callWithCorrelation(t *testing.T, h http.Handler, name string, args any, correlation string) toolResult {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptestNewRequestWithContext(context.WithValue(context.Background(), correlationKey{}, correlation), body)
	request.Header.Set("X-Owner-Id", testOwnerID)
	request.Header.Set("X-Owner-Email", testOwner)
	request.Header.Set("X-Client-Id", "cli-alice")
	recorder := &responseRecorder{header: make(http.Header)}
	h.ServeHTTP(recorder, request)
	var response jsonRPCResponse
	if err := json.Unmarshal(recorder.body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, recorder.body.String())
	}
	var result toolResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	return result
}

// These tiny HTTP test seams preserve an explicitly supplied request context;
// httptest.NewRequest starts from context.Background and cannot accept one.
func httptestNewRequestWithContext(ctx context.Context, body []byte) *http.Request {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://sites.test/mcp", bytes.NewReader(body))
	return request
}

type responseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *responseRecorder) Header() http.Header { return r.header }
func (r *responseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(body)
}
func (r *responseRecorder) WriteHeader(status int) { r.status = status }

func TestFileWriteCarriesLiveContextAndActingClientToCommit(t *testing.T) {
	// R-EQ1D-W2W0
	transport := &commitRecordingTransport{}
	h, _, _ := newWritePathHandler(t, transport)
	result := callWithCorrelation(t, h, "file_write", map[string]any{
		"site": "demo", "file_path": "index.html", "content": "hello",
	}, "corr-91")
	if result.IsError {
		t.Fatalf("file_write: %#v", result.StructuredContent)
	}
	calls := transport.snapshot()
	if len(calls) == 0 {
		t.Fatal("repos requests = 0, want at least one")
	}
	for i, call := range calls {
		if call.clientID != "cli-alice" || call.body["actor"] != "cli-alice" || call.correlation != "corr-91" {
			t.Errorf("request %d attribution/context = client %q actor %#v correlation %#v", i, call.clientID, call.body["actor"], call.correlation)
		}
	}
}

func TestFileWriteCommitsThenAppliesAndRecordsSha(t *testing.T) {
	// R-ESH6-NMDE
	transport := &commitRecordingTransport{}
	h, root, _ := newWritePathHandler(t, transport)
	want := []byte("<h1>hello</h1>")
	result := call(t, h, "file_write", map[string]any{"site": "demo", "file_path": "index.html", "content": string(want)})
	if result.IsError {
		t.Fatalf("file_write: %#v", result.StructuredContent)
	}
	calls := transport.snapshot()
	if len(calls) != 1 {
		t.Fatalf("commit calls = %d, want 1", len(calls))
	}
	path, data := commitChange(t, calls[0])
	if path != "index.html" || !bytes.Equal(data, want) || calls[0].body["message"] != "write index.html" {
		t.Fatalf("commit = path %q data %q message %#v", path, data, calls[0].body["message"])
	}
	got, err := os.ReadFile(filepath.Join(root, "public", "demo", "index.html"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("served file = %q, %v", got, err)
	}
	site, err := h.store.Get(context.Background(), "demo")
	if err != nil || site.RepoSha != "write-sha" {
		t.Fatalf("site repo sha = %q, %v", site.RepoSha, err)
	}
}

func TestFileEditCommitsPostEditBytes(t *testing.T) {
	// R-ETP3-1E43
	transport := &commitRecordingTransport{}
	h, root, _ := newWritePathHandler(t, transport)
	path := filepath.Join(root, "public", "demo", "index.html")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := call(t, h, "file_edit", map[string]any{
		"site": "demo", "file_path": "index.html", "old_string": "alpha", "new_string": "beta",
	})
	if result.IsError {
		t.Fatalf("file_edit: %#v", result.StructuredContent)
	}
	calls := transport.snapshot()
	if len(calls) != 1 {
		t.Fatalf("commit calls = %d, want 1", len(calls))
	}
	changePath, data := commitChange(t, calls[0])
	got, err := os.ReadFile(path)
	if changePath != "index.html" || string(data) != "beta" || err != nil || string(got) != "beta" {
		t.Fatalf("post-edit commit/file = path %q data %q file %q err %v", changePath, data, got, err)
	}
}

func TestMkdirStaysLocalAndNestedWriteCommitsFullPath(t *testing.T) {
	// R-EUWZ-F5US
	transport := &commitRecordingTransport{}
	h, root, _ := newWritePathHandler(t, transport)
	result := call(t, h, "mkdir", map[string]any{"slug": "demo", "path": "assets"})
	if result.IsError {
		t.Fatalf("mkdir: %#v", result.StructuredContent)
	}
	if calls := transport.snapshot(); len(calls) != 0 {
		t.Fatalf("repos calls after mkdir = %d, want 0", len(calls))
	}
	if info, err := os.Stat(filepath.Join(root, "public", "demo", "assets")); err != nil || !info.IsDir() {
		t.Fatalf("created directory = %#v, %v", info, err)
	}
	result = call(t, h, "file_write", map[string]any{"site": "demo", "file_path": "assets/app.css", "content": "body{}"})
	if result.IsError {
		t.Fatalf("nested file_write: %#v", result.StructuredContent)
	}
	calls := transport.snapshot()
	if len(calls) != 1 {
		t.Fatalf("repos calls after write = %d, want 1", len(calls))
	}
	path, _ := commitChange(t, calls[0])
	if path != "assets/app.css" {
		t.Fatalf("commit path = %q, want assets/app.css", path)
	}
}

func TestFailedCommitLeavesNewAndExistingFilesAndShaUnchanged(t *testing.T) {
	// R-EW4V-SXLH
	transport := &commitRecordingTransport{status: http.StatusBadRequest}
	h, root, _ := newWritePathHandler(t, transport)
	dir := filepath.Join(root, "public", "demo")
	existing := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(existing, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetRepoSha(context.Background(), "demo", "before-sha"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, content string }{{"new.txt", "new"}, {"existing.txt", "after"}} {
		result := call(t, h, "file_write", map[string]any{"site": "demo", "file_path": test.path, "content": test.content})
		if !result.IsError {
			t.Errorf("file_write %s succeeded, want error", test.path)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new file stat error = %v, want not exist", err)
	}
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "before" {
		t.Fatalf("existing file = %q, %v", got, err)
	}
	site, err := h.store.Get(context.Background(), "demo")
	if err != nil || site.RepoSha != "before-sha" {
		t.Fatalf("repo sha = %q, %v", site.RepoSha, err)
	}
}

func TestCommitFailureMapsUnavailableSeparatelyFromRejection(t *testing.T) {
	// R-EXCS-6PC6
	t.Run("connection refused", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		base := "http://" + listener.Addr().String()
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		h, _ := newTestHandlerWithVersion(t, sites.NewVersionClient(base, &http.Client{}))
		seedTestSite(t, h, "demo")
		result := call(t, h, "file_write", map[string]any{"site": "demo", "file_path": "index.html", "content": "x"})
		if !result.IsError || result.StructuredContent["code"] != "source_unavailable" {
			t.Fatalf("result = %#v, want source_unavailable", result)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		h, _, _ := newWritePathHandler(t, &commitRecordingTransport{status: http.StatusBadRequest})
		result := call(t, h, "file_write", map[string]any{"site": "demo", "file_path": "index.html", "content": "x"})
		if !result.IsError || result.StructuredContent["code"] != "internal" {
			t.Fatalf("result = %#v, want internal", result)
		}
	})
}
