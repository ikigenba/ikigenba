package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dropbox/internal/db"
	"dropbox/internal/dropbox"

	_ "modernc.org/sqlite"
)

func newHandler(t *testing.T) *Handler {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("fk: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewHandler(dropbox.NewService(conn))
}

// rpc drives one JSON-RPC call through ServeHTTP and returns the decoded result
// object. params is the raw JSON for "params".
func rpc(t *testing.T, h *Handler, method, params string) map[string]any {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("X-Owner-Email", "me@example.com")
	req.Header.Set("X-Client-Id", "client-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d", method, rec.Code)
	}
	var env struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s: decode envelope: %v\n%s", method, err, rec.Body.String())
	}
	if env.Error != nil {
		t.Fatalf("%s: transport error %v", method, env.Error)
	}
	return env.Result
}

// callTool invokes tools/call and returns the decoded text payload plus the
// isError flag.
func callTool(t *testing.T, h *Handler, name, args string) (map[string]any, bool) {
	t.Helper()
	res := rpc(t, h, "tools/call", `{"name":"`+name+`","arguments":`+args+`}`)
	isErr, _ := res["isError"].(bool)
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("%s: no content: %v", name, res)
	}
	text := content[0].(map[string]any)["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("%s: decode payload %q: %v", name, text, err)
	}
	return payload, isErr
}

func TestToolsList_HasExactlyTwo(t *testing.T) {
	h := newHandler(t)
	res := rpc(t, h, "tools/list", `{}`)
	tools, _ := res["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools/list returned %d tools, want 2", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"dropbox_whoami", "dropbox_health"} {
		if !names[want] {
			t.Errorf("tools/list missing %s", want)
		}
	}
}

func TestWhoami(t *testing.T) {
	h := newHandler(t)
	p, isErr := callTool(t, h, "dropbox_whoami", `{}`)
	if isErr {
		t.Fatal("whoami isError")
	}
	if p["owner_email"] != "me@example.com" || p["client_id"] != "client-123" {
		t.Errorf("whoami = %v", p)
	}
}

func TestHealth_ReturnsIdentity(t *testing.T) {
	h := newHandler(t)
	p, isErr := callTool(t, h, "dropbox_health", `{}`)
	if isErr {
		t.Fatal("health isError")
	}
	if p["owner_email"] != "me@example.com" || p["client_id"] != "client-123" {
		t.Errorf("health = %v", p)
	}
}

func TestHealth_ReturnsTelemetry(t *testing.T) {
	// Build a Service with a real DB + mirror so dropbox_health returns full
	// telemetry (identity + mirror_bytes + disk numbers + failed_files).
	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/dropbox.db?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mirror, err := dropbox.NewMirror(t.TempDir())
	if err != nil {
		t.Fatalf("mirror: %v", err)
	}
	svc := dropbox.NewService(conn)
	svc.Mirror = mirror
	h := NewHandler(svc)

	p, isErr := callTool(t, h, "dropbox_health", `{}`)
	if isErr {
		t.Fatal("health isError")
	}
	if p["owner_email"] != "me@example.com" || p["client_id"] != "client-123" {
		t.Errorf("health identity = %v", p)
	}
	for _, k := range []string{"mirror_bytes", "disk_free_bytes", "disk_total_bytes", "failed_files"} {
		if _, ok := p[k]; !ok {
			t.Errorf("health missing field %q (payload %v)", k, p)
		}
	}
	// Disk numbers must be plausible/non-zero for a real filesystem.
	if dt, _ := p["disk_total_bytes"].(float64); dt <= 0 {
		t.Errorf("disk_total_bytes = %v, want > 0", p["disk_total_bytes"])
	}
	if df, _ := p["disk_free_bytes"].(float64); df <= 0 {
		t.Errorf("disk_free_bytes = %v, want > 0", p["disk_free_bytes"])
	}
}

func TestUnknownTool_IsToolError(t *testing.T) {
	h := newHandler(t)
	res := rpc(t, h, "tools/call", `{"name":"dropbox_bogus","arguments":{}}`)
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Errorf("expected isError for unknown tool, got %v", res)
	}
}
