package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/web"
)

type pages struct {
	calls  []string
	caller identity.Caller
}

func (p *pages) reply(w http.ResponseWriter, r *http.Request, name string, status int) {
	p.calls = append(p.calls, name)
	p.caller, _ = identity.FromContext(r.Context())
	w.Header().Set("Content-Type", "test/page")
	w.WriteHeader(status)
	if r.Method != "HEAD" {
		_, _ = io.Copy(w, r.Body)
		_, _ = io.WriteString(w, name)
	}
}
func (p *pages) Landing(w http.ResponseWriter, r *http.Request)  { p.reply(w, r, "landing", 200) }
func (p *pages) About(w http.ResponseWriter, r *http.Request)    { p.reply(w, r, "about", 200) }
func (p *pages) NotFound(w http.ResponseWriter, r *http.Request) { p.reply(w, r, "missing", 404) }

type sink struct{ err error }

func (s sink) Deliver(context.Context, events.Event) error { return s.err }
func fixture(t *testing.T) (http.Handler, *pages, *telemetry.Writer, *telemetry.Capture, *mcp.Server) {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	c := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "events", Version: "test", Sink: c, Stderr: io.Discard, Now: func() time.Time { return time.Unix(100, 0) }, Rand: bytes.NewReader(make([]byte, 65536)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	p := &pages{}
	srv := mcp.NewServer(mcp.ServerConfig{Name: "events", Version: "test", Telemetry: w, Instructions: func(context.Context) string { return "" }})
	var _ web.Pages = p
	return web.Handler(web.Config{Pages: p, MCP: srv, Sink: sink{}, Telemetry: w}), p, w, c, srv
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader("input"))
	r.Host = "backend"
	r.Header["X-User-Id"] = []string{user, "ignored"}
	r.Header.Set("X-User-Email", "first@example.test")
	r.Header.Set("X-Request-Id", "request-first")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	return out
}
func flush(t *testing.T, w *telemetry.Writer, c *telemetry.Capture) []telemetry.Event {
	t.Helper()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c.Events()
}

// R-B02J-SDCA R-8JCP-V1XF R-8KKM-8TO4 R-9Q17-90VQ R-9R93-MSMF
func TestIdentityBeforeRoutes(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/about", "/mcp", "/nope", "/_appkit/theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			out := request(h, method, path, "")
			want := identity.MissingBody
			if method == "HEAD" {
				want = ""
			}
			if out.Code != 500 || out.Body.String() != want || !reflect.DeepEqual(out.Header(), http.Header{"Content-Type": {"text/plain; charset=utf-8"}}) {
				t.Fatalf("%s %s: %d %v %q", method, path, out.Code, out.Header(), out.Body.String())
			}
		}
	}
	if len(p.calls) != 0 {
		t.Fatal(p.calls)
	}
}

