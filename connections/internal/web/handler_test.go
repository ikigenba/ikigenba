package web_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	connections "github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/pages"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/tools"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
	"github.com/ikigenba/ikigenba/connections/internal/web"
)

type countedReader struct {
	n int
	r io.Reader
}

func (r *countedReader) Read(p []byte) (int, error) { n, e := r.r.Read(p); r.n += n; return n, e }

type untouchedBody struct{ reads int }

func (b *untouchedBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*untouchedBody) Close() error               { return nil }

type routingFixture struct {
	cfg     web.Config
	d       *db.DB
	capture *telemetry.Capture
	bus     *events.Capture
	random  *countedReader
	calls   atomic.Int64
	refuse  atomic.Bool
}

func routingNew(t *testing.T) *routingFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &routingFixture{capture: &telemetry.Capture{}, bus: &events.Capture{}, random: &countedReader{r: strings.NewReader(strings.Repeat("r", 32768))}}
	now := func() time.Time { return time.Date(2031, 4, 5, 6, 7, 8, 0, time.UTC) }
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state.db"), Migrations: connections.Migrations(), Now: now, Service: pages.ServiceName, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	st := store.New(f.d, store.Config{Now: now, Rand: strings.NewReader(strings.Repeat("s", 32768))})
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: "test", Sink: f.capture, Stderr: io.Discard, Now: now, Rand: strings.NewReader(strings.Repeat("w", 65536))})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: f.bus, Stderr: io.Discard, Now: now, Rand: strings.NewReader(strings.Repeat("e", 65536)), Telemetry: w, Emits: trail.Emits()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test finished")
	})
	ps := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if f.refuse.Load() {
			out.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(out, `{"error":"invalid_grant"}`)
			return
		}
		if r.URL.Path == "/revoke" {
			out.WriteHeader(http.StatusOK)
			return
		}
		out.Header().Set("Content-Type", "application/json")
		idToken := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"account@example.test","sub":"subject"}`)) + ".signature"
		reply, err := json.Marshal(map[string]any{"access_token": "provider-access", "refresh_token": "provider-refresh", "expires_in": 3600, "scope": "email", "id_token": idToken})
		if err != nil {
			t.Error(err)
			out.WriteHeader(500)
			return
		}
		_, _ = out.Write(reply)
	}))
	t.Cleanup(ps.Close)
	p := providers.Table()[0]
	p.AuthorizeURL = ps.URL + "/authorize"
	p.TokenURL = ps.URL + "/token"
	p.RevokeURL = ps.URL + "/revoke"
	f.cfg = web.Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "test", Telemetry: w}), ServicesPath: filepath.Join(t.TempDir(), "missing"), CallbackURL: "https://connections.example.test", Clients: []providers.Client{{Provider: p, ID: "test-id", Secret: "test-secret", HTTP: ps.Client(), Now: now}}, Store: st, Telemetry: w, Events: em, Now: now, Rand: f.random}
	return f
}
func (f *routingFixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.cfg.Events.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.cfg.Telemetry.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func routingRequest(h http.Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "connections.example.test"
	r.Header.Set("X-User-Id", "user-one")
	r.Header.Set("X-User-Email", "caller@example.test")
	r.Header.Set("X-Request-Id", "request-one")
	r.Header.Set("Origin", "https://connections.example.test")
	r.Header.Set(events.CauseHeader, "evt_0123456789abcdef")
	r.Header.Set(events.DepthHeader, "3")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	return out
}
func seedRouting(t *testing.T, f *routingFixture, expires time.Time) store.Connection {
	t.Helper()
	c, _, err := f.cfg.Store.Upsert(context.Background(), store.Connection{UserID: "user-one", UserEmail: "caller@example.test", Provider: "google", AccountEmail: "account@example.test", AccountSubject: "subject", AccessToken: "seed-access", RefreshToken: "seed-refresh", AccessExpires: expires, Scopes: []string{"email"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// R-2Q8I-QGMT R-A3L3-MYWR
func TestRoutingIdentityPrecedesEveryProtectedRoute(t *testing.T) {
	f := routingNew(t)
	f.cfg.Store = nil
	h := web.Handler(f.cfg)
	paths := []struct{ method, path string }{{"GET", "/"}, {"GET", "/about"}, {"GET", "/_appkit/theme.css"}, {"POST", "/mcp"}, {"GET", "/google/connect"}, {"POST", "/google/connect"}, {"GET", "/google/callback?state=connections.x&code=y"}, {"POST", "/google/disconnect"}, {"GET", "/token/google"}, {"GET", "/events/"}, {"HEAD", "/token/nope"}, {"DELETE", "/declarations/"}}
	for _, p := range paths {
		for _, values := range [][]string{nil, {"", "ignored-user"}} {
			r := httptest.NewRequest(p.method, p.path, nil)
			r.Header["X-User-Id"] = values
			r.Header.Set("X-User-Email", "ignored@example.test")
			r.Header.Set("X-Request-Id", "rejected-request")
			body := &untouchedBody{}
			r.Body = body
			out := httptest.NewRecorder()
			h.ServeHTTP(out, r)
			expected := identity.MissingBody
			if p.method == "HEAD" {
				expected = ""
			}
			if out.Code != 500 || out.Body.String() != expected || !reflect.DeepEqual(out.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || body.reads != 0 {
				t.Errorf("%s %s: status=%d headers=%v body=%q reads=%d", p.method, p.path, out.Code, out.Header(), out.Body.String(), body.reads)
			}
		}
	}
	f.flush(t)
	if f.random.n != 0 || f.calls.Load() != 0 || len(f.bus.Events()) != 0 {
		t.Fatal("identity rejection performed work")
	}
	es := f.capture.Events()
	if len(es) != len(paths)*4 {
		t.Fatalf("events=%d", len(es))
	}
	for i, e := range es {
		name := "request.started"
		if i%2 == 1 {
			name = "request.finished"
		}
		if e.Name != name || e.RequestID != "rejected-request" || e.User != "" {
			t.Errorf("unexpected event: %+v", e)
		}
	}
}

// R-27Y0-ZWIE
func TestRoutingBusOutsideIdentity(t *testing.T) {
	f := routingNew(t)
	h := web.Handler(f.cfg)
	for _, user := range []string{"", "user-one"} {
		for _, path := range []string{"/events", "/declarations"} {
			for _, method := range []string{"GET", "POST", "HEAD", "PUT"} {
				r := httptest.NewRequest(method, path, strings.NewReader("{}"))
				r.Header.Set("X-User-Id", user)
				r.Header.Set("X-Request-Id", "bus-request")
				r.Header.Set("Content-Type", "application/json")
				actual := httptest.NewRecorder()
				h.ServeHTTP(actual, r)
				expected := httptest.NewRecorder()
				target := events.DeliveryHandler(nil)
				if path == "/declarations" {
					target = events.DeclarationsHandler(f.cfg.Events, nil)
				}
				target.ServeHTTP(expected, r.Clone(context.Background()))
				if actual.Code != expected.Code || actual.Body.String() != expected.Body.String() || !reflect.DeepEqual(actual.Header(), expected.Header()) {
					t.Errorf("bus route %s %s differed: %d %q vs %d %q", method, path, actual.Code, actual.Body.String(), expected.Code, expected.Body.String())
				}
			}
		}
	}
	out := routingRequest(h, "GET", "/declarations")
	var declaration struct {
		Emits   []events.Emission `json:"emits"`
		Accepts []json.RawMessage `json:"accepts"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &declaration); err != nil {
		t.Fatal(err)
	}
	wantEmits := trail.Emits()
	slices.SortFunc(wantEmits, func(a, b events.Emission) int { return strings.Compare(a.Event, b.Event) })
	if out.Code != 200 || len(declaration.Accepts) != 0 || !reflect.DeepEqual(declaration.Emits, wantEmits) {
		t.Fatalf("declarations=%s", out.Body.String())
	}
}

