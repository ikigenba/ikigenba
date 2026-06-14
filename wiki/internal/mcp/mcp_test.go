package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wiki/internal/events"
)

func newTestHandler() *Handler {
	return NewHandler("1", "wiki",
		func(ctx context.Context) (map[string]any, error) { return map[string]any{"ok": true}, nil },
		events.Registry, nil)
}

func rpc(t *testing.T, h *Handler, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("X-Owner-Email", "owner@example.com")
	req.Header.Set("X-Client-Id", "client-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	return out
}

// TestToolsList: the full surface is registered (ingest_text, ingest_url,
// status, search, ask, timeline, health, reflection).
func TestToolsList(t *testing.T) {
	h := newTestHandler()
	out := rpc(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result := out["result"].(map[string]any)
	tools := result["tools"].([]any)
	got := map[string]bool{}
	for _, tl := range tools {
		got[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"ingest_text", "ingest_url", "status", "search", "ask", "timeline", "health", "reflection"} {
		if !got[want] {
			t.Errorf("tools/list missing %q", want)
		}
	}
}

// TestHealthLive: health returns the envelope plus the authenticated identity.
func TestHealthLive(t *testing.T) {
	h := newTestHandler()
	out := rpc(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"health","arguments":{}}}`)
	text := toolText(t, out)
	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("health envelope: %v", err)
	}
	if env["service"] != "wiki" {
		t.Errorf("service = %v", env["service"])
	}
	if env["owner_email"] != "owner@example.com" {
		t.Errorf("owner_email = %v", env["owner_email"])
	}
}

// TestReflectionLive: reflection publishes the two wiki.* events.
func TestReflectionLive(t *testing.T) {
	h := newTestHandler()
	out := rpc(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reflection","arguments":{}}}`)
	text := toolText(t, out)
	if !strings.Contains(text, events.TypeRowDeadLettered) || !strings.Contains(text, events.TypeIngestRefused) {
		t.Errorf("reflection index missing the two wiki events: %s", text)
	}

	// Detail for a known type round-trips a schema+example.
	out = rpc(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"reflection","arguments":{"event_type":"wiki.ingest_refused"}}}`)
	if d := toolText(t, out); !strings.Contains(d, "wiki.ingest_refused") {
		t.Errorf("reflection detail wrong: %s", d)
	}
}

// TestDomainToolsStubbed: the domain tools return a not-implemented error result
// (isError) until their owning phases land.
func TestDomainToolsStubbed(t *testing.T) {
	h := newTestHandler()
	for _, name := range []string{"ingest_text", "ingest_url", "status", "search", "ask", "timeline"} {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":{}}}`
		out := rpc(t, h, body)
		result := out["result"].(map[string]any)
		if result["isError"] != true {
			t.Errorf("%q: expected isError stub result, got %v", name, result)
		}
	}
}

func toolText(t *testing.T, out map[string]any) string {
	t.Helper()
	result, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", out)
	}
	content := result["content"].([]any)
	var b bytes.Buffer
	for _, c := range content {
		b.WriteString(c.(map[string]any)["text"].(string))
	}
	return b.String()
}
