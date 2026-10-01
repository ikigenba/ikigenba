package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/ikigenba/ikigenba/appkit/services"
)

const serverTestVersionKey = "io.modelcontextprotocol/protocolVersion"
const serverTestCapsKey = "io.modelcontextprotocol/clientCapabilities"
const serverTestInfoKey = "io.modelcontextprotocol/serverInfo"

func serverTestNew() *mcp.Server {
	return mcp.NewServer(mcp.ServerConfig{Name: "example", Version: "test-version"})
}

func serverTestRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(identity.NewContext(r.Context(), identity.Caller{UserID: "person", Email: "mail", RequestID: "request"}))
}

func serverTestMessage(method string, params any) string {
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func serverTestModern(method string, params map[string]any) *http.Request {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{serverTestVersionKey: mcp.ProtocolVersion, serverTestCapsKey: map[string]any{}}
	r := serverTestRequest(serverTestMessage(method, params))
	r.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
	r.Header.Set("Mcp-Method", method)
	if name, ok := params["name"].(string); ok {
		r.Header.Set("Mcp-Name", name)
	}
	return r
}

func serverTestServe(s *mcp.Server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func serverTestObject(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		t.Fatalf("invalid object %s: %v", data, err)
	}
	return obj
}

func serverTestResult(t *testing.T, w *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	return serverTestObject(t, serverTestObject(t, w.Body.Bytes())["result"])
}

func serverTestError(t *testing.T, w *httptest.ResponseRecorder, status, code int) map[string]json.RawMessage {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d: %s", w.Code, status, w.Body.String())
	}
	obj := serverTestObject(t, serverTestObject(t, w.Body.Bytes())["error"])
	if string(obj["code"]) != fmt.Sprint(code) {
		t.Fatalf("code = %s, want %d", obj["code"], code)
	}
	return obj
}

func TestServerPublicContract(t *testing.T) {
	// R-HPHM-R3GT R-HNEH-0JCI R-HOMD-EB37: external consumer uses the declared package and signatures.
	contract := struct {
		Create       func(mcp.ServerConfig) *mcp.Server
		Instructions func(context.Context) string
		Stderr       io.Writer
	}{mcp.NewServer, func(context.Context) string { return "hello" }, &bytes.Buffer{}}
	s := contract.Create(mcp.ServerConfig{"example", "test-version", contract.Stderr, contract.Instructions})
	var handler http.Handler = s
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, serverTestRequest(serverTestMessage("initialize", map[string]any{})))
	if got := string(serverTestResult(t, w)["instructions"]); got != `"hello"` {
		t.Fatal(got)
	}
	// R-HPU9-S2TW R-HSA2-JMBA R-HTHY-XE1Z: constant values usable by a consumer.
	const body string = mcp.MissingCallerBody
	const version string = mcp.ProtocolVersion
	var codes = [...]int{mcp.CodeParseError, mcp.CodeInvalidRequest, mcp.CodeMethodNotFound, mcp.CodeInvalidParams, mcp.CodeInternalError, mcp.CodeHeaderMismatch, mcp.CodeUnsupportedProtocolVersion}
	var wideCodes = [...]int64{mcp.CodeParseError, mcp.CodeInvalidRequest, mcp.CodeMethodNotFound, mcp.CodeInvalidParams, mcp.CodeInternalError, mcp.CodeHeaderMismatch, mcp.CodeUnsupportedProtocolVersion}
	if wideCodes != [7]int64{-32700, -32600, -32601, -32602, -32603, -32020, -32022} {
		t.Fatal(wideCodes)
	}
	if body != "identity middleware missing\n" || version != "2026-07-28" || codes != [7]int{-32700, -32600, -32601, -32602, -32603, -32020, -32022} {
		t.Fatal(body, version, codes)
	}
}

func TestServerEmptyName(t *testing.T) {
	// R-HUPV-B5SO
	defer func() {
		if p := recover(); p == nil || !strings.Contains(fmt.Sprint(p), "name") || !strings.Contains(fmt.Sprint(p), "empty") {
			t.Fatalf("panic = %v", p)
		}
	}()
	mcp.NewServer(mcp.ServerConfig{})
}

type serverTestWrites struct{ writes []string }

func (w *serverTestWrites) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	return len(p), nil
}

