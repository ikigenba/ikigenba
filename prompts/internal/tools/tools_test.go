package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/agentkit"
)

func TestAllReturnsThirteenBuiltInTools(t *testing.T) {
	// R-F5X1-XH6C
	got := All(t.TempDir(), func(int) bool { return false }, ShareConfig{})
	if len(got) != 13 {
		t.Fatalf("All returned %d tools, want 13", len(got))
	}

	gotNames := make([]string, 0, len(got))
	seen := map[string]bool{}
	for _, tool := range got {
		if tool.Name() == "" {
			t.Fatalf("tool has empty name")
		}
		if tool.Description() == "" {
			t.Fatalf("tool %q has empty description", tool.Name())
		}
		if seen[tool.Name()] {
			t.Fatalf("duplicate tool name %q", tool.Name())
		}
		seen[tool.Name()] = true
		gotNames = append(gotNames, tool.Name())

		var schema map[string]any
		if err := json.Unmarshal(tool.JSONSchema(), &schema); err != nil {
			t.Fatalf("tool %q schema is invalid JSON: %v", tool.Name(), err)
		}
		if schema["type"] != "object" {
			t.Fatalf("tool %q schema type = %v, want object", tool.Name(), schema["type"])
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Fatalf("tool %q schema has no object properties: %#v", tool.Name(), schema)
		}
	}

	wantNames := []string{nameBash, nameRead, nameWrite, nameEdit, nameGlob, nameGrep, nameFetch, nameFileList, nameFileGet, nameFilePut, nameFileDelete, nameFileMove, nameFileMkdir}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("All tool names = %v, want %v", gotNames, wantNames)
	}
	for _, tool := range got[7:] {
		if !strings.Contains(tool.Description(), "account's file share") {
			t.Fatalf("file-share tool %q description = %q, want account's file share", tool.Name(), tool.Description())
		}
	}
}

