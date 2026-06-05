package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler()
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

// TestToolsList_OnlyWhoami asserts the Phase-1 scaffold surface is exactly
// wiki_whoami and nothing else.
func TestToolsList_OnlyWhoami(t *testing.T) {
	h := newHandler(t)
	res := rpc(t, h, "tools/list", `{}`)
	tools, _ := res["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools/list returned %d tools, want 1", len(tools))
	}
	name := tools[0].(map[string]any)["name"].(string)
	if name != "wiki_whoami" {
		t.Fatalf("tools/list = %q, want wiki_whoami", name)
	}
}

func TestWhoami(t *testing.T) {
	h := newHandler(t)
	p, isErr := callTool(t, h, "wiki_whoami", `{}`)
	if isErr {
		t.Fatal("whoami isError")
	}
	if p["owner_email"] != "me@example.com" || p["client_id"] != "client-123" {
		t.Errorf("whoami = %v", p)
	}
}

func TestUnknownTool_IsToolError(t *testing.T) {
	h := newHandler(t)
	res := rpc(t, h, "tools/call", `{"name":"wiki_nope","arguments":{}}`)
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("expected isError for unknown tool, got %v", res)
	}
}