func TestServerMissingCaller(t *testing.T) {
	// R-4332-Q1AR R-44AZ-3T1G R-46QR-VCIU
	for _, method := range []string{"POST", "HEAD", "GET"} {
		for _, id := range []string{"", "first"} {
			log := &serverTestWrites{}
			called := false
			s := mcp.NewServer(mcp.ServerConfig{Name: "example", Stderr: log, Instructions: func(context.Context) string { called = true; return "" }})
			mcp.AddRawTool(s, mcp.RawTool[struct{}]{Name: "tool", Description: "Tool.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) {
				called = true
				return mcp.Result{}, nil
			}})
			for _, body := range []string{serverTestMessage("initialize", map[string]any{}), serverTestMessage("tools/call", map[string]any{"name": "tool"}), "invalid"} {
				r := httptest.NewRequest(method, "http://example.test", strings.NewReader(body))
				r.Header["X-Request-Id"] = []string{id, "second"}
				r.Header.Set("Origin", "null")
				w := serverTestServe(s, r)
				want := mcp.MissingCallerBody
				if method == "HEAD" {
					want = ""
				}
				if w.Code != 500 || w.Body.String() != want || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || called {
					t.Fatalf("missing caller response: %d %v %q called=%v", w.Code, w.Header(), w.Body.String(), called)
				}
			}
			wantID := id
			if wantID == "" {
				wantID = "-"
			}
			if len(log.writes) != 3 {
				t.Fatal(log.writes)
			}
			for _, line := range log.writes {
				if line != "example: request "+wantID+": identity middleware missing\n" {
					t.Fatal(line)
				}
			}
		}
	}
}

func TestServerNilStderr(t *testing.T) {
	// R-HVXR-OXJD
	var log bytes.Buffer
	a := mcp.NewServer(mcp.ServerConfig{Name: "example"})
	b := mcp.NewServer(mcp.ServerConfig{Name: "example", Stderr: &log})
	for _, r := range []*http.Request{httptest.NewRequest("POST", "/", nil), serverTestRequest(serverTestMessage("ping", map[string]any{}))} {
		var body []byte
		if r.Body != nil {
			var err error
			body, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		left := serverTestServe(a, r)
		r.Body = io.NopCloser(bytes.NewReader(body))
		right := serverTestServe(b, r)
		if left.Code != right.Code || !reflect.DeepEqual(left.Header(), right.Header()) || left.Body.String() != right.Body.String() {
			t.Fatal("nil writer changes response")
		}
	}
}

func TestServerTransport(t *testing.T) {
	// R-I395-ZJZJ
	for _, method := range []string{"GET", "HEAD", "DELETE", "PUT", "PATCH", "OPTIONS", "CUSTOM"} {
		r := serverTestRequest("invalid")
		r.Method = method
		w := serverTestServe(serverTestNew(), r)
		if w.Code != 405 || w.Body.Len() != 0 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"POST"}) {
			t.Fatalf("%s: %d %v %s", method, w.Code, w.Header(), w.Body.String())
		}
	}
	// R-I4H2-DBQ8
	for _, origins := range [][]string{nil, {"http://EXAMPLE.test"}, {"custom://example.test"}, {"null"}, {"://example.test"}, {"http://else"}, {"http://example.test", "http://else"}, {"http://example.test/"}} {
		r := serverTestRequest(serverTestMessage("ping", map[string]any{}))
		r.Header["Origin"] = origins
		w := serverTestServe(serverTestNew(), r)
		want := 403
		if len(origins) == 0 || len(origins) == 1 && (origins[0] == "http://EXAMPLE.test" || origins[0] == "custom://example.test") {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%v: %d", origins, w.Code)
		}
	}
	// R-I5OY-R3GX
	for _, media := range []string{"", "text/plain", "broken;", "application/json", "Application/JSON; charset=utf-8"} {
		r := serverTestRequest(serverTestMessage("ping", map[string]any{}))
		r.Header.Set("Content-Type", media)
		w := serverTestServe(serverTestNew(), r)
		want := 415
		if strings.HasPrefix(strings.ToLower(media), "application/json") {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%q: %d", media, w.Code)
		}
	}
	// R-I6WV-4V7M
	for _, length := range []int{1048576, 1048577} {
		for _, declared := range []int64{-1, int64(length)} {
			called := false
			s := serverTestNew()
			mcp.AddRawTool(s, mcp.RawTool[struct{}]{Name: "tool", Description: "Tool.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) {
				called = true
				return mcp.Result{}, nil
			}})
			body := serverTestMessage("tools/call", map[string]any{"name": "tool"})
			body += strings.Repeat(" ", length-len(body))
			r := serverTestRequest(body)
			r.ContentLength = declared
			w := serverTestServe(s, r)
			if length == 1048576 {
				if w.Code != 200 || !called {
					t.Fatal(w.Code, called)
				}
			} else if w.Code != 413 || called {
				t.Fatal(w.Code, called)
			}
		}
	}
}

