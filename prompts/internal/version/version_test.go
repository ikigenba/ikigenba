package version

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordedRequest struct {
	method  string
	path    string
	headers http.Header
	query   url.Values
	body    []byte
}

func recordRequest(t *testing.T, r *http.Request) recordedRequest {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return recordedRequest{
		method: r.Method, path: r.URL.Path, headers: r.Header.Clone(), query: r.URL.Query(), body: body,
	}
}

func writeMCPResult(t *testing.T, w http.ResponseWriter, isError bool, code, message string) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      "response",
		"result": map[string]any{
			"isError": isError,
			"structuredContent": map[string]string{
				"code": code, "message": message,
			},
		},
	}); err != nil {
		t.Fatalf("encode MCP response: %v", err)
	}
}

// R-IRRX-XZQ2
func TestDomainVerbsUsePinnedMCPEnvelopeAndOwnerIdentity(t *testing.T) {
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, recordRequest(t, r))
		writeMCPResult(t, w, false, "", "")
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, func(string) string { return "unexpected" })
	owner := Owner{ID: "owner-7", Email: "owner@example.test"}
	actor := "prompts:prompt-123"
	if err := client.Create(t.Context(), "../sites/blog", owner, actor); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := client.Rename(t.Context(), "old-key", "new-key", owner, actor); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := client.Archive(t.Context(), "new-key", owner, actor); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("request count = %d, want exactly 3", len(requests))
	}
	wants := []struct {
		tool string
		args map[string]any
	}{
		{"create", map[string]any{"kind": "prompts", "name": "../sites/blog"}},
		{"rename", map[string]any{"kind": "prompts", "name": "old-key", "to": "new-key"}},
		{"delete", map[string]any{"kind": "prompts", "name": "new-key"}},
	}
	for i, want := range wants {
		request := requests[i]
		if request.method != http.MethodPost || request.path != "/mcp" {
			t.Errorf("request %d method/path = %s %s, want POST /mcp", i, request.method, request.path)
		}
		for header, value := range map[string]string{
			"X-Owner-Id": owner.ID, "X-Owner-Email": owner.Email, "X-Client-Id": actor,
		} {
			if got := request.headers.Values(header); len(got) != 1 || got[0] != value {
				t.Errorf("request %d %s = %q, want exactly %q", i, header, got, value)
			}
		}
		var envelope struct {
			JSONRPC string `json:"jsonrpc"`
			ID      string `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(request.body, &envelope); err != nil {
			t.Fatalf("request %d envelope: %v", i, err)
		}
		if envelope.JSONRPC != "2.0" || envelope.ID != want.tool || envelope.Method != "tools/call" || envelope.Params.Name != want.tool {
			t.Errorf("request %d envelope = %+v", i, envelope)
		}
		if !reflect.DeepEqual(envelope.Params.Arguments, want.args) {
			t.Errorf("request %d arguments = %#v, want %#v", i, envelope.Params.Arguments, want.args)
		}
	}
	if strings.Contains(string(requests[0].body), `"kind":"sites"`) {
		t.Fatalf("hostile name escaped prompts kind: %s", requests[0].body)
	}
}

// R-ISZU-BRGR
func TestDomainVerbMapsMCPErrorEnvelopes(t *testing.T) {
	for _, test := range []struct {
		name    string
		isError bool
		code    string
		want    error
	}{
		{"conflict", true, "conflict", ErrConflict},
		{"not found", true, "not_found", ErrNotFound},
		{"validation", true, "validation", ErrValidation},
		{"success", false, "", nil},
		{"unknown", true, "future_code", ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeMCPResult(t, w, test.isError, test.code, "specific plane message")
			}))
			defer server.Close()
			err := New(server.URL, nil).Create(t.Context(), "demo", Owner{ID: "o", Email: "e"}, "prompts:p1")
			if test.want == nil {
				if err != nil {
					t.Fatalf("Create error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("Create error = %v, want errors.Is(_, %v)", err, test.want)
			}
			if !strings.Contains(err.Error(), "specific plane message") {
				t.Fatalf("Create error = %q, want envelope message", err)
			}
		})
	}
}

// R-IU7Q-PJ7G
func TestCommitBatchesPinnedChangesWithByteExactBase64(t *testing.T) {
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, recordRequest(t, r))
		_, _ = w.Write([]byte(`{"rev":"batch-sha"}`))
	}))
	t.Cleanup(server.Close)
	binaryData := []byte{'a', 0, 0xff, 0x80, 'z'}
	sha, err := New(server.URL, nil).Commit(t.Context(), "demo", []File{
		{Path: "prompt.md", Data: binaryData},
		{Path: "system.md", Delete: true},
	}, "one action", "prompts:p1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha != "batch-sha" || len(requests) != 1 {
		t.Fatalf("Commit sha/request count = %q/%d, want batch-sha/1", sha, len(requests))
	}
	request := requests[0]
	if request.method != http.MethodPost || request.path != "/commit" {
		t.Fatalf("Commit method/path = %s %s, want POST /commit", request.method, request.path)
	}
	var body struct {
		Kind, Name, Message, Actor string
		Changes                    []map[string]json.RawMessage
	}
	if err := json.Unmarshal(request.body, &body); err != nil {
		t.Fatalf("decode Commit body: %v", err)
	}
	if body.Kind != "prompts" || body.Name != "demo" || body.Message != "one action" || body.Actor != "prompts:p1" {
		t.Fatalf("Commit body metadata = %+v", body)
	}
	if len(body.Changes) != 2 {
		t.Fatalf("Commit changes = %d, want 2", len(body.Changes))
	}
	var encoded string
	if err := json.Unmarshal(body.Changes[0]["content_b64"], &encoded); err != nil {
		t.Fatalf("decode content_b64: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !bytes.Equal(decoded, binaryData) {
		t.Fatalf("decoded content = %v, %v; want %v", decoded, err, binaryData)
	}
	if string(body.Changes[0]["op"]) != `"put"` || string(body.Changes[0]["path"]) != `"prompt.md"` {
		t.Errorf("put change = %s", body.Changes[0])
	}
	if string(body.Changes[1]["op"]) != `"delete"` || string(body.Changes[1]["path"]) != `"system.md"` {
		t.Errorf("delete change = %s", body.Changes[1])
	}
	if _, ok := body.Changes[1]["content_b64"]; ok {
		t.Errorf("delete change contains content_b64: %s", body.Changes[1])
	}
}

func tarBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for _, name := range []string{"prompt.md", "system.md", "config.json", "notes.txt"} {
		data, ok := files[name]
		if !ok {
			continue
		}
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatalf("write tar data: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return output.Bytes()
}

// R-IVFN-3AY5
func TestReadUsesArchiveSnapshotAndEnforcesDefinitionFiles(t *testing.T) {
	allFiles := map[string][]byte{
		"prompt.md": []byte("user\x00bytes"), "system.md": []byte("system"),
		"config.json": []byte{0xff, 0, 1}, "notes.txt": []byte("ignored"),
	}
	for _, test := range []struct {
		name       string
		status     int
		files      map[string][]byte
		wantSystem string
		wantErr    error
		wantText   string
	}{
		{"all files", http.StatusOK, allFiles, "system", nil, ""},
		{"optional system absent", http.StatusOK, map[string][]byte{"prompt.md": []byte("user"), "config.json": []byte("cfg")}, "", nil, ""},
		{"prompt absent", http.StatusOK, map[string][]byte{"config.json": []byte("cfg")}, "", nil, "prompt.md"},
		{"config absent", http.StatusOK, map[string][]byte{"prompt.md": []byte("user")}, "", nil, "config.json"},
		{"archive absent", http.StatusNotFound, nil, "", ErrNotFound, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request recordedRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request = recordRequest(t, r)
				if test.status != http.StatusOK {
					http.Error(w, "missing", test.status)
					return
				}
				_, _ = w.Write(tarBytes(t, test.files))
			}))
			defer server.Close()
			definition, err := New(server.URL, nil).Read(t.Context(), "name/key", "feature/ref")
			if request.method != http.MethodGet || request.path != "/archive" || request.query.Get("kind") != "prompts" || request.query.Get("name") != "name/key" || request.query.Get("ref") != "feature/ref" {
				t.Fatalf("Read request = %+v", request)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("Read error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
			if test.wantText != "" && (err == nil || !strings.Contains(err.Error(), test.wantText)) {
				t.Fatalf("Read error = %v, want absent file %q", err, test.wantText)
			}
			if test.wantErr == nil && test.wantText == "" {
				if err != nil {
					t.Fatalf("Read: %v", err)
				}
				if definition.SystemPrompt != test.wantSystem {
					t.Errorf("SystemPrompt = %q, want %q", definition.SystemPrompt, test.wantSystem)
				}
				if test.name == "all files" && (definition.UserPrompt != "user\x00bytes" || !bytes.Equal(definition.ConfigJSON, allFiles["config.json"])) {
					t.Errorf("Definition = %+v, want exact archive bytes", definition)
				}
			}
		})
	}
}

// R-IWNJ-H2OU
func TestHeadUsesListRevAndTreatsEmptyRevAsNotFound(t *testing.T) {
	for _, test := range []struct {
		name string
		rev  string
		want error
	}{
		{"revision", "ABCdef123", nil},
		{"no commits", "", ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request recordedRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request = recordRequest(t, r)
				_ = json.NewEncoder(w).Encode(map[string]string{"rev": test.rev})
			}))
			defer server.Close()
			rev, err := New(server.URL, nil).Head(t.Context(), "demo/key")
			if request.method != http.MethodGet || request.path != "/list" || request.query.Get("kind") != "prompts" || request.query.Get("name") != "demo/key" || request.query.Get("ref") != "main" {
				t.Fatalf("Head request = %+v", request)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Head error = %v, want errors.Is(_, %v)", err, test.want)
			}
			if test.want == nil && (err != nil || rev != test.rev) {
				t.Fatalf("Head = %q, %v; want %q, nil", rev, err, test.rev)
			}
		})
	}
}

// R-IXVF-UUFJ
func TestRunTokenAndPlumbingRequestsUsePinnedShapesWithoutTrustHeaders(t *testing.T) {
	expiresAt := time.Date(2030, 1, 2, 3, 4, 5, 123, time.UTC)
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, recordRequest(t, r))
		switch r.URL.Path {
		case "/commit":
			_, _ = w.Write([]byte(`{"rev":"sha"}`))
		case "/list":
			_, _ = w.Write([]byte(`{"rev":"sha"}`))
		case "/archive":
			_, _ = w.Write(tarBytes(t, map[string][]byte{"prompt.md": []byte("p"), "config.json": []byte("{}")}))
		case "/run-token":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"clone_url": "https://repos.test/prompts/demo.git", "token": "served-token", "expires_at": expiresAt.Format(time.RFC3339Nano),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := New(server.URL, func(string) string { return "p1" })
	if _, err := client.Commit(t.Context(), "demo", []File{{Path: "prompt.md", Data: []byte("p")}}, "m", "prompts:p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Head(t.Context(), "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(t.Context(), "demo", "sha"); err != nil {
		t.Fatal(err)
	}
	credential, err := client.RunToken(t.Context(), "demo", "run-9", 35*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wantCredential := Credential{CloneURL: "https://repos.test/prompts/demo.git", Username: "run", Password: "served-token", ExpiresAt: expiresAt}
	if credential != wantCredential {
		t.Fatalf("Credential = %+v, want %+v", credential, wantCredential)
	}
	if len(requests) != 4 {
		t.Fatalf("plumbing request count = %d, want 4", len(requests))
	}
	runToken := requests[3]
	if runToken.method != http.MethodPost || runToken.path != "/run-token" {
		t.Fatalf("RunToken method/path = %s %s", runToken.method, runToken.path)
	}
	var body map[string]any
	if err := json.Unmarshal(runToken.body, &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, map[string]any{"kind": "prompts", "name": "demo", "ttl": "35m0s"}) {
		t.Fatalf("RunToken body = %#v", body)
	}
	for i, request := range requests {
		for _, forbidden := range []string{"X-Owner-Id", "X-Owner-Email", "X-Forwarded-Proto"} {
			if values, ok := request.headers[http.CanonicalHeaderKey(forbidden)]; ok {
				t.Errorf("plumbing request %d carries %s=%q", i, forbidden, values)
			}
		}
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
			_, err := New(server.URL, nil).Head(t.Context(), "demo")
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
	_, err = New(closedBase, nil).Head(context.Background(), "demo")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("connection-refused error = %v, want ErrUnavailable", err)
	}
}

// R-RJJ7-FJDI
func TestCommitBatchesWritesAndDeleteInOneRequest(t *testing.T) {
	var (
		count   int
		changes []map[string]json.RawMessage
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		var body struct {
			Changes []map[string]json.RawMessage `json:"changes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode commit: %v", err)
		}
		changes = body.Changes
		_, _ = w.Write([]byte(`{"rev":"batch-sha"}`))
	}))
	t.Cleanup(server.Close)
	sha, err := New(server.URL, nil).Commit(t.Context(), "demo", []File{
		{Path: "prompt.md", Data: []byte("user")},
		{Path: "config.json", Data: []byte("{}")},
		{Path: "system.md", Delete: true},
	}, "one action", "prompts:p1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha != "batch-sha" || count != 1 || len(changes) != 3 {
		t.Fatalf("Commit sha/count/changes = %q/%d/%d, want batch-sha/1/3", sha, count, len(changes))
	}
	if string(changes[0]["op"]) != `"put"` || string(changes[1]["op"]) != `"put"` || string(changes[2]["op"]) != `"delete"` {
		t.Fatalf("Commit operations = %s, %s, %s", changes[0]["op"], changes[1]["op"], changes[2]["op"])
	}
}
