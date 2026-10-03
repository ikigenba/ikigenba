package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type backendBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *backendBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.b.Write(p)
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
	writer   *telemetry.Writer
	capture  *telemetry.Capture
	finished chan struct{}
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
	f.writer, f.capture = backendTelemetry(t, nil, f.logs, nil)
	f.finished = make(chan struct{}, 10)
	handler := Handler(Config{MCP: NewServer("test-version", f.writer), ServicesPath: f.path, Budget: budget, Telemetry: f.writer, Banner: func(page.User) page.Banner { return page.Banner{} }})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
		select {
		case f.finished <- struct{}{}:
		default:
		}
	}))
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

// R-JMKA-0LPZ R-2K95-ZCLW R-2LH2-D4CL
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

// R-RQGJ-3WDU
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
	if len(f.snapshot()) != 4 || len(f.siblings(t)) != 4 || f.logs.String() != "" {
		t.Fatalf("requests/logs: %v %q", f.snapshot(), f.logs.String())
	}
}

// R-JWBH-2RNJ R-2MOY-QW3A R-DHRH-SXJP R-KLXD-3Y84
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
		if len(r.Header.Values("X-User-Email")) != 0 || len(r.Header.Values("X-Request-Id")) != 1 || r.Header.Get("X-Request-Id") == "" {
			t.Fatal("invented identity")
		}
	}
}

// R-RFLE-24TT R-KKPG-Q6HF R-K62O-4XL3
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

// R-2J19-LKV7
func TestBackendArgumentRefusals(t *testing.T) {
	f := backendSetup(t, 0, backendStatic(backendReadList, `{"content":[]}`))
	reference := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "test-version", Telemetry: f.writer})
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
	server := httptest.NewServer(identity.Require(reference))
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

// R-BN10-HXNQ R-KI9N-YN01 R-K172-LUMB R-K3MV-DE3P R-K4UR-R5UE R-KPL2-99G7
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
				want := ""
				switch failure {
				case "unreachable":
					want = "Service alpha could not be reached. Retry later."
				case "rpc":
					want = "Service alpha answered with an error: Backend\nrefusal\runchanged"
				case "http":
				case "broken":
				case "malformed":
				}
				if want == "" {
					want = "Service alpha gave an answer the gateway could not read. Retry later."
					if stage == "tools/call" {
						want = "Service alpha gave an answer the gateway could not read; the call may still have completed."
					}
				}
				backendError(t, result, want)
				count := 1
				if failure == "unreachable" {
					count = 0
				}
				if stage == "tools/call" {
					count++
					if failure != "unreachable" {
						count = 2
					}
				}
				statuses := []int64{200}
				status := int64(200)
				if failure == "unreachable" || failure == "broken" {
					status = 0
				}
				if failure == "http" {
					status = 503
				}
				if stage == "tools/call" {
					statuses = append(statuses, status)
				} else {
					statuses[0] = status
				}
				f.assertStatuses(t, statuses...)
				if len(f.snapshot()) != count {
					t.Fatalf("requests=%v", f.snapshot())
				}

			})
		}
	}
}

// R-KN59-HPYT R-O3U0-KWFR R-JQW7-8JSX R-JS43-MBJM
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
			refWriter, _ := backendTelemetry(t, nil, io.Discard, nil)
			ref := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "test-version", Telemetry: refWriter})
			mcp.AddRawTool(ref, mcp.RawTool[struct{}]{Name: "relay", Description: "Relay answer.", Effect: mcp.Destructive, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) { return backendResult, nil }})
			server := httptest.NewServer(identity.Require(ref))
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
			f.assertStatuses(t, 200, 200)
			events := f.capture.Events()
			if len(events) != 5 || events[0].Name != "request.started" || events[1].Name != "sibling.called" || events[2].Name != "sibling.called" || events[3].Name != "tool.called" || events[4].Name != "request.finished" {
				t.Fatalf("trail=%v", events)
			}
			outcome := "ok"
			if toolError {
				outcome = "error"
			}
			attrs := events[3].Attrs
			if len(attrs) != 4 || attrs["tool"] != "mutate" || attrs["kind"] != "destructive" || attrs["outcome"] != outcome || attrs["duration_us"] == nil {
				t.Fatalf("tool=%v", events[3])
			}
			for _, e := range events {
				if e.RequestID != "trace" || e.User != "user" {
					t.Fatalf("correlation=%v", e)
				}
			}
		})
	}
}

// R-VOL2-JHIA R-JRFV-JOOR R-K2EY-ZMD0 R-KOD5-VHPI R-DMN3-C0IH R-DNUZ-PS96 R-2NWV-4NTZ
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
				if stage == "tools/call" && len(f.siblings(t)) != 1 {
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
			if stage == "tools/call" {
				want = "Service alpha did not answer within 0.2 s; the call may still have completed."
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
			if stage == "tools/list" {
				f.assertStatuses(t, 0)
			} else {
				f.assertStatuses(t, 200, 0)
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
			if result.IsError() || f.logs.String() != "" {
				t.Fatalf("early timeout %v %q", result, f.logs.String())
			}
		})
	}
}

