package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/ingress"
	"github.com/ikigenba/ikigenba/webhooks/internal/pages"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/trail"
	"github.com/ikigenba/ikigenba/webhooks/internal/web"
)

type fixture struct {
	ctx context.Context
	d   *db.DB
	st  *store.Store
	cfg web.Config
	h   http.Handler
	tc  *telemetry.Capture
	ec  *events.Capture
}

var now = time.Date(2026, 10, 9, 9, 32, 0, 0, time.UTC)

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	clock := func() time.Time { return now }
	random := make([]byte, 64000)
	for i := range random {
		random[i] = byte(i/8 + 1)
	}
	f := &fixture{ctx: ctx, tc: &telemetry.Capture{}, ec: &events.Capture{}}
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	t.Cleanup(func() { _ = d.Close() })
	f.st = store.New(d, store.Config{Now: clock, Rand: bytes.NewReader(random)})
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: f.tc, Now: clock, Rand: bytes.NewReader(random), Stderr: &bytes.Buffer{}, Sleep: func(context.Context, time.Duration) {}})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: f.ec, Now: clock, Rand: bytes.NewReader(random), Stderr: &bytes.Buffer{}, Telemetry: w, Emits: trail.Emits()})
	t.Cleanup(func() {
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test complete")
	})
	// R-XY6V-9TUA
	f.cfg = web.Config{Banner: page.New(pages.ServiceName, version.Identity{Release: "test display"}).Banner, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "test display", Telemetry: w}), ServicesPath: "", Store: f.st, Telemetry: w, Events: em}
	f.h = web.Handler(f.cfg)
	return f
}

