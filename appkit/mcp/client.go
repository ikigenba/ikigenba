package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

// ClientConfig describes a stateless backend connection.
type ClientConfig struct {
	Endpoint      string
	HTTPClient    *http.Client
	Name, Version string
}

// Client sends independent MCP requests on behalf of callers.
type Client struct {
	cfg  ClientConfig
	next atomic.Uint64
}

// NewClient records cfg without contacting the endpoint.
func NewClient(cfg ClientConfig) *Client { return &Client{cfg: cfg} }

// ToolInfo describes a tool advertised by a backend.
type ToolInfo struct {
	Name, Description         string
	InputSchema, OutputSchema json.RawMessage
	Annotations               Annotations
}

// Annotations carries optional tool hints.
type Annotations struct{ ReadOnlyHint, DestructiveHint, IdempotentHint, OpenWorldHint *bool }

// Effect applies the protocol's conservative defaults.
func (t ToolInfo) Effect() Effect {
	if t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint {
		return Read
	}
	if t.Annotations.DestructiveHint != nil && !*t.Annotations.DestructiveHint {
		return Additive
	}
	return Destructive
}

// RPCError is an error returned by the backend protocol.
type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *RPCError) Error() string { return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, e.Message) }

// HTTPError reports a response that cannot answer the MCP request.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("mcp: unexpected HTTP response (status %d)", e.StatusCode)
}

