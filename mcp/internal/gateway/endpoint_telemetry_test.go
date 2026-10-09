package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func TestEndpointMissingCallerMatchesAppkit(t *testing.T) {
	// R-2CXR-OQ5Q
	t.Setenv(services.Variable, "")
	writer, capture := handlerTelemetry(t, nil)
	for _, version := range []string{"", "test-version", "a\"b\n"} {
		srv := gateway.NewServer(version, writer)
		reference := mcp.NewServer(mcp.ServerConfig{Name: gateway.ServiceName, Version: version, Telemetry: writer})
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, body := range []string{"", "invalid", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"services"}}`} {
				endpointSame(t, srv, reference, "/mcp", method, body, mcp.ProtocolVersion, http.Header{})
			}
		}
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if events := capture.Events(); len(events) != 0 {
		t.Fatal(events)
	}
}

func TestEndpointStandaloneTools(t *testing.T) {
	// R-2AHY-X6OC
	t.Setenv(services.Variable, "")
	writer, _ := handlerTelemetry(t, nil)
	srv := identity.Require(gateway.NewServer("test-version", writer))
	client := endpointClient(t, srv, "/mcp")
	tools, err := client.ListTools(context.Background(), endpointCaller)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		args := json.RawMessage(`{}`)
		if tool.Name == "describe" {
			args = json.RawMessage(`{"service":"dummy"}`)
		}
		if tool.Name == "call" || tool.Name == "mutate" {
			args = json.RawMessage(`{"service":"dummy","tool":"thing"}`)
		}
		if _, err := client.CallTool(context.Background(), endpointCaller, tool.Name, args); err != nil {
			t.Fatal(tool.Name, err)
		}
	}
}

func TestEndpointToolTrail(t *testing.T) {
	// R-O3U0-KWFR R-JQW7-8JSX R-ZPHR-94TJ R-VTGO-2KH2
	for _, path := range []string{"/mcp", "/mcp/missing"} {
		for _, tc := range []struct{ tool, args, kind, outcome string }{
			{"services", `{}`, "read", "ok"},
			{"services", `{"bogus":true}`, "read", "invalid_arguments"},
			{"describe", `{}`, "read", "invalid_arguments"},
			{"describe", `{"service":"missing"}`, "read", "error"},
			{"call", `{}`, "read", "invalid_arguments"},
			{"call", `{"service":"missing","tool":"thing"}`, "read", "error"},
			{"mutate", `{}`, "destructive", "invalid_arguments"},
			{"mutate", `{"service":"missing","tool":"thing"}`, "destructive", "error"},
		} {
			t.Run(path+"/"+tc.tool+"/"+tc.outcome, func(t *testing.T) {
				cfg := endpointConfig(t, "")
				writer, capture := handlerTelemetry(t, nil)
				cfg.Telemetry = writer
				cfg.MCP = gateway.NewServer("test-version", writer)
				c := endpointClient(t, gateway.Handler(cfg), path)
				result, err := c.CallTool(context.Background(), endpointCaller, tc.tool, json.RawMessage(tc.args))
				if err != nil || result.IsError() != (tc.outcome != "ok") {
					t.Fatal(result, err)
				}
				if err := writer.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				events := capture.Events()
				if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != "tool.called" || events[2].Name != "request.finished" {
					t.Fatal(events)
				}
				event := events[1]
				if event.RequestID != endpointCaller.RequestID || event.User != endpointCaller.UserID || event.Service != gateway.ServiceName {
					t.Fatal(event)
				}
				if len(event.Attrs) != 4 || event.Attrs["tool"] != tc.tool || event.Attrs["kind"] != tc.kind || event.Attrs["outcome"] != tc.outcome {
					t.Fatal(event)
				}
				if event.Attrs["duration_us"] != int64(0) {
					t.Fatal(event)
				}
			})
		}
	}
}

func TestEndpointNonToolTrail(t *testing.T) {
	// R-O3U0-KWFR R-VTGO-2KH2
	for _, tc := range []struct {
		path, method, body string
		identity           bool
	}{
		{"/mcp", "GET", "", true},
		{"/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"test"}}}`, true},
		{"/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`, true},
		{"/mcp/a,,b", "POST", `{}`, true},
		{"/mcp", "POST", `invalid`, true},
		{"/mcp", "POST", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"services","arguments":{}}}`, true},
		{"/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"services","arguments":{}}}`, false},
	} {
		cfg := endpointConfig(t, "")
		writer, capture := handlerTelemetry(t, nil)
		cfg.Telemetry = writer
		cfg.MCP = gateway.NewServer("test-version", writer)
		headers := http.Header{}
		if tc.identity {
			headers = endpointIdentity()
		}
		revision := mcp.ProtocolVersion
		if tc.method == "POST" && tc.body != "" {
			var message struct{ Method string }
			_ = json.Unmarshal([]byte(tc.body), &message)
			if message.Method == "initialize" {
				revision = "2025-11-25"
			}
		}
		endpointRaw(gateway.Handler(cfg), tc.path, tc.method, tc.body, revision, headers)
		if err := writer.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, event := range capture.Events() {
			if event.Name == "tool.called" {
				t.Fatal(event)
			}
		}
	}
}

func TestEndpointClientNonToolTrail(t *testing.T) {
	// R-O3U0-KWFR
	for _, path := range []string{"/mcp", "/mcp/missing"} {
		for _, method := range []string{"list", "unknown"} {
			t.Run(fmt.Sprint(path, method), func(t *testing.T) {
				writer, capture := handlerTelemetry(t, nil)
				c := endpointClient(t, gateway.Handler(handlerConfig(t, writer)), path)
				if method == "list" {
					if _, err := c.ListTools(context.Background(), endpointCaller); err != nil {
						t.Fatal(err)
					}
				} else if _, err := c.CallTool(context.Background(), endpointCaller, "unknown", nil); err == nil {
					t.Fatal("unknown tool received a result")
				}
				handlerFlush(t, writer)
				events := capture.Events()
				if len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" {
					t.Fatal(events)
				}
			})
		}
	}
}

func TestEndpointVersion(t *testing.T) {
	// R-2BPV-AYF1 R-VTGO-2KH2
	for _, version := range []string{"", "test-version", "a\"b\n"} {
		cfg := endpointConfig(t, "")
		cfg.MCP = gateway.NewServer(version, cfg.Telemetry)
		h := gateway.Handler(cfg)
		want, _ := json.Marshal(struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}{gateway.ServiceName, version})
		for _, path := range []string{"/mcp", "/mcp/missing"} {
			b := endpointCall(t, endpointClient(t, h, path), "")
			info := endpointPart(t, endpointPart(t, b, "_meta"), "io.modelcontextprotocol/serverInfo")
			endpointOrderedEqual(t, info, want)
			response := endpointRaw(h, path, "POST", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"test"}}}`, "2025-11-25", endpointIdentity())
			endpointOrderedEqual(t, endpointPart(t, endpointPart(t, response.Body.Bytes(), "result"), "serverInfo"), want)
		}
	}
}