func (f *fixture) flush(t *testing.T) {
	t.Helper()
	if err := f.cfg.Events.Flush(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.cfg.Telemetry.Flush(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func request(method, path, user, id string, body string) *http.Request {
	r := httptest.NewRequest(method, "https://webhooks.example.test"+path, strings.NewReader(body))
	r.Header.Set("X-Forwarded-Proto", "https")
	if user != "" {
		r.Header.Set("X-User-Id", user)
		r.Header.Set("X-User-Email", user+"@example.test")
	}
	if id != "" {
		r.Header.Set("X-Request-Id", id)
	}
	return r
}

func answer(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func same(t *testing.T, what string, a, b *httptest.ResponseRecorder) {
	t.Helper()
	ah, bh := a.Header().Clone(), b.Header().Clone()
	if a.Code != b.Code || !reflect.DeepEqual(ah, bh) || a.Body.String() != b.Body.String() {
		t.Fatalf("%s: answers differ: %d %v %q versus %d %v %q", what, a.Code, ah, a.Body.String(), b.Code, bh, b.Body.String())
	}
}

func TestEventBusPaths(t *testing.T) {
	// R-YCTN-V2QM
	f := setup(t)
	delivery := events.DeliveryHandler(nil)
	declarations := events.DeclarationsHandler(f.cfg.Events, nil)
	for _, user := range []string{"", "u_ada"} {
		for _, method := range []string{"GET", "POST", "PUT"} {
			same(t, "/events", answer(f.h, request(method, "/events", user, "rq", `{}`)), answer(delivery, request(method, "/events", user, "rq", `{}`)))
			same(t, "/declarations", answer(f.h, request(method, "/declarations", user, "rq", "")), answer(declarations, request(method, "/declarations", user, "rq", "")))
		}
		w := answer(f.h, request("GET", "/declarations", user, "", ""))
		if w.Code != 200 {
			t.Fatalf("declarations status %d", w.Code)
		}
		want := `{"emits":[{"event":"webhook.*.created","attrs":["hook","scheme"]},{"event":"webhook.*.deleted","attrs":["hook","scheme"]},{"event":"webhook.*.received","attrs":["hook","delivery","type","content_type","bytes"]},{"event":"webhook.*.rotated","attrs":["hook","scheme"]}],"accepts":[]}`
		if strings.TrimSuffix(w.Body.String(), "\n") != want {
			t.Fatalf("declarations body %q", w.Body.String())
		}
	}
}

func TestMCPIdentity(t *testing.T) {
	// R-YE1K-8UHB
	f := setup(t)
	for _, method := range []string{"GET", "POST"} {
		w := answer(f.h, request(method, "/mcp", "", "", `{}`))
		if w.Code != 500 || w.Body.String() != identity.MissingBody {
			t.Fatalf("missing identity: %d %q", w.Code, w.Body.String())
		}
	}
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	c := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client(), Name: "test", Version: "test"})
	caller := identity.Caller{UserID: "u_ada", Email: "ada@example.test"}
	toolsList, err := c.ListTools(f.ctx, caller)
	if err != nil || len(toolsList) != 6 {
		t.Fatalf("tools: %v %d", err, len(toolsList))
	}
	res, err := c.CallTool(f.ctx, caller, "create", json.RawMessage(`{"slug":"tick"}`))
	if err != nil || res.IsError() {
		t.Fatalf("create: %v %v", err, res)
	}
	x, err := f.st.Get(f.ctx, "tick")
	if err != nil || x.OwnerID != "u_ada" || x.OwnerEmail != "ada@example.test" {
		t.Fatalf("created as wrong caller: %+v %v", x, err)
	}
	raw, _ := res.MarshalJSON()
	// The url comes from the request's host since no services file names
	// webhooks, under https since no X-Forwarded-Proto says otherwise.
	if !strings.Contains(string(raw), `"url":"https://`+strings.TrimPrefix(srv.URL, "http://")+`/in/tick"`) {
		t.Fatalf("url not from the request: %s", raw)
	}
}

func TestRouteTable(t *testing.T) {
	// R-YF9G-MM80 R-YHP9-E5PE
	f := setup(t)
	x, secret, err := f.st.Create(f.ctx, store.Draft{Slug: "tick", Scheme: store.Bearer, OwnerID: "u_ada", OwnerEmail: "ada@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	in := ingress.Handler(ingress.Config{Store: f.st, Telemetry: f.cfg.Telemetry, Events: f.cfg.Events})
	static := page.Static()
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Optional(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.st}))
	decorate := func(r *http.Request) *http.Request {
		r.Header.Set("X-Event-Cause", "evt_0123456789abcdef")
		r.Header.Set("X-Event-Depth", "3")
		return r
	}
	for _, user := range []string{"", "u_ada", "u_bob"} {
		// The ingress answers whatever identity the request carries.
		for _, c := range []struct{ method, path string }{{"GET", "/in/tick"}, {"POST", "/in/nope"}, {"POST", "/in/"}, {"POST", "/in/tick"}, {"POST", "/in/Bad-Slug"}} {
			same(t, c.path, answer(f.h, decorate(request(c.method, c.path, user, "rq", "body"))), answer(in, decorate(request(c.method, c.path, user, "rq", "body"))))
		}
		for _, p := range []string{"/_appkit/theme.css", "/_appkit/nope.css", "/_appkit/feedback.js"} {
			same(t, p, answer(f.h, request("GET", p, user, "rq", "")), answer(static, request("GET", p, user, "rq", "")))
		}
		for _, p := range []string{"/", "/tools", "/about", "/nope", "/in", "/mcp/", "/_appkit", "//", "/about/"} {
			for _, method := range []string{"GET", "POST"} {
				a := answer(f.h, request(method, p, user, "rq", ""))
				same(t, p, a, answer(ph, request(method, p, user, "rq", "")))
				if a.Header().Values("Set-Cookie") != nil {
					t.Fatalf("cookie on %s", p)
				}
			}
		}
	}
	// An admitted delivery reaches the ingress through the route table.
	r := request("POST", "/in/tick", "", "rq", "payload")
	r.Header.Set(ingress.AuthHeader, secret)
	w := answer(f.h, r)
	if w.Code != 202 || w.Header().Values("Set-Cookie") != nil {
		t.Fatalf("admitted: %d %v", w.Code, w.Header())
	}
	got, err := f.st.Get(f.ctx, "tick")
	if err != nil || got.ID != x.ID || got.LastReceived.IsZero() {
		t.Fatalf("delivery not kept: %+v %v", got, err)
	}
	for _, p := range []string{"/events", "/declarations", "/mcp"} {
		if answer(f.h, request("GET", p, "u_ada", "", "")).Header().Values("Set-Cookie") != nil {
			t.Fatalf("cookie on %s", p)
		}
	}
}

func TestRequestEvents(t *testing.T) {
	// R-YGHD-0DYP
	f := setup(t)
	for _, p := range []string{"/", "/about", "/nope", "/in/nope", "/_appkit/theme.css", "/declarations"} {
		for _, user := range []string{"", "u_ada"} {
			req := httptest.NewRequest("GET", "https://webhooks.example.test"+p, nil)
			req.Header.Set("X-Request-Id", "req"+strings.ReplaceAll(p, "/", "_")+user)
			if user != "" {
				req.Header.Set("X-User-Id", user)
			}
			resp := answer(f.h, req)
			f.flush(t)
			var started, finished int
			for _, e := range f.tc.Events() {
				if e.RequestID != req.Header.Get("X-Request-Id") {
					continue
				}
				if e.User != user {
					t.Fatalf("%s: user %q", p, e.User)
				}
				switch e.Name {
				case "request.started":
					started++
					if e.Attrs["method"] != "GET" || e.Attrs["path"] != p {
						t.Fatalf("started attrs %v", e.Attrs)
					}
				case "request.finished":
					finished++
					if e.Attrs["status"] != int64(resp.Code) {
						t.Fatalf("finished attrs %v, status %d", e.Attrs, resp.Code)
					}
				default:
					t.Fatalf("unexpected event %s", e.Name)
				}
			}
			if started != 1 || finished != 1 {
				t.Fatalf("%s: %d started %d finished", p, started, finished)
			}
		}
	}
	// A tool call's lifecycle event carries the request's id and user, in both sinks.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"slug":"tock"},"_meta":{"io.modelcontextprotocol/protocolVersion":"` + mcp.ProtocolVersion + `","io.modelcontextprotocol/clientCapabilities":{}}}}`
	req := httptest.NewRequest("POST", "https://webhooks.example.test/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "create")
	req.Header.Set("X-Request-Id", "reqcreate")
	req.Header.Set("X-User-Id", "u_ada")
	resp := answer(f.h, req)
	f.flush(t)
	found := 0
	for _, e := range f.tc.Events() {
		if e.Name == trail.Name("tock", trail.Created) {
			found++
			if e.RequestID != "reqcreate" || e.User != "u_ada" {
				t.Fatalf("trail event %+v", e)
			}
		}
	}
	for _, e := range f.ec.Events() {
		if e.Name == trail.Name("tock", trail.Created) {
			found++
			if e.RequestID != "reqcreate" || e.User != "u_ada" {
				t.Fatalf("bus event %+v", e)
			}
		}
	}
	if found != 2 {
		t.Fatalf("lifecycle events found %d (status %d)", found, resp.Code)
	}
}