func TestFileListPassesPaginationAndReturnsShareJSON(t *testing.T) {
	// R-FASN-GK54
	response := `{"entries":[{"path":"/invoices/june.pdf","kind":"file","size":42,"content_hash":"hash-1","rev":"rev-1","updated_at":"2026-06-01T00:00:00Z"}],"cursor":"next cursor"}`
	wantQuery := url.Values{"path": {"/invoices 2026"}, "cursor": {"page 2"}, "limit": {"25"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertShareIdentityHeaders(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/list" {
			t.Errorf("request = %s %s, want GET /list", r.Method, r.URL.Path)
		}
		if got := r.URL.Query(); !reflect.DeepEqual(got, wantQuery) {
			t.Errorf("query = %v, want %v", got, wantQuery)
		}
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()

	tool := findTool(t, All(t.TempDir(), func(int) bool { return false }, ShareConfig{BaseURL: server.URL, ClientID: "prompts:prompt_123"}), nameFileList)
	raw := callTool(t, context.Background(), tool, map[string]any{"path": "/invoices 2026", "cursor": "page 2", "limit": 25})
	if raw != response {
		t.Fatalf("FileList result = %q, want share JSON %q", raw, response)
	}
}

func TestFileShareMutationsUsePinnedRoutesAndResults(t *testing.T) {
	// R-FC0J-UBVT
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertShareIdentityHeaders(t, r)
		requests[r.Method+" "+r.URL.Path]++
		switch r.Method + " " + r.URL.Path {
		case "DELETE /content":
			if got := r.URL.Query(); !reflect.DeepEqual(got, url.Values{"path": {"/old report.pdf"}}) {
				t.Errorf("delete query = %v", got)
			}
		case "POST /move":
			if got := r.URL.Query(); !reflect.DeepEqual(got, url.Values{"from": {"/old report.pdf"}, "to": {"/archive/new report.pdf"}}) {
				t.Errorf("move query = %v", got)
			}
		case "POST /mkdir":
			if got := r.URL.Query(); !reflect.DeepEqual(got, url.Values{"path": {"/archive 2026"}}) {
				t.Errorf("mkdir query = %v", got)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	builtins := All(t.TempDir(), func(int) bool { return false }, ShareConfig{BaseURL: server.URL, ClientID: "prompts:prompt_123"})
	if got := callTool(t, context.Background(), findTool(t, builtins, nameFileDelete), map[string]any{"share_path": "/old report.pdf"}); got != `{"deleted":"/old report.pdf"}` {
		t.Fatalf("FileDelete result = %s", got)
	}
	if got := callTool(t, context.Background(), findTool(t, builtins, nameFileMove), map[string]any{"from": "/old report.pdf", "to": "/archive/new report.pdf"}); got != `{"moved":"/old report.pdf","to":"/archive/new report.pdf"}` {
		t.Fatalf("FileMove result = %s", got)
	}
	if got := callTool(t, context.Background(), findTool(t, builtins, nameFileMkdir), map[string]any{"share_path": "/archive 2026"}); got != `{"created":"/archive 2026"}` {
		t.Fatalf("FileMkdir result = %s", got)
	}
	for request, count := range requests {
		if count != 1 {
			t.Fatalf("%s count = %d, want 1", request, count)
		}
	}
	if len(requests) != 3 {
		t.Fatalf("mutation requests = %v, want exactly three routes", requests)
	}
}

func TestAllUsesToolkitBashNonzeroExitSemantics(t *testing.T) {
	// R-GNY2-Y47H
	bash := findTool(t, All(t.TempDir(), func(int) bool { return false }, ShareConfig{}), nameBash)
	out, err := bash.Call(context.Background(), mustJSON(t, map[string]any{"command": "exit 7"}))
	if err != nil {
		t.Fatalf("Bash nonzero exit returned error: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "[exit status 7]") {
		t.Fatalf("Bash nonzero output = %q, want trailing [exit status 7]", out)
	}
}

func TestPromptsOwnedSandboxPathsRejectTraversal(t *testing.T) {
	// R-K1UK-T5K6
	root := t.TempDir()
	outsideName := "outside-" + filepath.Base(root) + ".bin"
	outsidePath := filepath.Join(filepath.Dir(root), outsideName)
	if err := os.WriteFile(outsidePath, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outsidePath) })

	builtins := All(root, func(int) bool { return false }, ShareConfig{})
	for _, tc := range []struct {
		name  string
		input map[string]any
	}{
		{nameFetch, map[string]any{"content_url": "http://127.0.0.1:1/content", "dest_path": "../" + outsideName}},
		{nameFileGet, map[string]any{"share_path": "/source", "dest_path": "../" + outsideName}},
		{nameFilePut, map[string]any{"source_path": "../" + outsideName, "share_path": "/dest"}},
	} {
		_, err := findTool(t, builtins, tc.name).Call(context.Background(), mustJSON(t, tc.input))
		if err == nil || !strings.Contains(err.Error(), "escapes sandbox") {
			t.Errorf("%s traversal error = %v, want escapes sandbox", tc.name, err)
		}
	}
	assertFileContent(t, outsidePath, "sentinel")
}

func TestFetchStreamsAllowedContentIntoSandbox(t *testing.T) {
	// R-65YV-4ES6
	body := []byte("known invoice bytes\n")
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	port := serverPort(t, server.URL)
	root := t.TempDir()
	tool := findTool(t, All(root, func(got int) bool { return got == port }, ShareConfig{}), nameFetch)
	raw := callTool(t, context.Background(), tool, map[string]any{"content_url": server.URL + "/content", "dest_path": "incoming/invoice.pdf"})
	var result struct {
		Path        string `json:"path"`
		Size        int64  `json:"size"`
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("fetch result JSON: %v", err)
	}
	if result.Path != "incoming/invoice.pdf" || result.Size != int64(len(body)) || result.ContentHash != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("fetch result = %+v", result)
	}
	if gets != 1 {
		t.Fatalf("GET count = %d, want 1", gets)
	}
	assertFileContent(t, filepath.Join(root, "incoming", "invoice.pdf"), string(body))
}

func TestFetchRejectsUnconfinedURLsAndDestinationsBeforeRequest(t *testing.T) {
	// R-676R-I6IV
	// R-68EN-VY9K
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { gets++ }))
	defer server.Close()
	port := serverPort(t, server.URL)
	root := t.TempDir()
	tool := findTool(t, All(root, func(got int) bool { return got == port }, ShareConfig{}), nameFetch)
	for _, input := range []map[string]any{
		{"content_url": "http://10.0.0.5:3202/file", "dest_path": "x"},
		{"content_url": fmt.Sprintf("http://localhost:%d/file", port), "dest_path": "x"},
		{"content_url": "http://127.0.0.1:1/file", "dest_path": "x"},
		{"content_url": server.URL + "/file", "dest_path": "../outside.bin"},
		{"content_url": server.URL + "/file", "dest_path": filepath.Join(filepath.Dir(root), "outside.bin")},
	} {
		_, err := tool.Call(context.Background(), mustJSON(t, input))
		if err == nil || !strings.HasPrefix(err.Error(), "validation:") {
			t.Fatalf("Fetch(%v) error = %v, want validation", input, err)
		}
	}
	if gets != 0 {
		t.Fatalf("rejected inputs made %d requests, want 0", gets)
	}
	callTool(t, context.Background(), tool, map[string]any{"content_url": server.URL + "/file", "dest_path": "allowed"})
	if gets != 1 {
		t.Fatalf("allowed loopback URL made %d requests, want 1", gets)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside.bin")); !os.IsNotExist(err) {
		t.Fatalf("escaping destination exists: %v", err)
	}
}

