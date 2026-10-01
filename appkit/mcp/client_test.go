package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

type clientTransport func(*http.Request) (*http.Response, error)

func (f clientTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func clientRequest(t *testing.T, r *http.Request) map[string]json.RawMessage {
	t.Helper()
	var msg map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		t.Fatal(err)
	}
	return msg
}
func clientReply(w http.ResponseWriter, id json.RawMessage, result string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, id, result)
}
func clientZero(t *testing.T, result mcp.Result) {
	t.Helper()
	raw, err := result.MarshalJSON()
	if err != nil || string(raw) != `{"content":[]}` {
		t.Fatalf("nonzero result: %s %v", raw, err)
	}
}
func clientPlain(t *testing.T, err error) {
	t.Helper()
	var rpc *mcp.RPCError
	var httpErr *mcp.HTTPError
	if err == nil || errors.As(err, &rpc) || errors.As(err, &httpErr) || !strings.HasPrefix(err.Error(), "mcp: ") {
		t.Fatalf("want plain protocol error, got %T %v", err, err)
	}
}

func TestClientPublicShapes(t *testing.T) {
	// R-N93H-E7LX R-NABD-RZCM R-NBJA-5R3B R-NCR6-JIU0: ordered literals and exact signatures compile and run.
	cfg := mcp.ClientConfig{"bad endpoint", nil, "name", "version"}
	construct := struct {
		f func(mcp.ClientConfig) *mcp.Client
	}{mcp.NewClient}
	c := construct.f(cfg)
	methods := struct {
		list func(context.Context, identity.Caller) ([]mcp.ToolInfo, error)
		call func(context.Context, identity.Caller, string, json.RawMessage) (mcp.Result, error)
	}{c.ListTools, c.CallTool}
	if _, err := methods.list(context.Background(), identity.Caller{}); err == nil {
		t.Fatal("list accepted bad endpoint")
	}
	if _, err := methods.call(context.Background(), identity.Caller{}, "tool", nil); err == nil {
		t.Fatal("call accepted bad endpoint")
	}
	// R-NDZ2-XAKP R-NF6Z-B2BE R-NGEV-OU23
	yes, no := true, false
	hints := mcp.Annotations{&yes, &no, &yes, &no}
	tool := mcp.ToolInfo{"tool", "description", json.RawMessage(`{}`), json.RawMessage(`{}`), hints}
	effect := struct{ f func() mcp.Effect }{tool.Effect}
	if tool.Name != "tool" || tool.Description != "description" || string(tool.InputSchema) != "{}" || string(tool.OutputSchema) != "{}" || tool.Annotations.IdempotentHint != &yes || tool.Annotations.OpenWorldHint != &no || effect.f() != mcp.Read {
		t.Fatalf("tool fields: %+v", tool)
	}
	// R-NHMS-2LSS R-NIUO-GDJH R-OI7R-JQ1M R-OJFN-XHSB
	rpc := mcp.RPCError{-123, "message", json.RawMessage(`{"x":1}`)}
	httpErr := mcp.HTTPError{503, "body"}
	texts := struct{ rpc, http func() string }{rpc.Error, httpErr.Error}
	if texts.rpc() != "mcp: rpc error -123: message" || string(rpc.Data) != `{"x":1}` || texts.http() != "mcp: unexpected HTTP response (status 503)" || httpErr.Body != "body" {
		t.Fatal("error fields or text")
	}
}
func TestClientConstructionAndTransport(t *testing.T) {
	// R-NLAH-7X0V: construction performs no I/O, even with unusable configurations.
	requests := 0
	transport := clientTransport(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("unused") })
	for _, endpoint := range []string{"", ":bad", "ftp://example.invalid/mcp", "http://"} {
		c := mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint, HTTPClient: &http.Client{Transport: transport}})
		if requests != 0 {
			t.Fatal("constructor did I/O")
		}
		if _, err := c.ListTools(context.Background(), identity.Caller{}); err == nil {
			t.Fatal("unusable list succeeded")
		}
		if _, err := c.CallTool(context.Background(), identity.Caller{}, "tool", nil); err == nil {
			t.Fatal("unusable call succeeded")
		}
		requests = 0
	}
	// R-NMID-LORK: configured and default HTTP clients both reach the controlled backend.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msg := clientRequest(t, r)
		clientReply(w, msg["id"], `{"tools":[]}`)
	}))
	defer server.Close()
	for _, hc := range []*http.Client{nil, server.Client()} {
		tools, err := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: hc}).ListTools(context.Background(), identity.Caller{})
		if err != nil || len(tools) != 0 {
			t.Fatalf("transport: %v %v", tools, err)
		}
	}
}
func TestClientRequests(t *testing.T) {
	// R-NNQ9-ZGI9 R-NOY6-D88Y R-NQ62-QZZN R-NRDZ-4RQC R-NSLV-IJH1 R-NTTR-WB7Q R-NYPD-FE6I
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "context")
	caller := identity.Caller{UserID: "user", Email: "email", RequestID: "request"}
	for _, clientName := range []string{"", "gateway"} {
		seen := map[uint64]bool{}
		expectedName := ""
		var expectedArgs json.RawMessage
		transport := clientTransport(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost || r.URL.String() != "http://backend.test/mcp?x=1" || r.Context() != ctx || r.Context().Value(key{}) != "context" {
				t.Fatal("request method/endpoint/context")
			}
			msg := clientRequest(t, r)
			var method string
			_ = json.Unmarshal(msg["method"], &method)
			if len(msg) != 4 || string(msg["jsonrpc"]) != `"2.0"` {
				t.Fatalf("envelope: %v", msg)
			}
			var id uint64
			if err := json.Unmarshal(msg["id"], &id); err != nil || seen[id] {
				t.Fatalf("invalid/reused id %s", msg["id"])
			}
			seen[id] = true
			for header, value := range map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "MCP-Protocol-Version": mcp.ProtocolVersion, "Mcp-Method": method} {
				if r.Header.Get(header) != value {
					t.Fatalf("%s=%q", header, r.Header.Get(header))
				}
			}
			forwarded := httptest.NewRequest(http.MethodPost, "/", nil)
			identity.Forward(caller, forwarded)
			for _, header := range []string{"X-User-Id", "X-User-Email", "X-Request-Id"} {
				if r.Header.Get(header) != forwarded.Header.Get(header) {
					t.Fatal("identity not forwarded")
				}
			}
			var params map[string]json.RawMessage
			_ = json.Unmarshal(msg["params"], &params)
			var meta map[string]json.RawMessage
			_ = json.Unmarshal(params["_meta"], &meta)
			count := 2
			if clientName != "" {
				count++
				if string(meta["io.modelcontextprotocol/clientInfo"]) != `{"name":"gateway","version":"v"}` {
					t.Fatal("client info")
				}
			}
			if len(meta) != count || string(meta["io.modelcontextprotocol/protocolVersion"]) != strconv.Quote(mcp.ProtocolVersion) || string(meta["io.modelcontextprotocol/clientCapabilities"]) != "{}" {
				t.Fatalf("meta: %v", meta)
			}
			result := `{"tools":[]}`
			if method == "tools/list" {
				if len(params) != 1 || r.Header.Get("Mcp-Name") != "" {
					t.Fatal("list params/header")
				}
			} else {
				if method != "tools/call" || len(params) != 3 || string(params["name"]) != strconv.Quote(expectedName) {
					t.Fatalf("call params: %v", params)
				}
				var got, want any
				_ = json.Unmarshal(params["arguments"], &got)
				_ = json.Unmarshal(expectedArgs, &want)
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(want)
				if !bytes.Equal(gb, wb) {
					t.Fatalf("arguments: %s", gb)
				}
				header := expectedName
				encode := strings.HasPrefix(header, "=?base64?")
				for i := 0; i < len(header); i++ {
					if header[i] < 0x21 || header[i] > 0x7e {
						encode = true
					}
				}
				if encode {
					header = "=?base64?" + base64.StdEncoding.EncodeToString([]byte(header)) + "?="
				}
				if r.Header.Get("Mcp-Name") != header {
					t.Fatalf("tool header: %q", r.Header.Get("Mcp-Name"))
				}
				result = `{"content":[]}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, id, result)))}, nil
		})
		c := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://backend.test/mcp?x=1", HTTPClient: &http.Client{Transport: transport}, Name: clientName, Version: "v"})
		if _, err := c.ListTools(ctx, caller); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"tool", "with space", "=?base64?literal", "é", "a\n", "~!"} {
			expectedName = name
			for _, args := range []json.RawMessage{nil, json.RawMessage(` [1, true, null] `)} {
				expectedArgs = args
				if args == nil {
					expectedArgs = json.RawMessage(`{}`)
				}
				if _, err := c.CallTool(ctx, caller, name, args); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
func TestClientInvalidArguments(t *testing.T) {
	// R-NZX9-T5X7
	count := 0
	c := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://backend.test", HTTPClient: &http.Client{Transport: clientTransport(func(*http.Request) (*http.Response, error) { count++; return nil, errors.New("sent") })}})
	for _, args := range []string{"", "{", "{} {}", "null garbage"} {
		if _, err := c.CallTool(context.Background(), identity.Caller{}, "tool", json.RawMessage(args)); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	if count != 0 {
		t.Fatal("sent invalid arguments")
	}
}
func TestClientPaginationAndToolInfo(t *testing.T) {
	// R-NV1O-A2YF R-NW9K-NUP4 R-OC49-MVC5 R-O156-6XNW
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msg := clientRequest(t, r)
		var params map[string]json.RawMessage
		_ = json.Unmarshal(msg["params"], &params)
		if string(msg["method"]) != `"tools/list"` {
			t.Fatal("method")
		}
		wantLen := 1
		if count > 0 {
			wantLen = 2
			want := `""`
			if count == 2 {
				want = `"second"`
			}
			if string(params["cursor"]) != want {
				t.Fatalf("cursor %s", params["cursor"])
			}
		}
		if len(params) != wantLen {
			t.Fatal("page params")
		}
		switch count {
		case 0:
			clientReply(w, msg["id"], `{"tools":[{"name":"a","inputSchema":{},"description":"A.","outputSchema":{"type":"object"},"annotations":{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}},{"name":"b","inputSchema":{"type":"object"}}],"nextCursor":""}`)
		case 1:
			clientReply(w, msg["id"], `{"tools":[{"name":"c","inputSchema":{}}],"nextCursor":"second"}`)
		case 2:
			clientReply(w, msg["id"], `{"tools":[{"name":"d","inputSchema":{}}]}`)
		default:
			t.Fatal("extra page")
		}
		count++
	}))
	defer server.Close()
	tools, err := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL}).ListTools(context.Background(), identity.Caller{})
	if err != nil || count != 3 || len(tools) != 4 {
		t.Fatalf("listing: %+v %v count=%d", tools, err, count)
	}
	for i, name := range []string{"a", "b", "c", "d"} {
		if tools[i].Name != name {
			t.Fatal("order")
		}
	}
	a := tools[0]
	if a.Description != "A." || string(a.InputSchema) != "{}" || string(a.OutputSchema) != `{"type":"object"}` || a.Annotations.ReadOnlyHint == nil || !*a.Annotations.ReadOnlyHint || a.Annotations.DestructiveHint == nil || *a.Annotations.DestructiveHint || a.Annotations.IdempotentHint == nil || !*a.Annotations.IdempotentHint || a.Annotations.OpenWorldHint == nil || *a.Annotations.OpenWorldHint {
		t.Fatalf("tool info %+v", a)
	}
	b := tools[1]
	if b.Description != "" || b.OutputSchema != nil || b.Annotations != (mcp.Annotations{}) || string(b.InputSchema) != `{"type":"object"}` {
		t.Fatalf("defaults: %+v", b)
	}
}
func TestClientInvalidPaginationAndTools(t *testing.T) {
	// R-NXHH-1MFT R-OAWD-93LG R-OFRY-S6K8
	badPages := []string{`{}`, `{"tools":null}`, `{"tools":{}}`, `{"tools":[null]}`, `{"tools":[{"name":1,"inputSchema":{}}]}`, `{"tools":[{"name":"a","inputSchema":null}]}`, `{"tools":[{"name":"a","inputSchema":{},"description":false}]}`, `{"tools":[{"name":"a","inputSchema":{},"outputSchema":[]}]}`, `{"tools":[{"name":"a","inputSchema":{},"annotations":null}]}`, `{"tools":[{"name":"a","inputSchema":{},"annotations":{"readOnlyHint":null}}]}`, `{"tools":[{"name":"a","inputSchema":{},"annotations":{"destructiveHint":1}}]}`, `{"tools":[{"name":"a","inputSchema":{},"annotations":{"idempotentHint":"true"}}]}`, `{"tools":[{"name":"a","inputSchema":{},"annotations":{"openWorldHint":{}}}]}`, `{"tools":[],"nextCursor":null}`, `{"tools":[],"nextCursor":42}`, `{"tools":[],"nextCursor":""}`}
	for _, page := range badPages {
		t.Run(page, func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				msg := clientRequest(t, r)
				clientReply(w, msg["id"], page)
			}))
			defer server.Close()
			tools, err := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL}).ListTools(context.Background(), identity.Caller{})
			clientPlain(t, err)
			if tools != nil {
				t.Fatal("partial tools")
			}
			want := 1
			if strings.HasSuffix(page, `""}`) {
				want = 2
			}
			if count != want {
				t.Fatalf("requests %d want %d", count, want)
			}
		})
	}
}
func TestClientEffectDefaults(t *testing.T) {
	// R-ODC6-0N2U
	yes, no := true, false
	for _, read := range []*bool{nil, &no, &yes} {
		for _, destroy := range []*bool{nil, &no, &yes} {
			want := mcp.Destructive
			if destroy == &no {
				want = mcp.Additive
			}
			if read == &yes {
				want = mcp.Read
			}
			tool := mcp.ToolInfo{Annotations: mcp.Annotations{ReadOnlyHint: read, DestructiveHint: destroy}}
			if tool.Effect() != want {
				t.Fatal("effect defaults")
			}
		}
	}
}
func TestClientResponseClassification(t *testing.T) {
	// R-O4SV-C8VZ R-O78O-3SDD R-O8GK-HK42 R-O9OG-VBUR R-OEK2-EETJ R-OFRY-S6K8
	for _, tc := range []struct {
		name              string
		status            int
		media, body, kind string
	}{
		{"rpc400", 400, "application/json", `{"jsonrpc":"2.0","id":%s,"error":{"code":-1,"message":"bad","data": { "x": 1 }}}`, "rpc"},
		{"rpc200", 200, "application/json", `{"jsonrpc":"2.0","error":{"code":-1,"message":"bad"}}`, "rpc"},
		{"rpc502", 502, "text/plain", `{"jsonrpc":"2.0","error":{"code":-1,"message":"bad"}}`, "rpc"},
		{"proxy", 401, "text/plain", "no access", "http"}, {"large", 502, "text/plain", strings.Repeat("x", 5000), "http"},
		{"wrongtype", 200, "text/plain", `{"jsonrpc":"2.0","id":%s,"result":{}}`, "http"},
		{"invalidJSON", 200, "application/json", "{bad", "http"}, {"notrpc", 200, "application/json", `{"result":{}}`, "http"},
		{"badrevision", 200, "application/json", `{"jsonrpc":"1.0","result":{}}`, "http"},
		{"wrongid", 200, "application/json", `{"jsonrpc":"2.0","id":999,"result":{}}`, "plain"},
		{"nonobject", 200, "application/json", `{"jsonrpc":"2.0","id":%s,"result":[]}`, "plain"},
		{"input", 200, "application/json", `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"input_required"}}`, "plain"},
		{"typeNull", 200, "application/json", `{"jsonrpc":"2.0","id":%s,"result":{"resultType":null}}`, "plain"},
		{"unknown", 200, "application/json", `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"future"}}`, "plain"},
		{"complete", 200, "application/json", `{"jsonrpc":"2.0","id":%s,"result":{"content":[],"resultType":"complete","isError":true,"extra":{"x":1}}}`, "ok"},
		{"absent", 200, "application/json", `{"jsonrpc":"2.0","id":%s,"result":{"content":[],"extra":{"x":1}}}`, "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				msg := clientRequest(t, r)
				sent = strings.ReplaceAll(tc.body, "%s", string(msg["id"]))
				w.Header().Set("Content-Type", tc.media)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, sent)
			}))
			defer server.Close()
			result, err := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL}).CallTool(context.Background(), identity.Caller{}, "tool", nil)
			switch tc.kind {
			case "rpc":
				var rpc *mcp.RPCError
				if !errors.As(err, &rpc) || rpc.Code != -1 || rpc.Message != "bad" {
					t.Fatalf("RPC error %v", err)
				}
				if tc.name == "rpc400" && string(rpc.Data) != `{ "x": 1 }` {
					t.Fatalf("data bytes: %q", rpc.Data)
				}
				if tc.name != "rpc400" && rpc.Data != nil {
					t.Fatal("absent data")
				}
			case "http":
				var httpErr *mcp.HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status {
					t.Fatalf("HTTP error %v", err)
				}
				want := sent
				if len(want) > 4096 {
					want = want[:4096]
				}
				if httpErr.Body != want {
					t.Fatalf("HTTP body %q", httpErr.Body)
				}
			case "plain":
				clientPlain(t, err)
			case "ok":
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := result.MarshalJSON()
				var expected mcp.Result
				var envelope map[string]json.RawMessage
				_ = json.Unmarshal([]byte(sent), &envelope)
				if err := expected.UnmarshalJSON(envelope["result"]); err != nil {
					t.Fatal(err)
				}
				want, _ := expected.MarshalJSON()
				if !bytes.Equal(raw, want) {
					t.Fatalf("result %s want %s", raw, want)
				}
			}
			if err != nil {
				clientZero(t, result)
			}
		})
	}
}
func TestClientSSE(t *testing.T) {
	// R-O2D2-KPEL: CR, LF, CRLF, BOM, comments, empty data, multiline data and ignored messages.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msg := clientRequest(t, r)
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = fmt.Fprintf(w, "\ufeff:comment\r\rdata:\r\rdata: {\"jsonrpc\":\"2.0\",\"method\":\"notice\"}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":77,\"method\":\"request\"}\r\n\r\nid: ignored\r\nevent: message\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\r\ndata: \"result\":{\"content\":[]}}\r\n\r\n", msg["id"])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	result, err := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL}).CallTool(context.Background(), identity.Caller{}, "tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	clientZero(t, result)
	// R-O3KY-YH5A
	for _, data := range []string{"", "data: nonsense\n\n", `data: {"jsonrpc":"2.0","id":999,"result":{}}` + "\n\n", `data: {"jsonrpc":"2.0","method":"notice"}` + "\n\n"} {
		t.Run(data, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, data)
			}))
			defer s.Close()
			_, err := mcp.NewClient(mcp.ClientConfig{Endpoint: s.URL}).CallTool(context.Background(), identity.Caller{}, "tool", nil)
			clientPlain(t, err)
		})
	}
}
func TestClientTransportErrors(t *testing.T) {
	// R-OGZV-5YAX
	dial := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("unreachable")}
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, dial} {
		c := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://backend.test", HTTPClient: &http.Client{Transport: clientTransport(func(*http.Request) (*http.Response, error) { return nil, failure })}})
		for _, call := range []bool{false, true} {
			var err error
			if call {
				_, err = c.CallTool(context.Background(), identity.Caller{}, "tool", nil)
			} else {
				_, err = c.ListTools(context.Background(), identity.Caller{})
			}
			if !errors.Is(err, failure) {
				t.Fatalf("lost failure: %v", err)
			}
			if errors.Is(failure, dial) {
				var got *net.OpError
				if !errors.As(err, &got) || got.Op != "dial" {
					t.Fatal("lost dial error")
				}
			}
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := context.Canceled
		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background() /* already expired, without sleeping */, time.Unix(0, 0))
			want = context.DeadlineExceeded
		} else {
			cancel()
		}
		_, err := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL}).ListTools(ctx, identity.Caller{})
		cancel()
		if !errors.Is(err, want) {
			t.Fatalf("context failure %v", err)
		}
	}
}
func TestClientConcurrent(t *testing.T) {
	// R-OKNK-B9J0 R-NSLV-IJH1
	var mu sync.Mutex
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msg := clientRequest(t, r)
		id := string(msg["id"])
		mu.Lock()
		if seen[id] {
			t.Error("duplicate id")
		}
		seen[id] = true
		mu.Unlock()
		var method string
		_ = json.Unmarshal(msg["method"], &method)
		if method == "tools/list" {
			clientReply(w, msg["id"], `{"tools":[{"name":"tool","inputSchema":{}}]}`)
		} else {
			clientReply(w, msg["id"], `{"content":[]}`)
		}
	}))
	defer server.Close()
	c := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL})
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Go(func() {
			if i%2 == 0 {
				tools, err := c.ListTools(context.Background(), identity.Caller{UserID: strconv.Itoa(i)})
				if err != nil || len(tools) != 1 || tools[0].Name != "tool" {
					t.Errorf("concurrent list: %v %v", tools, err)
				}
			} else {
				result, err := c.CallTool(context.Background(), identity.Caller{UserID: strconv.Itoa(i)}, "tool", nil)
				if err != nil {
					t.Error(err)
				}
				raw, _ := result.MarshalJSON()
				if string(raw) != `{"content":[]}` {
					t.Error("concurrent result")
				}
			}
		})
	}
	group.Wait()
	if len(seen) != 32 {
		t.Fatalf("request count %d", len(seen))
	}
}

func TestClientServerCrossing(t *testing.T) {
	caller := identity.Caller{UserID: "user", Email: "email", RequestID: "request"}
	backend := mcp.NewServer(mcp.ServerConfig{Name: "backend"})
	mcp.AddRawTool(backend, mcp.RawTool[struct{}]{Name: "ping", Description: "Ping.", Effect: mcp.Read, Handler: func(_ context.Context, got identity.Caller, _ struct{}) (mcp.Result, error) {
		if got != caller {
			t.Errorf("caller crossing: %+v", got)
		}
		return mcp.TextResult("pong"), nil
	}})
	server := httptest.NewServer(identity.Require("backend", nil, backend))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client(), Name: "gateway", Version: "test"})
	tools, err := client.ListTools(context.Background(), caller)
	if err != nil || len(tools) != 1 || tools[0].Name != "ping" || tools[0].Effect() != mcp.Read {
		t.Fatalf("list crossing: %v %v", tools, err)
	}
	result, err := client.CallTool(context.Background(), caller, "ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := result.MarshalJSON()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["content"]) != `[{"type":"text","text":"pong"}]` {
		t.Fatalf("call crossing: %s", raw)
	}
}

func TestClientSSEErrorStatus(t *testing.T) {
	// R-O4SV-C8VZ
	for _, status := range []int{200, 400, 403, 404, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			msg := clientRequest(t, r)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-10,\"message\":\"denied\",\"data\":null}}\n\n", msg["id"])
		}))
		_, err := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL}).CallTool(context.Background(), identity.Caller{}, "tool", nil)
		server.Close()
		var rpc *mcp.RPCError
		if !errors.As(err, &rpc) || rpc.Code != -10 || rpc.Message != "denied" || string(rpc.Data) != "null" {
			t.Fatalf("stream RPC status %d: %v", status, err)
		}
	}
}
