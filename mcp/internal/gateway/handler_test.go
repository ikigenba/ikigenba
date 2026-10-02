package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func endpointSame(t *testing.T, a, b http.Handler, path, method, body, revision string, headers http.Header) {
	t.Helper()
	x := endpointRaw(a, path, method, body, revision, headers)
	y := endpointRaw(b, path, method, body, revision, headers)
	if x.Code != y.Code || x.Header().Get("Content-Type") != y.Header().Get("Content-Type") || x.Body.String() != y.Body.String() {
		t.Fatalf("%s %s differs: %d %v %s / %d %v %s", method, path, x.Code, x.Header(), x.Body.String(), y.Code, y.Header(), y.Body.String())
	}
	endpointHeadersEqual(t, x.Header(), y.Header())
}
func endpointHeadersEqual(t *testing.T, ax, ay http.Header) {
	t.Helper()
	if len(ax) != len(ay) {
		t.Fatal(ax, ay)
	}
	for k, v := range ax {
		if strings.Join(v, "\x00") != strings.Join(ay[k], "\x00") {
			t.Fatal(ax, ay)
		}
	}
}
func TestEndpointAppkitEquivalence(t *testing.T) {
	// R-TQ6Z-2X2K R-29A2-JEXN R-2AHY-X6OC
	cfg := endpointConfig(t, "")
	h := gateway.Handler(cfg)
	reference := identity.Require(cfg.MCP)
	for _, path := range []string{"/mcp", "/mcp/dummy,notes"} {
		for _, tc := range []struct{ method, body, revision string }{{"GET", "", ""}, {"DELETE", "", ""}, {"POST", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, "2025-11-25"}, {"POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_widgets","arguments":{}}}`, mcp.ProtocolVersion}, {"POST", `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`, "2025-11-25"}, {"POST", "invalid", mcp.ProtocolVersion}} {
			endpointSame(t, h, reference, path, tc.method, tc.body, tc.revision, endpointIdentity())
		}
	}
	for _, name := range []string{"services", "describe", "call", "mutate"} {
		args := `{}`
		if name == "describe" {
			args = `{"service":"dummy"}`
		}
		if name == "call" || name == "mutate" {
			args = `{"service":"dummy","tool":"thing"}`
		}
		w := endpointRaw(reference, "/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`, mcp.ProtocolVersion, endpointIdentity())
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	for _, path := range []string{"/mcp", "/mcp/dummy,notes"} {
		for _, tc := range []struct{ method, params, revision string }{
			{"initialize", `{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"test"}}`, "2025-11-25"},
			{"server/discover", `{}`, mcp.ProtocolVersion},
			{"tools/call", `{"name":"services","arguments":{}}`, mcp.ProtocolVersion},
			{"tools/call", `{"name":"describe","arguments":{"service":"dummy"}}`, mcp.ProtocolVersion},
			{"tools/call", `{"name":"call","arguments":{"service":"dummy","tool":"thing"}}`, mcp.ProtocolVersion},
			{"tools/call", `{"name":"mutate","arguments":{"service":"dummy","tool":"thing"}}`, mcp.ProtocolVersion},
		} {
			body := `{"jsonrpc":"2.0","id":1,"method":"` + tc.method + `","params":` + tc.params + `}`
			a := endpointRaw(h, path, "POST", body, tc.revision, endpointIdentity())
			b := endpointRaw(reference, path, "POST", body, tc.revision, endpointIdentity())
			if a.Code != b.Code || a.Code != 200 {
				t.Fatal(a.Body.String(), b.Body.String())
			}
			endpointHeadersEqual(t, a.Header(), b.Header())
			var left, right map[string]json.RawMessage
			if err := json.Unmarshal(a.Body.Bytes(), &left); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b.Body.Bytes(), &right); err != nil {
				t.Fatal(err)
			}
			var lr, rr map[string]json.RawMessage
			if err := json.Unmarshal(left["result"], &lr); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(right["result"], &rr); err != nil {
				t.Fatal(err)
			}
			if tc.method == "tools/call" {
				for _, key := range []string{"_meta", "resultType"} {
					if !bytes.Equal(lr[key], rr[key]) {
						t.Fatalf("envelope %s differs: %s %s", key, lr[key], rr[key])
					}
				}
				delete(left, "result")
				delete(right, "result")
			} else {
				delete(lr, "instructions")
				delete(rr, "instructions")
				left["result"], _ = json.Marshal(lr)
				right["result"], _ = json.Marshal(rr)
			}
			x, _ := json.Marshal(left)
			y, _ := json.Marshal(right)
			if !bytes.Equal(x, y) {
				t.Fatal(string(x), string(y))
			}
		}
	}
	path := endpointFile(t, `[{"name":"changed","mcp":true,"enabled":true}]`)
	secondCfg := cfg
	secondCfg.ServicesPath = path
	second := gateway.Handler(secondCfg)
	c := endpointClient(t, second, "/mcp")
	if b := endpointCall(t, c, ""); !bytes.Contains(b, []byte(`"name":"changed"`)) {
		t.Fatal(string(b))
	}
	if b := endpointCall(t, endpointClient(t, h, "/mcp"), ""); !bytes.Contains(b, []byte(`"services":[]`)) {
		t.Fatal(string(b))
	}
}