func TestServerTransportPrecedenceAndErrors(t *testing.T) {
	// R-47YO-949J
	for stage, want := range []int{500, 405, 403, 415, 413, 400} {
		r := serverTestRequest(strings.Repeat("!", 1048577))
		r.Method = "GET"
		r.Header.Set("Origin", "null")
		r.Header.Del("Content-Type")
		if stage == 0 {
			r = r.WithContext(context.Background())
		}
		if stage >= 2 {
			r.Method = "POST"
		}
		if stage >= 3 {
			r.Header.Del("Origin")
		}
		if stage >= 4 {
			r.Header.Set("Content-Type", "application/json")
		}
		if stage >= 5 {
			r.Body = io.NopCloser(strings.NewReader("invalid"))
		}
		w := serverTestServe(serverTestNew(), r)
		if w.Code != want {
			t.Fatalf("stage %d: %d", stage, w.Code)
		}
	}
	// R-IAKK-A6FP
	for _, tc := range []struct {
		status  int
		message string
		change  func(*http.Request)
	}{{403, "Origin not allowed", func(r *http.Request) { r.Header.Set("Origin", "null") }}, {415, "Content-Type must be application/json", func(r *http.Request) { r.Header.Del("Content-Type") }}, {413, "Request body too large", func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(strings.Repeat("!", 1048577))) }}} {
		r := serverTestRequest("invalid")
		tc.change(r)
		w := serverTestServe(serverTestNew(), r)
		errorObj := serverTestError(t, w, tc.status, mcp.CodeInvalidRequest)
		obj := serverTestObject(t, w.Body.Bytes())
		if len(obj) != 2 || string(obj["jsonrpc"]) != `"2.0"` || len(errorObj) != 2 || string(errorObj["message"]) != string(serverTestJSON(tc.message)) || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
			t.Fatal(w.Body.String(), w.Header())
		}
	}
}

func serverTestJSON(value any) []byte {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return b
}

func TestServerParsing(t *testing.T) {
	// R-IBSG-NY6E
	for _, body := range []string{"", "{", "{} {}", "{} garbage", "\xef\xbb\xbf{}"} {
		w := serverTestServe(serverTestNew(), serverTestRequest(body))
		serverTestError(t, w, 400, mcp.CodeParseError)
		if _, present := serverTestObject(t, w.Body.Bytes())["id"]; present {
			t.Fatal(w.Body.String())
		}
	}
	// R-ID0D-1PX3 R-IE89-FHNS
	for _, body := range []string{`[]`, `[{}]`, `null`, `true`, `1`, `"text"`, `{}`, `{"jsonrpc":"1.0","id":7,"method":"ping"}`, `{"jsonrpc":"2.0","id":null,"method":"ping"}`, `{"jsonrpc":"2.0","id":true,"method":"ping"}`, `{"jsonrpc":"2.0","id":1.5,"method":"ping"}`, `{"jsonrpc":"2.0","id":1e0,"method":"ping"}`, `{"jsonrpc":"2.0","id":{},"method":"ping"}`, `{"jsonrpc":"2.0","id":[],"method":"ping"}`, `{"jsonrpc":"2.0","id":"valid","method":5}`, `{"jsonrpc":"2.0","id":7,"method":"ping","params":null}`, `{"jsonrpc":"2.0","method":"ping","params":[]}`} {
		w := serverTestServe(serverTestNew(), serverTestRequest(body))
		serverTestError(t, w, 400, mcp.CodeInvalidRequest)
		var in map[string]json.RawMessage
		_ = json.Unmarshal([]byte(body), &in)
		got := serverTestObject(t, w.Body.Bytes())["id"]
		want := []byte(nil)
		if string(in["id"]) == `"valid"` || string(in["id"]) == "7" {
			want = in["id"]
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: id=%s want=%s", body, got, want)
		}
	}
	for _, body := range []string{`{"jsonrpc":"2.0","id":-7,"method":"ping"}`, `{"jsonrpc":"2.0","id":"text","method":"ping","params":{}}`, `{"jsonrpc":"2.0","method":"whatever"}`, `{"jsonrpc":"2.0","result":null}`, `{"jsonrpc":"2.0","error":null}`} {
		w := serverTestServe(serverTestNew(), serverTestRequest(body))
		if w.Code != 200 && w.Code != 202 {
			t.Fatal(body, w.Code)
		}
	}
}