func TestFileGetStreamsShareContentAndRejectsEscapingDestination(t *testing.T) {
	// R-F74Y-B8X1
	// R-F8CU-P0NQ
	body := []byte("known share invoice bytes\n")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/content" {
			t.Errorf("request = %s %s, want GET /content", r.Method, r.URL.Path)
		}
		if r.URL.RawQuery != "path="+url.QueryEscape("/invoices/june 2026.pdf") {
			t.Errorf("raw query = %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("X-Client-Id"); got != "prompts:prompt_123" {
			t.Errorf("X-Client-Id = %q", got)
		}
		if got := r.Header.Get("X-Owner-Email"); got != "" {
			t.Errorf("X-Owner-Email = %q, want empty", got)
		}
		if got := r.Header.Get("X-Forwarded-Proto"); got != "" {
			t.Errorf("X-Forwarded-Proto = %q, want empty", got)
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	root := t.TempDir()
	tool := findTool(t, All(root, func(int) bool { return false }, ShareConfig{BaseURL: server.URL, ClientID: "prompts:prompt_123"}), nameFileGet)
	raw := callTool(t, context.Background(), tool, map[string]any{"share_path": "/invoices/june 2026.pdf", "dest_path": "inbox/june.pdf"})
	var result struct {
		Path        string `json:"path"`
		Size        int64  `json:"size"`
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("result JSON: %v", err)
	}
	if result.Path != "inbox/june.pdf" || result.Size != int64(len(body)) || result.ContentHash != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("result = %+v", result)
	}
	assertFileContent(t, filepath.Join(root, "inbox", "june.pdf"), string(body))
	_, err := tool.Call(context.Background(), mustJSON(t, map[string]any{"share_path": "/ignored", "dest_path": "../outside.bin"}))
	if err == nil || !strings.HasPrefix(err.Error(), "validation:") {
		t.Fatalf("escape error = %v, want validation", err)
	}
	if requests != 1 {
		t.Fatalf("request count = %d, want 1", requests)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside.bin")); !os.IsNotExist(err) {
		t.Fatalf("escaping destination exists: %v", err)
	}
}

