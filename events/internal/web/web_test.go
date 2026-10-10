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
	"regexp"
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
func (p *pages) Tools(w http.ResponseWriter, r *http.Request)    { p.reply(w, r, "tools", 200) }
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

// R-DA3Y-A2PX R-8JCP-V1XF R-8KKM-8TO4 R-9Q17-90VQ R-9R93-MSMF
func TestIdentityBeforeRoutes(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/tools", "/about", "/mcp", "/nope", "/_appkit/theme.css"} {
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

// R-DBBU-NUGM R-DCJR-1M7B R-DEZJ-T5OP R-DG7G-6XFE
// R-CXKT-LALV R-DFVB-BUQA R-DH37-PMGZ
func TestRoutesAndCaller(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	for _, tc := range []struct {
		path, name string
		code       int
	}{{"/?q=x", "landing", 200}, {"/tools?q=x", "tools", 200}, {"/about?q=x", "about", 200}, {"/mcp/", "missing", 404}, {"/mcp/tools", "missing", 404}, {"/emit/", "missing", 404}, {"/about/", "missing", 404}, {"/tools/", "missing", 404}, {"/nope", "missing", 404}, {"/assets/theme.css", "missing", 404}, {"/_appkit", "missing", 404}, {"//", "missing", 404}} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "custom"} {
			if tc.code == 200 && method != "GET" && method != "HEAD" {
				continue
			}
			before := len(p.calls)
			r := httptest.NewRequest(method, tc.path, strings.NewReader("input"))
			r.Host = "unrelated.example.test"
			r.Header.Set("X-Original-URI", "/different-route")
			identity.Forward(identity.Caller{UserID: "user-first", Email: "first@example.test", RequestID: "request-first"}, r)
			out := httptest.NewRecorder()
			h.ServeHTTP(out, r)
			wantBody := "input" + tc.name
			if method == "HEAD" {
				wantBody = ""
			}
			if out.Body.String() != wantBody || out.Header().Get("Content-Type") != "test/page" || out.Code != tc.code || len(p.calls) != before+1 || p.calls[before] != tc.name || out.Header().Get("Location") != "" || out.Header().Get("Set-Cookie") != "" {
				t.Fatalf("%s: %d %v", tc.path, out.Code, out.Header())
			}
			if p.caller != (identity.Caller{UserID: "user-first", Email: "first@example.test", RequestID: "request-first"}) {
				t.Fatal(p.caller)
			}
		}
	}
	for _, path := range []string{"/", "/tools", "/about"} {
		for _, method := range []string{"POST", "DELETE", "PUT", "PATCH", "OPTIONS", "TRACE", "custom"} {
			before := len(p.calls)
			out := request(h, method, path, "user")
			if out.Code != 405 || out.Header().Get("Allow") != "GET, HEAD" || len(out.Header().Values("Allow")) != 1 || out.Header().Get("Location") != "" || out.Body.Len() != 0 || len(p.calls) != before {
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

// R-6J25-NCF9
func sharedFiles(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	files := map[string]string{"theme.css": "text/css; charset=utf-8", "feedback.js": "text/javascript; charset=utf-8", "OFL.txt": "text/plain; charset=utf-8", "TABLER-LICENSE.txt": "text/plain; charset=utf-8", "favicon.svg": "image/svg+xml"}
	css := request(h, "GET", page.StaticPrefix+"theme.css", "u")
	if css.Code != http.StatusOK {
		t.Fatalf("stylesheet discovery: status %d", css.Code)
	}
	files[strings.TrimPrefix(page.PreloadURL(), page.StaticPrefix)] = "font/woff2"
	for _, match := range regexp.MustCompile(`url\("([^"]*)"\)`).FindAllStringSubmatch(css.Body.String(), -1) {
		name := match[1]
		if strings.HasSuffix(name, ".woff2") && !strings.ContainsAny(name, "/\\:\"?#%") {
			files[name] = "font/woff2"
		}
	}
	return files
}

// R-6KA2-145Y R-F55W-M3K3 R-FM8H-YVXT R-6LHY-EVWN R-GKDO-OGP9 R-H1GA-192Z R-6NXR-6FE1 R-DDFI-KB8W R-DENE-Y2ZL
func TestSharedFiles(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	secondHandler, secondPages, _, _, _ := fixture(t)
	for name, media := range sharedFiles(t, h) {
		path := page.StaticPrefix + name
		cache := "no-cache"
		if media == "font/woff2" {
			cache = "public, max-age=31536000, immutable"
		}
		plain := request(h, "GET", path, "u")
		etag := plain.Header().Get("ETag")
		if plain.Code != 200 || plain.Body.Len() == 0 || plain.Header().Get("Content-Type") != media || len(plain.Header().Values("Content-Type")) != 1 || len(plain.Header().Values("Cache-Control")) != 1 || len(plain.Header().Values("ETag")) != 1 || len(etag) < 2 || etag[0] != '"' || etag[len(etag)-1] != '"' || plain.Header().Get("Cache-Control") != cache {
			t.Fatal(name, plain.Code, plain.Header())
		}
		for _, b := range []byte(etag[1 : len(etag)-1]) {
			if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
				t.Fatal(etag)
			}
		}
		second := request(secondHandler, "GET", path, "u")
		if second.Code != 200 || second.Body.String() != plain.Body.String() || second.Header().Get("ETag") != etag {
			t.Fatal(name)
		}
		head := request(h, "HEAD", path, "u")
		if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Type") != media || len(head.Header().Values("Content-Type")) != 1 || head.Header().Get("ETag") != etag || head.Header().Get("Cache-Control") != plain.Header().Get("Cache-Control") || len(head.Header().Values("ETag")) != 1 || len(head.Header().Values("Cache-Control")) != 1 {
			t.Fatal(head)
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, tag := range []string{"*", etag, "W/" + etag, " ,\tW/" + etag + "\t,", `"other", ` + etag, etag + ", " + etag} {
				for _, modified := range []string{"", "Sun, 06 Nov 1994 08:49:37 GMT", "Sun, 06 Nov 2094 08:49:37 GMT", "malformed"} {
					r := httptest.NewRequest(method, path, nil)
					r.Header.Set("X-User-Id", "u")
					r.Header.Set("If-None-Match", tag)
					r.Header.Set("If-Modified-Since", modified)
					out := httptest.NewRecorder()
					h.ServeHTTP(out, r)
					if out.Code != 304 || out.Body.Len() != 0 || out.Header().Get("ETag") != etag || len(out.Header().Values("ETag")) != 1 || out.Header().Get("Cache-Control") != cache || len(out.Header().Values("Cache-Control")) != 1 {
						t.Fatal(out)
					}
				}
			}
		}
		for _, tag := range []string{`"other"`, `W/"other"`, ` , "other" , , W/"stale" ,`, `"*"`} {
			for _, modified := range []string{"", "Sun, 06 Nov 1994 08:49:37 GMT", "Sun, 06 Nov 2094 08:49:37 GMT", "malformed"} {
				r := httptest.NewRequest("GET", path, nil)
				r.Header.Set("X-User-Id", "u")
				r.Header.Set("If-None-Match", tag)
				r.Header.Set("If-Modified-Since", modified)
				out := httptest.NewRecorder()
				h.ServeHTTP(out, r)
				if out.Code != 200 || out.Body.String() != plain.Body.String() || out.Header().Get("ETag") != etag || out.Header().Get("Content-Type") != media || len(out.Header().Values("Content-Type")) != 1 || len(out.Header().Values("ETag")) != 1 || out.Header().Get("Cache-Control") != cache || len(out.Header().Values("Cache-Control")) != 1 {
					t.Fatal(out)
				}
			}
		}
		for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, tag := range []string{"", "*", etag} {
				r := httptest.NewRequest(method, path, strings.NewReader("input"))
				r.Header.Set("X-User-Id", "u")
				r.Header.Set("If-None-Match", tag)
				out := httptest.NewRecorder()
				h.ServeHTTP(out, r)
				if out.Code != 405 || out.Header().Get("Allow") != "GET, HEAD" || len(out.Header().Values("Allow")) != 1 {
					t.Fatal(out)
				}
			}
		}
	}
	for _, path := range []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/nope.css", "/_appkit/theme.css/", "/_appkit/theme.css/x", "/_appkit/THEME.CSS", "/_appkit/favicon.svg/", "/_appkit/favicon.svg/x", "/_appkit/FAVICON.SVG"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			out := request(h, method, path, "u")
			if out.Code != 404 {
				t.Fatal(path, out)
			}
		}
	}
	if len(p.calls) != 0 || len(secondPages.calls) != 0 {
		t.Fatal(p.calls, secondPages.calls)
	}
}