// R-KDE2-FK19 R-O51W-YO6G R-BN10-HXNQ
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
			answers := make(chan []byte, 1)
			handler := f.server.Config.Handler
			f.server.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				out := &backendAnswerWriter{ResponseWriter: w}
				handler.ServeHTTP(out, r)
				answers <- out.body.Bytes()
			}))
			defer server.Close()
			f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
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
			select {
			case <-f.finished:
			case <-time.After(time.Second):
				t.Fatal("gateway cancellation delayed")
			}
			var answer struct {
				Result mcp.Result
				Error  json.RawMessage
			}
			select {
			case raw := <-answers:
				if err := json.Unmarshal(raw, &answer); err != nil || answer.Error != nil || !answer.Result.IsError() {
					t.Fatalf("cancelled answer=%s error=%v", raw, err)
				}
			case <-time.After(time.Second):
				t.Fatal("gateway did not produce its cancelled result")
			}
			count := 1
			if stage == "tools/call" {
				count = 2
				f.assertStatuses(t, 200, 0)
			} else {
				f.assertStatuses(t, 0)
			}
			if len(f.snapshot()) != count {
				t.Fatal("extra backend request")
			}
			events := f.capture.Events()
			var tool, finished bool
			for _, e := range events {
				if e.Name == "tool.called" {
					tool = e.Attrs["outcome"] == "error"
				}
				if e.Name == "request.finished" {
					finished = e.Attrs["status"] == int64(200)
				}
			}
			if !tool || !finished {
				t.Fatalf("cancelled trail=%v", events)
			}

		})
	}
}

type backendAnswerWriter struct {
	http.ResponseWriter
	body bytes.Buffer
}