// R-CTX4-FZDS R-CXKT-LALV R-D00M-CU39 R-D18I-QLTY R-DFVB-BUQA R-DH37-PMGZ
func TestRoutesAndCaller(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	for _, tc := range []struct {
		path, name string
		code       int
	}{{"/?q=x", "landing", 200}, {"/about?q=x", "about", 200}, {"/mcp/", "missing", 404}, {"/mcp/tools", "missing", 404}, {"/emit/", "missing", 404}, {"/about/", "missing", 404}, {"/assets/theme.css", "missing", 404}, {"/_appkit", "missing", 404}, {"//", "missing", 404}} {
		for _, method := range []string{"GET", "HEAD"} {
			before := len(p.calls)
			out := request(h, method, tc.path, "user-first")
			if out.Code != tc.code || len(p.calls) != before+1 || p.calls[before] != tc.name || out.Header().Get("Location") != "" || out.Header().Get("Set-Cookie") != "" {
				t.Fatalf("%s: %d %v", tc.path, out.Code, out.Header())
			}
			if p.caller != (identity.Caller{UserID: "user-first", Email: "first@example.test", RequestID: "request-first"}) {
				t.Fatal(p.caller)
			}
		}
	}
	for _, path := range []string{"/", "/about"} {
		for _, method := range []string{"POST", "DELETE", "PUT"} {
			before := len(p.calls)
			out := request(h, method, path, "user")
			if out.Code != 405 || out.Header().Get("Allow") != "GET, HEAD" || out.Body.Len() != 0 || len(p.calls) != before {
				t.Fatal(out)
			}
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-User-Id", "user")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if out.Code != 200 {
		t.Fatal(out.Code)
	}
}

// R-CV50-TR4H R-CWCX-7IV6
func TestEmitBypass(t *testing.T) {
	h, _, w, c, _ := fixture(t)
	body := `{"id":"evt_0000000000000001","time":"2026-01-01T00:00:00.000000Z","service":"repos","event":"repo.pushed","request_id":"request","user":"user","attrs":{},"cause":"","depth":0}`
	for _, method := range []string{"GET", "POST", "HEAD"} {
		r := httptest.NewRequest(method, "/emit", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		actual := httptest.NewRecorder()
		h.ServeHTTP(actual, r)
		r2 := httptest.NewRequest(method, "/emit", strings.NewReader(body))
		r2.Header.Set("Content-Type", "application/json")
		expected := httptest.NewRecorder()
		events.EmitHandler(sink{}).ServeHTTP(expected, r2)
		if actual.Code != expected.Code || actual.Body.String() != expected.Body.String() || !reflect.DeepEqual(actual.Header(), expected.Header()) || actual.Header().Get("Set-Cookie") != "" {
			t.Fatal(actual, expected)
		}
	}
	for _, failure := range []error{errors.Join(events.ErrRejected, errors.New("refused")), errors.New("unavailable")} {
		routed := web.Handler(web.Config{Sink: sink{err: failure}, Telemetry: w})
		r := httptest.NewRequest("POST", "/emit", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		routed.ServeHTTP(out, r)
		want := 500
		if errors.Is(failure, events.ErrRejected) {
			want = 422
		}
		if out.Code != want || out.Body.Len() != 0 || out.Header().Get("Set-Cookie") != "" {
			t.Fatal(out)
		}
	}
	if len(flush(t, w, c)) != 0 {
		t.Fatal(c.Events())
	}
}

// R-D2GF-4DKN
func TestMCPDelegation(t *testing.T) {
	h, _, _, _, srv := fixture(t)
	for _, method := range []string{"GET", "DELETE", "POST"} {
		r := httptest.NewRequest(method, "/mcp?x=y", strings.NewReader(`{}`))
		identity.Forward(identity.Caller{UserID: "u", RequestID: "r"}, r)
		actual := httptest.NewRecorder()
		h.ServeHTTP(actual, r)
		r2 := httptest.NewRequest(method, "/mcp?x=y", strings.NewReader(`{}`))
		expected := httptest.NewRecorder()
		srv.ServeHTTP(expected, r2.WithContext(identity.NewContext(r2.Context(), identity.Caller{UserID: "u", RequestID: "r"})))
		if actual.Code != expected.Code || !reflect.DeepEqual(actual.Header(), expected.Header()) || actual.Body.String() != expected.Body.String() {
			t.Fatal(actual, expected)
		}
	}
	h, p, _, _, srv := fixture(t)
	type echoInput struct {
		Value string `json:"value"`
	}
	type echoOutput struct {
		Value   string `json:"value"`
		User    string `json:"user"`
		Request string `json:"request"`
	}
	mcp.AddTool(srv, mcp.Tool[echoInput, echoOutput]{Name: "echo", Description: "Echo the caller.", Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, in echoInput) (echoOutput, error) {
		caller, _ := identity.FromContext(ctx)
		return echoOutput{Value: in.Value, User: caller.UserID, Request: caller.RequestID}, nil
	}})
	host := httptest.NewServer(h)
	defer host.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL + "/mcp", HTTPClient: host.Client(), Name: "test", Version: "test"})
	caller := identity.Caller{UserID: "signed-user", Email: "signed@example.test", RequestID: "signed-request"}
	list, err := client.ListTools(context.Background(), caller)
	if err != nil || len(list) != 1 || list[0].Name != "echo" {
		t.Fatal(list, err)
	}
	result, err := client.CallTool(context.Background(), caller, "echo", json.RawMessage(`{"value":"delivered"}`))
	if err != nil || result.IsError() {
		t.Fatal(result, err)
	}
	wire, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Structured echoOutput `json:"structuredContent"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Structured != (echoOutput{Value: "delivered", User: caller.UserID, Request: caller.RequestID}) {
		t.Fatal(string(wire))
	}
	if len(p.calls) != 0 {
		t.Fatal(p.calls)
	}
}

// R-D3OB-I5BC R-D4W7-VX21 R-D644-9OSQ R-D7C0-NGJF R-D8JX-18A4 R-D9RT-F00T R-P12O-UL4K R-DC7M-6JI7 R-DDFI-KB8W R-DENE-Y2ZL
func TestSharedFiles(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	files := map[string]string{"theme.css": "text/css; charset=utf-8", "launcher.js": "text/javascript; charset=utf-8", "feedback.js": "text/javascript; charset=utf-8", "InterVariable.woff2": "font/woff2", "InterVariable-Italic.woff2": "font/woff2", "JetBrainsMono.woff2": "font/woff2", "OFL.txt": "text/plain; charset=utf-8", "TABLER-LICENSE.txt": "text/plain; charset=utf-8"}
	for name, media := range files {
		path := page.StaticPrefix + name
		plain := request(h, "GET", path, "u")
		etag := plain.Header().Get("ETag")
		if plain.Code != 200 || plain.Body.Len() == 0 || plain.Header().Get("Content-Type") != media || len(plain.Header().Values("ETag")) != 1 || len(etag) < 2 || etag[0] != '"' || etag[len(etag)-1] != '"' || plain.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal(name, plain.Code, plain.Header())
		}
		for _, b := range []byte(etag[1 : len(etag)-1]) {
			if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
				t.Fatal(etag)
			}
		}
		second := request(h, "GET", path, "u")
		if second.Body.String() != plain.Body.String() || second.Header().Get("ETag") != etag {
			t.Fatal(name)
		}
		head := request(h, "HEAD", path, "u")
		if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Type") != media || head.Header().Get("ETag") != etag || head.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal(head)
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, tag := range []string{"*", `"other", W/` + etag + ", "} {
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("X-User-Id", "u")
				r.Header.Set("If-None-Match", tag)
				r.Header.Set("If-Modified-Since", "Sun, 06 Nov 2094 08:49:37 GMT")
				out := httptest.NewRecorder()
				h.ServeHTTP(out, r)
				if out.Code != 304 || out.Body.Len() != 0 || out.Header().Get("ETag") != etag {
					t.Fatal(out)
				}
			}
		}
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-User-Id", "u")
		r.Header.Set("If-None-Match", `W/"other"`)
		r.Header.Set("If-Modified-Since", "Sun, 06 Nov 2094 08:49:37 GMT")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		if out.Code != 200 || out.Body.String() != plain.Body.String() || out.Header().Get("ETag") != etag || out.Header().Get("Content-Type") != media {
			t.Fatal(out)
		}
		out = request(h, "POST", path, "u")
		if out.Code != 405 || out.Header().Get("Allow") != "GET, HEAD" {
			t.Fatal(out)
		}
	}
	for _, path := range []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/nope.css", "/_appkit/theme.css/", "/_appkit/theme.css/x", "/_appkit/THEME.CSS"} {
		for _, method := range []string{"GET", "POST"} {
			out := request(h, method, path, "u")
			if out.Code != 404 {
				t.Fatal(path, out)
			}
		}
	}
	if len(p.calls) != 0 {
		t.Fatal(p.calls)
	}
}

// R-EYWG-JUFR R-F04C-XM6G R-F1C9-BDX5
func TestRequestTrail(t *testing.T) {
	h, p, w, c, _ := fixture(t)
	for _, tc := range []struct{ method, path, user, id string }{{"GET", "/?q=x", "user", "explicit"}, {"HEAD", "/", "user", ""}, {"POST", "/about", "", ""}, {"GET", "/nope", "user", ""}} {
		before := len(flush(t, w, c))
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader("input"))
		r.Header.Set("X-User-Id", tc.user)
		r.Header.Set("X-Request-Id", tc.id)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		es := flush(t, w, c)[before:]
		if len(es) != 2 || es[0].Name != "request.started" || es[1].Name != "request.finished" || es[0].RequestID == "" || es[0].RequestID != es[1].RequestID || es[0].User != tc.user || es[1].User != tc.user {
			t.Fatal(es)
		}
		if tc.id != "" && es[0].RequestID != tc.id {
			t.Fatal(es)
		}
		if !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"method": tc.method, "path": r.URL.Path}) {
			t.Fatal(es[0])
		}
		read := int64(0)
		if tc.method == "GET" && tc.user != "" {
			read = 5
			if p.caller.RequestID != es[0].RequestID {
				t.Fatal(p.caller, es)
			}
		}
		want := telemetry.Attrs{"status": int64(out.Code), "duration_us": int64(0), "request_bytes": read, "response_bytes": int64(out.Body.Len())}
		if !reflect.DeepEqual(es[1].Attrs, want) {
			t.Fatal(es[1].Attrs, want)
		}
	}
}
