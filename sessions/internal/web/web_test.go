package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/settings"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
	"github.com/ikigenba/ikigenba/sessions/internal/web"
)

func now() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) }

type countingReader byte

func (r *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		*r++
		p[i] = byte(*r)
	}
	return len(p), nil
}

type cookieTransport struct {
	t    *testing.T
	base http.RoundTripper
}

func (c cookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(r)
	if err == nil {
		if _, ok := resp.Header["Set-Cookie"]; ok {
			c.t.Errorf("%s %s sent Set-Cookie", r.Method, r.URL.Path)
		}
	}
	return resp, err
}

type fixture struct {
	cfg     web.Config
	d       *db.DB
	capture *telemetry.Capture
	server  *httptest.Server
	client  *http.Client
	mcp     *mcp.Client
}

// R-ULZ2-ZPPO R-UN6Z-DHGD R-UOEV-R972
func newFixture(t *testing.T, path, servicesPath string) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: sessions.Migrations(), Now: now, Service: pages.ServiceName, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: now})
	capture := &telemetry.Capture{}
	rand := countingReader(0)
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: "fixture-display", Sink: capture, Stderr: io.Discard, Now: now,
		Sleep: func(context.Context, time.Duration) {}, Rand: &rand})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.Shutdown(ctx, "test complete")
	})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "fixture-mcp", Version: "fixture-display", Telemetry: w})
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: "fixture-banner", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Release: "fixture-release", Commit: "fixture-commit", Tools: true}
	}
	s, err := settings.Read(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	// The positional literal proves Config's complete field order by use.
	cfg := web.Config{banner, srv, servicesPath, st, w, s}
	build := web.Handler
	server := httptest.NewServer(build(cfg))
	t.Cleanup(server.Close)
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Transport = cookieTransport{t: t, base: client.Transport}
	client.Timeout = 3 * time.Second
	return &fixture{cfg: cfg, d: d, capture: capture, server: server, client: client,
		mcp: mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: client})}
}

func (f *fixture) call(t *testing.T, caller identity.Caller, name, args string) mcp.Result {
	t.Helper()
	r, err := f.mcp.CallTool(context.Background(), caller, name, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError() {
		b, _ := json.Marshal(r)
		t.Fatalf("%s refused: %s", name, b)
	}
	return r
}

func structured(t *testing.T, r mcp.Result) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil || len(decoded.Structured) == 0 {
		t.Fatalf("result has no structuredContent: %s (%v)", b, err)
	}
	return decoded.Structured
}

func (f *fixture) flush(t *testing.T) []telemetry.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.cfg.Telemetry.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	return f.capture.Events()
}

func (f *fixture) request(t *testing.T, method, path string, headers http.Header, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = headers.Clone()
	req.Host = "sessions.fixture.invalid:443"
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, b
}

func state(t *testing.T, f *fixture) map[string]json.RawMessage {
	t.Helper()
	caller := identity.Caller{UserID: "reader", Email: "reader@fixture.invalid", RequestID: "state-read"}
	out := make(map[string]json.RawMessage)
	for _, name := range []string{tools.ToolListSessions, tools.ToolListRules, tools.ToolListPrices, tools.ToolGetAwayLimit} {
		out[name] = structured(t, f.call(t, caller, name, `{}`))
	}
	return out
}

func siblingSockets(t *testing.T) (string, string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "scratch.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var connections atomic.Int32
	var listeners []net.Listener
	var stopped []chan struct{}
	for _, name := range []string{"auth", "home"} {
		ln, err := net.Listen("unix", filepath.Join(dir, name+".sock"))
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, ln)
		done := make(chan struct{})
		stopped = append(stopped, done)
		go func() {
			defer close(done)
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				connections.Add(1)
				_ = c.Close()
			}
		}()
	}
	check := func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
		for _, done := range stopped {
			<-done
		}
		if n := connections.Load(); n != 0 {
			t.Errorf("page requests opened %d sibling connections", n)
		}
	}
	t.Cleanup(check)
	return filepath.Join(dir, "auth.sock"), filepath.Join(dir, "home.sock"), check
}

