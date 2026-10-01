package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
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
	// R-TMAS-F0JM R-TNIO-SSAB R-TQ6Z-2X2K
	cfg := endpointConfig(t, "", nil)
	h := gateway.Handler(cfg)
	reference := identity.Require(gateway.ServiceName, io.Discard, cfg.MCP)
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
func TestEndpointMissingCallerServer(t *testing.T) {
	// R-Q12L-59E7
	t.Setenv(services.Variable, "")
	var a, b bytes.Buffer
	srv := gateway.NewServer("odd-version", &a)
	reference := mcp.NewServer(mcp.ServerConfig{Name: gateway.ServiceName, Version: "odd-version", Stderr: &b})
	for _, method := range []string{"GET", "POST", "HEAD"} {
		endpointSame(t, srv, reference, "/mcp", method, `{}`, mcp.ProtocolVersion, http.Header{})
		if a.String() != b.String() {
			t.Fatal(a.String(), b.String())
		}
	}
}
func TestHandlerIdentity(t *testing.T) {
	// R-WXPC-OZXZ R-WYX9-2ROO
	var actual, want bytes.Buffer
	cfg := endpointConfig(t, "", &actual)
	h := gateway.Handler(cfg)
	reference := identity.Require(gateway.ServiceName, &want, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("identity gate bypassed") }))
	for _, path := range []string{"/", "/mcp", "/mcp/a,,b", "/_appkit/theme.css", "/unknown"} {
		for _, method := range []string{"GET", "POST", "HEAD"} {
			for _, headers := range []http.Header{{}, {"X-User-Id": []string{"", "later"}, "X-Request-Id": []string{"req"}}} {
				endpointSame(t, h, reference, path, method, `bogus`, mcp.ProtocolVersion, headers)
			}
		}
	}
	nilCfg := cfg
	nilCfg.Stderr = nil
	nilHandler := gateway.Handler(nilCfg)
	endpointSame(t, nilHandler, h, "/mcp", "GET", "", "", http.Header{})
	// The comparison above makes one additional missing-identity write only to h.
	endpointRaw(reference, "/mcp", "GET", "", "", http.Header{})
	if actual.String() != want.String() {
		t.Fatal(actual.String(), want.String())
	}
	actual.Reset()
	c := endpointClient(t, h, "/mcp")
	if _, err := c.ListTools(context.Background(), endpointCaller); err != nil {
		t.Fatal(err)
	}
	endpointCall(t, c, "")
	endpointCall(t, c, `{"bogus":true}`)
	endpointRaw(h, "/unknown", "GET", "", "", endpointIdentity())
	if actual.Len() != 0 {
		t.Fatal(actual.String())
	}
}

type endpointWriter struct {
	in      atomic.Int32
	overlap atomic.Bool
	mu      sync.Mutex
	lines   [][]byte
}

func (w *endpointWriter) Write(p []byte) (int, error) {
	if w.in.Add(1) != 1 {
		w.overlap.Store(true)
	}
	defer w.in.Add(-1)
	w.mu.Lock()
	w.lines = append(w.lines, append([]byte(nil), p...))
	w.mu.Unlock()
	return len(p), nil
}
func TestHandlerConcurrent(t *testing.T) {
	// R-X055-GJFD R-WYX9-2ROO
	writer := &endpointWriter{}
	h := gateway.Handler(endpointConfig(t, "", writer))
	c := endpointClient(t, h, "/mcp")
	var group sync.WaitGroup
	for i := 0; i < 40; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := c.CallTool(context.Background(), endpointCaller, "services", nil)
			if err != nil || result.IsError() {
				t.Errorf("call: %v %v", result, err)
			}
			endpointRaw(h, "/mcp", "GET", "", "", http.Header{})
		}()
	}
	group.Wait()
	if writer.overlap.Load() {
		t.Fatal("concurrent writes")
	}
	if len(writer.lines) != 40 {
		t.Fatal(len(writer.lines))
	}
	for _, line := range writer.lines {
		if string(line) != "mcp: request -: X-User-Id is missing\n" {
			t.Fatal(string(line))
		}
	}
}