func (w *backendAnswerWriter) Write(p []byte) (int, error) {
	_, _ = w.body.Write(p)
	return w.ResponseWriter.Write(p)
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

// R-2MOY-QW3A
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
						if name == "X-Request-Id" {
							if len(r.Header.Values(name)) != 1 || r.Header.Get(name) == "" {
								t.Fatal("request id not minted")
							}
							continue
						}
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

func (f *backendFixture) siblings(t *testing.T) []telemetry.Event {
	t.Helper()
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var events []telemetry.Event
	for _, e := range f.capture.Events() {
		if e.Name == "sibling.called" {
			events = append(events, e)
		}
	}
	return events
}
func (f *backendFixture) assertStatuses(t *testing.T, statuses ...int64) {
	t.Helper()
	events := f.siblings(t)
	if len(events) != len(statuses) {
		t.Fatalf("siblings=%v want statuses=%v", events, statuses)
	}
	nextStatus, stop := iter.Pull(slices.Values(statuses))
	defer stop()
	for _, e := range events {
		status, ok := nextStatus()
		if !ok {
			t.Fatal("unexpected sibling event")
			return
		}
		if e.Attrs["status"] != status || e.Attrs["target"] != "alpha" || e.Attrs["method"] != "POST" || e.Attrs["path"] != "/mcp" || len(e.Attrs) != 5 || e.RequestID != "trace" || e.User != "user" {
			t.Fatalf("sibling=%v", e)
		}
	}

	if f.logs.String() != "" {
		t.Fatalf("backend wrote stderr: %q", f.logs.String())
	}
}

// R-BPGT-9H54 R-K1A0-LYHZ R-3WBT-7JY3 R-BN10-HXNQ R-2NWV-4NTZ
func TestBackendResponseStatuses(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308, 500, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			reached := make(chan struct{}, 1)
			destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached <- struct{}{} }))
			defer destination.Close()
			f := backendSetup(t, 0, func(w http.ResponseWriter, _ *http.Request, _ backendRequest) {
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(status)
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32123,"message":"refused"}}`, w.Header().Get("Test-RPC-ID"))
			})
			want := "Service alpha gave an answer the gateway could not read. Retry later."
			if status == 500 || status == 502 {
				want = "Service alpha answered with an error: refused"
			}
			backendError(t, f.call(t, "describe", `{"service":"alpha"}`), want)
			f.assertStatuses(t, int64(status))
			select {
			case <-reached:
				t.Fatal("redirect followed")
			default:
			}
			if len(f.snapshot()) != 1 {
				t.Fatal("extra backend request")
			}
		})
	}
	for _, head := range []string{"", "HTTP/1.1 200 OK\r\nPartial: unfinished", "HTTP/1.1 100 Continue\r\n\r\n"} {
		t.Run(fmt.Sprintf("incomplete-%q", head), func(t *testing.T) {
			f := backendSetup(t, 0, func(w http.ResponseWriter, _ *http.Request, _ backendRequest) {
				conn, buf, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				_, _ = buf.WriteString(head)
				_ = buf.Flush()
			})
			backendError(t, f.call(t, "describe", `{"service":"alpha"}`), "Service alpha gave an answer the gateway could not read. Retry later.")
			f.assertStatuses(t, 0)
		})
	}
}

// R-2QCN-W7BD R-2NWV-4NTZ
func TestBackendDuration(t *testing.T) {
	for _, delta := range []time.Duration{123456 * time.Microsecond, -time.Second} {
		t.Run(delta.String(), func(t *testing.T) {
			var clockMu sync.Mutex
			clock := time.Unix(1, 0)
			now := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock }
			f := backendSetup(t, 0, backendStatic(backendReadList, `{"content":[]}`))
			f.server.Close()
			writer, capture := backendTelemetry(t, nil, f.logs, now)
			entries, err := services.Read(f.path)
			if err != nil {
				t.Fatal(err)
			}
			socket := backendUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var msg struct{ ID json.RawMessage }
				if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
					t.Error(err)
					return
				}
				clockMu.Lock()
				clock = clock.Add(delta)
				clockMu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Test-RPC-ID", string(msg.ID))
				backendReply(w, backendReadList)
			}))
			backendEntries(t, f.path, socket, entries[0].Enabled, true)
			f.writer, f.capture = writer, capture
			server := httptest.NewServer(Handler(Config{MCP: NewServer("test-version", writer), Telemetry: writer, ServicesPath: f.path, Banner: func(page.User) page.Banner { return page.Banner{} }}))
			defer server.Close()
			f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
			f.call(t, "describe", `{"service":"alpha"}`)
			f.assertStatuses(t, 200)
			want := int64(delta / time.Microsecond)
			if want < 0 {
				want = 0
			}
			if got := f.siblings(t)[0].Attrs["duration_us"]; got != want {
				t.Fatalf("duration=%v want=%d", got, want)
			}
		})
	}
}

func backendTelemetry(t testing.TB, sink telemetry.Sink, stderr io.Writer, now func() time.Time) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := &telemetry.Capture{}
	if sink == nil {
		sink = capture
	}
	writer := telemetry.New(telemetry.Config{Service: ServiceName, Version: "test-version", Sink: sink, Stderr: stderr, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0xab}, 4096))})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		writer.Shutdown(ctx, "test ended")
	})
	return writer, capture
}

// R-O3U0-KWFR R-JQW7-8JSX R-JS43-MBJM
func TestBackendArgumentErrorTrail(t *testing.T) {
	f := backendSetup(t, 0, backendStatic(backendReadList, `{"content":[{"type":"text","text":"invalid tool arguments"}],"isError":true}`))
	result := f.call(t, "call", `{"service":"alpha","tool":"read","args":{"wrong":true}}`)
	if !result.IsError() {
		t.Fatal("backend argument error lost")
	}
	f.assertStatuses(t, 200, 200)
	events := f.capture.Events()
	if len(events) != 5 {
		t.Fatalf("events=%v", events)
	}
	if events[0].Name != "request.started" || events[1].Name != "sibling.called" || events[2].Name != "sibling.called" || events[3].Name != "tool.called" || events[4].Name != "request.finished" {
		t.Fatalf("event order=%v", events)
	}
	attrs := events[3].Attrs
	if len(attrs) != 4 || attrs["tool"] != "call" || attrs["kind"] != "read" || attrs["outcome"] != "error" || attrs["duration_us"] == nil {
		t.Fatalf("tool event=%v", events[3])
	}
	for _, e := range events {
		if e.RequestID != "trace" || e.User != "user" {
			t.Fatalf("correlation=%v", e)
		}
	}
}

func TestBackendSuccessfulToolTrail(t *testing.T) {
	// R-O3U0-KWFR R-JQW7-8JSX R-JS43-MBJM
	for _, tc := range []struct{ tool, args, result string }{
		{"describe", `{"service":"alpha"}`, `{"content":[]}`},
		{"call", `{"service":"alpha","tool":"read"}`, `{"content":[]}`},
		{"call", `{"service":"alpha","tool":"read"}`, `{"content":[],"isError":false}`},
	} {
		t.Run(tc.tool+tc.result, func(t *testing.T) {
			f := backendSetup(t, 0, backendStatic(backendReadList, tc.result))
			if result := f.call(t, tc.tool, tc.args); result.IsError() {
				t.Fatal(result)
			}
			handlerCount := 1
			if tc.tool == "call" {
				handlerCount = 2
			}
			handlerCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := f.writer.Flush(handlerCtx); err != nil {
				t.Fatal(err)
			}
			events := f.capture.Events()
			if len(events) != handlerCount+3 || events[0].Name != "request.started" || events[len(events)-1].Name != "request.finished" {
				t.Fatal(events)
			}
			for _, event := range events[1 : handlerCount+1] {
				if event.Name != "sibling.called" {
					t.Fatal(events)
				}
			}
			event := events[handlerCount+1]
			if event.Name != "tool.called" || len(event.Attrs) != 4 || event.Attrs["tool"] != tc.tool || event.Attrs["kind"] != "read" || event.Attrs["outcome"] != "ok" {
				t.Fatal(event)
			}
			if _, ok := event.Attrs["duration_us"]; !ok {
				t.Fatal(event)
			}
		})
	}
}
