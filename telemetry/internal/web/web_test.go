package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	at "github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
	"github.com/ikigenba/ikigenba/telemetry/internal/tools"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

type fixture struct {
	h      http.Handler
	s      *store.Store
	w      *at.Writer
	c      *at.Capture
	stderr *bytes.Buffer
	cfg    web.Config
	source string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	source := filepath.Join(t.TempDir(), "trail.db")
	s, err := store.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	c := new(at.Capture)
	stderr := new(bytes.Buffer)
	w := at.New(at.Config{Service: web.ServiceName, Sink: c, Stderr: stderr, Now: func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{0x41}, 65536))})
	cfg := web.Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Service: web.ServiceName, Version: "test-version", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, MCP: mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: "test-version", Telemetry: w}), Store: s, Telemetry: w}
	f := &fixture{s: s, w: w, c: c, stderr: stderr, cfg: cfg, source: source}
	f.h = web.Handler(cfg)
	t.Cleanup(func() { w.Shutdown(context.Background(), "test"); _ = s.Close() })
	return f
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://telemetry.example"+path, nil)
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	r.Header.Set("X-Request-Id", "fixed-request")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	return out
}
func flush(t *testing.T, f *fixture) []at.Event {
	t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f.c.Events()
}
func equalResponse(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("responses differ: %d %v %q / %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

func TestNamesAndHandlerContract(t *testing.T) {
	// R-UFPP-GJ4Q R-UGXL-UAVF R-UJDE-LUCT R-UKLA-ZM3I
	const n = web.ServiceName
	const d = web.Description
	if n != "telemetry" || d != "The suite's trail of events" {
		t.Fatal(n, d)
	}
	f := newFixture(t)
	if request(f.h, "GET", "/", "u").Code != 200 {
		t.Fatal("handler")
	}
}
func TestIdentityBeforeRouting(t *testing.T) {
	// R-QKVN-9DAG
	f := newFixture(t)
	for _, path := range []string{"/", "/about", "/_appkit/theme.css", "/mcp", "/missing"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT"} {
			for _, empty := range []bool{false, true} {
				r := httptest.NewRequest(method, "http://example"+path, strings.NewReader("ignored"))
				if empty {
					r.Header["X-User-Id"] = []string{"", "later"}
				}
				got := httptest.NewRecorder()
				f.h.ServeHTTP(got, r)
				want := httptest.NewRecorder()
				identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("reached route") })).ServeHTTP(want, r)
				equalResponse(t, got, want)
			}
		}
	}
}
func TestExactPathsAnd404(t *testing.T) {
	// R-QM3J-N515 R-QNBG-0WRU R-8IF2-EAU1 R-RHSX-L6B7
	f := newFixture(t)
	for _, path := range []string{"/mcp/", "/mcp/a", "/ingest/", "/ingest/a", "/about/", "/_appkit", "/assets/", "/assets/a", "/logout", "/index.html", "//", "/nope", "/nope/", "/x/../", "/x/./", "/_APPKIT/feedback.js"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			out := request(f.h, method, path+"?x=1", "u")
			want := "not found\n"
			if method == "HEAD" {
				want = ""
			}
			if out.Code != 404 || out.Body.String() != want || !reflect.DeepEqual(out.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || out.Header().Get("Location") != "" || len(out.Header().Values("Set-Cookie")) != 0 {
				t.Fatalf("%s %s: %d %v %q", method, path, out.Code, out.Header(), out.Body.String())
			}
		}
	}
	for _, path := range []string{"/", "/about", "/_appkit/theme.css", "/mcp", "/ingest"} {
		out := request(f.h, "GET", path, "u")
		if out.Code >= 300 && out.Code < 400 || out.Header().Get("Location") != "" || len(out.Header().Values("Set-Cookie")) > 0 {
			t.Fatal(out)
		}
	}
}
func TestIngestWiring(t *testing.T) {
	// R-BTVA-7AFE R-1U8H-EEDM R-1XW6-JPLP R-QUMU-BJ80
	f := newFixture(t)
	e := at.Event{Time: time.Date(2025, 1, 2, 0, 0, 0, 123000, time.UTC), Service: "sibling", Name: "thing.done", RequestID: "shared", User: "person", Attrs: at.Attrs{"value": "é"}}
	body, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, content, body string }{{"GET", "", ""}, {"POST", "text/plain", "bad"}, {"POST", "application/json", "bad"}, {"POST", "application/json", strings.Repeat("x", at.MaxEventBytes+1)}} {
		r := httptest.NewRequest(tc.method, "http://example/ingest", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.content)
		got, want := httptest.NewRecorder(), httptest.NewRecorder()
		f.h.ServeHTTP(got, r)
		reference := httptest.NewRequest(tc.method, "http://example/ingest", strings.NewReader(tc.body))
		reference.Header.Set("Content-Type", tc.content)
		at.IngestHandler(f.s).ServeHTTP(want, reference)
		equalResponse(t, got, want)
	}
	count, err := f.s.Count(context.Background(), store.Filter{})
	if err != nil || count != 0 {
		t.Fatal("refused ingest stored a record", count, err)
	}
	prior := e
	prior.Attrs = at.Attrs{"before": true}
	if err := f.s.Deliver(context.Background(), prior); err != nil {
		t.Fatal(err)
	}
	priorBody, err := prior.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example/ingest", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		f.h.ServeHTTP(out, r)
		return out
	}
	for range 2 {
		out := post()
		if out.Code != 204 || out.Body.Len() != 0 {
			t.Fatal(out)
		}
	}
	records, err := f.s.Trace(context.Background(), "shared")
	if err != nil || len(records) != 3 {
		t.Fatal(records, err)
	}
	for i, r := range records {
		var attrs at.Attrs
		if err := json.Unmarshal(r.Attrs, &attrs); err != nil {
			t.Fatal(err)
		}
		b, err := (at.Event{Time: r.Time, Service: r.Service, Name: r.Event, RequestID: r.RequestID, User: r.User, Attrs: attrs}).MarshalJSON()
		expected := body
		if i == 0 {
			expected = priorBody
		}
		if err != nil || !bytes.Equal(b, expected) {
			t.Fatal(string(b), err)
		}
	}
	if len(flush(t, f)) != 0 {
		t.Fatal("ingest emitted events")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if post().Code != 500 {
		t.Fatal("closed ingest")
	}
	reopened, err := store.Open(f.source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	after, err := reopened.Trace(context.Background(), "shared")
	if err != nil || len(after) != 3 {
		t.Fatal("failed ingest changed trail", after, err)
	}

	if len(flush(t, f)) != 0 || f.stderr.Len() != 0 {
		t.Fatal("ingest failure logged")
	}
}
func TestRequestEvents(t *testing.T) {
	// R-8EEY-XQ1D R-R81Q-J0DN R-O075-CZ1C R-RCXC-23CF R-B6J6-YLTM
	f := newFixture(t)
	for _, tc := range []struct {
		method, path, user, body string
		status                   int
	}{
		{"GET", "/", "u", "", 200},
		{"GET", "/", "", "", 500},
		{"GET", "/nope", "u", "", 404},
		{"GET", "/_appkit/theme.css", "u", "", 200},
		{"GET", "/mcp", "u", "", 405},
		{"HEAD", "/about", "u", "", 200},
		{"HEAD", "/nope", "u", "", 404},
		{"POST", "/", "u", "unread body", 405},
		{"POST", "/mcp", "", "unread body", 500},
	} {
		before := len(flush(t, f))
		r := httptest.NewRequest(tc.method, "http://example"+tc.path, strings.NewReader(tc.body))
		r.Header.Set("X-Request-Id", "fixed-request")
		if tc.user != "" {
			r.Header.Set("X-User-Id", tc.user)
		}
		out := httptest.NewRecorder()
		f.h.ServeHTTP(out, r)
		ev := flush(t, f)[before:]
		if out.Code != tc.status || len(ev) != 2 {
			t.Fatal(out.Code, ev)
		}
		if ev[0].Name != "request.started" || !reflect.DeepEqual(ev[0].Attrs, at.Attrs{"method": tc.method, "path": tc.path}) || ev[1].Name != "request.finished" || !reflect.DeepEqual(ev[1].Attrs, at.Attrs{"status": int64(tc.status), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": int64(out.Body.Len())}) {
			t.Fatal(ev)
		}
		for _, e := range ev {
			if e.RequestID != "fixed-request" || e.User != tc.user {
				t.Fatal(e)
			}
		}
	}
	r := httptest.NewRequest("GET", "http://example/", nil)
	r.Header.Set("X-User-Id", "minted")
	out := httptest.NewRecorder()
	f.h.ServeHTTP(out, r)
	ev := flush(t, f)
	for _, e := range ev[len(ev)-2:] {
		if e.RequestID != strings.Repeat("41", 16) || e.User != "minted" {
			t.Fatal(e)
		}
	}
	if f.stderr.Len() != 0 {
		t.Fatal(f.stderr.String())
	}
}
func TestClosedStoreDoesNotChangePages(t *testing.T) {
	// R-C391-ONO0
	f := newFixture(t)
	w := at.New(at.Config{Service: web.ServiceName, Sink: f.s, Stderr: f.stderr,
		Now:   func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) },
		Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{0x41}, 65536))})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	cfg := f.cfg
	cfg.Telemetry = w
	cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: "test-version", Telemetry: w})
	h := web.Handler(cfg)
	type call struct{ method, path, user string }
	calls := []call{{"GET", "/", "u"}, {"GET", "/about", "u"}, {"GET", "/nope", "u"}, {"GET", "/_appkit/theme.css", "u"}, {"GET", "/mcp", "u"}, {"GET", "/", ""}, {"HEAD", "/about", "u"}, {"POST", "/", "u"}}
	before := make([]*httptest.ResponseRecorder, len(calls))
	for i, c := range calls {
		before[i] = request(h, c.method, c.path, c.user)
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.stderr.Len() != 0 {
		t.Fatal("healthy store delivery failed", f.stderr.String())
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	for i, c := range calls {
		equalResponse(t, before[i], request(h, c.method, c.path, c.user))
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stderr.String(), "undelivered event") {
		t.Fatal("closed store did not fail writer delivery", f.stderr.String())
	}
}
func TestMCPMountAndRegistration(t *testing.T) {
	// R-R6TU-58MY R-R99M-WS4C
	f := newFixture(t)
	ts := httptest.NewServer(f.h)
	defer ts.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: ts.URL + "/mcp", HTTPClient: ts.Client()})
	caller := identity.Caller{UserID: "reader", Email: "a@example", RequestID: "mcp-request"}
	got, err := client.ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	other := mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: "test-version", Telemetry: f.w})
	tools.Register(other, f.s)
	plain := httptest.NewServer(identity.Require(other))
	defer plain.Close()
	reference := mcp.NewClient(mcp.ClientConfig{Endpoint: plain.URL, HTTPClient: plain.Client()})
	want, err := reference.ListTools(context.Background(), caller)
	if err != nil || !reflect.DeepEqual(got, want) || len(got) != 4 {
		t.Fatal(got, want, err)
	}
	e := at.Event{Time: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), Service: "sibling", Name: "item.done", RequestID: "trace-me", Attrs: at.Attrs{}}
	if err := f.s.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), caller, "trace", json.RawMessage(`{"request_id":"trace-me"}`))
	if err != nil || result.IsError() {
		t.Fatal(result, err)
	}
	b, err := result.MarshalJSON()
	if err != nil || !bytes.Contains(b, []byte(`"event":"item.done"`)) {
		t.Fatal(string(b), err)
	}
	out := request(f.h, "GET", "/mcp", "u")
	if out.Code != 405 || out.Header().Get("Allow") != "POST" || out.Body.Len() != 0 {
		t.Fatal(out)
	}
	events := flush(t, f)
	found := false
	for _, e := range events {
		if e.Name != "request.started" && e.Name != "request.finished" && e.Name != "tool.called" {
			t.Fatal(e)
		}
		if e.Name == "tool.called" && e.User == caller.UserID && e.RequestID == caller.RequestID {
			found = true
		}
	}
	if !found {
		t.Fatal("caller context not forwarded")
	}
}
func TestConcurrentRequests(t *testing.T) {
	// R-RK8Q-CPSL
	f := newFixture(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			r := httptest.NewRequest("GET", "http://example/", nil)
			id := fmt.Sprintf("request-%d", i)
			r.Header.Set("X-Request-Id", id)
			r.Header.Set("X-User-Id", fmt.Sprintf("user-%d", i))
			out := httptest.NewRecorder()
			f.h.ServeHTTP(out, r)
			if out.Code != 200 {
				t.Errorf("status %d", out.Code)
			}
		})
	}
	wg.Wait()
	events := flush(t, f)
	if len(events) != 40 {
		t.Fatal(len(events))
	}
	seen := map[string]int{}
	for _, e := range events {
		var i int
		if _, err := fmt.Sscanf(e.RequestID, "request-%d", &i); err != nil || e.User != fmt.Sprintf("user-%d", i) {
			t.Fatal(e)
		}
		seen[e.RequestID]++
	}
	for _, n := range seen {
		if n != 2 {
			t.Fatal(seen)
		}
	}
}