// R-SIJ0-1GZM R-SKYS-T0H0
func TestRoutingPreservesPathsAndPageFallback(t *testing.T) {
	f := routingNew(t)
	original := seedRouting(t, f, f.cfg.Now().Add(time.Hour))
	pending := store.Pending{State: "untouched-pending", Verifier: "untouched-verifier", UserID: "user-one", UserEmail: "caller@example.test", Provider: "google", Expires: f.cfg.Now().Add(time.Minute)}
	if err := f.cfg.Store.PutPending(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	h := web.Handler(f.cfg)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := broker.New(broker.Config{Clients: f.cfg.Clients, Store: f.cfg.Store, Now: f.cfg.Now, Rand: f.cfg.Rand})
	ph := identity.Require(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Providers: []providers.Provider{f.cfg.Clients[0].Provider}, Store: f.cfg.Store, Broker: b}))
	paths := []string{"/", "/about", "/tools", "/nope", "/nope/connect", "/google", "/google/", "/google/connect/", "/google//connect", "/goo%67le/connect", "/google%2fother/connect", "/google%25/connect", "/token", "/mcp/", "/events/", "/_appkit", "//google/connect", "/google/../google/connect", "/google/connect/.."}
	for _, path := range paths {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE", "HEAD"} {
			actual := routingRequest(h, method, path)
			expected := routingRequest(ph, method, path)
			if actual.Code != expected.Code || actual.Body.String() != expected.Body.String() || !reflect.DeepEqual(actual.Header(), expected.Header()) {
				t.Errorf("fallback %s %s differs: %d %q vs %d %q", method, path, actual.Code, actual.Body.String(), expected.Code, expected.Body.String())
			}
		}
	}
	for _, p := range []struct{ method, path string }{{"PUT", "/google/connect"}, {"DELETE", "/google/connect"}, {"POST", "/google/callback"}, {"GET", "/google/disconnect"}} {
		a := routingRequest(h, p.method, p.path)
		e := routingRequest(ph, p.method, p.path)
		if a.Code != e.Code || a.Body.String() != e.Body.String() || !reflect.DeepEqual(a.Header(), e.Header()) {
			t.Errorf("wrong-method fallback %s %s", p.method, p.path)
		}
	}
	for _, path := range []string{"/_appkit/theme.css", "/_appkit/nope", "/_appkit/../theme.css"} {
		a := routingRequest(h, "GET", path)
		e := routingRequest(page.Static(), "GET", path)
		if a.Code != e.Code || a.Body.String() != e.Body.String() || !reflect.DeepEqual(a.Header(), e.Header()) {
			t.Errorf("static %s differs", path)
		}
	}
	f.flush(t)
	if f.random.n != 0 || f.calls.Load() != 0 || len(f.bus.Events()) != 0 {
		t.Fatal("fallback performed mutation")
	}
	cs, err := f.cfg.Store.List(context.Background(), "user-one")
	if err != nil || !reflect.DeepEqual(cs, []store.Connection{original}) {
		t.Fatalf("connections=%v err=%v", cs, err)
	}
	gotPending, err := f.cfg.Store.TakePending(context.Background(), pending.State, pending.UserID, pending.Provider)
	if err != nil || !reflect.DeepEqual(gotPending, pending) {
		t.Fatalf("pending=%+v err=%v", gotPending, err)
	}
}

