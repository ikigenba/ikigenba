package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// R-RH3E-NZW4
func TestAllMethodsUsePromptScopedIdentityAndRepositoryKeys(t *testing.T) {
	type recordedRequest struct {
		path    string
		headers http.Header
		body    string
		query   url.Values
	}
	var (
		mu       sync.Mutex
		requests []recordedRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, recordedRequest{r.URL.Path, r.Header.Clone(), string(body), r.URL.Query()})
		mu.Unlock()
		switch r.URL.Path {
		case "/repositories/head":
			_, _ = w.Write([]byte(`{"sha":"main-sha"}`))
		case "/repositories/file":
			switch r.URL.Query().Get("path") {
			case "prompt.md":
				_, _ = w.Write([]byte("user"))
			case "system.md":
				_, _ = w.Write([]byte("system"))
			case "config.json":
				_, _ = w.Write([]byte(`{"model":"test"}`))
			}
		case "/repositories/commit":
			_, _ = w.Write([]byte(`{"sha":"commit-sha"}`))
		case "/repositories/run-token":
			_, _ = w.Write([]byte(`{"clone_url":"http://clone/prompts/demo.git","token":"secret","expires_at":"2030-01-02T03:04:05Z"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, func(string) string { return "prompt-123" })
	ctx := context.Background()
	if err := client.Create(ctx, "../sites/blog", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := client.Commit(ctx, "demo", []File{{Path: "prompt.md", Data: []byte("user")}}, "update", ""); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := client.Head(ctx, "demo"); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if _, err := client.Read(ctx, "demo", "main-sha"); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if err := client.Rename(ctx, "demo", "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := client.Archive(ctx, "renamed"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, err := client.RunToken(ctx, "renamed", "run-1", time.Minute); err != nil {
		t.Fatalf("RunToken: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 9 {
		t.Fatalf("request count = %d, want 9 (Read uses three file requests)", len(requests))
	}
	for i, request := range requests {
		if got := request.headers.Values("X-Client-Id"); len(got) != 1 || got[0] != "prompts:prompt-123" {
			t.Errorf("request %d X-Client-Id = %q, want exactly prompts:prompt-123", i, got)
		}
		for _, forbidden := range []string{"X-Owner-Email", "X-Owner-Id", "X-Forwarded-Proto"} {
			if values, ok := request.headers[http.CanonicalHeaderKey(forbidden)]; ok {
				t.Errorf("request %d carries forbidden %s header %q", i, forbidden, values)
			}
		}
		wire := request.body
		if key := request.query.Get("key"); key != "" {
			wire += key
		}
		if !strings.Contains(wire, "prompts/") {
			t.Errorf("request %d to %s does not address prompts kind: body/query %q", i, request.path, wire)
		}
	}
	if got := requests[0].body; !strings.Contains(got, `"key":"prompts/..%2Fsites%2Fblog"`) {
		t.Fatalf("adversarial Create body = %s, want escaped name beneath prompts kind", got)
	}
	if strings.Contains(requests[0].path, "/sites/") || strings.Contains(requests[0].body, `"key":"sites/`) {
		t.Fatalf("adversarial name escaped prompts boundary: path=%q body=%s", requests[0].path, requests[0].body)
	}
}

// R-RIBB-1RMT
func TestStatusAndTransportFailuresMapToTypedErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		detail string
		want   error
	}{
		{"not found", http.StatusNotFound, "missing", ErrNotFound},
		{"conflict", http.StatusConflict, "occupied", ErrConflict},
		{"validation", http.StatusBadRequest, "name must be lowercase", ErrValidation},
		{"server failure", http.StatusInternalServerError, "database offline", ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, test.detail, test.status)
			}))
			defer server.Close()
			_, err := New(server.URL, func(string) string { return "p1" }).Head(context.Background(), "demo")
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
			if test.status == http.StatusBadRequest && !strings.Contains(err.Error(), test.detail) {
				t.Fatalf("validation error %q does not contain response detail %q", err, test.detail)
			}
		})
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	closedBase := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}
	_, err = New(closedBase, func(string) string { return "p1" }).Head(context.Background(), "demo")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("connection-refused error = %v, want ErrUnavailable", err)
	}
}

// R-RJJ7-FJDI
func TestCommitBatchesWritesAndDeleteInOneRequest(t *testing.T) {
	var (
		count int
		body  struct {
			Key   string `json:"key"`
			Files []File `json:"files"`
		}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode commit: %v", err)
		}
		_, _ = w.Write([]byte(`{"sha":"batch-sha"}`))
	}))
	t.Cleanup(server.Close)
	files := []File{
		{Path: "prompt.md", Data: []byte("new user prompt")},
		{Path: "config.json", Data: []byte(`{"model":"test"}`)},
		{Path: "system.md", Delete: true},
	}
	sha, err := New(server.URL, func(string) string { return "p1" }).Commit(context.Background(), "demo", files, "one action", "")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha != "batch-sha" {
		t.Fatalf("Commit sha = %q, want batch-sha", sha)
	}
	if count != 1 {
		t.Fatalf("Commit request count = %d, want exactly 1", count)
	}
	if body.Key != "prompts/demo" || len(body.Files) != 3 {
		t.Fatalf("Commit body key/files = %q/%+v", body.Key, body.Files)
	}
	for i, want := range []string{"prompt.md", "config.json", "system.md"} {
		if body.Files[i].Path != want {
			t.Errorf("file %d path = %q, want %q", i, body.Files[i].Path, want)
		}
	}
	if body.Files[0].Delete || body.Files[1].Delete || !body.Files[2].Delete {
		t.Fatalf("Commit delete flags = %v, %v, %v", body.Files[0].Delete, body.Files[1].Delete, body.Files[2].Delete)
	}
}

func TestReadAllowsOnlyMissingSystemPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("path") {
		case "prompt.md":
			_, _ = w.Write([]byte("user"))
		case "system.md":
			http.NotFound(w, r)
		case "config.json":
			_, _ = w.Write([]byte(`{"temperature":0}`))
		default:
			http.Error(w, fmt.Sprintf("unexpected path %q", r.URL.Query().Get("path")), http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	definition, err := New(server.URL, func(string) string { return "p1" }).Read(context.Background(), "demo", "abc")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if definition.UserPrompt != "user" || definition.SystemPrompt != "" || string(definition.ConfigJSON) != `{"temperature":0}` {
		t.Fatalf("Read definition = %+v", definition)
	}
}
