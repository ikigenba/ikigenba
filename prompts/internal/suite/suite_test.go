package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"eventplane/correlation"
	"github.com/ikigenba/agentkit"
	"registry"
)

func TestDiscoverMapsInventoryToPrefixedMCPServers(t *testing.T) {
	// R-ZDZU-IC81
	// R-EF0V-TP9R
	root := t.TempDir()
	writeManifest(t, root, "crm")
	writeManifest(t, root, "gmail")
	writeManifest(t, root, "prompts")

	servers := Discover(context.Background(), root, "owner-1", "owner@example.com", "prompt-1", "")
	if len(servers) != 2 {
		t.Fatalf("Discover returned %d servers, want 2: %#v", len(servers), servers)
	}
	byName := map[string]agentkit.MCPServer{}
	for _, server := range servers {
		byName[server.Name] = server
		if server.Headers["X-Owner-Id"] != "owner-1" || server.Headers["X-Owner-Email"] != "owner@example.com" || server.Headers["X-Client-Id"] != "prompts:prompt-1" {
			t.Fatalf("headers for %q = %#v", server.Name, server.Headers)
		}
	}
	for _, service := range []string{"crm", "gmail"} {
		server, ok := byName["ikigenba_"+service]
		if !ok {
			t.Fatalf("missing prefixed server for %q: %#v", service, servers)
		}
		want := registry.BaseURL(service) + "/mcp"
		if server.URL != want {
			t.Fatalf("%s URL = %q, want %q", service, server.URL, want)
		}
	}
	if _, ok := byName["ikigenba_prompts"]; ok {
		t.Fatalf("Discover included prompts self entry: %#v", servers)
	}
}

func TestDiscoverCorrelationHeaderPresentExactlyOrAbsent(t *testing.T) {
	// R-HPLU-D8WR
	root := t.TempDir()
	writeManifest(t, root, "crm")
	writeManifest(t, root, "gmail")

	const correlationID = "chain-X"
	withChain := Discover(context.Background(), root, "owner-1", "owner@example.com", "prompt-1", correlationID)
	if len(withChain) != 2 {
		t.Fatalf("Discover with chain returned %d servers, want 2", len(withChain))
	}
	for _, server := range withChain {
		want := map[string]string{
			"X-Owner-Id":       "owner-1",
			"X-Owner-Email":    "owner@example.com",
			"X-Client-Id":      "prompts:prompt-1",
			correlation.Header: correlationID,
		}
		if !maps.Equal(server.Headers, want) {
			t.Fatalf("headers for %q = %#v, want %#v", server.Name, server.Headers, want)
		}
	}

	withoutChain := Discover(context.Background(), root, "owner-1", "owner@example.com", "prompt-1", "")
	for _, server := range withoutChain {
		if value, ok := server.Headers[correlation.Header]; ok {
			t.Fatalf("headers for %q contain %s=%q, want key absent", server.Name, correlation.Header, value)
		}
	}
}

func TestDiscoverAttachmentQualifiesAndDispatchesBareMCPTool(t *testing.T) {
	// R-ZF7Q-W3YQ
	// R-HS1N-4SE5
	root := t.TempDir()
	writeManifest(t, root, "crm")
	const correlationID = "chain-X"
	servers := Discover(context.Background(), root, "owner-1", "owner@example.com", "prompt-1", correlationID)

	var calledName string
	gotHeaders := make(map[string]http.Header)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode MCP request: %v", err)
		}
		gotHeaders[req.Method] = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-11-25"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []any{map[string]any{"name": "health", "description": "Health", "inputSchema": map[string]any{"type": "object"}}}})
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("decode call params: %v", err)
			}
			calledName = params.Name
			writeRPCResult(t, w, req.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}})
		}
	}))
	defer peer.Close()
	servers[0].URL = peer.URL

	provider := &recordingProvider{}
	provider.rounds = []*agentkit.RoundTrip{
		agentkit.NewRoundTrip(agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.ToolUseBlock{ID: "call-1", Name: "ikigenba_crm_health", Input: json.RawMessage(`{}`)}}}, agentkit.FinishToolUse, agentkit.Usage{}, nil, nil, 0, false),
		agentkit.NewRoundTrip(agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: "done"}}}, agentkit.FinishStop, agentkit.Usage{}, nil, nil, 0, false),
	}
	conv := &agentkit.Conversation{Provider: provider, Model: "test", MCPServers: servers}
	stream := conv.Send(context.Background(), "check")
	for range stream.Events() {
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("Conversation.Send: %v", err)
	}
	if len(provider.requests) == 0 || len(provider.requests[0].Tools) != 1 || provider.requests[0].Tools[0].Name() != "ikigenba_crm_health" {
		t.Fatalf("provider tools = %#v, want ikigenba_crm_health", provider.requests)
	}
	if calledName != "health" {
		t.Fatalf("tools/call name = %q, want bare health", calledName)
	}
	for _, method := range []string{"tools/list", "tools/call"} {
		headers, ok := gotHeaders[method]
		if !ok {
			t.Fatalf("peer did not receive %s; methods = %#v", method, gotHeaders)
		}
		if headers.Get("X-Owner-Id") != "owner-1" || headers.Get("X-Owner-Email") != "owner@example.com" || headers.Get("X-Client-Id") != "prompts:prompt-1" {
			t.Fatalf("%s identity headers = %#v", method, headers)
		}
		if got := headers.Get(correlation.Header); got != correlationID {
			t.Fatalf("%s %s = %q, want %q", method, correlation.Header, got, correlationID)
		}
	}
}

type recordingProvider struct {
	rounds   []*agentkit.RoundTrip
	requests []*agentkit.Request
}

func (p *recordingProvider) Identity() agentkit.Identity { return agentkit.Identity{Provider: "fake"} }
func (p *recordingProvider) RoundTrip(_ context.Context, req *agentkit.Request) *agentkit.RoundTrip {
	p.requests = append(p.requests, req)
	round := p.rounds[0]
	p.rounds = p.rounds[1:]
	return round
}

func writeManifest(t *testing.T, root, service string) {
	t.Helper()
	dir := filepath.Join(root, service, "etc", "current")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	port, _ := registry.Port(service)
	data := []byte("APP=" + service + "\nMOUNT=/" + service + "/\nPORT=" + fmt.Sprint(port) + "\nMCP=true\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.env"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		t.Fatal(err)
	}
}