// R-2BLQ-57QH
func TestRoutingTokenAlwaysReturnsJSONAndCarriesCause(t *testing.T) {
	f := routingNew(t)
	h := web.Handler(f.cfg)
	for _, p := range []struct {
		method, path, refusal string
		status                int
	}{{"GET", "/token/google", broker.RefusalNotConnected, 404}, {"GET", "/token/nope", broker.RefusalUnknownProvider, 404}, {"GET", "/token/", broker.RefusalUnknownProvider, 404}, {"POST", "/token/google", broker.RefusalMethod, 405}} {
		out := routingRequest(h, p.method, p.path)
		var v broker.Refusal
		if err := json.Unmarshal(out.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if out.Code != p.status || v.Error != p.refusal || !strings.HasPrefix(out.Header().Get("Content-Type"), "application/json") {
			t.Errorf("token %s %s: %d %s", p.method, p.path, out.Code, out.Body.String())
		}
	}
	seedRouting(t, f, f.cfg.Now())
	f.refuse.Store(true)
	out := routingRequest(h, "GET", "/token/google")
	if out.Code != 409 {
		t.Fatalf("refresh status=%d body=%s", out.Code, out.Body.String())
	}
	f.flush(t)
	es := f.bus.Events()
	if len(es) != 1 || es[0].Name != trail.Broken || es[0].Cause != "evt_0123456789abcdef" || es[0].Depth != 4 || es[0].User != "user-one" || es[0].RequestID != "request-one" {
		t.Fatalf("broken event=%+v", es)
	}
}

// R-SJQW-F8QB R-25I8-8D10 R-2F9F-AIYK
func TestRoutingProviderActionsCarryCallerAndCause(t *testing.T) {
	f := routingNew(t)
	h := web.Handler(f.cfg)
	for _, method := range []string{"GET", "POST"} {
		out := routingRequest(h, method, "/google/connect")
		if out.Code != 302 {
			t.Fatalf("connect %s status=%d body=%s", method, out.Code, out.Body.String())
		}
		u, err := url.Parse(out.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		pd, err := f.cfg.Store.TakePending(context.Background(), u.Query().Get("state"), "user-one", "google")
		if err != nil || pd.UserEmail != "caller@example.test" {
			t.Fatalf("pending=%+v err=%v", pd, err)
		}
	}
	if err := f.cfg.Store.PutPending(context.Background(), store.Pending{State: "flow", Verifier: "verifier", UserID: "user-one", UserEmail: "caller@example.test", Provider: "google", Expires: f.cfg.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/google/callback?state=flow&code=sample-code", nil)
	r.Host = "unrelated.example.test"
	r.Header["X-User-Id"] = []string{"user-one", "wrong-user"}
	r.Header["X-User-Email"] = []string{"caller@example.test", "wrong@example.test"}
	r.Header["X-Request-Id"] = []string{"callback-request", "wrong-request"}
	r.Header.Set(events.CauseHeader, "evt_0123456789abcdef")
	r.Header.Set(events.DepthHeader, "3")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if len(out.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("callback set cookie")
	}
	if out.Code != 302 {
		t.Fatalf("callback=%d %s", out.Code, out.Body.String())
	}
	c, err := f.cfg.Store.Get(context.Background(), "user-one", "google")
	if err != nil || c.UserEmail != "caller@example.test" {
		t.Fatalf("connection=%+v err=%v", c, err)
	}
	out = routingRequest(h, "POST", "/google/disconnect")
	if out.Code != 302 {
		t.Fatalf("disconnect=%d %s", out.Code, out.Body.String())
	}
	f.flush(t)
	es := f.bus.Events()
	if len(es) != 2 {
		t.Fatalf("events=%+v", es)
	}
	for i, e := range es {
		want := trail.Created
		rid := "callback-request"
		if i == 1 {
			want = trail.Deleted
			rid = "request-one"
		}
		if e.Name != want || e.User != "user-one" || e.RequestID != rid || e.Cause != "evt_0123456789abcdef" || e.Depth != 4 {
			t.Errorf("action event=%+v", e)
		}
	}
	starts, finishes := map[string]int{}, map[string]int{}
	for _, e := range f.capture.Events() {
		if e.User != "user-one" || (e.RequestID != "request-one" && e.RequestID != "callback-request") {
			t.Errorf("action telemetry identity=%+v", e)
		}
		switch e.Name {
		case "request.started":
			starts[e.RequestID]++
		case "request.finished":
			finishes[e.RequestID]++
			if e.Attrs["status"] != int64(302) {
				t.Errorf("action status=%+v", e)
			}
		}
		if e.Name == trail.Created && e.RequestID != "callback-request" {
			t.Errorf("created identity=%+v", e)
		}
		if e.Name == trail.Deleted && e.RequestID != "request-one" {
			t.Errorf("deleted identity=%+v", e)
		}
	}
	if !reflect.DeepEqual(starts, map[string]int{"request-one": 3, "callback-request": 1}) || !reflect.DeepEqual(finishes, starts) {
		t.Fatalf("request pairs=%v/%v", starts, finishes)
	}

}

// R-2F9F-AIYK R-2GHB-OAP9 R-25I8-8D10
func TestRoutingEveryRequestHasTelemetryWithoutQueriesOrCookies(t *testing.T) {
	f := routingNew(t)
	h := web.Handler(f.cfg)
	for _, p := range []struct{ method, path string }{{"GET", "/"}, {"GET", "/about"}, {"GET", "/tools"}, {"GET", "/google/connect"}, {"GET", "/google/callback?state=secret-state&code=secret-code"}, {"POST", "/google/disconnect"}, {"GET", "/token/google"}, {"GET", "/_appkit/theme.css"}, {"GET", "/unknown"}, {"GET", "/declarations"}, {"POST", "/events"}, {"POST", "/mcp"}} {
		f.flush(t)
		start := len(f.capture.Events())
		r := httptest.NewRequest(p.method, p.path, nil)
		r.Header.Set("X-User-Id", "user-one")
		r.Header.Set("X-User-Email", "caller@example.test")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		f.flush(t)
		if len(out.Header().Values("Set-Cookie")) != 0 {
			t.Errorf("cookie on %s", p.path)
		}
		es := f.capture.Events()[start:]
		if len(es) != 2 {
			t.Fatalf("%s telemetry=%+v", p.path, es)
		}
		if es[0].Name != "request.started" || es[1].Name != "request.finished" || es[0].RequestID != es[1].RequestID || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(es[0].RequestID) {
			t.Fatalf("%s telemetry=%+v", p.path, es)
		}
		for _, e := range es {
			if e.User != "user-one" {
				t.Errorf("user=%q", e.User)
			}
		}
		if !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"method": p.method, "path": r.URL.Path}) || es[1].Attrs["status"] != int64(out.Code) {
			t.Errorf("attrs=%+v %+v", es[0].Attrs, es[1].Attrs)
		}
		data, err := json.Marshal(es)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("secret-state")) || bytes.Contains(data, []byte("secret-code")) {
			t.Fatal("query leaked")
		}
	}
	f.flush(t)
	start := len(f.capture.Events())
	r := httptest.NewRequest("GET", "/about", nil)
	r.Header["X-User-Id"] = []string{"user-one", "ignored-user"}
	r.Header["X-Request-Id"] = []string{"", "ignored-request"}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	f.flush(t)
	es := f.capture.Events()[start:]
	if len(es) != 2 || es[0].RequestID == "ignored-request" || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(es[0].RequestID) || es[0].RequestID != es[1].RequestID || es[0].User != "user-one" {
		t.Fatalf("empty first request id=%+v", es)
	}

}

type routingTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (tr routingTransport) RoundTrip(in *http.Request) (*http.Response, error) {
	r := in.Clone(in.Context())
	r.Host = "gateway.backend.example.test"
	r.Header.Set("X-Forwarded-Proto", "http")
	r.Header.Set(events.CauseHeader, "evt_0123456789abcdef")
	r.Header.Set(events.DepthHeader, "3")
	response, err := tr.base.RoundTrip(r)
	if err == nil && len(response.Header.Values("Set-Cookie")) != 0 {
		tr.t.Error("MCP response set cookie")
	}
	return response, err
}
func routingClient(t *testing.T, h http.Handler) *mcp.Client {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", Name: "routing-test", Version: "test", HTTPClient: &http.Client{Timeout: 5 * time.Second, Transport: routingTransport{base: server.Client().Transport, t: t}}})
}
func routingStructured(t *testing.T, result mcp.Result, v any) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if err = json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if result.IsError() {
		t.Fatalf("tool error=%s", data)
	}
	if err = json.Unmarshal(r.Structured, v); err != nil {
		t.Fatalf("structured result=%s: %v", data, err)
	}
}

