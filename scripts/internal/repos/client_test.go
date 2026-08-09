package repos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"scripts/internal/script"
)

// R-25E7-7ZFH
func TestCommitPreservesFilesMessageAttributionAndSHA(t *testing.T) {
	const source = "print('snowman: ☃')\n"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-Client-Id") != "scripts:S1" {
			t.Errorf("X-Client-Id = %q, want scripts:S1", r.Header.Get("X-Client-Id"))
		}
		var body struct {
			Key     string            `json:"key"`
			Files   map[string]string `json:"files"`
			Message string            `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Key != "scripts/nightly" || body.Message != "refresh nightly" {
			t.Errorf("request key/message = %q/%q", body.Key, body.Message)
		}
		if got := body.Files["main.py"]; got != source {
			t.Errorf("main.py bytes = %q, want %q", []byte(got), []byte(source))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sha":"sha-from-repos"}`))
	}))
	t.Cleanup(server.Close)

	sha, err := New(server.URL, server.Client()).Commit(context.Background(), "scripts/nightly", map[string]string{"main.py": source}, "refresh nightly", "scripts:S1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha != "sha-from-repos" {
		t.Fatalf("Commit sha = %q, want sha-from-repos", sha)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Commit requests = %d, want 1", got)
	}
}

// R-27TZ-ZIWV
func TestHeadReadFileAndRunTokenReturnVerbatimValues(t *testing.T) {
	binary := []byte{'a', 0, 0xff, '\n'}
	counts := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts[r.URL.Path]++
		switch r.URL.Path {
		case "/repositories/head":
			_, _ = w.Write([]byte(`{"sha":"head-sha"}`))
		case "/repositories/file":
			if r.URL.Query().Get("key") != "scripts/nightly" || r.URL.Query().Get("ref") != "head-sha" || r.URL.Query().Get("path") != "main.py" {
				t.Errorf("ReadFile query = %v", r.URL.Query())
			}
			_, _ = w.Write(binary)
		case "/repositories/run-token":
			_, _ = w.Write([]byte(`{"token":"token:opaque","clone_url":"http://clone/verbatim.git"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := New(server.URL, server.Client())

	sha, err := client.Head(context.Background(), "scripts/nightly", "main")
	if err != nil || sha != "head-sha" {
		t.Fatalf("Head = %q, %v; want head-sha, nil", sha, err)
	}
	gotBytes, err := client.ReadFile(context.Background(), "scripts/nightly", sha, "main.py")
	if err != nil || !bytes.Equal(gotBytes, binary) {
		t.Fatalf("ReadFile = %v, %v; want %v, nil", gotBytes, err, binary)
	}
	token, cloneURL, err := client.RunToken(context.Background(), "scripts/nightly")
	if err != nil || token != "token:opaque" || cloneURL != "http://clone/verbatim.git" {
		t.Fatalf("RunToken = %q, %q, %v", token, cloneURL, err)
	}
	for _, path := range []string{"/repositories/head", "/repositories/file", "/repositories/run-token"} {
		if counts[path] != 1 {
			t.Errorf("requests to %s = %d, want 1", path, counts[path])
		}
	}
}

// R-291W-DANK
func TestStatusAndTransportFailuresMapToDomainErrors(t *testing.T) {
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
			_, err := New(server.URL, server.Client()).Head(context.Background(), "scripts/x", "main")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "bug", http.StatusInternalServerError) }))
	_, internalErr := New(server.URL, server.Client()).Head(context.Background(), "scripts/x", "main")
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
	_, err = New(closedBase, http.DefaultClient).Head(context.Background(), "scripts/x", "main")
	if !errors.Is(err, script.ErrSourceUnavailable) {
		t.Fatalf("closed-port error = %v, want ErrSourceUnavailable", err)
	}
}
