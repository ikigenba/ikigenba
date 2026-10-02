package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func mcpTestWriter(t *testing.T, capture *telemetry.Capture, now func() time.Time) *telemetry.Writer {
	t.Helper()
	if capture == nil {
		capture = &telemetry.Capture{}
	}
	if now == nil {
		now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	}
	w := telemetry.New(telemetry.Config{Service: "test", Sink: capture, Now: now, Rand: bytes.NewReader(make([]byte, 256)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test complete") })
	return w
}

func mcpToolEvents(t *testing.T, w *telemetry.Writer, c *telemetry.Capture) []telemetry.Event {
	t.Helper()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var events []telemetry.Event
	for _, e := range c.Events() {
		if e.Name == "tool.called" {
			events = append(events, e)
		}
	}
	return events
}

func TestToolTelemetry(t *testing.T) {
	// R-2WYQ-47FY R-2Y6M-HZ6N R-2ZEI-VQXC R-30MF-9IO1 R-31UB-NAEQ R-JPSY-MGRS R-VHX9-7P1A
	type output struct {
		Value float64 `json:"value"`
	}
	cases := []struct {
		name, args, outcome                              string
		raw, rawError, handlerError, panics, unencodable bool
		elapsed                                          time.Duration
	}{
		{name: "ok", args: `{}`, outcome: "ok", elapsed: 2500 * time.Nanosecond},
		{name: "raw_ok", args: `{}`, outcome: "ok", raw: true, elapsed: 4 * time.Microsecond},
		{name: "raw_error_result", args: `{}`, outcome: "error", raw: true, rawError: true, elapsed: time.Microsecond},
		{name: "typed_error", args: `{}`, outcome: "error", handlerError: true, elapsed: time.Microsecond},
		{name: "raw_error", args: `{}`, outcome: "error", raw: true, handlerError: true, elapsed: time.Microsecond},
		{name: "typed_panic", args: `{}`, outcome: "panicked", panics: true, elapsed: 3 * time.Microsecond},
		{name: "raw_panic", args: `{}`, outcome: "panicked", raw: true, panics: true, elapsed: 3 * time.Microsecond},
		{name: "unencodable", args: `{}`, outcome: "unencodable_output", unencodable: true, elapsed: time.Microsecond},
		{name: "invalid_typed", args: `[]`, outcome: "invalid_arguments"},
		{name: "invalid_raw", args: `[]`, outcome: "invalid_arguments", raw: true},
		{name: "decode_typed", args: `{"extra":1}`, outcome: "invalid_arguments"},
		{name: "decode_raw", args: `{"extra":1}`, outcome: "invalid_arguments", raw: true},
		{name: "negative", args: `{}`, outcome: "ok", elapsed: -time.Microsecond},
		{name: "submicrosecond", args: `{}`, outcome: "ok", elapsed: 999 * time.Nanosecond},
	}
	for _, modern := range []bool{false, true} {
		for _, effect := range []mcp.Effect{mcp.Read, mcp.Additive, mcp.Destructive} {
			for _, tc := range cases {
				t.Run(tc.name+"/"+map[bool]string{false: "legacy", true: "modern"}[modern]+"/"+map[mcp.Effect]string{mcp.Read: "read", mcp.Additive: "additive", mcp.Destructive: "destructive"}[effect], func(t *testing.T) {
					capture := &telemetry.Capture{}
					current := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
					// Only the handler moves the injected clock. Writer reads leave it unchanged.
					writer := mcpTestWriter(t, capture, func() time.Time { return current })
					if err := writer.Flush(context.Background()); err != nil {
						t.Fatal(err)
					}
					s := mcp.NewServer(mcp.ServerConfig{Name: "example", Telemetry: writer})
					calls := 0
					run := func() error {
						calls++
						current = current.Add(tc.elapsed)
						if tc.panics {
							panic("private panic details")
						}
						if tc.handlerError {
							return errors.New("private handler error")
						}
						return nil
					}
					if tc.raw {
						mcp.AddRawTool(s, mcp.RawTool[struct{}]{Name: "tool", Description: "Tool.", Effect: effect, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) {
							err := run()
							if tc.rawError {
								return mcp.ErrorResult("private content"), err
							}
							return mcp.TextResult("private content"), err
						}})
					} else {
						mcp.AddTool(s, mcp.Tool[struct{}, output]{Name: "tool", Description: "Tool.", Effect: effect, Handler: func(context.Context, identity.Caller, struct{}) (output, error) {
							err := run()
							if tc.unencodable {
								return output{math.NaN()}, err
							}
							return output{5}, err
						}})
					}
					params := map[string]any{"name": "tool"}
					// Preserve arguments as their JSON values, including nonobjects.
					params["arguments"] = jsonValue(t, tc.args)
					r := serverTestRequest(serverTestMessage("tools/call", params))
					if modern {
						r = serverTestModern("tools/call", params)
					}
					caller := identity.Caller{UserID: "unique-user", Email: "not-an-attribute", RequestID: "unique-request"}
					r = r.WithContext(identity.NewContext(r.Context(), caller))
					response := serverTestServe(s, r)
					if _, ok := serverTestObject(t, response.Body.Bytes())["result"]; !ok {
						t.Fatal(response.Body.String())
					}
					events := mcpToolEvents(t, writer, capture)
					if len(events) != 1 {
						t.Fatalf("events = %v", events)
					}
					e := events[0]
					duration := int64(tc.elapsed / time.Microsecond)
					if duration < 0 || tc.outcome == "invalid_arguments" {
						duration = 0
					}
					want := telemetry.Attrs{"tool": "tool", "kind": map[mcp.Effect]string{mcp.Read: "read", mcp.Additive: "additive", mcp.Destructive: "destructive"}[effect], "outcome": tc.outcome, "duration_us": duration}
					if e.RequestID != caller.RequestID || e.User != caller.UserID || !reflect.DeepEqual(e.Attrs, want) {
						t.Fatalf("event = %#v, attrs want %#v", e, want)
					}
					wantCalls := 1
					if tc.outcome == "invalid_arguments" {
						wantCalls = 0
					}
					if calls != wantCalls {
						t.Fatalf("handler calls=%d", calls)
					}
				})
			}
		}
	}
}

func TestToolTelemetryNonCalls(t *testing.T) {
	// R-2WYQ-47FY
	for _, modern := range []bool{false, true} {
		capture := &telemetry.Capture{}
		writer := mcpTestWriter(t, capture, nil)
		s := mcp.NewServer(mcp.ServerConfig{Name: "example", Telemetry: writer})
		mcp.AddRawTool(s, toolsRaw("tool"))
		for _, tc := range []struct {
			method string
			params map[string]any
		}{
			{"tools/list", map[string]any{}}, {"initialize", map[string]any{}}, {"server/discover", map[string]any{}}, {"ping", map[string]any{}}, {"absent", map[string]any{}},
			{"tools/call", map[string]any{"name": "unknown"}}, {"tools/call", map[string]any{}}, {"tools/call", map[string]any{"name": 5}},
		} {
			r := serverTestRequest(serverTestMessage(tc.method, tc.params))
			if modern {
				r = serverTestModern(tc.method, tc.params)
			}
			serverTestServe(s, r)
		}
		for _, body := range []string{"invalid", `[]`, `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"tool"}}`, `{"jsonrpc":"2.0","result":{}}`} {
			serverTestServe(s, serverTestRequest(body))
		}
		for stage := range 6 {
			r := serverTestRequest(serverTestMessage("tools/call", map[string]any{"name": "tool"}))
			switch stage {
			case 0:
				r = r.WithContext(context.Background())
			case 1:
				r.Method = "GET"
			case 2:
				r.Header.Set("Origin", "null")
			case 3:
				r.Header.Del("Content-Type")
			case 4:
				r.Header.Set("MCP-Protocol-Version", "unsupported")
			case 5:
				r = serverTestModern("tools/call", map[string]any{"name": "tool"})
				r.Header.Set("Mcp-Name", "mismatch")
			}
			serverTestServe(s, r)
		}
		if events := mcpToolEvents(t, writer, capture); len(events) != 0 {
			t.Fatal(events)
		}
	}
}

func jsonValue(t *testing.T, text string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
