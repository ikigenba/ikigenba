package sites

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestVersionClientUsesInjectedHTTPClientAcrossBothTransports(t *testing.T) {
	// R-EOTH-IB5B
	server := newRecordingReposServer()
	defer server.close()
	injected := &versionRecordingRoundTripper{delegate: http.DefaultTransport}
	escaped := &versionRecordingRoundTripper{delegate: http.DefaultTransport}
	oldDefault := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: escaped}
	defer func() { http.DefaultClient = oldDefault }()

	client := NewVersionClient(server.server.URL, &http.Client{Transport: injected})
	ctx := context.Background()
	owner := Owner{ID: "owner-id", Email: "owner@example.test"}
	if err := client.Create(ctx, "demo", owner); err != nil {
		t.Fatal(err)
	}
	commit, err := client.Commit(ctx, "demo", "two changes", []FileChange{
		{Path: "index.html", Data: []byte("new")},
		{Path: "old.css", Delete: true},
	})
	if err != nil || commit.Sha != "commit-sha" {
		t.Fatalf("commit = %#v, %v", commit, err)
	}
	entries, tip, err := client.Export(ctx, "demo")
	if err != nil || tip.Sha != "export-sha" || len(entries) != 1 || entries[0].Path != "index.html" || string(entries[0].Data) != "hello" {
		t.Fatalf("export = %#v, %#v, %v", entries, tip, err)
	}
	if err := client.Rename(ctx, "demo", "renamed", owner); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "renamed", owner); err != nil {
		t.Fatal(err)
	}

	requests := injected.snapshot()
	if len(requests) != 5 {
		t.Fatalf("injected client requests = %d, want 5", len(requests))
	}
	if got := len(escaped.snapshot()); got != 0 {
		t.Fatalf("requests escaping to http.DefaultClient = %d, want 0", got)
	}
	want := []struct{ method, path string }{
		{http.MethodPost, "/mcp"},
		{http.MethodPost, "/commit"},
		{http.MethodGet, "/archive"},
		{http.MethodPost, "/mcp"},
		{http.MethodPost, "/mcp"},
	}
	for i, request := range requests {
		if !strings.HasPrefix(request.URL.String(), server.server.URL) || request.Method != want[i].method || request.URL.Path != want[i].path {
			t.Errorf("request %d = %s %s, want base %q and %s %s", i, request.Method, request.URL, server.server.URL, want[i].method, want[i].path)
		}
	}
	calls := server.snapshot()
	if got := []string{calls[0].Tool, calls[3].Tool, calls[4].Tool}; !reflect.DeepEqual(got, []string{"create", "rename", "delete"}) {
		t.Fatalf("MCP tools = %v", got)
	}
	changes, ok := calls[1].Body["changes"].([]any)
	if !ok || len(changes) != 2 {
		t.Fatalf("commit changes = %#v, want two", calls[1].Body["changes"])
	}
	first := changes[0].(map[string]any)
	second := changes[1].(map[string]any)
	if first["op"] != "put" || first["path"] != "index.html" || first["content_b64"] != base64.StdEncoding.EncodeToString([]byte("new")) || second["op"] != "delete" || second["path"] != "old.css" {
		t.Fatalf("decoded changes = %#v", changes)
	}
	if calls[2].Arguments["kind"] != "sites" || calls[2].Arguments["name"] != "demo" || calls[2].Arguments["ref"] != "main" {
		t.Fatalf("export request = %#v", calls[2].Arguments)
	}
}

func TestVersionClientAssertsSiteOwnerAndSeparateClientAttribution(t *testing.T) {
	// R-NF7R-V95S
	server := newRecordingReposServer()
	defer server.close()
	client := NewVersionClient(server.server.URL, server.server.Client())
	owner := Owner{ID: "stable-owner-91", Email: "unrelated.person@example.test"}
	if err := client.Create(context.Background(), "first", owner); err != nil {
		t.Fatal(err)
	}
	if err := client.Rename(context.Background(), "first", "second", owner); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), "second", owner); err != nil {
		t.Fatal(err)
	}
	for i, call := range server.snapshot() {
		if call.OwnerID != owner.ID || call.OwnerEmail != owner.Email {
			t.Errorf("call %d owner headers = %q/%q, want %q/%q", i, call.OwnerID, call.OwnerEmail, owner.ID, owner.Email)
		}
		if call.ClientID == "" || call.ClientID == owner.ID || call.ClientID == owner.Email {
			t.Errorf("call %d client id = %q, want non-empty attribution distinct from owner", i, call.ClientID)
		}
	}
}

func TestVersionClientAbsorbsOnlyRequiredDomainCodes(t *testing.T) {
	// R-NGFO-90WH
	tests := []struct {
		name      string
		operation string
		code      string
		call      func(VersionClient) error
		wantError bool
	}{
		{name: "create conflict", operation: "create", code: "conflict", call: func(c VersionClient) error { return c.Create(context.Background(), "demo", Owner{}) }},
		{name: "delete not found", operation: "delete", code: "not_found", call: func(c VersionClient) error { return c.Delete(context.Background(), "demo", Owner{}) }},
		{name: "rename not found", operation: "rename", code: "not_found", call: func(c VersionClient) error { return c.Rename(context.Background(), "demo", "other", Owner{}) }, wantError: true},
		{name: "create validation", operation: "create", code: "validation", call: func(c VersionClient) error { return c.Create(context.Background(), "demo", Owner{}) }, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newRecordingReposServer()
			defer server.close()
			server.fail(test.operation, test.code)
			err := test.call(NewVersionClient(server.server.URL, server.server.Client()))
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestVersionClientJoinsTransportFailuresToUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	client := NewVersionClient(base, &http.Client{})
	owner := Owner{ID: "owner", Email: "owner@example.test"}
	tests := map[string]func() error{
		"create": func() error { return client.Create(context.Background(), "demo", owner) },
		"commit": func() error {
			_, err := client.Commit(context.Background(), "demo", "message", []FileChange{{Path: "index.html", Data: []byte("x")}})
			return err
		},
		"export": func() error { _, _, err := client.Export(context.Background(), "demo"); return err },
		"rename": func() error { return client.Rename(context.Background(), "demo", "other", owner) },
		"delete": func() error { return client.Delete(context.Background(), "demo", owner) },
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrVersionUnavailable) {
				t.Fatalf("error = %v, want ErrVersionUnavailable", err)
			}
		})
	}
}

func TestVersionClientTreatsUnbornMainAsAnEmptyExport(t *testing.T) {
	server := newRecordingReposServer()
	defer server.close()
	server.noCommits()
	client := NewVersionClient(server.server.URL, server.server.Client())
	entries, tip, err := client.Export(context.Background(), "fresh")
	if err != nil || entries == nil || len(entries) != 0 || tip.Sha != "" {
		t.Fatalf("empty export = %#v, %#v, %v", entries, tip, err)
	}
	commit, err := client.Commit(context.Background(), "fresh", "ignored no-op subject", nil)
	if err != nil || commit.Sha != "" {
		t.Fatalf("zero-change commit = %#v, %v", commit, err)
	}
	calls := server.snapshot()
	if len(calls) != 2 || calls[0].Path != "/archive" || calls[1].Path != "/archive" {
		t.Fatalf("empty operations = %#v, want two archive reads", calls)
	}
}