func TestFilePutStreamsSandboxContentAndRejectsInvalidSources(t *testing.T) {
	// R-F74Y-B8X1
	// R-F9KR-2SEF
	root := t.TempDir()
	body := []byte("extraction artifact\n")
	if err := os.WriteFile(filepath.Join(root, "result.json"), body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	requests := 0
	response := `{"path":"/results/result.json","size":19,"content_hash":"opaque-rev-hash","rev":"rev-7"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPut || r.URL.Path != "/content" {
			t.Errorf("request = %s %s, want PUT /content", r.Method, r.URL.Path)
		}
		if r.URL.RawQuery != "path="+url.QueryEscape("/results/result.json") {
			t.Errorf("raw query = %q", r.URL.RawQuery)
		}
		gotBody, err := io.ReadAll(r.Body)
		if err != nil || string(gotBody) != string(body) {
			t.Errorf("body = %q, err = %v", gotBody, err)
		}
		if got := r.Header.Get("X-Client-Id"); got != "prompts:prompt_123" {
			t.Errorf("X-Client-Id = %q", got)
		}
		if r.Header.Get("X-Owner-Email") != "" || r.Header.Get("X-Forwarded-Proto") != "" {
			t.Errorf("unexpected nginx identity headers")
		}
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()
	tool := findTool(t, All(root, func(int) bool { return false }, ShareConfig{BaseURL: server.URL, ClientID: "prompts:prompt_123"}), nameFilePut)
	if got := callTool(t, context.Background(), tool, map[string]any{"source_path": "result.json", "share_path": "/results/result.json"}); got != response {
		t.Fatalf("result = %q, want verbatim %q", got, response)
	}
	for _, source := range []string{"../outside.json", "missing.json"} {
		_, err := tool.Call(context.Background(), mustJSON(t, map[string]any{"source_path": source, "share_path": "/ignored"}))
		want := "not_found:"
		if source == "../outside.json" {
			want = "validation:"
		}
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Fatalf("source %q error = %v, want %s", source, err, want)
		}
	}
	if requests != 1 {
		t.Fatalf("request count = %d, want 1", requests)
	}
}

func TestFileShareFailureMapping(t *testing.T) {
	// R-FD8G-83MI
	root := t.TempDir()
	status := http.StatusBadRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "share detail")
	}))
	defer server.Close()
	get := findTool(t, All(root, func(int) bool { return false }, ShareConfig{BaseURL: server.URL, ClientID: "prompts:p"}), nameFileGet)
	for _, tc := range []struct {
		status int
		prefix string
	}{{http.StatusBadRequest, "validation:"}, {http.StatusNotFound, "not_found:"}, {http.StatusConflict, "conflict:"}} {
		status = tc.status
		_, err := get.Call(context.Background(), mustJSON(t, map[string]any{"share_path": "/missing", "dest_path": "failed.bin"}))
		if err == nil || !strings.HasPrefix(err.Error(), tc.prefix) {
			t.Fatalf("HTTP %d error = %v, want %s", tc.status, err, tc.prefix)
		}
		if tc.status == http.StatusBadRequest && !strings.Contains(err.Error(), "share detail") {
			t.Fatalf("400 error omitted detail: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(root, "failed.bin")); !os.IsNotExist(statErr) {
			t.Fatalf("failed FileGet left destination: %v", statErr)
		}
	}
	put := findTool(t, All(root, func(int) bool { return false }, ShareConfig{BaseURL: "http://127.0.0.1:1", ClientID: "prompts:p"}), nameFilePut)
	if err := os.WriteFile(filepath.Join(root, "source.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := put.Call(context.Background(), mustJSON(t, map[string]any{"source_path": "source.bin", "share_path": "/x"}))
	if err == nil || !strings.HasPrefix(err.Error(), "source_unavailable:") {
		t.Fatalf("connection-refused error = %v", err)
	}
}

func TestFetchMapsSourceFailuresWithoutDestinationFile(t *testing.T) {
	// R-69MK-9Q09
	redirectTargetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectTargetCalls++ }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/conflict":
			w.WriteHeader(http.StatusConflict)
		case "/redirect":
			http.Redirect(w, r, target.URL, http.StatusFound)
		}
	}))
	defer source.Close()
	port := serverPort(t, source.URL)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	root := t.TempDir()
	tool := findTool(t, All(root, func(got int) bool { return got == port || got == closedPort }, ShareConfig{}), nameFetch)
	for path, prefix := range map[string]string{"/missing": "not_found:", "/conflict": "conflict:", "/redirect": "source_unavailable:"} {
		dest := "failed" + strings.ReplaceAll(path, "/", "_")
		_, err := tool.Call(context.Background(), mustJSON(t, map[string]any{"content_url": source.URL + path, "dest_path": dest}))
		if err == nil || !strings.HasPrefix(err.Error(), prefix) {
			t.Fatalf("%s error = %v, want %s", path, err, prefix)
		}
		if _, statErr := os.Stat(filepath.Join(root, dest)); !os.IsNotExist(statErr) {
			t.Fatalf("%s destination exists: %v", path, statErr)
		}
	}
	_, err = tool.Call(context.Background(), mustJSON(t, map[string]any{"content_url": fmt.Sprintf("http://127.0.0.1:%d/file", closedPort), "dest_path": "refused"}))
	if err == nil || !strings.HasPrefix(err.Error(), "source_unavailable:") {
		t.Fatalf("refused error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "refused")); !os.IsNotExist(statErr) {
		t.Fatalf("refused destination exists: %v", statErr)
	}
	if redirectTargetCalls != 0 {
		t.Fatalf("redirect target received %d requests", redirectTargetCalls)
	}
}

func serverPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func findTool(t *testing.T, tools []agentkit.Tool, name string) agentkit.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func callTool(t *testing.T, ctx context.Context, tool agentkit.Tool, input map[string]any) string {
	t.Helper()
	out, err := tool.Call(ctx, mustJSON(t, input))
	if err != nil {
		t.Fatalf("%s.Call(%v): %v", tool.Name(), input, err)
	}
	return out
}

func assertShareIdentityHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Client-Id"); got != "prompts:prompt_123" {
		t.Errorf("X-Client-Id = %q, want prompts:prompt_123", got)
	}
	if got := r.Header.Get("X-Owner-Email"); got != "" {
		t.Errorf("X-Owner-Email = %q, want empty", got)
	}
	if got := r.Header.Get("X-Forwarded-Proto"); got != "" {
		t.Errorf("X-Forwarded-Proto = %q, want empty", got)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(b) != want {
		t.Fatalf("%s content = %q, want %q", path, string(b), want)
	}
}
