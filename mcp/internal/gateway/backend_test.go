package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
)

type backendBuffer struct {
	mu     sync.Mutex
	b      bytes.Buffer
	writes chan string
}

func (b *backendBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.b.Write(p)
	if b.writes != nil {
		b.writes <- string(p)
	}
	return n, err
}
func (b *backendBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type backendRequest struct {
	Method string
	Params map[string]json.RawMessage
	Header http.Header
	Path   string
}
type backendFixture struct {
	client   *mcp.Client
	caller   identity.Caller
	logs     *backendBuffer
	requests []backendRequest
	mu       sync.Mutex
	path     string
	server   *httptest.Server
}

func (f *backendFixture) snapshot() []backendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]backendRequest(nil), f.requests...)
}
func backendUnix(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "b.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close() })
	return socket
}
func backendSetup(t *testing.T, budget time.Duration, answer func(http.ResponseWriter, *http.Request, backendRequest)) *backendFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &backendFixture{logs: &backendBuffer{}, caller: identity.Caller{UserID: "user", Email: "user@example.test", RequestID: "trace"}}
	socket := backendUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			ID     json.RawMessage
			Method string
			Params map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Error(err)
			return
		}
		req := backendRequest{msg.Method, msg.Params, r.Header.Clone(), r.URL.Path}
		f.mu.Lock()
		f.requests = append(f.requests, req)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Test-RPC-ID", string(msg.ID))
		answer(w, r, req)
	}))
	f.path = filepath.Join(t.TempDir(), "services.json")
	backendEntries(t, f.path, socket, true, true)
	f.server = httptest.NewServer(Handler(Config{MCP: NewServer("test-version", io.Discard), ServicesPath: f.path, Budget: budget, Stderr: f.logs, Banner: func(page.User) page.Banner { return page.Banner{} }}))
	t.Cleanup(f.server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.server.URL + "/mcp"})
	return f
}
func backendEntries(t *testing.T, path, socket string, enabled, isMCP bool) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"services": []map[string]any{{"name": "alpha", "url": "https://never.invalid", "description": "Alpha", "socket": socket, "enabled": enabled, "mcp": isMCP}}})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func backendReply(w http.ResponseWriter, raw string) {
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, w.Header().Get("Test-RPC-ID"), raw)
}

const backendReadList = `{"tools":[{"name":"read","description":"Read summary.\nFull detail.","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]}`
const backendWriteList = `{"tools":[{"name":"write","description":"Write summary.","inputSchema":{"type":"object"}}]}`

