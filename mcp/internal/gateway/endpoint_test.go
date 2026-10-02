package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func endpointConfig(t *testing.T, path string) gateway.Config {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _ := handlerTelemetry(t, nil)
	return gateway.Config{Banner: func(_ page.User) page.Banner { return page.Banner{} }, MCP: gateway.NewServer("test-version", writer), ServicesPath: path, Budget: time.Second, Telemetry: writer}
}
func endpointClient(t *testing.T, h http.Handler, path string) *mcp.Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: s.URL + path, HTTPClient: s.Client(), Name: "test", Version: "test"})
}

var endpointCaller = identity.Caller{UserID: "person", Email: "person@example.test", RequestID: "req"}

func endpointCall(t *testing.T, c *mcp.Client, args string) []byte {
	t.Helper()
	var arguments json.RawMessage
	if args != "" {
		arguments = json.RawMessage(args)
	}
	result, err := c.CallTool(context.Background(), endpointCaller, "services", arguments)
	if err != nil {
		t.Fatal(err)
	}
	b, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func endpointFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "services.json")
	endpointWrite(t, p, body)
	return p
}
func endpointWrite(t *testing.T, path, body string) {
	t.Helper()
	if strings.HasPrefix(body, "[") {
		var entries []map[string]any
		if err := json.Unmarshal([]byte(body), &entries); err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			for _, key := range []string{"url", "socket", "description"} {
				if _, ok := entry[key]; !ok {
					entry[key] = ""
				}
			}
		}
		document, err := json.Marshal(map[string]any{"services": entries})
		if err != nil {
			t.Fatal(err)
		}
		body = string(document)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func endpointRaw(h http.Handler, path, method, body, revision string, headers http.Header) *httptest.ResponseRecorder {
	var wire map[string]any
	if revision == mcp.ProtocolVersion && json.Unmarshal([]byte(body), &wire) == nil && wire != nil {
		params, ok := wire["params"].(map[string]any)
		if !ok {
			params = map[string]any{}
			wire["params"] = params
		}
		params["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": mcp.ProtocolVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
		data, _ := json.Marshal(wire)
		body = string(data)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header = headers.Clone()
	r.Header.Set("Content-Type", "application/json")
	if revision != "" {
		r.Header.Set("MCP-Protocol-Version", revision)
		if revision == mcp.ProtocolVersion && wire != nil {
			rpcMethod, _ := wire["method"].(string)
			r.Header.Set("Mcp-Method", rpcMethod)
			params, _ := wire["params"].(map[string]any)
			if name, ok := params["name"].(string); ok {
				r.Header.Set("Mcp-Name", name)
			}
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func endpointIdentity() http.Header { return http.Header{"X-User-Id": []string{"person"}} }
func endpointPart(t *testing.T, b []byte, key string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m[key]
}

func endpointOrderedEqual(t *testing.T, actual, expected []byte) {
	t.Helper()
	tokens := func(raw []byte) []any {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		out := []any{}
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				return out
			}
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, token)
		}
	}
	if !reflect.DeepEqual(tokens(actual), tokens(expected)) {
		t.Fatalf("JSON value or member order differs: got %s want %s", actual, expected)
	}
}

func TestEndpointDeclarations(t *testing.T) {
	// R-VH9O-8V24 R-VIHK-MMST R-1H8D-QOVO R-VM59-RY0W
	const name = gateway.ServiceName
	const budget time.Duration = gateway.DefaultBudget
	if name != "mcp" || budget != 50*time.Second {
		t.Fatal(name, budget)
	}
	cfg := endpointConfig(t, "")
	h := gateway.Handler(cfg)
	if got := endpointCall(t, endpointClient(t, h, "/mcp"), ""); !bytes.Contains(got, []byte(`"services":[]`)) {
		t.Fatal(string(got))
	}
}

func TestEndpointCatalogue(t *testing.T) {
	// R-VPSY-X98Z R-VR0V-B0ZO R-VTGO-2KH2 R-VUOK-GC7R R-3SC8-IDER R-3CHJ-JCRQ
	path := endpointFile(t, `[{"name":"zeta","mcp":true,"enabled":false,"description":"Z"},{"name":"alpha","mcp":true,"enabled":true,"description":"A"},{"name":"alpha","mcp":false,"enabled":false},{"name":"web","mcp":false,"enabled":true},{"name":"mcp","mcp":true,"enabled":true}]`)
	cfg := endpointConfig(t, path)
	h := gateway.Handler(cfg)
	referenceServer := mcp.NewServer(mcp.ServerConfig{Name: "reference", Version: "test", Telemetry: cfg.Telemetry})
	mcp.AddTool(referenceServer, mcp.Tool[struct{}, struct{}]{Name: "services", Description: "List fixture data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
	reference := endpointClient(t, identity.Require(referenceServer), "/mcp")
	var allowedMembers map[string]json.RawMessage
	if err := json.Unmarshal(endpointCall(t, reference, ""), &allowedMembers); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{{"/mcp", `{"services":[{"name":"alpha","description":"A","available":true},{"name":"zeta","description":"Z","available":false,"reason":"disabled"}]}`}, {"/mcp/zeta,missing,mcp,web,alpha", `{"services":[{"name":"alpha","description":"A","available":true},{"name":"mcp","description":"","available":false,"reason":"not installed"},{"name":"missing","description":"","available":false,"reason":"not installed"},{"name":"web","description":"","available":false,"reason":"not installed"},{"name":"zeta","description":"Z","available":false,"reason":"disabled"}]}`}} {
		c := endpointClient(t, h, tc.path)
		for _, args := range []string{"", `{}`} {
			b := endpointCall(t, c, args)

			var result map[string]json.RawMessage
			if err := json.Unmarshal(b, &result); err != nil {
				t.Fatal(err)
			}
			if len(result) != len(allowedMembers) {
				t.Fatalf("unexpected result members: %s", b)
			}
			for key := range result {
				if _, ok := allowedMembers[key]; !ok {
					t.Fatalf("unexpected result member %s", key)
				}
			}
			endpointOrderedEqual(t, result["structuredContent"], []byte(tc.want))
			var content []map[string]json.RawMessage
			if err := json.Unmarshal(result["content"], &content); err != nil {
				t.Fatal(err)
			}
			if len(content) != 1 || len(content[0]) != 2 {
				t.Fatalf("unexpected content shape: %s", result["content"])
			}
			var kind, text string
			if err := json.Unmarshal(content[0]["type"], &kind); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(content[0]["text"], &text); err != nil {
				t.Fatal(err)
			}
			if kind != "text" {
				t.Fatal(kind)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(text)); err != nil {
				t.Fatal(err)
			}
			if compact.String() != text {
				t.Fatalf("text has whitespace outside strings: %q", text)
			}
			endpointOrderedEqual(t, []byte(text), []byte(tc.want))
		}
	}
	endpointWrite(t, path, `[{"name":"new","mcp":true,"enabled":true,"description":"changed"}]`)
	c := endpointClient(t, h, "/mcp")
	endpointOrderedEqual(t, endpointPart(t, endpointCall(t, c, ""), "structuredContent"), []byte(`{"services":[{"name":"new","description":"changed","available":true}]}`))
	for _, body := range []string{"broken", `{}`, `[]`} {
		endpointWrite(t, path, body)
		endpointOrderedEqual(t, endpointPart(t, endpointCall(t, c, ""), "structuredContent"), []byte(`{"services":[]}`))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	endpointOrderedEqual(t, endpointPart(t, endpointCall(t, c, ""), "structuredContent"), []byte(`{"services":[]}`))
}

func TestEndpointScopes(t *testing.T) {
	// R-VS8R-OSQD R-ZBX8-V01U
	h := gateway.Handler(endpointConfig(t, ""))
	for _, scope := range []string{"", "a,,b", ",dummy", "dummy,", "dummy,dummy", "du_mmy", "dummy/", "-dummy", "dummy-", strings.Repeat("x", 64), "/dummy", "./dummy", "a/../dummy", "é"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			w := endpointRaw(h, "/mcp/"+scope, method, `{}`, mcp.ProtocolVersion, endpointIdentity())
			if w.Code != 404 || w.Body.String() != "not found\n" {
				t.Fatalf("%s %q %d %q", method, scope, w.Code, w.Body.String())
			}
		}
	}
	for _, scope := range []string{"Dummy", "a-1,Z9", strings.Repeat("a", 63)} {
		c := endpointClient(t, h, "/mcp/"+scope)
		if _, err := c.ListTools(context.Background(), endpointCaller); err != nil {
			t.Fatal(scope, err)
		}
	}
}

func TestEndpointInstructions(t *testing.T) {
	// R-3R4C-4LO2 R-2BPV-AYF1
	path := endpointFile(t, `[{"name":"zeta","mcp":true,"enabled":false},{"name":"alpha","mcp":true,"enabled":true}]`)
	h := gateway.Handler(endpointConfig(t, path))
	for _, tc := range []struct{ path, names string }{{"/mcp", "alpha, zeta"}, {"/mcp/Z,unknown", "Z, unknown"}} {
		for _, method := range []string{"initialize", "server/discover"} {
			params := `{}`
			revision := mcp.ProtocolVersion
			if method == "initialize" {
				params = `{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"test"}}`
				revision = "2025-11-25"
			}
			w := endpointRaw(h, tc.path, "POST", `{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`, revision, endpointIdentity())
			result := endpointPart(t, w.Body.Bytes(), "result")
			var text string
			if err := json.Unmarshal(endpointPart(t, result, "instructions"), &text); err != nil {
				t.Fatal(w.Body.String(), err)
			}
			want := "This server reaches these services: " + tc.names + ".\nCall services to see which are available, and describe before call or mutate."
			if text != want {
				t.Fatal(text)
			}
			info := endpointPart(t, result, "serverInfo")
			if method == "server/discover" {
				info = endpointPart(t, endpointPart(t, result, "_meta"), "io.modelcontextprotocol/serverInfo")
			}
			if string(info) != `{"name":"mcp","version":"test-version"}` {
				t.Fatal(string(info))
			}
		}
	}
	endpointWrite(t, path, `[]`)
	w := endpointRaw(h, "/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`, mcp.ProtocolVersion, endpointIdentity())
	if !bytes.Contains(w.Body.Bytes(), []byte(`This server reaches no services.\nCall services`)) {
		t.Fatal(w.Body.String())
	}
	b := endpointCall(t, endpointClient(t, h, "/mcp"), "")
	if info := endpointPart(t, endpointPart(t, b, "_meta"), "io.modelcontextprotocol/serverInfo"); string(info) != `{"name":"mcp","version":"test-version"}` {
		t.Fatal(string(info))
	}
}

func TestEndpointServicesArgumentRefusal(t *testing.T) {
	// R-TPYH-KBRP
	h := gateway.Handler(endpointConfig(t, ""))
	b := endpointCall(t, endpointClient(t, h, "/mcp"), `{"bogus":true}`)
	expected, _ := mcp.ErrorResult("invalid arguments:\nbogus: unknown field").MarshalJSON()
	var actual, want map[string]json.RawMessage
	if err := json.Unmarshal(b, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expected, &want); err != nil {
		t.Fatal(err)
	}
	delete(actual, "_meta")
	a, _ := json.Marshal(actual)
	e, _ := json.Marshal(want)
	if !bytes.Equal(a, e) {
		t.Fatal(string(b))
	}
}

func TestEndpointNoBackend(t *testing.T) {
	// R-R545-LF6D
	dir, err := os.MkdirTemp("", "gateway-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	socket := filepath.Join(dir, "s")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
	}()
	body, _ := json.Marshal([]map[string]any{{"name": "dummy", "mcp": true, "enabled": true, "socket": socket}, {"name": "disabled", "mcp": true, "enabled": false, "socket": socket}, {"name": "web", "mcp": false, "enabled": true, "socket": socket}, {"name": "off", "mcp": false, "enabled": false, "socket": socket}})
	cfg := endpointConfig(t, endpointFile(t, string(body)))
	h := gateway.Handler(cfg)
	server := httptest.NewServer(h)
	c := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp/dummy", HTTPClient: server.Client(), Name: "test", Version: "test"})
	t.Cleanup(server.Close)
	if _, err := c.ListTools(context.Background(), endpointCaller); err != nil {
		t.Fatal(err)
	}
	endpointCall(t, c, "")
	endpointCall(t, c, `{"bogus":true}`)
	for _, tc := range []struct{ path, method, body string }{{"/mcp", "GET", ""}, {"/mcp/a,,b", "POST", `{}`}, {"/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`}, {"/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unknown","arguments":{}}}`}} {
		endpointRaw(h, tc.path, tc.method, tc.body, mcp.ProtocolVersion, endpointIdentity())
	}
	endpointRaw(h, "/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"thing"}}}`, mcp.ProtocolVersion, http.Header{})
	server.Close()
	if err := ln.SetDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	connection, err := ln.Accept()
	if err == nil {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("unexpected backend connection")
	}
	var networkError net.Error
	if !errors.As(err, &networkError) || !networkError.Timeout() {
		t.Fatal(err)
	}
}

func TestEndpointTools(t *testing.T) {
	// R-3DPF-X4IF R-3EXC-AW94 R-ZAPC-H8B5 R-3HD5-2FQI R-3IL1-G7H7 R-3JSX-TZ7W
	h := gateway.Handler(endpointConfig(t, endpointFile(t, `[{"name":"dummy","enabled":true,"mcp":true}]`)))
	expected := []string{
		`{"name":"services","description":"List the services this connection reaches, and whether each is available.\n\nAn unavailable service says why: disabled or not installed. Call describe to see a service's tools.","inputSchema":{"type":"object","additionalProperties":false},"outputSchema":{"type":"object","properties":{"services":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string","description":"The service's name."},"description":{"type":"string","description":"What the service is for, from the services file; empty when it is not installed."},"available":{"type":"boolean","description":"Whether the service can be used now."},"reason":{"type":"string","description":"Why the service is unavailable: disabled or not installed. Present only when available is false."}},"required":["name","description","available"],"additionalProperties":false},"description":"Every service this connection reaches, in name order."}},"required":["services"],"additionalProperties":false},"annotations":{"readOnlyHint":true,"destructiveHint":false,"openWorldHint":false}}`,
		`{"name":"describe","description":"Show a service's tools, or one tool's full description and schemas.\n\nWithout tool, lists each tool of the service with its one-line summary and its kind: a read tool runs with call, a write tool with mutate. With tool, gives that tool's full description, its input schema, its output schema when it has one, and its kind. Call describe before call or mutate.","inputSchema":{"type":"object","properties":{"service":{"type":"string","description":"The service's name, as services lists it."},"tool":{"type":"string","description":"A tool's name, as describe lists it. Leave it out to list the service's tools."}},"required":["service"],"additionalProperties":false},"annotations":{"readOnlyHint":true,"destructiveHint":false,"openWorldHint":false}}`,
		`{"name":"call","description":"Run a read tool of a service and return its result.\n\nName the service and the tool as describe shows them, and pass the tool's arguments in args, an object matching its input schema ({} when left out). Only a tool of kind read runs here; a write tool runs with mutate.","inputSchema":{"type":"object","properties":{"service":{"type":"string","description":"The service's name, as services lists it."},"tool":{"type":"string","description":"The tool's name, as describe lists it."},"args":{"type":"object","description":"The tool's arguments, matching its input schema. Leave it out for {}."}},"required":["service","tool"],"additionalProperties":false},"annotations":{"readOnlyHint":true,"destructiveHint":false,"openWorldHint":false}}`,
		`{"name":"mutate","description":"Run a write tool of a service and return its result.\n\nName the service and the tool as describe shows them, and pass the tool's arguments in args, an object matching its input schema ({} when left out). Only a tool of kind write runs here; a read tool runs with call. A write tool may change or remove data.","inputSchema":{"type":"object","properties":{"service":{"type":"string","description":"The service's name, as services lists it."},"tool":{"type":"string","description":"The tool's name, as describe lists it."},"args":{"type":"object","description":"The tool's arguments, matching its input schema. Leave it out for {}."}},"required":["service","tool"],"additionalProperties":false},"annotations":{"readOnlyHint":false,"destructiveHint":true,"openWorldHint":false}}`,
	}
	for _, path := range []string{"/mcp", "/mcp/dummy", "/mcp/missing"} {
		c := endpointClient(t, h, path)
		tools, err := c.ListTools(context.Background(), endpointCaller)
		if err != nil {
			t.Fatal(err)
		}
		if len(tools) != 4 {
			t.Fatal(tools)
		}
		for i, name := range []string{"services", "describe", "call", "mutate"} {
			if tools[i].Name != name {
				t.Fatal(tools)
			}
		}
		w := endpointRaw(h, path, "POST", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, "2025-11-25", endpointIdentity())
		var raw []json.RawMessage
		if err := json.Unmarshal(endpointPart(t, endpointPart(t, w.Body.Bytes(), "result"), "tools"), &raw); err != nil {
			t.Fatal(w.Body.String(), err)
		}
		if len(raw) != len(expected) {
			t.Fatal(w.Body.String())
		}
		for i, b := range raw {
			endpointOrderedEqual(t, b, []byte(expected[i]))
		}
	}
}
