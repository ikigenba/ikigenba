package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// rpcRequest mirrors the JSON-RPC 2.0 request envelope the client sends, so the
// test server can route on method and read params.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func writeResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  result,
	}); err != nil {
		t.Fatalf("encode result: %v", err)
	}
}

func writeError(t *testing.T, w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": msg},
	}); err != nil {
		t.Fatalf("encode error: %v", err)
	}
}

func decodeReq(t *testing.T, r *http.Request) rpcRequest {
	t.Helper()
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return req
}

func TestListToolsParsesResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeReq(t, r)
		if req.Method != "tools/list" {
			t.Errorf("method = %q, want tools/list", req.Method)
		}
		writeResult(t, w, req.ID, map[string]any{
			"tools": []map[string]any{
				{
					"name":        "alpha",
					"description": "first tool",
					"inputSchema": map[string]any{"type": "object", "required": []string{"x"}},
				},
				{
					"name":        "beta",
					"description": "second tool",
					"inputSchema": map[string]any{"type": "object"},
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, nil, time.Second)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}
	if tools[0].Name != "alpha" || tools[0].Description != "first tool" {
		t.Errorf("tool[0] = %+v", tools[0])
	}
	if tools[1].Name != "beta" {
		t.Errorf("tool[1].Name = %q, want beta", tools[1].Name)
	}
	// InputSchema preserved as raw JSON, parseable back into the original shape.
	var schema struct {
		Type     string   `json:"type"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	if schema.Type != "object" || len(schema.Required) != 1 || schema.Required[0] != "x" {
		t.Errorf("InputSchema = %s, not preserved", tools[0].InputSchema)
	}
}

func TestCallToolFlattensContentAndSurfacesIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeReq(t, r)
		if req.Method != "tools/call" {
			t.Errorf("method = %q, want tools/call", req.Method)
		}
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		if p.Name != "doit" {
			t.Errorf("params.name = %q, want doit", p.Name)
		}
		if string(p.Arguments) != `{"k":"v"}` {
			t.Errorf("params.arguments = %s, want {\"k\":\"v\"}", p.Arguments)
		}
		writeResult(t, w, req.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{
				{"type": "text", "text": "part one "},
				{"type": "text", "text": "part two"},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, nil, time.Second)
	text, isError, err := c.CallTool(context.Background(), "doit", json.RawMessage(`{"k":"v"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !isError {
		t.Errorf("isError = false, want true")
	}
	if text != "part one part two" {
		t.Errorf("text = %q, want %q", text, "part one part two")
	}
}

func TestRequestCarriesInjectedHeaders(t *testing.T) {
	var gotEmail, gotClient string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmail = r.Header.Get("X-Owner-Email")
		gotClient = r.Header.Get("X-Client-Id")
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		req := decodeReq(t, r)
		writeResult(t, w, req.ID, map[string]any{"tools": []map[string]any{}})
	}))
	defer srv.Close()

	c := New(srv.URL, map[string]string{
		"X-Owner-Email": "owner@example.com",
		"X-Client-Id":   "client-123",
	}, time.Second)
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if gotEmail != "owner@example.com" {
		t.Errorf("X-Owner-Email = %q, want owner@example.com", gotEmail)
	}
	if gotClient != "client-123" {
		t.Errorf("X-Client-Id = %q, want client-123", gotClient)
	}
}

func TestTimeoutFires(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // block well past the client's timeout
	}))
	defer srv.Close()
	defer close(release)

	c := New(srv.URL, nil, 50*time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, _, err := c.CallTool(context.Background(), "slow", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("CallTool returned nil error, want timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool did not return within the timeout bound")
	}
}

func TestTransportErrorSurfaces(t *testing.T) {
	// Point at a closed server so Do() fails at the transport layer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := New(url, nil, time.Second)
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("ListTools returned nil error against a closed server, want transport error")
	}
}

func TestJSONRPCErrorObjectSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeReq(t, r)
		writeError(t, w, req.ID, -32601, "method not found")
	}))
	defer srv.Close()

	c := New(srv.URL, nil, time.Second)
	_, _, err := c.CallTool(context.Background(), "nope", nil)
	if err == nil {
		t.Fatal("CallTool returned nil error for a JSON-RPC error object, want non-nil")
	}
}