func backendStatic(list, result string) func(http.ResponseWriter, *http.Request, backendRequest) {
	return func(w http.ResponseWriter, _ *http.Request, r backendRequest) {
		if r.Method == "tools/list" {
			backendReply(w, list)
		} else {
			backendReply(w, result)
		}
	}
}
func (f *backendFixture) call(t *testing.T, op, args string) mcp.Result {
	t.Helper()
	result, err := f.client.CallTool(context.Background(), f.caller, op, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func backendJSON(t *testing.T, r mcp.Result) map[string]json.RawMessage {
	t.Helper()
	raw, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err = json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	return object
}
func backendError(t *testing.T, r mcp.Result, text string) {
	t.Helper()
	got := backendJSON(t, r)
	delete(got, "_meta")
	want := backendJSON(t, mcp.ErrorResult(text))
	if !r.IsError() || !reflect.DeepEqual(got, want) {
		t.Fatalf("refusal=%s want=%s", got, want)
	}
}
func backendObject(t *testing.T, r mcp.Result, expected string) {
	t.Helper()
	got := backendJSON(t, r)
	delete(got, "_meta")
	if r.IsError() || len(got) != 2 || backendComparableJSON(t, got["structuredContent"]) != backendComparableJSON(t, []byte(expected)) {
		t.Fatalf("object=%s want=%s", got, expected)
	}
	var content []map[string]json.RawMessage
	if err := json.Unmarshal(got["content"], &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || len(content[0]) != 2 || string(content[0]["type"]) != `"text"` {
		t.Fatalf("content=%v", content)
	}
	var text string
	if err := json.Unmarshal(content[0]["text"], &text); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err != nil {
		t.Fatal(err)
	}
	if text != compact.String() || backendComparableJSON(t, []byte(text)) != backendComparableJSON(t, []byte(expected)) {
		t.Fatalf("text=%s want=%s", text, expected)
	}
}

// Retain gateway object member order while comparing embedded schemas as JSON values.
func backendComparableJSON(t *testing.T, raw []byte) string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func() string
	value = func() string {
		token, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		if delimiter, ok := token.(json.Delim); ok {
			parts := []string{}
			if delimiter == '{' {
				for decoder.More() {
					key, err := decoder.Token()
					if err != nil {
						t.Fatal(err)
					}
					name, ok := key.(string)
					if !ok {
						t.Fatal("object key is not string")
					}
					keyRaw, _ := json.Marshal(name)
					var encoded string
					if name == "inputSchema" || name == "outputSchema" {
						var schema any
						if err = decoder.Decode(&schema); err != nil {
							t.Fatal(err)
						}
						schemaRaw, err := json.Marshal(schema)
						if err != nil {
							t.Fatal(err)
						}
						encoded = string(schemaRaw)
					} else {
						encoded = value()
					}
					parts = append(parts, string(keyRaw)+":"+encoded)
				}
				if _, err = decoder.Token(); err != nil {
					t.Fatal(err)
				}
				return "{" + strings.Join(parts, ",") + "}"
			}
			if delimiter != '[' {
				t.Fatal("unexpected delimiter")
			}
			for decoder.More() {
				parts = append(parts, value())
			}
			if _, err = decoder.Token(); err != nil {
				t.Fatal(err)
			}
			return "[" + strings.Join(parts, ",") + "]"
		}
		encoded, err := json.Marshal(token)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	return value()
}

// R-JMKA-0LPZ R-JTVO-B865 R-JV3K-OZWU
func TestBackendPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, scope, args, want string
		enabled, isMCP          bool
	}{{"unknown", "/mcp", `{"service":"missing"}`, "Unknown service: missing. Call services to see the services you can use.", true, true}, {"disabled", "/mcp", `{"service":"alpha"}`, "Service alpha is unavailable: disabled. Do not retry; call services to see the services you can use.", false, true}, {"absent", "/mcp/alpha", `{"service":"alpha"}`, "Service alpha is unavailable: not installed. Do not retry; call services to see the services you can use.", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := backendSetup(t, 0, backendStatic(backendReadList, `{"content":[]}`))
			entries, err := services.Read(f.path)
			if err != nil {
				t.Fatal(err)
			}
			backendEntries(t, f.path, entries[0].Socket, tc.enabled, tc.isMCP)
			f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.server.URL + tc.scope})
			backendError(t, f.call(t, "describe", tc.args), tc.want)
			if len(f.snapshot()) != 0 || f.logs.String() != "" {
				t.Fatal("preflight contacted backend or logged")
			}
		})
	}
}

// R-JNS6-EDGO R-K7AK-IPBS R-K8IG-WH2H R-K9QD-A8T6 R-JP02-S57D
func TestBackendDescribe(t *testing.T) {
	list := `{"tools":[{"name":"first","description":"Summary.\nDetails.","inputSchema":{"type":"object","properties":{"x":{"type":"string"}}},"outputSchema":{"type":"object"},"annotations":{"readOnlyHint":true,"destructiveHint":true}},{"name":"absent","inputSchema":{}},{"name":"false","description":"False.","inputSchema":{},"annotations":{"readOnlyHint":false,"destructiveHint":false}},{"name":"other","inputSchema":{},"annotations":{"idempotentHint":true}}]}`
	f := backendSetup(t, 0, backendStatic(list, `{"content":[]}`))
	expected := `{"service":"alpha","tools":[{"name":"first","summary":"Summary.","kind":"read"},{"name":"absent","summary":"","kind":"write"},{"name":"false","summary":"False.","kind":"write"},{"name":"other","summary":"","kind":"write"}]}`
	backendObject(t, f.call(t, "describe", `{"service":"alpha"}`), expected)
	backendObject(t, f.call(t, "describe", `{"service":"alpha","tool":null}`), expected)
	backendObject(t, f.call(t, "describe", `{"service":"alpha","tool":"first"}`), `{"service":"alpha","tool":{"name":"first","description":"Summary.\nDetails.","kind":"read","inputSchema":{"properties":{"x":{"type":"string"}},"type":"object"},"outputSchema":{"type":"object"}}}`)
	backendObject(t, f.call(t, "describe", `{"service":"alpha","tool":"absent"}`), `{"service":"alpha","tool":{"name":"absent","description":"","kind":"write","inputSchema":{}}}`)
	for _, r := range f.snapshot() {
		if r.Method != "tools/list" {
			t.Fatal("describe ran tool")
		}
	}
}

