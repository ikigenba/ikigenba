package gateway_test

import (
	"context"
	"encoding/json"
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
	// R-JWEF-2VJ7 R-JXMB-GN9W R-JYU7-UF0L R-VTGO-2KH2
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
				if _, err := c.CallTool(context.Background(), endpointCaller, tc.tool, json.RawMessage(tc.args)); err != nil {
					t.Fatal(err)
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
				if _, ok := event.Attrs["duration_us"]; !ok {
					t.Fatal(event)
				}
			})
		}
	}
}

func TestEndpointNonToolTrail(t *testing.T) {
	// R-JWEF-2VJ7 R-VTGO-2KH2
	for _, tc := range []struct {
		path, method, body string
		identity           bool
	}{
		{"/mcp", "GET", "", true},
		{"/mcp/a,,b", "POST", `{}`, true},
		{"/mcp", "POST", `invalid`, true},
		{"/mcp", "POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unknown","arguments":{}}}`, true},
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
		endpointRaw(gateway.Handler(cfg), tc.path, tc.method, tc.body, mcp.ProtocolVersion, headers)
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
