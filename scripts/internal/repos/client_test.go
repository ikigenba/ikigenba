package repos

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"scripts/internal/script"
)

// R-IKGJ-ND9W
func TestDomainVerbsUseMCPWireShapeAndOwnerIdentity(t *testing.T) {
	type call struct {
		verb, name, to string
	}
	wantCalls := []call{{"create", "nightly", ""}, {"rename", "nightly", "morning"}, {"delete", "morning", ""}}
	var gotCalls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mcp" {
			t.Errorf("request = %s %s, want POST /mcp", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Owner-Id"); got != "owner-1" {
			t.Errorf("X-Owner-Id = %q", got)
		}
		if got := r.Header.Get("X-Owner-Email"); got != "owner@example.test" {
			t.Errorf("X-Owner-Email = %q", got)
		}
		if got := r.Header.Get("X-Client-Id"); got != "scripts:S1" {
			t.Errorf("X-Client-Id = %q", got)
		}
		var envelope struct {
			JSONRPC string `json:"jsonrpc"`
			ID      string `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if envelope.JSONRPC != "2.0" || envelope.Method != "tools/call" || envelope.ID != envelope.Params.Name {
			t.Errorf("envelope = %+v", envelope)
		}
		if envelope.Params.Arguments["kind"] != "scripts" || strings.Contains(envelope.Params.Arguments["name"], "/") {
			t.Errorf("arguments = %v", envelope.Params.Arguments)
		}
		gotCalls = append(gotCalls, call{envelope.Params.Name, envelope.Params.Arguments["name"], envelope.Params.Arguments["to"]})
		_, _ = w.Write([]byte(`{"result":{"isError":false}}`))
	}))
	t.Cleanup(server.Close)
	client := New(server.URL, server.Client())
	owner := script.Owner{ID: "owner-1", Email: "owner@example.test"}
	if err := client.Create(context.Background(), "nightly", owner, "scripts:S1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := client.Rename(context.Background(), "nightly", "morning", owner, "scripts:S1"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := client.Delete(context.Background(), "morning", owner, "scripts:S1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !reflect.DeepEqual(gotCalls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", gotCalls, wantCalls)
	}
}

// R-ILOG-150L
func TestMCPEnvelopeErrorsMapToDomainSentinels(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{"conflict", script.ErrConflict},
		{"not_found", script.ErrNotFound},
		{"validation", script.ErrValidation},
		{"mystery", nil},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"result":{"isError":true,"structuredContent":{"code":%q,"message":"wire detail"}}}`, tc.code)
			}))
			defer server.Close()
			err := New(server.URL, server.Client()).Create(context.Background(), "nightly", script.Owner{}, "scripts:S1")
			if err == nil || !strings.Contains(err.Error(), "wire detail") {
				t.Fatalf("error = %v, want message", err)
			}
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
			} else {
				for _, sentinel := range []error{script.ErrConflict, script.ErrNotFound, script.ErrValidation} {
					if errors.Is(err, sentinel) {
						t.Fatalf("unknown error %v matches %v", err, sentinel)
					}
				}
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"isError":false}}`))
	}))
	defer server.Close()
	if err := New(server.URL, server.Client()).Delete(context.Background(), "nightly", script.Owner{}, "scripts:S1"); err != nil {
		t.Fatalf("non-error envelope: %v", err)
	}
}

// R-25E7-7ZFH
// R-IO48-SOHZ
func TestCommitPreservesFilesMessageAttributionAndSHA(t *testing.T) {
	source := string([]byte{'a', 0, 0xff, '\n'})
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/commit" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Kind, Name, Message, Actor string
			Changes                    []struct {
				Op         string `json:"op"`
				Path       string `json:"path"`
				ContentB64 string `json:"content_b64"`
			} `json:"changes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Kind != "scripts" || body.Name != "nightly" || body.Message != "refresh nightly" || body.Actor != "scripts:S1" {
			t.Errorf("request body = %+v", body)
		}
		if len(body.Changes) != 1 || body.Changes[0].Op != "put" || body.Changes[0].Path != "main.py" {
			t.Fatalf("changes = %+v", body.Changes)
		}
		decoded, err := base64.StdEncoding.DecodeString(body.Changes[0].ContentB64)
		if err != nil || !bytes.Equal(decoded, []byte(source)) {
			t.Errorf("decoded content = %v, %v; want %v", decoded, err, []byte(source))
		}
		_, _ = w.Write([]byte(`{"rev":"sha-from-repos"}`))
	}))
	t.Cleanup(server.Close)

	sha, err := New(server.URL, server.Client()).Commit(context.Background(), "nightly", map[string]string{"main.py": source}, "refresh nightly", "scripts:S1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha != "sha-from-repos" {
		t.Fatalf("Commit sha = %q, want sha-from-repos", sha)
	}
	if requests != 1 {
		t.Fatalf("Commit requests = %d, want 1", requests)
	}
}

