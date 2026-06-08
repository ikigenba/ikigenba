package suite

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// rpcRequest mirrors the JSON-RPC 2.0 request envelope mcpclient sends, so a peer
// can route on method and read params.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// peer is a fake suite MCP service: it serves tools/list and tools/call over the
// JSON-RPC wire mcpclient speaks, recording the identity headers and tools/call
// names it saw so tests can assert routing and identity.
type peer struct {
	srv *httptest.Server

	mu          sync.Mutex
	listed      bool
	calledNames []string
	gotEmail    string
	gotClient   string

	tools    []map[string]any // tools/list payload
	callText string           // text returned by tools/call
	callErr  bool             // isError returned by tools/call
}

func newPeer(t *testing.T, tools []map[string]any, callText string, callErr bool) *peer {
	t.Helper()
	p := &peer{tools: tools, callText: callText, callErr: callErr}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("peer decode request: %v", err)
			return
		}

		p.mu.Lock()
		p.gotEmail = r.Header.Get("X-Owner-Email")
		p.gotClient = r.Header.Get("X-Client-Id")
		p.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "tools/list":
			p.mu.Lock()
			p.listed = true
			p.mu.Unlock()
			writeResult(t, w, req.ID, map[string]any{"tools": p.tools})
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Errorf("peer unmarshal params: %v", err)
				return
			}
			p.mu.Lock()
			p.calledNames = append(p.calledNames, params.Name)
			p.mu.Unlock()
			writeResult(t, w, req.ID, map[string]any{
				"isError": p.callErr,
				"content": []map[string]any{{"type": "text", "text": p.callText}},
			})
		default:
			writeError(t, w, req.ID, -32601, "method not found")
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func writeResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result,
	}); err != nil {
		t.Fatalf("encode result: %v", err)
	}
}

func writeError(t *testing.T, w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": json.RawMessage(id),
		"error": map[string]any{"code": code, "message": msg},
	}); err != nil {
		t.Fatalf("encode error: %v", err)
	}
}

// portOf extracts the TCP port from an httptest.Server URL (it binds 127.0.0.1,
// so http://127.0.0.1:<port>/mcp reaches it).
func portOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return u.Port()
}

// writeManifest creates <root>/<svc>/etc/manifest.env with an MCP=true manifest
// pointing at the given port.
func writeManifest(t *testing.T, root, svc, port string) {
	t.Helper()
	dir := filepath.Join(root, svc, "etc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	contents := "APP=" + svc + "\nMOUNT=/srv/" + svc + "/\nPORT=" + port + "\nMCP=true\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.env"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// freeClosedPort returns a port that is currently free (a listener was opened to
// claim it, then closed), so a manifest can point at a dead address.
func freeClosedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	ln.Close()
	return port
}

func tool(name string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": name + " does a thing",
		"inputSchema": map[string]any{"type": "object"},
	}
}

// TestSelfExcluded: a prompts manifest in the root is not contacted and
// contributes no tools.
func TestSelfExcluded(t *testing.T) {
	root := t.TempDir()
	self := newPeer(t, []map[string]any{tool("ikigenba_prompts_run")}, "", false)
	crm := newPeer(t, []map[string]any{tool("ikigenba_crm_list")}, "ok", false)
	writeManifest(t, root, "prompts", portOf(t, self.srv.URL))
	writeManifest(t, root, "crm", portOf(t, crm.srv.URL))

	src := Discover(context.Background(), root, "owner@example.com", "p_123")

	if src.Owns("ikigenba_prompts_run") {
		t.Error("self tool should not be owned")
	}
	if !src.Owns("ikigenba_crm_list") {
		t.Error("crm tool should be owned")
	}
	self.mu.Lock()
	listed := self.listed
	self.mu.Unlock()
	if listed {
		t.Error("self peer (prompts) was contacted; it must be excluded before any call")
	}
}

// TestDownPeerSkipped: a manifest pointing at a dead port is skipped and
// discovery still succeeds with the live peer's tools.
func TestDownPeerSkipped(t *testing.T) {
	root := t.TempDir()
	live := newPeer(t, []map[string]any{tool("ikigenba_crm_list")}, "ok", false)
	writeManifest(t, root, "crm", portOf(t, live.srv.URL))
	writeManifest(t, root, "ledger", freeClosedPort(t)) // dead

	src := Discover(context.Background(), root, "owner@example.com", "p_123")

	if !src.Owns("ikigenba_crm_list") {
		t.Error("live peer's tool missing; down peer broke discovery")
	}
	if got := len(src.Descriptors()); got != 1 {
		t.Errorf("Descriptors len = %d, want 1 (down peer must contribute nothing)", got)
	}
}

