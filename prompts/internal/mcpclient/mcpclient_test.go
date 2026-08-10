package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordedRequest struct {
	path    string
	headers http.Header
	method  string
	params  json.RawMessage
}

func TestListToolsHandshakesAndRelaysDescriptors(t *testing.T) {
	// R-OLVJ-TO7G
	requests := make([]recordedRequest, 0, 3)
	schema := json.RawMessage(`{ "type": "object", "properties": {"query":{"type":"string"}} }`)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests = append(requests, recordedRequest{r.URL.Path, r.Header.Clone(), request.Method, request.Params})
		if request.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{"protocolVersion": protocolVersion}
		if request.Method == "tools/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"search","description":"Search records","inputSchema":%s}]}}`, request.ID, schema)
			return
		}
		writeResult(t, w, request.ID, result)
	}))
	defer peer.Close()

	tools, err := New(peer.Client(), peer.URL+"/mcp", map[string]string{
		"X-Owner-Id": "owner exactly",
		"X-Trace":    "case-SENSITIVE-value",
	}).ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(requests) != 3 || requests[0].method != "initialize" || requests[1].method != "notifications/initialized" || requests[2].method != "tools/list" {
		t.Fatalf("request methods = %#v, want initialize, notification, tools/list", requests)
	}
	for _, request := range requests {
		if request.path != "/mcp" {
			t.Errorf("request path = %q, want /mcp", request.path)
		}
		if request.headers.Get("X-Owner-Id") != "owner exactly" || request.headers.Get("X-Trace") != "case-SENSITIVE-value" {
			t.Errorf("request headers = %#v, configured values missing", request.headers)
		}
	}
	if len(tools) != 1 || tools[0].Name != "search" || tools[0].Description != "Search records" {
		t.Fatalf("tools = %#v, want relayed search descriptor", tools)
	}
	if !bytes.Equal(tools[0].InputSchema, schema) {
		t.Fatalf("InputSchema = %q, want byte-identical %q", tools[0].InputSchema, schema)
	}
}

func TestCallToolForwardsArgumentsAndReturnsToolError(t *testing.T) {
	// R-OOBC-L7OU
	args := json.RawMessage(`{ "query": [1,  2], "nested": {"enabled":true} }`)
	var observedArgs json.RawMessage
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		switch request.Method {
		case "initialize":
			writeResult(t, w, request.ID, map[string]any{"protocolVersion": protocolVersion})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "tools/call":
			observedArgs = append(observedArgs[:0], request.Params.Arguments...)
			writeResult(t, w, request.ID, map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "first"},
					map[string]any{"type": "image", "data": "ignored"},
					map[string]any{"type": "text", "text": " second"},
				},
				"isError": true,
			})
		}
	}))
	defer peer.Close()

	result, err := New(peer.Client(), peer.URL+"/mcp", nil).CallTool(context.Background(), "search", args)
	if err != nil {
		t.Fatalf("CallTool returned Go error for MCP tool error: %v", err)
	}
	if !bytes.Equal(observedArgs, args) {
		t.Fatalf("peer arguments = %q, want byte-identical %q", observedArgs, args)
	}
	if result.Text != "first second" || !result.IsError {
		t.Fatalf("result = %#v, want concatenated text with IsError", result)
	}
}

type countingDoer struct {
	count int
}

func (d *countingDoer) Do(request *http.Request) (*http.Response, error) {
	d.count++
	method := requestMethod(request)
	status := http.StatusOK
	body := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"` + protocolVersion + `"}}`
	if method == "notifications/initialized" {
		status = http.StatusAccepted
		body = ""
	} else if method == "tools/list" {
		body = `{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d test", status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func TestEveryRequestUsesInjectedDoer(t *testing.T) {
	// R-OPJ8-YZFJ
	doer := &countingDoer{}
	tools, err := New(doer, "http://default-transport-must-not-resolve.invalid/mcp", nil).ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 0 || doer.count != 3 {
		t.Fatalf("tools = %#v, injected Do calls = %d; want empty tools and 3 calls", tools, doer.count)
	}
}

func TestExportedMethodsNamePeerOnTransportAndRPCFailures(t *testing.T) {
	// R-OQR5-CR68
	refusing := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	refusingURL := refusing.URL + "/mcp"
	refusingClient := refusing.Client()
	refusing.Close()

	errorPeer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"error":   map[string]any{"code": -32000, "message": "peer failed"},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer errorPeer.Close()

	tests := []struct {
		name string
		call func(*Client) error
	}{
		{"Instructions", func(client *Client) error { _, err := client.Instructions(context.Background()); return err }},
		{"ListTools", func(client *Client) error { _, err := client.ListTools(context.Background()); return err }},
		{"CallTool", func(client *Client) error {
			_, err := client.CallTool(context.Background(), "health", json.RawMessage(`{}`))
			return err
		}},
	}
	peers := []struct {
		name   string
		doer   Doer
		url    string
		needle string
	}{
		{"refusing", refusingClient, refusingURL, refusingURL},
		{"rpc-error", errorPeer.Client(), errorPeer.URL + "/mcp", errorPeer.URL + "/mcp"},
	}
	for _, peer := range peers {
		for _, test := range tests {
			t.Run(peer.name+"/"+test.name, func(t *testing.T) {
				err := test.call(New(peer.doer, peer.url, nil))
				if err == nil || !strings.Contains(err.Error(), peer.needle) {
					t.Fatalf("error = %v, want error naming peer %q", err, peer.needle)
				}
			})
		}
	}
}

func writeResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func requestMethod(request *http.Request) string {
	var envelope struct {
		Method string `json:"method"`
	}
	_ = json.NewDecoder(request.Body).Decode(&envelope)
	return envelope.Method
}