// R-AVP9-7UT1
func TestRoutingMCPConnectBaseCapturedFromServices(t *testing.T) {
	for _, entry := range []string{"https://connections.configured.example.test", "https://connections.configured.example.test/", "", "missing"} {
		t.Run(entry, func(t *testing.T) {
			f := routingNew(t)
			if entry != "missing" {
				doc := `{"services":[]}`
				if entry != "" {
					doc = `{"services":[{"name":"connections","url":` + strconvQuote(entry) + `,"description":"example","socket":"/example.sock","enabled":true,"mcp":true}]}`
				}
				if err := os.WriteFile(f.cfg.ServicesPath, []byte(doc), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h := web.Handler(f.cfg)
			if entry != "missing" {
				if err := os.WriteFile(f.cfg.ServicesPath, []byte(`{"services":[]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			client := routingClient(t, h)
			for range 2 {
				result, err := client.CallTool(context.Background(), identity.Caller{UserID: "user-one", Email: "caller@example.test", RequestID: "mcp-request"}, "connect", json.RawMessage(`{"provider":"google"}`))
				if err != nil {
					t.Fatal(err)
				}
				var link toolsLink
				routingStructured(t, result, &link)
				want := "/google/connect"
				if entry != "missing" && entry != "" {
					want = strings.TrimSuffix(entry, "/") + want
				}
				if link.URL != want {
					t.Fatalf("url=%q want=%q", link.URL, want)
				}
			}
		})
	}
}

type toolsLink struct {
	URL string `json:"url"`
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

// R-2ADT-RFZS
func TestRoutingMCPRegistersOnceAndForwardsEventCause(t *testing.T) {
	f := routingNew(t)
	seedRouting(t, f, f.cfg.Now().Add(time.Hour))
	h := web.Handler(f.cfg)
	eager := f.cfg.MCP.Tools()
	catalog := tools.Catalog()
	if len(eager) != len(catalog) {
		t.Fatalf("eager tools=%+v", eager)
	}
	for i, tool := range eager {
		if tool.Name != catalog[i].Name || tool.Description != catalog[i].Description {
			t.Errorf("eager tool=%+v", tool)
		}
	}
	client := routingClient(t, h)
	caller := identity.Caller{UserID: "user-one", Email: "caller@example.test", RequestID: "mcp-request"}
	for range 2 {
		catalog, err := client.ListTools(context.Background(), caller)
		if err != nil {
			t.Fatal(err)
		}
		if len(catalog) != 3 || catalog[0].Name != "list" || catalog[1].Name != "connect" || catalog[2].Name != "disconnect" {
			t.Fatalf("catalog=%+v", catalog)
		}
	}
	result, err := client.CallTool(context.Background(), caller, "disconnect", json.RawMessage(`{"provider":"google"}`))
	if err != nil {
		t.Fatal(err)
	}
	var deleted struct {
		Deleted bool `json:"deleted"`
	}
	routingStructured(t, result, &deleted)
	if !deleted.Deleted {
		t.Fatal("disconnect did not succeed")
	}
	f.flush(t)
	es := f.bus.Events()
	if len(es) != 1 || es[0].Name != trail.Deleted || es[0].RequestID != "mcp-request" || es[0].User != "user-one" || es[0].Cause != "evt_0123456789abcdef" || es[0].Depth != 4 {
		t.Fatalf("MCP event=%+v", es)
	}
	for _, e := range f.capture.Events() {
		if e.User != "user-one" || e.RequestID != "mcp-request" {
			t.Errorf("MCP telemetry identity=%+v", e)
		}
	}
}

// R-SJQW-F8QB
func TestRoutingDuplicateProviderUsesFirstClientForCallback(t *testing.T) {
	f := routingNew(t)
	first := f.cfg.Clients[0]
	first.Provider.DisplayName = "first-provider"
	second := first
	second.Provider.DisplayName = "second-provider"
	f.cfg.Clients = []providers.Client{first, second}
	h := web.Handler(f.cfg)
	actual := routingRequest(h, "GET", "/google/callback")
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	expected := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/google/callback", nil)
	banner := f.cfg.Banner(page.User{Email: "caller@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})
	set.Write(expected, r, 400, "notice", pages.NoticeData{Banner: banner, DisplayName: first.Provider.DisplayName, Failure: pages.FailureMissing})
	if actual.Code != expected.Code || actual.Body.String() != expected.Body.String() || !reflect.DeepEqual(actual.Header(), expected.Header()) {
		t.Fatalf("duplicate callback=%d %q expected=%d %q", actual.Code, actual.Body.String(), expected.Code, expected.Body.String())
	}
}