// R-UQUO-ISOG R-UPMS-50XR
func TestGuestMCPIsGatedBeforeTools(t *testing.T) {
	f := newFixture(t, filepath.Join(t.TempDir(), "sessions.db"), "")
	before := state(t, f)
	for i, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		id := fmt.Sprintf("guest-call-%d", i)
		headers := http.Header{"X-Request-Id": {id, "ignored-id"}, "X-User-Email": {"untrusted@fixture.invalid"}, "Content-Type": {"application/json"}, "MCP-Protocol-Version": {mcp.ProtocolVersion}}
		if i%2 == 1 {
			headers["X-User-Id"] = []string{"", "ignored-user"}
		}
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_away_limit","arguments":{"minutes":33}}}`
		resp, data := f.request(t, method, "/mcp?ignored=1", headers, body)
		want := identity.MissingBody
		if method == http.MethodHead {
			want = ""
		}
		if resp.StatusCode != http.StatusInternalServerError || string(data) != want || !reflect.DeepEqual(resp.Header.Values("Content-Type"), []string{"text/plain; charset=utf-8"}) {
			t.Fatalf("guest %s = %d %q %v", method, resp.StatusCode, data, resp.Header)
		}
		var events []telemetry.Event
		for _, e := range f.flush(t) {
			if e.RequestID == id {
				events = append(events, e)
			}
		}
		if len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" {
			t.Fatalf("guest events = %+v", events)
		}
		for _, e := range events {
			if e.User != "" || e.RequestID != id {
				t.Fatalf("guest caller = %+v", e)
			}
		}
	}
	if after := state(t, f); !reflect.DeepEqual(after, before) {
		t.Fatalf("guest changed store: before %s after %s", before, after)
	}
}