// R-D4W7-VX21
func TestAppkitDelegation(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	names := []string{"", "nope.css", "favicon.svg/", "FAVICON.SVG"}
	for name := range sharedFiles(t, h) {
		names = append(names, name)
	}
	for _, name := range names {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, headers := range []http.Header{
				{},
				{"If-None-Match": {"*"}, "If-Modified-Since": {"Sun, 06 Nov 2094 08:49:37 GMT"}},
				{"Range": {"bytes=0-7"}, "If-Range": {`"other"`}},
				{"If-Match": {`"other"`}, "If-Unmodified-Since": {"Sun, 06 Nov 1994 08:49:37 GMT"}},
			} {
				r := httptest.NewRequest(method, page.StaticPrefix+name+"?q=x", strings.NewReader("body"))
				r.Header = headers.Clone()
				r.Header.Set("X-User-Id", "u")
				actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
				page.Static().ServeHTTP(expected, r.Clone(r.Context()))
				h.ServeHTTP(actual, r)
				if actual.Code != expected.Code || actual.Body.String() != expected.Body.String() || !reflect.DeepEqual(actual.Header(), expected.Header()) {
					t.Fatalf("%s %s: routed response differs from page.Static", method, r.URL.Path)
				}
			}
		}
	}
	if len(p.calls) != 0 {
		t.Fatal(p.calls)
	}
}

// R-HZLG-QTUF
func TestPlainFontAliasesNotFound(t *testing.T) {
	h, p, _, _, _ := fixture(t)
	hashed := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for name, media := range sharedFiles(t, h) {
		if media != "font/woff2" || !hashed.MatchString(name) {
			continue
		}
		path := page.StaticPrefix + hashed.ReplaceAllString(name, ".woff2")
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			out := request(h, method, path, "u")
			if out.Code != http.StatusNotFound {
				t.Fatalf("%s %s: status %d", method, path, out.Code)
			}
		}
	}
	if len(p.calls) != 0 {
		t.Fatal("font aliases called pages", p.calls)
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