// TestIdentityHeaders: a live peer sees X-Owner-Email and X-Client-Id
// (prompts:<promptID>) on the tools/list request.
func TestIdentityHeaders(t *testing.T) {
	root := t.TempDir()
	crm := newPeer(t, []map[string]any{tool("ikigenba_crm_list")}, "ok", false)
	writeManifest(t, root, "crm", portOf(t, crm.srv.URL))

	Discover(context.Background(), root, "alice@example.com", "p_abc")

	crm.mu.Lock()
	defer crm.mu.Unlock()
	if crm.gotEmail != "alice@example.com" {
		t.Errorf("X-Owner-Email = %q, want alice@example.com", crm.gotEmail)
	}
	if crm.gotClient != "prompts:p_abc" {
		t.Errorf("X-Client-Id = %q, want prompts:p_abc", crm.gotClient)
	}
}

// TestDispatchRoutesToOwningPeer: Dispatch routes a discovered tool to the
// correct peer and flattens the success result into a non-error block.
func TestDispatchRoutesToOwningPeer(t *testing.T) {
	root := t.TempDir()
	crm := newPeer(t, []map[string]any{tool("ikigenba_crm_list")}, "crm-result", false)
	ledger := newPeer(t, []map[string]any{tool("ikigenba_ledger_post")}, "ledger-result", false)
	writeManifest(t, root, "crm", portOf(t, crm.srv.URL))
	writeManifest(t, root, "ledger", portOf(t, ledger.srv.URL))

	src := Discover(context.Background(), root, "owner@example.com", "p_123")

	block, err := src.Dispatch(context.Background(), "ikigenba_crm_list", json.RawMessage(`{"q":"x"}`))
	if err != nil {
		t.Fatalf("Dispatch returned a Go error, want nil: %v", err)
	}
	if block.IsError {
		t.Errorf("block.IsError = true, want false on success")
	}
	var content string
	if err := json.Unmarshal(block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if content != "crm-result" {
		t.Errorf("content = %q, want crm-result", content)
	}
	if block.ToolUseID != "" {
		t.Errorf("ToolUseID = %q, want empty (the loop attaches it)", block.ToolUseID)
	}

	// The crm peer received the call; the ledger peer did not.
	crm.mu.Lock()
	crmCalls := append([]string(nil), crm.calledNames...)
	crm.mu.Unlock()
	ledger.mu.Lock()
	ledgerCalls := append([]string(nil), ledger.calledNames...)
	ledger.mu.Unlock()
	if len(crmCalls) != 1 || crmCalls[0] != "ikigenba_crm_list" {
		t.Errorf("crm calledNames = %v, want [ikigenba_crm_list]", crmCalls)
	}
	if len(ledgerCalls) != 0 {
		t.Errorf("ledger calledNames = %v, want none", ledgerCalls)
	}
}

// TestDispatchDownstreamIsError: a downstream isError result yields an is_error
// block with a nil Go error.
func TestDispatchDownstreamIsError(t *testing.T) {
	root := t.TempDir()
	crm := newPeer(t, []map[string]any{tool("ikigenba_crm_list")}, "boom", true)
	writeManifest(t, root, "crm", portOf(t, crm.srv.URL))

	src := Discover(context.Background(), root, "owner@example.com", "p_123")

	block, err := src.Dispatch(context.Background(), "ikigenba_crm_list", nil)
	if err != nil {
		t.Fatalf("Dispatch returned a Go error, want nil: %v", err)
	}
	if !block.IsError {
		t.Error("block.IsError = false, want true for a downstream isError")
	}
	var content string
	if err := json.Unmarshal(block.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if content != "boom" {
		t.Errorf("content = %q, want boom", content)
	}
}

// TestDispatchTransportFailureIsError: a Dispatch against a peer that died after
// discovery yields an is_error block with a nil Go error (never run-crashing).
func TestDispatchTransportFailureIsError(t *testing.T) {
	root := t.TempDir()
	crm := newPeer(t, []map[string]any{tool("ikigenba_crm_list")}, "ok", false)
	writeManifest(t, root, "crm", portOf(t, crm.srv.URL))

	src := Discover(context.Background(), root, "owner@example.com", "p_123")
	crm.srv.Close() // kill the peer after the snapshot

	block, err := src.Dispatch(context.Background(), "ikigenba_crm_list", nil)
	if err != nil {
		t.Fatalf("Dispatch returned a Go error, want nil: %v", err)
	}
	if !block.IsError {
		t.Error("block.IsError = false, want true for a transport failure")
	}
}

// TestInventoryErrorEmptySource: an inventory read error degrades to a non-nil,
// empty source (Discover never returns nil, never panics).
func TestInventoryErrorEmptySource(t *testing.T) {
	// An unclosed '[' in the root makes inventory.Read's filepath.Glob return a
	// bad-pattern error, exercising the inventory-error branch (not the empty
	// match path).
	src := Discover(context.Background(), "bad[root", "owner@example.com", "p_123")
	if src == nil {
		t.Fatal("Discover returned nil, want a non-nil empty source")
	}
	if got := len(src.Descriptors()); got != 0 {
		t.Errorf("Descriptors len = %d, want 0", got)
	}
	if src.Owns("anything") {
		t.Error("empty source should own nothing")
	}
}