// R-RQGJ-3WDU R-KELY-TBRY R-DIZE-6PAE
func TestBackendPagedFirstTool(t *testing.T) {
	f := backendSetup(t, 0, func(w http.ResponseWriter, _ *http.Request, r backendRequest) {
		if _, next := r.Params["cursor"]; next {
			backendReply(w, `{"tools":[{"name":"read","description":"Second.","inputSchema":{},"annotations":{"readOnlyHint":false}},{"name":"tail","inputSchema":{}}]}`)
		} else {
			backendReply(w, `{"tools":[{"name":"read","description":"First.","inputSchema":{},"annotations":{"readOnlyHint":true}}],"nextCursor":"next"}`)
		}
	})
	backendObject(t, f.call(t, "describe", `{"service":"alpha","tool":"read"}`), `{"service":"alpha","tool":{"name":"read","description":"First.","kind":"read","inputSchema":{}}}`)
	backendObject(t, f.call(t, "describe", `{"service":"alpha"}`), `{"service":"alpha","tools":[{"name":"read","summary":"First.","kind":"read"},{"name":"read","summary":"Second.","kind":"write"},{"name":"tail","summary":"","kind":"write"}]}`)
	if len(f.snapshot()) != 4 || f.logs.String() != strings.Repeat("mcp: request trace: alpha tools/list: ok\n", 2) {
		t.Fatalf("requests/logs: %v %q", f.snapshot(), f.logs.String())
	}
}

// R-JWBH-2RNJ R-B0U1-6ZZR R-DHRH-SXJP R-KLXD-3Y84
func TestBackendHopAndArguments(t *testing.T) {
	f := backendSetup(t, 0, backendStatic(backendReadList, `{"content":[]}`))
	for _, args := range []string{`{"service":"alpha","tool":"read"}`, `{"service":"alpha","tool":"read","args":{"extra":[true,2,{"nested":"value"}]}}`} {
		f.call(t, "call", args)
	}
	requests := f.snapshot()
	if len(requests) != 4 {
		t.Fatalf("request count %d", len(requests))
	}
	for _, r := range requests {
		if r.Path != "/mcp" || r.Header.Get("MCP-Protocol-Version") != mcp.ProtocolVersion {
			t.Fatalf("hop=%v", r)
		}
		for h, want := range map[string]string{"X-User-Id": "user", "X-User-Email": "user@example.test", "X-Request-Id": "trace"} {
			if !reflect.DeepEqual(r.Header.Values(h), []string{want}) {
				t.Fatalf("header %s=%v", h, r.Header.Values(h))
			}
		}
		var meta map[string]json.RawMessage
		_ = json.Unmarshal(r.Params["_meta"], &meta)
		var info map[string]string
		_ = json.Unmarshal(meta["io.modelcontextprotocol/clientInfo"], &info)
		if !reflect.DeepEqual(info, map[string]string{"name": ServiceName, "version": "test-version"}) {
			t.Fatalf("client info=%v", info)
		}
	}
	if string(requests[1].Params["name"]) != `"read"` || string(requests[1].Params["arguments"]) != `{}` {
		t.Fatal("default args")
	}
	if string(requests[3].Params["arguments"]) != `{"extra":[true,2,{"nested":"value"}]}` {
		t.Fatal("args changed")
	}
	f.caller.Email = ""
	f.caller.RequestID = ""
	f.call(t, "call", `{"service":"alpha","tool":"read"}`)
	for _, r := range f.snapshot()[4:] {
		if len(r.Header.Values("X-User-Email")) != 0 || len(r.Header.Values("X-Request-Id")) != 0 {
			t.Fatal("invented identity")
		}
	}
}