func TestServerNotificationsAndResponses(t *testing.T) {
	// R-IFG5-T9EH
	for _, method := range []string{"notifications/initialized", "unknown", "tools/call"} {
		for _, version := range []string{"", mcp.ProtocolVersion, "bad"} {
			r := serverTestRequest(string(serverTestJSON(map[string]any{"jsonrpc": "2.0", "method": method, "params": map[string]any{"_meta": map[string]any{serverTestVersionKey: "bad"}}})))
			r.Header.Set("MCP-Protocol-Version", version)
			r.Header.Set("Mcp-Method", "bad")
			r.Header.Set("Mcp-Name", "bad")
			w := serverTestServe(serverTestNew(), r)
			if w.Code != 202 || w.Body.Len() != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	// R-IGO2-7156
	for _, kind := range []string{"result", "error"} {
		for _, version := range []string{"absent", "2025-11-25", "2025-06-18", mcp.ProtocolVersion, "bad", ""} {
			r := serverTestRequest(fmt.Sprintf(`{"jsonrpc":"2.0","id":8,%q:{}}`, kind))
			if version != "absent" {
				r.Header.Set("MCP-Protocol-Version", version)
			}
			w := serverTestServe(serverTestNew(), r)
			if version == "absent" || strings.HasPrefix(version, "2025-") {
				if w.Code != 202 || w.Body.Len() != 0 {
					t.Fatal(w.Code, w.Body.String())
				}
			} else {
				serverTestError(t, w, 400, mcp.CodeInvalidRequest)
				if _, present := serverTestObject(t, w.Body.Bytes())["id"]; present {
					t.Fatal(w.Body.String())
				}
			}
		}
	}
}

func TestServerResponseEnvelope(t *testing.T) {
	// R-IHVY-KSVV R-IJ3U-YKMK
	for _, id := range []string{`"string"`, `-9007199254740993`, `0`} {
		for _, method := range []string{"ping", "unknown"} {
			for _, accept := range []string{"", "text/event-stream", "nonsense"} {
				r := serverTestRequest(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":%q}`, id, method))
				r.Header.Set("Accept", accept)
				w := serverTestServe(serverTestNew(), r)
				obj := serverTestObject(t, w.Body.Bytes())
				if len(obj) != 3 || string(obj["jsonrpc"]) != `"2.0"` || string(obj["id"]) != id || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
					t.Fatal(w.Body.String(), w.Header())
				}
				result, hasResult := obj["result"]
				failure, hasError := obj["error"]
				if hasResult == hasError {
					t.Fatal(obj)
				}
				if hasResult {
					serverTestObject(t, result)
				} else {
					e := serverTestObject(t, failure)
					var c int
					var m string
					if json.Unmarshal(e["code"], &c) != nil || json.Unmarshal(e["message"], &m) != nil {
						t.Fatal(e)
					}
				}
			}
		}
	}
}

func TestServerPathAndStatelessness(t *testing.T) {
	// R-I219-LS8U R-IKBR-CCD9
	body := serverTestMessage("ping", map[string]any{})
	baseline := serverTestServe(serverTestNew(), serverTestRequest(body))
	for _, path := range []string{"/", "/mcp/", "/else?thing=value"} {
		r := serverTestRequest(body)
		r.URL.Path = strings.Split(path, "?")[0]
		r.URL.RawQuery = "thing=value"
		r.Header.Set("Mcp-Session-Id", "arbitrary")
		r.Header.Set("Last-Event-ID", "resume")
		w := serverTestServe(serverTestNew(), r)
		if w.Code != baseline.Code || w.Body.String() != baseline.Body.String() || !reflect.DeepEqual(w.Header(), baseline.Header()) || len(w.Header().Values("Mcp-Session-Id")) != 0 {
			t.Fatal(w)
		}
	}
	// R-J2M9-2WHO R-J3U5-GO8D R-4CU9-S78B
	s := serverTestNew()
	for _, method := range []string{"ping", "tools/list", "tools/call", "unknown"} {
		params := map[string]any{}
		if method == "tools/call" {
			params["name"] = "missing"
		}
		before := serverTestServe(s, serverTestRequest(serverTestMessage(method, params)))
		serverTestServe(s, serverTestRequest(serverTestMessage("initialize", map[string]any{})))
		for _, version := range []string{"absent", "2025-11-25", "2025-06-18"} {
			r := serverTestRequest(serverTestMessage(method, params))
			if version != "absent" {
				r.Header.Set("MCP-Protocol-Version", version)
			}
			after := serverTestServe(s, r)
			if before.Code != after.Code || before.Body.String() != after.Body.String() || !reflect.DeepEqual(before.Header(), after.Header()) {
				t.Fatal(method, version, before.Body.String(), after.Body.String())
			}
		}
	}
}

func TestServerVersionSelection(t *testing.T) {
	// R-ILJN-Q43Y R-INZG-HNLC
	r := serverTestModern("initialize", nil)
	serverTestError(t, serverTestServe(serverTestNew(), r), 404, mcp.CodeMethodNotFound)
	r = serverTestRequest(serverTestMessage("initialize", map[string]any{}))
	r.Header.Set("MCP-Protocol-Version", "unsupported")
	if w := serverTestServe(serverTestNew(), r); w.Code != 200 {
		t.Fatal(w.Code)
	}
	// R-IMRK-3VUN
	for _, version := range []any{"other", nil, 5, true, []any{}, map[string]any{}} {
		r := serverTestRequest(serverTestMessage("tools/list", map[string]any{"_meta": map[string]any{serverTestVersionKey: version}}))
		w := serverTestServe(serverTestNew(), r)
		if text, ok := version.(string); ok {
			e := serverTestError(t, w, 400, mcp.CodeUnsupportedProtocolVersion)
			serverTestVersionData(t, e, text)
		} else {
			serverTestError(t, w, 400, mcp.CodeInvalidParams)
		}
	}
	// R-4E26-5YZ0 R-4FA2-JQPP
	for _, version := range []string{mcp.ProtocolVersion, "unsupported", ""} {
		r := serverTestRequest(serverTestMessage("ping", map[string]any{}))
		r.Header.Set("MCP-Protocol-Version", version)
		w := serverTestServe(serverTestNew(), r)
		if version == mcp.ProtocolVersion {
			serverTestError(t, w, 400, mcp.CodeInvalidParams)
		} else {
			e := serverTestError(t, w, 400, mcp.CodeUnsupportedProtocolVersion)
			serverTestVersionData(t, e, version)
		}
	}
}

func serverTestVersionData(t *testing.T, e map[string]json.RawMessage, requested string) {
	t.Helper()
	want := map[string]any{"supported": []string{mcp.ProtocolVersion, "2025-11-25", "2025-06-18"}, "requested": requested}
	if string(e["message"]) != `"Unsupported protocol version"` || !serverTestEqualJSON(e["data"], serverTestJSON(want)) {
		t.Fatal(e)
	}
}
func serverTestEqualJSON(a, b []byte) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

func TestServerModernMetadata(t *testing.T) {
	// R-IQF9-972Q
	for _, caps := range []any{"absent", nil, true, 1, "text", []any{}} {
		meta := map[string]any{serverTestVersionKey: mcp.ProtocolVersion}
		if caps != "absent" {
			meta[serverTestCapsKey] = caps
		}
		r := serverTestRequest(serverTestMessage("tools/list", map[string]any{"_meta": meta}))
		serverTestError(t, serverTestServe(serverTestNew(), r), 400, mcp.CodeInvalidParams)
	}
	// R-IRN5-MYTF R-IU2Y-EIAT
	for _, header := range []string{"MCP-Protocol-Version", "Mcp-Method"} {
		for _, value := range []string{"absent", "wrong"} {
			r := serverTestModern("tools/list", nil)
			if value == "absent" {
				r.Header.Del(header)
			} else {
				r.Header.Set(header, value)
			}
			serverTestError(t, serverTestServe(serverTestNew(), r), 400, mcp.CodeHeaderMismatch)
		}
	}
	// R-8MUI-3SL1
	for _, name := range []string{"tool", "unicode_世界", "=?base64?literal?="} {
		for _, encoding := range []string{"literal", "encoded", "absent", "wrong"} {
			r := serverTestModern("tools/call", map[string]any{"name": name})
			switch encoding {
			case "encoded":
				r.Header.Set("Mcp-Name", "=?base64?"+base64.StdEncoding.EncodeToString([]byte(name))+"?=")
			case "absent":
				r.Header.Del("Mcp-Name")
			case "wrong":
				r.Header.Set("Mcp-Name", "different")
			}
			w := serverTestServe(serverTestNew(), r)
			if encoding == "absent" || encoding == "wrong" || encoding == "literal" && (strings.HasPrefix(name, "=?base64?") || name == "unicode_世界") {
				serverTestError(t, w, 400, mcp.CodeHeaderMismatch)
			} else {
				serverTestError(t, w, 400, mcp.CodeInvalidParams)
			}
		}
	}
	// R-8QI7-93T4
	for _, header := range []string{"MCP-Protocol-Version", "Mcp-Method", "Mcp-Name"} {
		for _, value := range []string{"control\x1f", "delete\x7f", "世界"} {
			r := serverTestModern("tools/call", map[string]any{"name": "tool"})
			r.Header.Set(header, value)
			serverTestError(t, serverTestServe(serverTestNew(), r), 400, mcp.CodeHeaderMismatch)
		}
	}
	for _, value := range []string{"=?base64?%%%?=", "=?base64?SGVsbG8?=", "=?base64?_w==?=", "=?base64?/w==?="} {
		r := serverTestModern("tools/call", map[string]any{"name": "tool"})
		r.Header.Set("Mcp-Name", value)
		serverTestError(t, serverTestServe(serverTestNew(), r), 400, mcp.CodeHeaderMismatch)
	}
}

func TestServerModernPrecedence(t *testing.T) {
	// R-8O2E-HKBQ
	for _, tc := range []struct {
		version              any
		caps                 any
		method, header, name string
		code                 int
	}{{"bad", nil, "bad", "\x01", "bad", mcp.CodeUnsupportedProtocolVersion}, {mcp.ProtocolVersion, nil, "bad", "\x01", "bad", mcp.CodeInvalidParams}, {mcp.ProtocolVersion, map[string]any{}, "bad", "\x01", "bad", mcp.CodeHeaderMismatch}, {mcp.ProtocolVersion, map[string]any{}, "bad", "wrong", "bad", mcp.CodeHeaderMismatch}, {mcp.ProtocolVersion, map[string]any{}, "bad", mcp.ProtocolVersion, "bad", mcp.CodeMethodNotFound}} {
		r := serverTestRequest(serverTestMessage(tc.method, map[string]any{"cursor": nil, "name": tc.name, "_meta": map[string]any{serverTestVersionKey: tc.version, serverTestCapsKey: tc.caps}}))
		r.Header.Set("MCP-Protocol-Version", tc.header)
		r.Header.Set("Mcp-Method", tc.method)
		r.Header.Set("Mcp-Name", "wrong")
		status := 400
		if tc.code == mcp.CodeMethodNotFound {
			status = 404
		}
		serverTestError(t, serverTestServe(serverTestNew(), r), status, tc.code)
	}
	for _, method := range []string{"tools/list", "tools/call"} {
		r := serverTestModern(method, map[string]any{"cursor": true, "name": nil})
		r.Header.Set("Mcp-Method", "wrong")
		serverTestError(t, serverTestServe(serverTestNew(), r), 400, mcp.CodeHeaderMismatch)
	}
}

func TestServerMethodsAndStatus(t *testing.T) {
	// R-IXQN-JTIW R-IYYJ-XL9L R-J06G-BD0A R-J1EC-P4QZ
	for _, modern := range []bool{false, true} {
		for _, method := range []string{"unknown", "ping", "initialize", "server/discover", "tools/list", "tools/call"} {
			params := map[string]any{}
			r := serverTestRequest(serverTestMessage(method, params))
			if modern {
				r = serverTestModern(method, params)
			}
			w := serverTestServe(serverTestNew(), r)
			unknown := method == "unknown" || modern && (method == "ping" || method == "initialize") || !modern && method == "server/discover"
			switch {
			case unknown:
				status := 200
				if modern {
					status = 404
				}
				serverTestError(t, w, status, mcp.CodeMethodNotFound)
			case method == "tools/call":
				status := 200
				if modern {
					status = 400
				}
				serverTestError(t, w, status, mcp.CodeInvalidParams)
			case w.Code != 200:
				t.Fatal(modern, method, w.Code)
			}
		}
	}
}

func TestServerInitializationAndDiscovery(t *testing.T) {
	// R-496K-MW08 R-J8PQ-ZR75 R-J9XN-DIXU R-4AEH-0NQX
	for _, instructions := range []string{"", "Instructions."} {
		s := mcp.NewServer(mcp.ServerConfig{Name: "example", Version: "test-version", Instructions: func(context.Context) string { return instructions }})
		for _, version := range []any{"absent", "2025-11-25", "2025-06-18", "other", nil, 7} {
			params := map[string]any{}
			if version != "absent" {
				params["protocolVersion"] = version
			}
			w := serverTestServe(s, serverTestRequest(serverTestMessage("initialize", params)))
			wantVersion := "2025-11-25"
			if version == "2025-06-18" {
				wantVersion = "2025-06-18"
			}
			want := map[string]any{"protocolVersion": wantVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "example", "version": "test-version"}}
			if instructions != "" {
				want["instructions"] = instructions
			}
			if !serverTestEqualJSON(serverTestObject(t, w.Body.Bytes())["result"], serverTestJSON(want)) {
				t.Fatal(w.Body.String())
			}
		}
		w := serverTestServe(s, serverTestRequest(serverTestMessage("ping", map[string]any{})))
		if string(serverTestObject(t, w.Body.Bytes())["result"]) != "{}" {
			t.Fatal(w.Body.String())
		}
		w = serverTestServe(s, serverTestModern("server/discover", nil))
		want := map[string]any{"supportedVersions": []string{mcp.ProtocolVersion, "2025-11-25", "2025-06-18"}, "capabilities": map[string]any{"tools": map[string]any{}}, "ttlMs": 0, "cacheScope": "private", "resultType": "complete", "_meta": map[string]any{serverTestInfoKey: map[string]any{"name": "example", "version": "test-version"}}}
		if instructions != "" {
			want["instructions"] = instructions
		}
		if !serverTestEqualJSON(serverTestObject(t, w.Body.Bytes())["result"], serverTestJSON(want)) {
			t.Fatal(w.Body.String())
		}
	}
}

func TestServerListAndCall(t *testing.T) {
	// R-JDLC-IU5X
	for _, count := range []int{0, 3} {
		s := serverTestNew()
		names := []string{"z", "a", "middle"}
		for _, name := range names[:count] {
			mcp.AddRawTool(s, mcp.RawTool[struct{}]{Name: name, Description: "Tool.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) { return mcp.Result{}, nil }})
		}
		for _, modern := range []bool{false, true} {
			r := serverTestRequest(serverTestMessage("tools/list", map[string]any{}))
			if modern {
				r = serverTestModern("tools/list", nil)
			}
			obj := serverTestResult(t, serverTestServe(s, r))
			var tools []map[string]json.RawMessage
			if json.Unmarshal(obj["tools"], &tools) != nil || tools == nil || len(tools) != count {
				t.Fatal(obj)
			}
			for i, tool := range tools {
				if string(tool["name"]) != string(serverTestJSON(names[i])) {
					t.Fatal(tools)
				}
			}
			if _, present := obj["nextCursor"]; present {
				t.Fatal(obj)
			}
			if modern && (string(obj["ttlMs"]) != "0" || string(obj["cacheScope"]) != `"private"`) {
				t.Fatal(obj)
			}
		}
	}
	// R-JH91-O5E0
	for _, cursor := range []any{"", nil, true, 0, map[string]any{}} {
		for _, modern := range []bool{false, true} {
			params := map[string]any{"cursor": cursor}
			r := serverTestRequest(serverTestMessage("tools/list", params))
			status := 200
			if modern {
				r = serverTestModern("tools/list", params)
				status = 400
			}
			serverTestError(t, serverTestServe(serverTestNew(), r), status, mcp.CodeInvalidParams)
		}
	}
	// R-JIGY-1X4P
	s := serverTestNew()
	called := false
	mcp.AddRawTool(s, mcp.RawTool[struct{}]{Name: "tool", Description: "Tool.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) {
		called = true
		return mcp.Result{}, nil
	}})
	for _, name := range []any{"absent", nil, true, 7, []any{}, "missing"} {
		params := map[string]any{}
		if name != "absent" {
			params["name"] = name
		}
		w := serverTestServe(s, serverTestRequest(serverTestMessage("tools/call", params)))
		e := serverTestError(t, w, 200, mcp.CodeInvalidParams)
		if name == "missing" && string(e["message"]) != `"Unknown tool: missing"` {
			t.Fatal(e)
		}
		if called {
			t.Fatal("handler called")
		}
	}
}

func TestServerToolCallerAndEnvelope(t *testing.T) {
	// R-I0TD-80I5 R-8J6S-YHCY R-8KEP-C93N R-8LML-Q0UC
	for _, meta := range []string{`{"keep":{"n":1},"io.modelcontextprotocol/serverInfo":{"old":true}}`, `{"io.modelcontextprotocol/serverInfo":{}}`, `"literal"`, `null`, `absent`} {
		s := serverTestNew()
		var callers []identity.Caller
		var inputs []struct {
			Value string `json:"value"`
		}
		raw := `{"content":[{"type":"text","text":"same"}],"custom":{"a":[1,true]},"ttlMs":9,"cacheScope":"public"`
		if meta != "absent" {
			raw += `,"_meta":` + meta
		}
		raw += "}"
		var returned mcp.Result
		if err := returned.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		mcp.AddRawTool(s, mcp.RawTool[struct {
			Value string `json:"value"`
		}]{Name: "tool", Description: "Tool.", Effect: mcp.Read, Handler: func(_ context.Context, c identity.Caller, in struct {
			Value string `json:"value"`
		}) (mcp.Result, error) {
			callers = append(callers, c)
			inputs = append(inputs, in)
			return returned, nil
		}})
		var results []map[string]json.RawMessage
		for _, modern := range []bool{false, true} {
			params := map[string]any{"name": "tool", "arguments": map[string]any{"value": "input"}}
			r := serverTestRequest(serverTestMessage("tools/call", params))
			if modern {
				r = serverTestModern("tools/call", params)
			}
			w := serverTestServe(s, r)
			obj := serverTestResult(t, w)
			results = append(results, obj)
			if modern {
				if string(obj["resultType"]) != `"complete"` {
					t.Fatal(obj)
				}
				m := serverTestObject(t, obj["_meta"])
				if !serverTestEqualJSON(m[serverTestInfoKey], []byte(`{"name":"example","version":"test-version"}`)) {
					t.Fatal(m)
				}
				if strings.Contains(meta, `"keep"`) && string(m["keep"]) != `{"n":1}` {
					t.Fatal(m)
				}
				if !strings.Contains(meta, `"keep"`) && len(m) != 1 {
					t.Fatal(m)
				}
			} else {
				for _, key := range []string{"resultType", "ttlMs", "cacheScope"} {
					if _, present := obj[key]; present {
						t.Fatal(obj)
					}
				}
				switch meta {
				case `"literal"`, `null`:
					if string(obj["_meta"]) != meta {
						t.Fatal(obj)
					}
				case `absent`, `{"io.modelcontextprotocol/serverInfo":{}}`:
					if _, present := obj["_meta"]; present {
						t.Fatal(obj)
					}
				default:
					if string(obj["_meta"]) != `{"keep":{"n":1}}` {
						t.Fatal(obj)
					}
				}
			}
		}
		if !reflect.DeepEqual(callers, []identity.Caller{{UserID: "person", Email: "mail", RequestID: "request"}, {UserID: "person", Email: "mail", RequestID: "request"}}) || len(inputs) != 2 || inputs[0].Value != "input" || inputs[0] != inputs[1] {
			t.Fatal(callers, inputs)
		}
		for _, key := range []string{"content", "custom"} {
			if !bytes.Equal(results[0][key], results[1][key]) || !bytes.Equal(results[0][key], serverTestObject(t, []byte(raw))[key]) {
				t.Fatal(results)
			}
		}
	}
	// The same modern envelope is applied to every built-in successful method and typed tool output.
	s := serverTestNew()
	mcp.AddTool(s, mcp.Tool[struct{}, struct{}]{Name: "typed", Description: "Tool.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
	for _, method := range []string{"server/discover", "tools/list", "tools/call"} {
		params := map[string]any{}
		if method == "tools/call" {
			params["name"] = "typed"
		}
		obj := serverTestResult(t, serverTestServe(s, serverTestModern(method, params)))
		if string(obj["resultType"]) != `"complete"` || !serverTestEqualJSON(serverTestObject(t, obj["_meta"])[serverTestInfoKey], []byte(`{"name":"example","version":"test-version"}`)) {
			t.Fatal(obj)
		}
	}
}

func TestServerInstructionContext(t *testing.T) {
	// R-4BMD-EFHM
	type contextKey struct{}
	base, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "value"), time.Now().Add(time.Hour))
	cancel()
	caller := identity.Caller{UserID: "context-person"}
	ctx := identity.NewContext(base, caller)
	calls := 0
	s := mcp.NewServer(mcp.ServerConfig{Name: "example", Instructions: func(got context.Context) string {
		calls++
		c, ok := identity.FromContext(got)
		deadline, hasDeadline := got.Deadline()
		wantDeadline, _ := ctx.Deadline()
		if got.Value(contextKey{}) != "value" || !ok || c != caller || !hasDeadline || deadline != wantDeadline || got.Err() != context.Canceled {
			t.Error("context lost")
		}
		return fmt.Sprint(calls)
	}})
	for i, method := range []string{"initialize", "server/discover", "initialize", "server/discover"} {
		r := serverTestRequest(serverTestMessage(method, map[string]any{}))
		if method == "server/discover" {
			r = serverTestModern(method, nil)
		}
		r = r.WithContext(ctx)
		obj := serverTestResult(t, serverTestServe(s, r))
		if calls != i+1 || string(obj["instructions"]) != string(serverTestJSON(fmt.Sprint(i+1))) {
			t.Fatal(calls, obj)
		}
	}
}

func serverTestManifest(t *testing.T, path, description string) {
	t.Helper()
	body := map[string]any{"services": []any{map[string]any{"name": "example", "url": "/", "description": description, "socket": "unused", "enabled": true, "mcp": true}}}
	if err := os.WriteFile(path, serverTestJSON(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestServerManifestInstructions(t *testing.T) {
	// R-HX5O-2PA2 R-JG15-ADNB
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	serverTestManifest(t, a, "first")
	serverTestManifest(t, b, "other")
	t.Setenv(services.Variable, a)
	s := serverTestNew()
	t.Setenv(services.Variable, b)
	for i, description := range []string{"first", "changed", ""} {
		if i > 0 {
			serverTestManifest(t, a, description)
		}
		for _, method := range []string{"initialize", "server/discover"} {
			r := serverTestRequest(serverTestMessage(method, map[string]any{}))
			if method == "server/discover" {
				r = serverTestModern(method, nil)
			}
			obj := serverTestResult(t, serverTestServe(s, r))
			if description == "" {
				if _, present := obj["instructions"]; present {
					t.Fatal(obj)
				}
			} else if string(obj["instructions"]) != string(serverTestJSON(description)) {
				t.Fatal(obj)
			}
		}
		if err := os.Unsetenv(services.Variable); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"services":[]}`, `invalid`} {
		if err := os.WriteFile(a, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		obj := serverTestResult(t, serverTestServe(s, serverTestRequest(serverTestMessage("initialize", map[string]any{}))))
		if _, present := obj["instructions"]; present {
			t.Fatal(obj)
		}
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	obj := serverTestResult(t, serverTestServe(s, serverTestRequest(serverTestMessage("initialize", map[string]any{}))))
	if _, present := obj["instructions"]; present {
		t.Fatal(obj)
	}
	for _, initial := range []string{"unset", ""} {
		if initial == "unset" {
			if err := os.Unsetenv(services.Variable); err != nil {
				t.Fatal(err)
			}
		} else {
			t.Setenv(services.Variable, "")
		}
		fresh := serverTestNew()
		t.Setenv(services.Variable, b)
		for _, method := range []string{"initialize", "server/discover"} {
			r := serverTestRequest(serverTestMessage(method, map[string]any{}))
			if method == "server/discover" {
				r = serverTestModern(method, nil)
			}
			obj := serverTestResult(t, serverTestServe(fresh, r))
			if _, present := obj["instructions"]; present {
				t.Fatal(obj)
			}
		}
	}
}

func TestServerConcurrentRequests(t *testing.T) {
	// R-JKWQ-TGM3
	s := mcp.NewServer(mcp.ServerConfig{Name: "example", Instructions: func(context.Context) string { return "hello" }})
	mcp.AddRawTool(s, mcp.RawTool[struct{}]{Name: "tool", Description: "Tool.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (mcp.Result, error) { return mcp.TextResult("ok"), nil }})
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Go(func() {
			for _, method := range []string{"tools/list", "tools/call", "server/discover", "initialize"} {
				params := map[string]any{}
				if method == "tools/call" {
					params["name"] = "tool"
				}
				r := serverTestRequest(serverTestMessage(method, params))
				if method == "server/discover" {
					r = serverTestModern(method, params)
				}
				w := serverTestServe(s, r)
				if w.Code != 200 {
					t.Errorf("%s: %d %s", method, w.Code, w.Body.String())
				}
			}
		})
	}
	wg.Wait()
}