func TestRequestEventOrderBeforeAnswer(t *testing.T) {
	// R-8EEY-XQ1D
	f := newFixture(t)
	beforeWrite := func() {
		events := flush(t, f)
		if len(events) != 1 || events[0].Name != "request.started" {
			t.Fatalf("before response: %v", events)
		}
	}
	r := httptest.NewRequest("GET", "http://example/", nil)
	r.Header.Set("X-User-Id", "u")
	out := &observedResponse{ResponseRecorder: httptest.NewRecorder(), beforeWrite: beforeWrite}
	f.h.ServeHTTP(out, r)
	events := flush(t, f)
	if len(events) != 2 || events[1].Name != "request.finished" {
		t.Fatal(events)
	}
}

type observedResponse struct {
	*httptest.ResponseRecorder
	beforeWrite func()
}

func (w *observedResponse) WriteHeader(status int) {
	w.beforeWrite()
	w.ResponseRecorder.WriteHeader(status)
}

func (w *observedResponse) Write(body []byte) (int, error) {
	w.beforeWrite()
	return w.ResponseRecorder.Write(body)
}

type observedBody struct {
	io.ReadCloser
	bytesRead int64
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytesRead += int64(n)
	return n, err
}

func TestMCPRequestEventBytesAndOrder(t *testing.T) {
	// R-8EEY-XQ1D
	f := newFixture(t)
	type answer struct {
		requestBytes int64
		response     *httptest.ResponseRecorder
	}
	answers := make(chan answer, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := &observedBody{ReadCloser: r.Body}
		r.Body = body
		out := httptest.NewRecorder()
		f.h.ServeHTTP(out, r)
		for name, values := range out.Header() {
			w.Header()[name] = values
		}
		w.WriteHeader(out.Code)
		_, _ = w.Write(out.Body.Bytes())
		answers <- answer{requestBytes: body.bytesRead, response: out}
	}))
	defer ts.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: ts.URL + "/mcp", HTTPClient: ts.Client()})
	caller := identity.Caller{UserID: "reader", RequestID: "mcp-byte-request"}
	result, err := client.CallTool(context.Background(), caller, "count", json.RawMessage(`{}`))
	if err != nil || result.IsError() {
		t.Fatal(result, err)
	}
	out := <-answers
	events := flush(t, f)
	if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != "tool.called" || events[2].Name != "request.finished" {
		t.Fatal(events)
	}
	if !reflect.DeepEqual(events[0].Attrs, at.Attrs{"method": "POST", "path": "/mcp"}) {
		t.Fatal(events[0])
	}
	if out.requestBytes == 0 || out.response.Body.Len() == 0 {
		t.Fatal("MCP did not read and write bodies", out)
	}
	want := at.Attrs{"status": int64(out.response.Code), "duration_us": int64(0), "request_bytes": out.requestBytes, "response_bytes": int64(out.response.Body.Len())}
	if !reflect.DeepEqual(events[2].Attrs, want) {
		t.Fatal(events[2], want)
	}
}