// ListTools collects every page in backend order.
func (c *Client) ListTools(ctx context.Context, caller identity.Caller) ([]ToolInfo, error) {
	params := map[string]any{}
	used := map[string]bool{}
	var tools []ToolInfo
	for {
		result, err := c.request(ctx, caller, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page map[string]json.RawMessage
		if err := json.Unmarshal(result, &page); err != nil {
			return nil, fmt.Errorf("mcp: invalid tools page: %w", err)
		}
		raw, ok := page["tools"]
		if !ok || !clientArray(raw) {
			return nil, fmt.Errorf("mcp: tools must be an array")
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("mcp: invalid tools: %w", err)
		}
		for _, entry := range entries {
			tool, err := clientTool(entry)
			if err != nil {
				return nil, err
			}
			tools = append(tools, tool)
		}
		raw, ok = page["nextCursor"]
		if !ok {
			return tools, nil
		}
		var cursor string
		if !clientString(raw, &cursor) || used[cursor] {
			return nil, fmt.Errorf("mcp: invalid or repeated cursor")
		}
		used[cursor] = true
		params["cursor"] = cursor
	}
}

// CallTool returns the backend's tool result, including tool-level errors.
func (c *Client) CallTool(ctx context.Context, caller identity.Caller, name string, args json.RawMessage) (Result, error) {
	if args == nil {
		args = json.RawMessage(`{}`)
	}
	if !json.Valid(args) {
		return Result{}, fmt.Errorf("mcp: invalid arguments JSON")
	}
	raw, err := c.request(ctx, caller, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return Result{}, err
	}
	var result Result
	if err := result.UnmarshalJSON(raw); err != nil {
		return Result{}, fmt.Errorf("mcp: invalid tool result: %w", err)
	}
	return result, nil
}
func (c *Client) request(ctx context.Context, caller identity.Caller, method string, params map[string]any) (json.RawMessage, error) {
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	if c.cfg.Name != "" {
		meta["io.modelcontextprotocol/clientInfo"] = map[string]string{"name": c.cfg.Name, "version": c.cfg.Version}
	}
	params["_meta"] = meta
	id := c.next.Add(1)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, fmt.Errorf("mcp: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	req.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		name := params["name"].(string)
		req.Header.Set("Mcp-Name", clientHeaderName(name))
	}
	identity.Forward(caller, req)
	hc := c.cfg.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media == "text/event-stream" && resp.StatusCode == http.StatusOK {
		return clientStream(resp.Body, id)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp: read response: %w", err)
	}
	if media == "text/event-stream" {
		_, streamErr := clientStream(bytes.NewReader(data), id)
		var rpc *RPCError
		if errors.As(streamErr, &rpc) {
			return nil, rpc
		}
	}
	result, rpc, valid, err := clientResponse(data, id, false)
	if rpc != nil {
		return nil, rpc
	}
	if resp.StatusCode != http.StatusOK || media != "application/json" || !valid {
		if len(data) > 4096 {
			data = data[:4096]
		}
		return nil, &HTTPError{resp.StatusCode, string(data)}
	}
	return result, err
}
func clientHeaderName(name string) string {
	plain := !strings.HasPrefix(name, "=?base64?")
	for i := 0; i < len(name); i++ {
		if name[i] < 0x21 || name[i] > 0x7e {
			plain = false
		}
	}
	if plain {
		return name
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(name)) + "?="
}
func clientObject(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{' && json.Valid(raw)
}
func clientArray(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '[' && json.Valid(raw)
}
func clientString(raw []byte, value *string) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, value) == nil
}
func clientResponse(data []byte, id uint64, stream bool) (json.RawMessage, *RPCError, bool, error) {
	var msg map[string]json.RawMessage
	if !clientObject(data) || json.Unmarshal(data, &msg) != nil || string(msg["jsonrpc"]) != `"2.0"` {
		return nil, nil, false, fmt.Errorf("mcp: invalid JSON-RPC message")
	}
	if raw, ok := msg["error"]; ok && clientObject(raw) {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		var code int
		var message string
		if json.Unmarshal(fields["code"], &code) == nil && string(fields["code"]) != "null" && clientString(fields["message"], &message) {
			return nil, &RPCError{code, message, fields["data"]}, true, nil
		}
	}
	raw, hasResult := msg["result"]
	_, hasError := msg["error"]
	if !hasResult && !hasError {
		if stream {
			var method string
			if clientString(msg["method"], &method) {
				return nil, nil, true, nil
			}
		}
		return nil, nil, false, fmt.Errorf("mcp: missing response")
	}
	var got uint64
	if json.Unmarshal(msg["id"], &got) != nil || string(msg["id"]) == "null" || got != id {
		return nil, nil, true, fmt.Errorf("mcp: response id mismatch")
	}
	if !hasResult || !clientObject(raw) {
		return nil, nil, true, fmt.Errorf("mcp: result must be an object")
	}
	var result map[string]json.RawMessage
	_ = json.Unmarshal(raw, &result)
	if typ, ok := result["resultType"]; ok {
		var value string
		if !clientString(typ, &value) || value != "complete" {
			return nil, nil, true, fmt.Errorf("mcp: unsupported resultType")
		}
	}
	return raw, nil, true, nil
}
func clientStream(reader io.Reader, id uint64) (json.RawMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), int(^uint(0)>>1))
	scanner.Split(clientEventLine)
	var data []string
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		if line == "" {
			if len(data) == 0 {
				continue
			}
			joined := strings.Join(data, "\n")
			data = nil
			if joined == "" {
				continue
			}
			raw, rpc, _, err := clientResponse([]byte(joined), id, true)
			if rpc != nil {
				return nil, rpc
			}
			if err != nil {
				return nil, err
			}
			if raw != nil {
				return raw, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		if field == "data" {
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcp: read event stream: %w", err)
	}
	return nil, fmt.Errorf("mcp: event stream ended without response")
}
func clientEventLine(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' {
			return i + 1, data[:i], nil
		}
		if b == '\r' {
			if i+1 == len(data) && !atEOF {
				return 0, nil, nil
			}
			advance := i + 1
			if advance < len(data) && data[advance] == '\n' {
				advance++
			}
			return advance, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
func clientTool(raw json.RawMessage) (ToolInfo, error) {
	var t ToolInfo
	var fields map[string]json.RawMessage
	bad := func() (ToolInfo, error) { return ToolInfo{}, fmt.Errorf("mcp: invalid tool description") }
	if !clientObject(raw) || json.Unmarshal(raw, &fields) != nil || !clientString(fields["name"], &t.Name) || !clientObject(fields["inputSchema"]) {
		return bad()
	}
	t.InputSchema = fields["inputSchema"]
	if value, ok := fields["description"]; ok && !clientString(value, &t.Description) {
		return bad()
	}
	if value, ok := fields["outputSchema"]; ok {
		if !clientObject(value) {
			return bad()
		}
		t.OutputSchema = value
	}
	if value, ok := fields["annotations"]; ok {
		if !clientObject(value) {
			return bad()
		}
		var hints map[string]json.RawMessage
		_ = json.Unmarshal(value, &hints)
		for _, hint := range []struct {
			name   string
			target **bool
		}{{"readOnlyHint", &t.Annotations.ReadOnlyHint}, {"destructiveHint", &t.Annotations.DestructiveHint}, {"idempotentHint", &t.Annotations.IdempotentHint}, {"openWorldHint", &t.Annotations.OpenWorldHint}} {
			if v, present := hints[hint.name]; present {
				b, err := strconv.ParseBool(string(v))
				if err != nil || (string(v) != "true" && string(v) != "false") {
					return bad()
				}
				*hint.target = &b
			}
		}
	}
	return t, nil
}