// R-RROF-HO4J R-KKPG-Q6HF R-K62O-4XL3
func TestBackendKindAndMissing(t *testing.T) {
	for _, tc := range []struct{ op, tool, list, want string }{{"call", "write", backendWriteList, "Tool write of service alpha is a write tool. Use mutate to run it."}, {"mutate", "read", backendReadList, "Tool read of service alpha is a read tool. Use call to run it."}, {"call", "missing", backendReadList, "Service alpha has no tool missing. Call describe with service alpha to see its tools."}} {
		t.Run(tc.op+tc.tool, func(t *testing.T) {
			f := backendSetup(t, 0, backendStatic(tc.list, `{"content":[]}`))
			backendError(t, f.call(t, tc.op, fmt.Sprintf(`{"service":"alpha","tool":%q}`, tc.tool)), tc.want)
			if len(f.snapshot()) != 1 {
				t.Fatal("refused tool ran")
			}
		})
	}
}

// R-JZZ6-82VM
func TestBackendFreshTools(t *testing.T) {
	var mu sync.Mutex
	list := `{"tools":[]}`
	f := backendSetup(t, 0, func(w http.ResponseWriter, _ *http.Request, r backendRequest) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == "tools/list" {
			backendReply(w, list)
		} else {
			backendReply(w, `{"content":[]}`)
		}
	})
	backendObject(t, f.call(t, "describe", `{"service":"alpha"}`), `{"service":"alpha","tools":[]}`)
	mu.Lock()
	list = backendReadList
	mu.Unlock()
	backendObject(t, f.call(t, "describe", `{"service":"alpha","tool":"read"}`), `{"service":"alpha","tool":{"name":"read","description":"Read summary.\nFull detail.","kind":"read","inputSchema":{"type":"object"}}}`)
	backendFreshRun(t, f, "call")
	mu.Lock()
	list = strings.ReplaceAll(backendReadList, `"readOnlyHint":true`, `"readOnlyHint":false`)
	mu.Unlock()
	backendError(t, f.call(t, "call", `{"service":"alpha","tool":"read"}`), "Tool read of service alpha is a write tool. Use mutate to run it.")
	backendFreshRun(t, f, "mutate")
	mu.Lock()
	list = `{"tools":[]}`
	mu.Unlock()
	backendError(t, f.call(t, "mutate", `{"service":"alpha","tool":"read"}`), "Service alpha has no tool read. Call describe with service alpha to see its tools.")
}

func backendFreshRun(t *testing.T, f *backendFixture, operation string) {
	t.Helper()
	before := len(f.snapshot())
	result := f.call(t, operation, `{"service":"alpha","tool":"read"}`)
	if result.IsError() {
		t.Fatalf("fresh tool refused: %s", backendJSON(t, result))
	}
	got := backendJSON(t, result)
	delete(got, "_meta")
	if len(got) != 1 || string(got["content"]) != `[]` {
		t.Fatalf("fresh result=%s", got)
	}
	requests := f.snapshot()[before:]
	if len(requests) != 2 || requests[0].Method != "tools/list" || requests[1].Method != "tools/call" || string(requests[1].Params["name"]) != `"read"` {
		t.Fatalf("fresh tool was not run exactly once: %v", requests)
	}
}

// R-DGJL-F5T0
func TestBackendArgumentRefusals(t *testing.T) {
	f := backendSetup(t, 0, backendStatic(backendReadList, `{"content":[]}`))
	reference := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "test-version"})
	mcp.AddRawTool(reference, mcp.RawTool[describeInput]{Name: "describe", Description: describeDescription, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, describeInput) (mcp.Result, error) {
		t.Error("invalid args accepted")
		return mcp.Result{}, nil
	}})
	for _, op := range []string{"call", "mutate"} {
		mcp.AddRawTool(reference, mcp.RawTool[runInput]{Name: op, Description: callDescription, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, runInput) (mcp.Result, error) {
			t.Error("invalid args accepted")
			return mcp.Result{}, nil
		}})
	}
	server := httptest.NewServer(identity.Require(ServiceName, io.Discard, reference))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	for _, op := range []string{"describe", "call", "mutate"} {
		for _, args := range []string{`{}`, `{"service":12}`, `{"service":"alpha","tool":4}`, `{"service":"alpha","tool":"read","args":null}`, `{"service":"alpha","tool":"read","args":[]}`} {
			if op == "describe" && strings.Contains(args, `"args"`) {
				continue
			}
			got := f.call(t, op, args)
			want, err := client.CallTool(context.Background(), f.caller, op, json.RawMessage(args))
			if err != nil {
				t.Fatal(err)
			}
			a, _ := got.MarshalJSON()
			b, _ := want.MarshalJSON()
			if !bytes.Equal(a, b) {
				t.Fatalf("%s %s: %s != %s", op, args, a, b)
			}
		}
	}
	if len(f.snapshot()) != 0 || f.logs.String() != "" {
		t.Fatal("schema refusal sent backend request/log")
	}
}

