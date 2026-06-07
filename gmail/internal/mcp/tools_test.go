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
	// P1 stub: no domain service, no reporter; events falls back to the static
	// mail.* registry (nil → Events).
	return NewHandler("v-test", "gmail", nil, nil, nil)
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

// TestToolsList asserts the P1 stub advertises exactly the two chassis tools and
// none of the P4 mailbox verbs (which must not leak in early).
func TestToolsList(t *testing.T) {
	h := newHandler(t)
	res := rpc(t, h, "tools/list", `{}`)
	tools, _ := res["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	if !names["ikigenba_gmail_health"] {
		t.Errorf("tools/list missing ikigenba_gmail_health (got %v)", names)
	}
	if !names["ikigenba_gmail_reflection"] {
		t.Errorf("tools/list missing ikigenba_gmail_reflection (got %v)", names)
	}
	if len(names) != 2 {
		t.Errorf("P1 stub must advertise exactly 2 tools, got %d: %v", len(names), names)
	}
	// P4 mailbox verbs must not appear in the P1 stub.
	for _, leaked := range []string{
		"ikigenba_gmail_list", "ikigenba_gmail_read", "ikigenba_gmail_send",
		"ikigenba_gmail_draft", "ikigenba_gmail_trash", "ikigenba_gmail_delete",
	} {
		if names[leaked] {
			t.Errorf("P1 stub leaks P4 tool %q", leaked)
		}
	}
}

// TestReflection covers the ikigenba_gmail_reflection tool: the no-arg index (the
// three published mail.* types, empty subscribes — gmail is a producer), the
// event_type detail (schema + example), and the corrective error for an unknown
// type.
func TestReflection(t *testing.T) {
	h := newHandler(t)

	// No-arg → the index {publishes, subscribes}.
	idx, isErr := callTool(t, h, "ikigenba_gmail_reflection", `{}`)
	if isErr {
		t.Fatalf("reflection index isError: %v", idx)
	}
	publishes, ok := idx["publishes"].([]any)
	if !ok {
		t.Fatalf("reflection index missing publishes array: %v", idx)
	}
	got := map[string]bool{}
	for _, pe := range publishes {
		p := pe.(map[string]any)
		got[p["type"].(string)] = true
		if p["description"] == "" {
			t.Errorf("published type %v has empty description", p["type"])
		}
	}
	for _, want := range []string{"mail.received", "mail.sent", "mail.deleted"} {
		if !got[want] {
			t.Errorf("publishes missing %q (got %v)", want, got)
		}
	}
	if len(publishes) != 3 {
		t.Errorf("expected exactly 3 published types, got %d: %v", len(publishes), publishes)
	}

	// gmail is a producer: subscribes is present and empty.
	subscribes, ok := idx["subscribes"].([]any)
	if !ok {
		t.Fatalf("reflection index missing subscribes array: %v", idx)
	}
	if len(subscribes) != 0 {
		t.Fatalf("expected empty subscribes for gmail, got %v", subscribes)
	}

	// event_type → the publish detail (schema + example).
	detail, isErr := callTool(t, h, "ikigenba_gmail_reflection", `{"event_type":"mail.received"}`)
	if isErr {
		t.Fatalf("reflection detail isError: %v", detail)
	}
	if detail["type"] != "mail.received" {
		t.Fatalf("detail type mismatch: %v", detail)
	}
	if detail["description"] == "" {
		t.Fatalf("detail missing description: %v", detail)
	}
	sch, ok := detail["schema"].(map[string]any)
	if !ok || sch["type"] != "object" {
		t.Fatalf("detail schema not an object schema: %v", detail["schema"])
	}
	if _, ok := sch["properties"].(map[string]any); !ok {
		t.Fatalf("detail schema missing properties: %v", sch)
	}
	if _, ok := detail["example"].(map[string]any); !ok {
		t.Fatalf("detail missing example object: %v", detail["example"])
	}

	// Unknown event_type → corrective error listing valid types.
	badErr, isErr := callTool(t, h, "ikigenba_gmail_reflection", `{"event_type":"mail.nope"}`)
	if !isErr {
		t.Fatalf("expected error for unknown event_type, got %v", badErr)
	}
	em, _ := badErr["error"].(map[string]any)
	if em == nil || em["code"] != "unknown_event_type" {
		t.Fatalf("expected unknown_event_type code, got %v", badErr)
	}
	msg, _ := em["message"].(string)
	if !strings.Contains(msg, "mail.received") {
		t.Errorf("corrective message missing valid type: %q", msg)
	}
}

func TestHealth_Envelope(t *testing.T) {
	h := newHandler(t)
	p, isErr := callTool(t, h, "ikigenba_gmail_health", `{}`)
	if isErr {
		t.Fatal("health isError")
	}
	// Envelope required top-level keys + identity (no reporter here → details {}).
	if p["status"] != "ok" || p["version"] != "v-test" || p["service"] != "gmail" {
		t.Errorf("health envelope keys = %v", p)
	}
	if p["owner_email"] != "me@example.com" || p["client_id"] != "client-123" {
		t.Errorf("health identity = %v", p)
	}
	d, ok := p["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing or not an object: %v", p["details"])
	}
	if len(d) != 0 {
		t.Errorf("details = %v, want empty {} with no reporter", d)
	}
}

func TestUnknownTool_IsToolError(t *testing.T) {
	h := newHandler(t)
	res := rpc(t, h, "tools/call", `{"name":"gmail_bogus","arguments":{}}`)
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Errorf("expected isError for unknown tool, got %v", res)
	}
}