// R-27TZ-ZIWV
// R-IPC5-6G8O
func TestHeadReadFileAndPlumbingFailures(t *testing.T) {
	binary := []byte{'a', 0, 0xff, '\n'}
	counts := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts[r.URL.Path]++
		switch r.URL.Path {
		case "/list":
			if r.Method != http.MethodGet || r.URL.Query().Get("kind") != "scripts" || r.URL.Query().Get("name") != "nightly" || r.URL.Query().Get("ref") != "main" {
				t.Errorf("Head request = %s %v", r.Method, r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"rev":"head-sha"}`))
		case "/content":
			if r.Method != http.MethodGet || r.URL.Query().Get("kind") != "scripts" || r.URL.Query().Get("name") != "nightly" || r.URL.Query().Get("ref") != "head-sha" || r.URL.Query().Get("path") != "main.py" {
				t.Errorf("ReadFile query = %v", r.URL.Query())
			}
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := New(server.URL, server.Client())

	sha, err := client.Head(context.Background(), "nightly", "main")
	if err != nil || sha != "head-sha" {
		t.Fatalf("Head = %q, %v; want head-sha, nil", sha, err)
	}
	gotBytes, err := client.ReadFile(context.Background(), "nightly", sha, "main.py")
	if err != nil || !bytes.Equal(gotBytes, binary) {
		t.Fatalf("ReadFile = %v, %v; want %v, nil", gotBytes, err, binary)
	}
	for _, path := range []string{"/list", "/content"} {
		if counts[path] != 1 {
			t.Errorf("requests to %s = %d, want 1", path, counts[path])
		}
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"rev":""}`)) }))
	defer empty.Close()
	if _, err := New(empty.URL, empty.Client()).Head(context.Background(), "nightly", "main"); !errors.Is(err, script.ErrNotFound) {
		t.Fatalf("empty head error = %v", err)
	}
	testPlumbingFailures(t)
}

// R-291W-DANK
func TestStatusAndTransportFailuresMapToDomainErrors(t *testing.T) {
	testPlumbingFailures(t)
}

func testPlumbingFailures(t *testing.T) {
	t.Helper()
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"not found", http.StatusNotFound, script.ErrNotFound},
		{"conflict", http.StatusConflict, script.ErrConflict},
		{"bad request", http.StatusBadRequest, script.ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, tc.name, tc.status) }))
			defer server.Close()
			_, err := New(server.URL, server.Client()).Head(context.Background(), "x", "main")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "bug", http.StatusInternalServerError) }))
	_, internalErr := New(server.URL, server.Client()).Head(context.Background(), "x", "main")
	server.Close()
	if internalErr == nil {
		t.Fatal("500 error = nil")
	}
	for _, sentinel := range []error{script.ErrNotFound, script.ErrConflict, script.ErrValidation, script.ErrSourceUnavailable} {
		if errors.Is(internalErr, sentinel) {
			t.Errorf("500 error %v unexpectedly matches %v", internalErr, sentinel)
		}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve closed port: %v", err)
	}
	closedBase := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}
	_, err = New(closedBase, http.DefaultClient).Head(context.Background(), "x", "main")
	if !errors.Is(err, script.ErrSourceUnavailable) {
		t.Fatalf("closed-port error = %v, want ErrSourceUnavailable", err)
	}
}

// R-IQK1-K7ZD
func TestRunTokenTTLAndPlumbingRequestsOmitFrontDoorHeaders(t *testing.T) {
	counts := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts[r.URL.Path]++
		for _, header := range []string{"X-Owner-Id", "X-Owner-Email", "X-Forwarded-Proto"} {
			if got := r.Header.Get(header); got != "" {
				t.Errorf("%s on %s = %q", header, r.URL.Path, got)
			}
		}
		switch r.URL.Path {
		case "/commit":
			_, _ = w.Write([]byte(`{"rev":"sha"}`))
		case "/list":
			_, _ = w.Write([]byte(`{"rev":"sha"}`))
		case "/content":
			_, _ = w.Write([]byte("body"))
		case "/run-token":
			if r.Method != http.MethodPost {
				t.Errorf("RunToken method = %s", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode RunToken: %v", err)
			}
			if !reflect.DeepEqual(body, map[string]string{"kind": "scripts", "name": "nightly", "ttl": "35m0s"}) {
				t.Errorf("RunToken body = %v", body)
			}
			_, _ = w.Write([]byte(`{"token":"token:opaque","expires_at":"later","clone_url":"http://clone/verbatim.git"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New(server.URL, server.Client())
	if _, err := client.Commit(context.Background(), "nightly", map[string]string{"main.py": "body"}, "msg", "scripts:S1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Head(context.Background(), "nightly", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadFile(context.Background(), "nightly", "main", "main.py"); err != nil {
		t.Fatal(err)
	}
	token, cloneURL, err := client.RunToken(context.Background(), "nightly", 35*time.Minute)
	if err != nil || token != "token:opaque" || cloneURL != "http://clone/verbatim.git" {
		t.Fatalf("RunToken = %q, %q, %v", token, cloneURL, err)
	}
	for _, path := range []string{"/commit", "/list", "/content", "/run-token"} {
		if counts[path] != 1 {
			t.Errorf("requests to %s = %d, want 1", path, counts[path])
		}
	}
}
