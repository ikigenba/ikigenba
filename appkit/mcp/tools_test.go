package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

type toolsEmpty struct{}
type toolsIn struct {
	Value string `json:"value"`
}
type toolsOut struct {
	Value string `json:"value"`
}
type toolsEnum string

func (toolsEnum) Enum() []string { return []string{"one", "two"} }

func toolsServer(t *testing.T) *mcp.Server {
	return mcp.NewServer(mcp.ServerConfig{Name: "test", Telemetry: mcpTestWriter(t, nil, nil)})
}
func toolsRequest(ctx context.Context, s *mcp.Server, method, params string) json.RawMessage {
	body := `{"jsonrpc":"2.0","id":1,"method":` + strconv.Quote(method) + `,"params":` + params + `}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		panic(err)
	}
	if len(reply.Error) > 0 {
		panic(string(reply.Error))
	}
	return reply.Result
}
func toolsContext() context.Context {
	return identity.NewContext(context.Background(), identity.Caller{UserID: "user", Email: "user@example.test", RequestID: "request"})
}
func toolsCall(s *mcp.Server, name, args string) string {
	params := `{"name":` + strconv.Quote(name)
	if args != "" {
		params += `,"arguments":` + args
	}
	return string(toolsRequest(toolsContext(), s, "tools/call", params+`}`))
}
func toolsList(s *mcp.Server) []json.RawMessage {
	var v struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(toolsRequest(toolsContext(), s, "tools/list", `{}`), &v); err != nil {
		panic(err)
	}
	return v.Tools
}
func toolsPanic(fn func()) (value any) { defer func() { value = recover() }(); fn(); return nil }
func toolsRaw(name string) mcp.RawTool[toolsEmpty] {
	return mcp.RawTool[toolsEmpty]{name, "Read data.", mcp.Read, func(context.Context, identity.Caller, toolsEmpty) (mcp.Result, error) {
		return mcp.TextResult("ok"), nil
	}}
}
func toolsTyped(name string) mcp.Tool[toolsEmpty, toolsEmpty] {
	return mcp.Tool[toolsEmpty, toolsEmpty]{name, "Read data.", mcp.Read, func(context.Context, identity.Caller, toolsEmpty) (toolsEmpty, error) { return toolsEmpty{}, nil }}
}

func TestToolPublicShapes(t *testing.T) {
	// R-K4WQ-7BE1
	if mcp.Read != mcp.Effect(1) || mcp.Additive != mcp.Effect(2) || mcp.Destructive != mcp.Effect(3) {
		t.Fatal("effect values")
	}
	// R-K64M-L34Q
	var enum mcp.Enumerator = toolsEnum("")
	if strings.Join(enum.Enum(), ",") != "one,two" {
		t.Fatal("enumerator contract")
	}
	// R-K7CI-YUVF R-KB08-463I: unkeyed construction checks field ordering and handler signature by use.
	s := toolsServer(t)
	add := mcp.AddTool[toolsEmpty, toolsEmpty]
	add(s, toolsTyped("read_typed"))
	// R-K8KF-CMM4 R-KC84-HXU7
	addRaw := mcp.AddRawTool[toolsEmpty]
	addRaw(s, toolsRaw("read_raw"))
	if got := toolsCall(s, "read_typed", `{}`); got != `{"content":[{"type":"text","text":"{}"}],"structuredContent":{}}` {
		t.Fatal(got)
	}
	if got := toolsCall(s, "read_raw", `{}`); got != resultBytes(t, mcp.TextResult("ok")) {
		t.Fatal(got)
	}

}

func TestToolRegistrationFailures(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*mcp.Tool[toolsEmpty, toolsEmpty], *mcp.RawTool[toolsEmpty])
	}{
		// R-KJJI-SKAD
		{"name", func(a *mcp.Tool[toolsEmpty, toolsEmpty], b *mcp.RawTool[toolsEmpty]) {
			a.Name = "Read"
			b.Name = "Read"
		}},
		// R-KLZB-K3RR
		{"handler", func(a *mcp.Tool[toolsEmpty, toolsEmpty], b *mcp.RawTool[toolsEmpty]) {
			a.Handler = nil
			b.Handler = nil
		}},
		// R-KN77-XVIG
		{"effect", func(a *mcp.Tool[toolsEmpty, toolsEmpty], b *mcp.RawTool[toolsEmpty]) { a.Effect = 0; b.Effect = 0 }},
	}
	for _, c := range cases {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/raw=%t", c.name, raw), func(t *testing.T) {
				s := toolsServer(t)
				a, b := toolsTyped("read_data"), toolsRaw("read_data")
				c.mutate(&a, &b)
				var name string
				var p any
				if raw {
					name = b.Name
					p = toolsPanic(func() { mcp.AddRawTool(s, b) })
				} else {
					name = a.Name
					p = toolsPanic(func() { mcp.AddTool(s, a) })
				}
				// R-KIBM-ESJO
				prefix := "mcp: tool " + strconv.Quote(name) + ": "
				text, ok := p.(string)
				if !ok || !strings.HasPrefix(text, prefix) || len(text) == len(prefix) {
					t.Fatalf("panic: %#v", p)
				}
				if len(toolsList(s)) != 0 {
					t.Fatal("panicking registration inserted a tool")
				}
			})
		}
	}
}

func TestToolNameRules(t *testing.T) {
	// R-KJJI-SKAD
	for _, name := range []string{"", "Read", "_read", "read_", "read__data", "read-data", "read data", "écho", "read.thing", strings.Repeat("a", 65)} {
		for _, raw := range []bool{false, true} {
			s := toolsServer(t)
			if p := toolsPanic(func() {
				if raw {
					mcp.AddRawTool(s, toolsRaw(name))
				} else {
					mcp.AddTool(s, toolsTyped(name))
				}
			}); p == nil {
				t.Fatalf("accepted %q", name)
			}
		}
	}
	for _, name := range []string{"r", "read", "read_1", strings.Repeat("a", 64)} {
		s := toolsServer(t)
		mcp.AddRawTool(s, toolsRaw(name))
		if len(toolsList(s)) != 1 {
			t.Fatal(name)
		}
	}
}

func TestToolDescriptionRules(t *testing.T) {
	// R-KOF4-BN95
	descriptions := []string{"", "\nDetails.", " Read data.", "Read data. ", "\tRead data.", "Read data.\u00a0", "Read data", "Read data. More data.", strings.Repeat("a", 120) + ".", "Read data.\n\xff"}
	for _, description := range descriptions {
		for _, raw := range []bool{false, true} {
			s := toolsServer(t)
			a, b := toolsTyped("read_data"), toolsRaw("read_data")
			a.Description = description
			b.Description = description
			if p := toolsPanic(func() {
				if raw {
					mcp.AddRawTool(s, b)
				} else {
					mcp.AddTool(s, a)
				}
			}); p == nil {
				t.Fatalf("accepted %q", description)
			}
		}
	}
	for _, description := range []string{"Read data.\nFurther details.", strings.Repeat("é", 119) + "."} {
		s := toolsServer(t)
		tool := toolsRaw("read_data")
		tool.Description = description
		mcp.AddRawTool(s, tool)
	}
}

func TestToolDuplicateAndClosedRegistry(t *testing.T) {
	// R-KKRF-6C12
	for _, firstRaw := range []bool{false, true} {
		for _, secondRaw := range []bool{false, true} {
			s := toolsServer(t)
			if firstRaw {
				mcp.AddRawTool(s, toolsRaw("read_data"))
			} else {
				mcp.AddTool(s, toolsTyped("read_data"))
			}
			if p := toolsPanic(func() {
				if secondRaw {
					mcp.AddRawTool(s, toolsRaw("read_data"))
				} else {
					mcp.AddTool(s, toolsTyped("read_data"))
				}
			}); p == nil {
				t.Fatal("duplicate accepted")
			}
			if len(toolsList(s)) != 1 {
				t.Fatal("duplicate modified registry")
			}
		}
	}
	// R-KPN0-PEZU: even an unauthenticated request closes registration.
	for _, raw := range []bool{false, true} {
		s := toolsServer(t)
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		if p := toolsPanic(func() {
			if raw {
				mcp.AddRawTool(s, toolsRaw("read_data"))
			} else {
				mcp.AddTool(s, toolsTyped("read_data"))
			}
		}); p == nil {
			t.Fatal("late registration accepted")
		}
	}
}

func TestToolConcurrentRegistration(t *testing.T) {
	// R-KQUX-36QJ
	for range 40 {
		s := toolsServer(t)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-start; _ = toolsPanic(func() { mcp.AddTool(s, toolsTyped("read_typed")) }) })
		wg.Go(func() { <-start; _ = toolsPanic(func() { mcp.AddRawTool(s, toolsRaw("read_raw")) }) })
		wg.Go(func() { <-start; s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)) })
		close(start)
		wg.Wait()
		if count := len(toolsList(s)); count > 2 {
			t.Fatalf("registered %d tools", count)
		}
	}
}

func TestToolListObjects(t *testing.T) {
	// R-BTJA-WZ53 R-BUR7-AQVS R-BVZ3-OIMH R-BYEW-G23V R-BZMS-TTUK
	type in struct {
		Value string `json:"value" description:"Text to read."`
	}
	type out struct {
		Count int `json:"count"`
	}
	const description = "Read data.\nDetailed \"text\" with a \\ and café."
	const inputSchema = `{"type":"object","properties":{"value":{"type":"string","description":"Text to read."}},"additionalProperties":false}`
	const outputSchema = `{"type":"object","properties":{"count":{"type":"integer"}},"additionalProperties":false}`
	for _, effect := range []struct {
		name        string
		value       mcp.Effect
		annotations string
	}{
		{"read_data", mcp.Read, `{"readOnlyHint":true,"destructiveHint":false,"openWorldHint":false}`},
		{"add_data", mcp.Additive, `{"readOnlyHint":false,"destructiveHint":false,"openWorldHint":false}`},
		{"delete_data", mcp.Destructive, `{"readOnlyHint":false,"destructiveHint":true,"openWorldHint":false}`},
	} {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/raw=%t", effect.name, raw), func(t *testing.T) {
				s := toolsServer(t)
				if raw {
					mcp.AddRawTool(s, mcp.RawTool[in]{Name: effect.name, Description: description, Effect: effect.value, Handler: func(context.Context, identity.Caller, in) (mcp.Result, error) { return mcp.TextResult("ok"), nil }})
				} else {
					mcp.AddTool(s, mcp.Tool[in, out]{Name: effect.name, Description: description, Effect: effect.value, Handler: func(context.Context, identity.Caller, in) (out, error) { return out{}, nil }})
				}
				want := `{"name":` + strconv.Quote(effect.name) + `,"description":` + strconv.Quote(description) + `,"inputSchema":` + inputSchema
				if !raw {
					want += `,"outputSchema":` + outputSchema
				}
				want += `,"annotations":` + effect.annotations + `}`
				list := toolsList(s)
				if len(list) != 1 || string(list[0]) != want {
					t.Fatalf("tools/list = %s, want [%s]", list, want)
				}
			})
		}
	}
}

func TestToolArgumentsAndResults(t *testing.T) {
	s := toolsServer(t)
	calls := 0
	mcp.AddTool(s, mcp.Tool[toolsIn, toolsOut]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(_ context.Context, _ identity.Caller, in toolsIn) (toolsOut, error) {
		calls++
		return toolsOut(in), nil
	}})
	// R-L0M4-5CO3
	absent := toolsCall(s, "read_data", "")
	empty := toolsCall(s, "read_data", `{}`)
	if absent != empty || calls != 2 {
		t.Fatalf("absent %s empty %s calls %d", absent, empty, calls)
	}
	// R-L1U0-J4ES
	for _, c := range []struct{ args, kind string }{{`null`, "null"}, {`true`, "boolean"}, {`1`, "number"}, {`"s"`, "string"}, {`[]`, "array"}} {
		if got := toolsCall(s, "read_data", c.args); got != resultBytes(t, mcp.ErrorResult("invalid arguments:\narguments: expected object, got "+c.kind)) {
			t.Fatal(got)
		}
	}
	if calls != 2 {
		t.Fatal("invalid arguments called handler")
	}
	// R-L31W-WW5H
	want := mcp.ErrorResult("invalid arguments:\nvalue: expected string, got number\nextra: unknown field")
	if got := toolsCall(s, "read_data", `{"extra":true,"value":1}`); got != resultBytes(t, want) || calls != 2 {
		t.Fatal(got, calls)
	}
	// R-LHOP-I51T
	if got := toolsCall(s, "read_data", `{"value":"hello"}`); got != `{"content":[{"type":"text","text":"{\"value\":\"hello\"}"}],"structuredContent":{"value":"hello"}}` {
		t.Fatal(got)
	}
}

func TestToolHandlerErrors(t *testing.T) {
	// R-LIWL-VWSI
	for _, raw := range []bool{false, true} {
		s := toolsServer(t)
		if raw {
			mcp.AddRawTool(s, mcp.RawTool[toolsEmpty]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, toolsEmpty) (mcp.Result, error) {
				return mcp.TextResult("discard"), errors.New("explain failure")
			}})
		} else {
			mcp.AddTool(s, mcp.Tool[toolsEmpty, toolsOut]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, toolsEmpty) (toolsOut, error) {
				return toolsOut{Value: "discard"}, errors.New("explain failure")
			}})
		}
		if got := toolsCall(s, "read_data", `{}`); got != resultBytes(t, mcp.ErrorResult("explain failure")) {
			t.Fatal(got)
		}
	}
}

func TestToolPanicsRecover(t *testing.T) {
	// R-LK4I-9OJ7
	for _, raw := range []bool{false, true} {
		for _, requestID := range []string{"request", ""} {
			s := toolsServer(t)
			if raw {
				mcp.AddRawTool(s, mcp.RawTool[toolsEmpty]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, toolsEmpty) (mcp.Result, error) { panic("bad\r\nthing") }})
			} else {
				mcp.AddTool(s, mcp.Tool[toolsEmpty, toolsEmpty]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, toolsEmpty) (toolsEmpty, error) { panic("bad\r\nthing") }})
			}
			mcp.AddRawTool(s, toolsRaw("read_later"))
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "user", RequestID: requestID})
			got := string(toolsRequest(ctx, s, "tools/call", `{"name":"read_data","arguments":{}}`))
			// R-D797-6J8U: the exported string constant supplies the recovered failure.
			const panicText string = mcp.PanicText
			if got != resultBytes(t, mcp.ErrorResult(panicText)) {
				t.Fatal(got)
			}
			if got := toolsCall(s, "read_later", `{}`); got != resultBytes(t, mcp.TextResult("ok")) {
				t.Fatal(got)
			}
		}
	}
}

func TestToolUnencodableOutput(t *testing.T) {
	// R-2UIX-CNYK
	type out struct {
		Float float64   `json:"float"`
		Enum  toolsEnum `json:"enum"`
	}
	for _, value := range []out{{Float: math.NaN(), Enum: "one"}, {Float: math.Inf(1), Enum: "one"}, {Enum: "invalid"}} {
		s := toolsServer(t)
		mcp.AddTool(s, mcp.Tool[toolsEmpty, out]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, toolsEmpty) (out, error) { return value, nil }})
		if got := toolsCall(s, "read_data", `{}`); got != resultBytes(t, mcp.ErrorResult(mcp.PanicText)) {
			t.Fatal(got)
		}
	}
}

func TestRawToolPassthrough(t *testing.T) {
	// R-H0J4-YSFR
	var answer mcp.Result
	if err := answer.UnmarshalJSON([]byte(`{"custom":{"ordered":1},"content":[{"type":"image","data":"abc","mimeType":"image/png"}],"isError":true,"_meta":{"extra":"kept"}}`)); err != nil {
		t.Fatal(err)
	}
	s := toolsServer(t)
	mcp.AddRawTool(s, mcp.RawTool[toolsEmpty]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, toolsEmpty) (mcp.Result, error) { return answer, nil }})
	if got := toolsCall(s, "read_data", `{}`); got != resultBytes(t, answer) {
		t.Fatal(got)
	}
}

func TestToolHandlerContext(t *testing.T) {
	// R-1VNJ-S2WS
	type contextKey struct{}
	deadline := time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
	base, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "value"), deadline)
	defer cancel()
	ctx := identity.NewContext(base, identity.Caller{UserID: "user", RequestID: "context"})
	s := toolsServer(t)
	var received context.Context
	mcp.AddRawTool(s, mcp.RawTool[toolsEmpty]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(c context.Context, _ identity.Caller, _ toolsEmpty) (mcp.Result, error) {
		received = c
		return mcp.TextResult("ok"), nil
	}})
	toolsRequest(ctx, s, "tools/call", `{"name":"read_data","arguments":{}}`)
	gotDeadline, ok := received.Deadline()
	if !ok || !gotDeadline.Equal(deadline) || received.Value(contextKey{}) != "value" {
		t.Fatal("request context not preserved")
	}
	cancel()
	select {
	case <-received.Done():
	default:
		t.Fatal("request cancellation not preserved")
	}
}

func TestToolHandlersRunConcurrently(t *testing.T) {
	// R-M173-MGWX
	s := toolsServer(t)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	mcp.AddRawTool(s, mcp.RawTool[toolsEmpty]{Name: "read_data", Description: "Read data.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, toolsEmpty) (mcp.Result, error) {
		started <- struct{}{}
		<-release
		return mcp.TextResult("ok"), nil
	}})
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { toolsCall(s, "read_data", `{}`) })
	}
	watchdog := time.NewTimer(5 * time.Second)
	defer watchdog.Stop()
	defer func() { close(release); wg.Wait() }()
	for range 2 {
		select {
		case <-started:
		case <-watchdog.C:
			t.Fatal("handlers did not start concurrently")
		}
	}
}