// R-RMUN-5VUG R-UPMS-50XR
func TestRegisteredMCPUsesConfiguredServerStoreAndCaller(t *testing.T) {
	f := newFixture(t, filepath.Join(t.TempDir(), "sessions.db"), "")
	want := []string{tools.ToolPushTurns, tools.ToolGetLatestTurns, tools.ToolListSessions, tools.ToolTotalSessions, tools.ToolListRules, tools.ToolAddRule, tools.ToolRemoveRule, tools.ToolListPrices, tools.ToolSetPrice, tools.ToolRemovePrice, tools.ToolGetAwayLimit, tools.ToolSetAwayLimit}
	registered := f.cfg.MCP.Tools()
	if len(registered) != len(want) {
		t.Fatalf("registered %d tools, want %d", len(registered), len(want))
	}
	for i, tool := range registered {
		if tool.Name != want[i] {
			t.Fatalf("tool %d = %q, want %q", i, tool.Name, want[i])
		}
	}
	caller := identity.Caller{UserID: "writer-one", Email: "writer-one@fixture.invalid", RequestID: "push-one"}
	f.call(t, caller, tools.ToolPushTurns, `{"sessions":[{"tool":"claude","id":"fixture-session","host":"fixture-host","directory":"/fixture/work","headless":false,"turns":[{"moment":"2026-10-27T09:00:00Z","whose":"agent","model":"fixture-model","input":7,"cached":0,"output":4,"reasoning":0}]}]}`)
	caller.RequestID = "list-one"
	r := f.call(t, caller, tools.ToolListSessions, `{}`)
	var answer struct {
		Sessions []struct{ Tool, ID, Person string }
	}
	if err := json.Unmarshal(structured(t, r), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Sessions) != 1 || answer.Sessions[0].Tool != "claude" || answer.Sessions[0].ID != "fixture-session" || answer.Sessions[0].Person != caller.Email {
		t.Fatalf("list answer = %+v", answer)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	var info map[string]string
	if err := json.Unmarshal(envelope.Meta["io.modelcontextprotocol/serverInfo"], &info); err != nil || !reflect.DeepEqual(info, map[string]string{"name": "fixture-mcp", "version": "fixture-display"}) {
		t.Fatalf("configured serverInfo = %v (%v)", info, err)
	}
	latest, err := f.cfg.Store.Latest(context.Background(), []store.SessionRef{{Tool: "claude", ID: "fixture-session"}})
	if err != nil || len(latest) != 1 || latest[0].Latest.IsZero() {
		t.Fatalf("configured store Latest = %+v, %v", latest, err)
	}
	for _, e := range f.flush(t) {
		if e.RequestID == "push-one" && e.Name == "change.finished" && e.User == caller.UserID {
			return
		}
	}
	t.Fatal("configured writer did not receive the push's caller")
}

// R-UTAH-AC5U R-UUID-O3WJ R-UVQA-1VN8 R-UWY6-FNDX
func TestRouteMatrixMatchesPagesAndStaticWithoutChangingStore(t *testing.T) {
	dir := t.TempDir()
	servicesPath := filepath.Join(dir, "services.json")
	authSocket, homeSocket, checkConnections := siblingSockets(t)
	servicesDocument, err := json.Marshal(map[string]any{"services": []map[string]any{
		{"name": "auth", "url": "https://signin.fixture.invalid", "description": "fixture", "socket": authSocket, "enabled": true, "mcp": false},
		{"name": "home", "url": "https://home.fixture.invalid", "description": "fixture", "socket": homeSocket, "enabled": true, "mcp": false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicesPath, servicesDocument, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sessions.db")
	f := newFixture(t, path, servicesPath)
	caller := identity.Caller{UserID: "matrix-writer", Email: "matrix@fixture.invalid", RequestID: "matrix-push"}
	f.call(t, caller, tools.ToolPushTurns, `{"sessions":[{"tool":"codex","id":"matrix-session","host":"matrix-host","directory":"/fixture/matrix","headless":false,"turns":[{"moment":"2026-10-27T09:00:00Z","whose":"person","model":"","input":0,"cached":0,"output":0,"reasoning":0}]}]}`)
	before := state(t, f)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	referencePages := identity.Optional(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: servicesPath, Store: f.cfg.Store, MCP: f.cfg.MCP}))
	paths := []string{"/", "/about", "/tools", "/nope", "/mcp/", "/events", "/declarations", "/_appkit", "//tools", "/./about", "/nope/../mcp", "/%6dcp/", "/_appkit/theme.css", "/_appkit/missing", "/_appkit/../tools"}
	for _, user := range []bool{false, true} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete} {
			for _, route := range paths {
				headers := http.Header{"X-Request-Id": {"matrix-request"}, "X-User-Email": {"matrix@fixture.invalid"}}
				if user {
					headers.Set("X-User-Id", caller.UserID)
				}
				requestPath := route + "?probe=1"
				resp, data := f.request(t, method, requestPath, headers, "fixture-body")
				r := httptest.NewRequest(method, "https://sessions.fixture.invalid:443"+requestPath, strings.NewReader("fixture-body"))
				r.RequestURI = requestPath
				r.Header = headers.Clone()
				r.Header.Set("X-Forwarded-Proto", "https")
				expected := httptest.NewRecorder()
				if strings.HasPrefix(r.URL.Path, "/_appkit/") {
					page.Static().ServeHTTP(expected, r)
				} else {
					referencePages.ServeHTTP(expected, r)
				}
				wantBody := expected.Body.Bytes()
				if method == http.MethodHead {
					wantBody = nil
				}
				if resp.StatusCode != expected.Code || !bytes.Equal(data, wantBody) {
					t.Fatalf("user=%v %s %s = %d %q; reference %d %q", user, method, requestPath, resp.StatusCode, data, expected.Code, wantBody)
				}
				for _, name := range []string{"Content-Type", "Location", "Allow"} {
					if !reflect.DeepEqual(resp.Header.Values(name), expected.Header().Values(name)) {
						t.Fatalf("user=%v %s %s %s = %v, want %v", user, method, route, name, resp.Header.Values(name), expected.Header().Values(name))
					}
				}
				if !user && route == "/" && (resp.StatusCode != http.StatusFound || bytes.Equal(data, []byte(identity.MissingBody)) || !strings.HasPrefix(resp.Header.Get("Location"), "https://signin.fixture.invalid/")) {
					t.Fatalf("guest landing did not use configured services: %d %q", resp.StatusCode, data)
				}
			}
		}
	}
	f.server.Close()
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := newFixture(t, path, servicesPath)
	if after := state(t, reopened); !reflect.DeepEqual(after, before) {
		t.Fatalf("page/static requests changed persisted records: before %s after %s", before, after)
	}
	checkConnections()
}

// R-UPMS-50XR
func TestRequestIDsAndFirstIdentityHeadersReachTelemetry(t *testing.T) {
	f := newFixture(t, filepath.Join(t.TempDir(), "sessions.db"), "")
	for _, id := range []string{"chosen-id", ""} {
		headers := http.Header{"X-Request-Id": {id, "ignored-id"}, "X-User-Id": {"first-user", "ignored-user"}, "X-User-Email": {"first@fixture.invalid", "ignored@fixture.invalid"}}
		resp, body := f.request(t, http.MethodGet, "/about", headers, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatal(resp.StatusCode)
		}
		if !bytes.Contains(body, []byte("first@fixture.invalid")) || bytes.Contains(body, []byte("ignored@fixture.invalid")) {
			t.Fatalf("page caller email = %q", body)
		}
	}
	events := f.flush(t)
	if len(events) != 4 {
		t.Fatalf("events = %+v", events)
	}
	for i := 0; i < 4; i += 2 {
		start, end := events[i], events[i+1]
		if start.Name != "request.started" || end.Name != "request.finished" || start.RequestID == "" || start.RequestID != end.RequestID || start.User != "first-user" || end.User != "first-user" {
			t.Fatalf("request caller pair = %+v %+v", start, end)
		}
	}
	if events[0].RequestID != "chosen-id" || events[2].RequestID != "0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("chosen/generated ids = %q / %q", events[0].RequestID, events[2].RequestID)
	}
}