// R-RP8M-Q4N5 R-KI9N-YN01 R-K172-LUMB R-K3MV-DE3P R-K4UR-R5UE R-KPL2-99G7
func TestBackendFailures(t *testing.T) {
	for _, stage := range []string{"tools/list", "tools/call"} {
		for _, failure := range []string{"unreachable", "rpc", "http", "broken", "malformed"} {
			t.Run(stage+failure, func(t *testing.T) {
				var f *backendFixture
				f = backendSetup(t, 0, func(w http.ResponseWriter, _ *http.Request, req backendRequest) {
					if req.Method != stage {
						if failure == "unreachable" {
							entries, err := services.Read(f.path)
							if err != nil {
								t.Error(err)
								return
							}
							if err = os.Remove(entries[0].Socket); err != nil {
								t.Error(err)
							}
							w.Header().Set("Connection", "close")
						}
						backendReply(w, backendReadList)
						return
					}
					switch failure {
					case "rpc":
						_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32123,"message":"Backend\nrefusal\runchanged"}}`, w.Header().Get("Test-RPC-ID"))
					case "http":
						w.WriteHeader(503)
						_, _ = io.WriteString(w, "bad")
					case "broken":
						h, ok := w.(http.Hijacker)
						if !ok {
							t.Error("no hijacker")
							return
						}
						conn, _, err := h.Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
					case "malformed":
						backendReply(w, `{"content":[],"content":[]}`)
					case "unreachable":
						backendReply(w, `{"content":[]}`)
					}
				})
				if failure == "unreachable" && stage == "tools/list" {
					backendEntries(t, f.path, filepath.Join(t.TempDir(), "absent.sock"), true, true)
				}
				result := f.call(t, "call", `{"service":"alpha","tool":"read"}`)
				outcome, want := "", ""
				switch failure {
				case "unreachable":
					outcome = "unreachable"
					want = "Service alpha could not be reached. Retry later."
				case "rpc":
					outcome = "rpc error -32123: Backend refusal unchanged"
					want = "Service alpha answered with an error: Backend\nrefusal\runchanged"
				case "http":
					outcome = "bad response (status 503)"
				case "broken":
					outcome = "bad response"
				case "malformed":
					outcome = "bad response"
				}
				if want == "" {
					want = "Service alpha gave an answer the gateway could not read. Retry later."
					if stage == "tools/call" {
						want = "Service alpha gave an answer the gateway could not read; the call may still have completed."
					}
				}
				backendError(t, result, want)
				expected := "mcp: request trace: alpha tools/list: " + outcome + "\n"
				count := 1
				if failure == "unreachable" {
					count = 0
				}
				if stage == "tools/call" {
					count = 2
					if failure == "unreachable" {
						count = 1
					}
					expected = "mcp: request trace: alpha tools/list: ok\nmcp: request trace: alpha tools/call read: " + outcome + "\n"
				}
				if f.logs.String() != expected || len(f.snapshot()) != count {
					t.Fatalf("logs=%q requests=%v", f.logs.String(), f.snapshot())
				}
			})
		}
	}
}

// R-KN59-HPYT R-DIZE-6PAE R-KELY-TBRY
func TestBackendRelay(t *testing.T) {
	for _, toolError := range []bool{false, true} {
		t.Run(fmt.Sprint(toolError), func(t *testing.T) {
			raw := fmt.Sprintf(`{"content":[{"type":"text","text":"answer"},{"type":"image","data":"YWJj","mimeType":"image/png"}],"custom":{"z":1,"a":2},"structuredContent":{"b":2,"a":1},"isError":%t,"_meta":{"custom":"kept","io.modelcontextprotocol/serverInfo":{"name":"backend","version":"backend-version"}}}`, toolError)
			f := backendSetup(t, 0, backendStatic(backendWriteList, raw))
			direct := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://backend/mcp", HTTPClient: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				entries, err := services.Read(f.path)
				if err != nil {
					return nil, err
				}
				return (&net.Dialer{}).DialContext(ctx, "unix", entries[0].Socket)
			}}}})
			backendResult, err := direct.CallTool(context.Background(), f.caller, "write", nil)
			if err != nil {
				t.Fatal(err)
			}
			ref := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "test-version"})
			mcp.AddRawTool(ref, mcp.RawTool[struct{}]{Name: "relay", Description: "Relay answer.", Effect: mcp.Destructive, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) { return backendResult, nil }})
			server := httptest.NewServer(identity.Require(ServiceName, io.Discard, ref))
			defer server.Close()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
			want, err := client.CallTool(context.Background(), f.caller, "relay", nil)
			if err != nil {
				t.Fatal(err)
			}
			got := f.call(t, "mutate", `{"service":"alpha","tool":"write"}`)
			a, _ := got.MarshalJSON()
			b, _ := want.MarshalJSON()
			if !bytes.Equal(a, b) {
				t.Fatalf("relay %s != %s", a, b)
			}
			outcome := "ok"
			if toolError {
				outcome = "tool error"
			}
			if f.logs.String() != "mcp: request trace: alpha tools/list: ok\nmcp: request trace: alpha tools/call write: "+outcome+"\n" {
				t.Fatal(f.logs.String())
			}
		})
	}
}

// R-VOL2-JHIA R-JRFV-JOOR R-K2EY-ZMD0 R-KOD5-VHPI R-DMN3-C0IH R-DNUZ-PS96 R-DK7A-KH13
func TestBackendBudget(t *testing.T) {
	for _, stage := range []string{"tools/list", "tools/call"} {
		t.Run(stage, func(t *testing.T) {
			const budget = 200 * time.Millisecond
			closed := make(chan struct{})
			entered := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			var f *backendFixture
			f = backendSetup(t, budget, func(w http.ResponseWriter, r *http.Request, req backendRequest) {
				if req.Method != stage {
					if f.logs.String() != "" {
						t.Error("premature tools/list log")
					}
					timer := time.NewTimer(budget / 2)
					defer timer.Stop()
					select {
					case <-timer.C:
						backendReply(w, backendReadList)
					case <-r.Context().Done():
					}
					return
				}
				if stage == "tools/call" && f.logs.String() != "mcp: request trace: alpha tools/list: ok\n" {
					t.Error("list log missing before call")
				}
				close(entered)
				select {
				case <-r.Context().Done():
					close(closed)
				case <-release:
				}
			})
			started := time.Now()
			result := f.call(t, "call", `{"service":"alpha","tool":"read"}`)
			elapsed := time.Since(started)
			want := "Service alpha did not answer within 0.2 s. Retry later."
			expected := "mcp: request trace: alpha tools/list: timed out\n"
			if stage == "tools/call" {
				want = "Service alpha did not answer within 0.2 s; the call may still have completed."
				expected = "mcp: request trace: alpha tools/list: ok\nmcp: request trace: alpha tools/call read: timed out\n"
			}
			backendError(t, result, want)
			if elapsed > budget+100*time.Millisecond {
				t.Fatalf("budget not shared/answer delayed: %v", elapsed)
			}
			select {
			case <-entered:
			default:
				t.Fatal("backend never entered")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("backend connection not closed")
			}
			if f.logs.String() != expected {
				t.Fatalf("logs %q", f.logs.String())
			}
			if len(f.snapshot()) != map[string]int{"tools/list": 1, "tools/call": 2}[stage] {
				t.Fatal("further backend requests")
			}
		})
	}
}

// R-KC66-1SAK R-VOL2-JHIA
func TestBackendDoesNotTimeoutEarly(t *testing.T) {
	for _, budget := range []time.Duration{0, -time.Second, 300 * time.Millisecond} {
		t.Run(budget.String(), func(t *testing.T) {
			f := backendSetup(t, budget, func(w http.ResponseWriter, r *http.Request, _ backendRequest) {
				hold := 100 * time.Millisecond
				if budget <= 0 {
					hold = 2 * time.Second
				}
				timer := time.NewTimer(hold)
				defer timer.Stop()
				select {
				case <-timer.C:
					backendReply(w, backendReadList)
				case <-r.Context().Done():
					t.Error("closed before budget")
				}
			})
			result := f.call(t, "describe", `{"service":"alpha"}`)
			if result.IsError() || f.logs.String() != "mcp: request trace: alpha tools/list: ok\n" {
				t.Fatalf("early timeout %v %q", result, f.logs.String())
			}
		})
	}
}

// R-KDE2-FK19 R-DLF6-Y8RS R-RP8M-Q4N5
func TestBackendCancellation(t *testing.T) {
	for _, stage := range []string{"tools/list", "tools/call"} {
		t.Run(stage, func(t *testing.T) {
			entered := make(chan struct{})
			ended := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			f := backendSetup(t, 2*time.Second, func(w http.ResponseWriter, r *http.Request, req backendRequest) {
				if req.Method != stage {
					backendReply(w, backendReadList)
					return
				}
				close(entered)
				select {
				case <-r.Context().Done():
					close(ended)
				case <-release:
				}
			})
			f.logs.writes = make(chan string, 3)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := f.client.CallTool(ctx, f.caller, "call", json.RawMessage(`{"service":"alpha","tool":"read"}`))
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("backend not entered")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled client received answer")
				}
			case <-time.After(time.Second):
				t.Fatal("client cancellation delayed")
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("backend cancellation delayed")
			}
			expected := []string{"mcp: request trace: alpha tools/list: cancelled\n"}
			count := 1
			if stage == "tools/call" {
				expected = []string{"mcp: request trace: alpha tools/list: ok\n", "mcp: request trace: alpha tools/call read: cancelled\n"}
				count = 2
			}
			for _, want := range expected {
				select {
				case got := <-f.logs.writes:
					if got != want {
						t.Fatalf("log=%q want=%q", got, want)
					}
				case <-time.After(time.Second):
					t.Fatal("cancelled request log delayed")
				}
			}
			if len(f.snapshot()) != count || f.logs.String() != strings.Join(expected, "") {
				t.Fatal("extra backend request/log")
			}
		})
	}
}

type backendHeaderTransport struct {
	base   http.RoundTripper
	values map[string][]string
}

func (tr backendHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for name, values := range tr.values {
		req.Header[name] = values
	}
	return tr.base.RoundTrip(req)
}

// R-B0U1-6ZZR
func TestBackendFirstHeaderValues(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			f := backendSetup(t, 0, backendStatic(backendReadList, `{"content":[]}`))
			first := "first"
			if empty {
				first = ""
			}
			f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: backendHeaderTransport{http.DefaultTransport, map[string][]string{"X-User-Id": {"first-user", "second-user"}, "X-User-Email": {first, "ignored-email"}, "X-Request-Id": {first, "ignored-id"}}}}})
			f.call(t, "call", `{"service":"alpha","tool":"read"}`)
			for _, r := range f.snapshot() {
				if !reflect.DeepEqual(r.Header.Values("X-User-Id"), []string{"first-user"}) {
					t.Fatal("duplicate user forwarding")
				}
				for _, name := range []string{"X-User-Email", "X-Request-Id"} {
					want := []string{first}
					if empty {
						want = nil
					}
					if !reflect.DeepEqual(r.Header.Values(name), want) {
						t.Fatalf("%s=%v", name, r.Header.Values(name))
					}
				}
			}
		})
	}
}

// R-KELY-TBRY
func TestBackendSingleLineVariables(t *testing.T) {
	name := "alpha\nservice\rname"
	tool := "tool\nwith\rbreaks"
	list := fmt.Sprintf(`{"tools":[{"name":%q,"inputSchema":{},"annotations":{"readOnlyHint":true}}]}`, tool)
	f := backendSetup(t, 0, backendStatic(list, `{"content":[]}`))
	entries, err := services.Read(f.path)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"services": []map[string]any{{"name": name, "url": "", "description": "", "socket": entries[0].Socket, "enabled": true, "mcp": true}}})
	if err = os.WriteFile(f.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.caller.RequestID = ""
	f.call(t, "call", fmt.Sprintf(`{"service":%q,"tool":%q}`, name, tool))
	want := "mcp: request -: alpha service name tools/list: ok\nmcp: request -: alpha service name tools/call tool with breaks: ok\n"
	if f.logs.String() != want {
		t.Fatalf("logs=%q", f.logs.String())
	}
}
