package sites

import (
	"archive/tar"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

type recordedReposCall struct {
	Method     string
	Path       string
	Tool       string
	OwnerID    string
	OwnerEmail string
	ClientID   string
	Arguments  map[string]any
	Body       map[string]any
}

type recordingReposServer struct {
	server *httptest.Server

	mu       sync.Mutex
	calls    []recordedReposCall
	failures map[string]string
	empty    bool
}

func newRecordingReposServer() *recordingReposServer {
	recorder := &recordingReposServer{failures: map[string]string{}}
	recorder.server = httptest.NewServer(http.HandlerFunc(recorder.serveHTTP))
	return recorder
}

func (s *recordingReposServer) close() { s.server.Close() }

func (s *recordingReposServer) fail(operation, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[operation] = code
}

func (s *recordingReposServer) noCommits() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.empty = true
}

func (s *recordingReposServer) snapshot() []recordedReposCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedReposCall(nil), s.calls...)
}

func (s *recordingReposServer) serveHTTP(w http.ResponseWriter, request *http.Request) {
	call := recordedReposCall{
		Method: request.Method, Path: request.URL.Path,
		OwnerID: request.Header.Get("X-Owner-Id"), OwnerEmail: request.Header.Get("X-Owner-Email"),
		ClientID: request.Header.Get("X-Client-Id"),
	}
	operation := ""
	if request.URL.Path == "/mcp" {
		var envelope struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(request.Body).Decode(&envelope)
		call.Tool = envelope.Params.Name
		call.Arguments = envelope.Params.Arguments
		operation = call.Tool
	} else if request.URL.Path == "/commit" {
		_ = json.NewDecoder(request.Body).Decode(&call.Body)
		operation = "commit"
	} else if request.URL.Path == "/archive" {
		call.Arguments = map[string]any{"kind": request.URL.Query().Get("kind"), "name": request.URL.Query().Get("name"), "ref": request.URL.Query().Get("ref")}
		operation = "export"
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	code := s.failures[operation]
	empty := s.empty
	s.mu.Unlock()

	if request.URL.Path == "/mcp" {
		result := map[string]any{"structuredContent": map[string]any{"kind": "sites", "name": call.Arguments["name"]}}
		if code != "" {
			result = map[string]any{"isError": true, "structuredContent": map[string]any{"code": code, "message": operation + " failed"}}
		}
		writeTestJSON(w, map[string]any{"jsonrpc": "2.0", "id": operation, "result": result})
		return
	}
	if request.URL.Path == "/commit" {
		writeTestJSON(w, map[string]any{"rev": "commit-sha"})
		return
	}
	if request.URL.Path == "/archive" {
		if empty {
			http.Error(w, `repos: not found: no ref "main"`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("X-Repos-Rev", "export-sha")
		tarWriter := tar.NewWriter(w)
		_ = tarWriter.WriteHeader(&tar.Header{Name: "index.html", Mode: 0o644, Size: int64(len("hello"))})
		_, _ = tarWriter.Write([]byte("hello"))
		_ = tarWriter.Close()
		return
	}
	http.NotFound(w, request)
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

type versionRecordingRoundTripper struct {
	delegate http.RoundTripper

	mu       sync.Mutex
	requests []*http.Request
}

func (r *versionRecordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request.Clone(request.Context()))
	r.mu.Unlock()
	return r.delegate.RoundTrip(request)
}

func (r *versionRecordingRoundTripper) snapshot() []*http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*http.Request(nil), r.requests...)
}
