package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/files"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
	"github.com/ikigenba/ikigenba/artifacts/internal/settings"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
	"github.com/ikigenba/ikigenba/artifacts/internal/web"
)

type fixture struct {
	cfg    web.Config
	logs   *telemetry.Capture
	bus    *events.Capture
	handle *db.DB
	blobs  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Unix(1700000000, 0) }
	dir := t.TempDir()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "db"), Migrations: artifacts.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	blobs := filepath.Join(dir, "blobs")
	if e = os.Mkdir(blobs, 0700); e != nil {
		t.Fatal(e)
	}
	logs := &telemetry.Capture{}
	bus := &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: logs, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x53}, 65536)), Stderr: io.Discard})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: bus, Telemetry: w, Emits: trail.Emits(), Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x54}, 65536)), Stderr: io.Discard})
	f := &fixture{cfg: web.Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), Store: store.New(d, blobs, store.Config{Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x55}, 65536))}), Telemetry: w, Events: em, Settings: settings.Defaults(), Now: now}, logs: logs, bus: bus, handle: d, blobs: blobs}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test complete")
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	return f
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := f.cfg.Events.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.cfg.Telemetry.Flush(ctx); e != nil {
		t.Fatal(e)
	}
}
func request(method, path, body string, authenticated bool) *http.Request {
	r := httptest.NewRequest(method, "https://artifacts.example"+path, strings.NewReader(body))
	r.Header.Set("X-Request-Id", "request-chosen")
	if authenticated {
		r.Header.Set("X-User-Id", "owner-chosen")
		r.Header.Set("X-User-Email", "owner@example")
	}
	return r
}
func answer(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("responses differ: %d %v %q versus %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-UF10-O94L
func TestAPI(t *testing.T) {
	f := newFixture(t)
	cfg := web.Config{Banner: f.cfg.Banner, MCP: f.cfg.MCP, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Telemetry: f.cfg.Telemetry, Events: f.cfg.Events, Settings: f.cfg.Settings, Now: f.cfg.Now}
	if h := web.Handler(cfg); h == nil {
		t.Fatal("nil handler")
	}
}

// R-DXTM-67I8
func TestEventRoutes(t *testing.T) {
	f := newFixture(t)
	h := web.Handler(f.cfg)
	for _, authenticated := range []bool{false, true} {
		for _, method := range []string{"GET", "POST", "HEAD"} {
			for _, path := range []string{"/events", "/declarations"} {
				direct := events.DeliveryHandler(nil)
				if path == "/declarations" {
					direct = events.DeclarationsHandler(f.cfg.Events, nil)
				}
				same(t, answer(h, request(method, path, "{}", authenticated)), answer(direct, request(method, path, "{}", authenticated)))
			}
		}
	}
}

type countedBody struct{ reads int }

func (b *countedBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*countedBody) Close() error               { return nil }

// R-DZ1I-JZ8X
func TestIdentityGate(t *testing.T) {
	f := newFixture(t)
	f.cfg.Store = nil
	h := web.Handler(f.cfg)
	for _, path := range []string{"/", "/about", "/_appkit/theme.css", "/mcp", "/files/scratch/blob.bin", "/art_5555555555555555", "/events/", "/files/x"} {
		for _, method := range []string{"GET", "HEAD", "PUT", "POST", "DELETE"} {
			r := request(method, path, "", false)
			r.Header["X-User-Id"] = []string{"", "ignored"}
			body := &countedBody{}
			r.Body = body
			got := answer(h, r)
			want := answer(identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("gate delegated") })), r.Clone(context.Background()))
			same(t, got, want)
			if body.reads != 0 {
				t.Fatal("read missing identity body")
			}
		}
	}
	f.flush(t)
	for _, e := range f.logs.Events() {
		if e.Name != "request.started" && e.Name != "request.finished" {
			t.Fatal(e)
		}
	}
	if len(f.bus.Events()) != 0 {
		t.Fatal(f.bus.Events())
	}
}

// R-DWLP-SFRJ R-E3X4-327P
func TestRequestIdentityAndEvents(t *testing.T) {
	f := newFixture(t)
	h := web.Handler(f.cfg)
	for _, first := range []string{"provided-request", ""} {
		r := request("PUT", "/files/identity.bin", "chosen bytes", true)
		r.Header["X-Request-Id"] = []string{first, "ignored"}
		r.Header["X-User-Id"] = []string{"owner-first", "ignored-owner"}
		r.Header["X-User-Email"] = []string{"first@example", "ignored@example"}
		w := answer(h, r)
		if w.Code != 201 && w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		a, e := f.cfg.Store.Get(context.Background(), "identity.bin")
		if e != nil || a.OwnerID != "owner-first" || a.OwnerEmail != "first@example" {
			t.Fatal(a, e)
		}
		f.flush(t)
		logs := f.logs.Events()
		last := logs[len(logs)-3:]
		expected := first
		if expected == "" {
			expected = strings.Repeat("53", 16)
		}
		if last[0].Name != "request.started" || last[1].Name != map[int]string{201: trail.Created, 200: trail.Replaced}[w.Code] || last[2].Name != "request.finished" {
			t.Fatal(last)
		}
		for _, event := range last {
			if event.RequestID != expected || event.User != "owner-first" {
				t.Fatal(event)
			}
		}
		if !reflect.DeepEqual(last[0].Attrs, telemetry.Attrs{"method": "PUT", "path": "/files/identity.bin"}) || last[2].Attrs["status"] != int64(w.Code) {
			t.Fatal(last)
		}
		bus := f.bus.Events()
		if bus[len(bus)-1].RequestID != expected {
			t.Fatal(bus)
		}
		for _, event := range bus {
			if event.User != "owner-first" {
				t.Fatal(event)
			}
		}
	}
}

// R-E1HB-BIQB
func TestFilesRouting(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.ArtifactMaxBytes = 4
	h := web.Handler(f.cfg)
	direct := telemetry.Middleware(f.cfg.Telemetry, identity.Require(events.Middleware(files.Handler(files.Config{Store: f.cfg.Store, Max: 4, ServicesPath: f.cfg.ServicesPath, Telemetry: f.cfg.Telemetry, Events: f.cfg.Events}))))
	for _, c := range []struct{ method, path, body string }{{"PUT", "/files/a//b", "x"}, {"PUT", "/files/../x", "x"}, {"PUT", "/files/./x", "x"}, {"GET", "/files/a%2Fb", ""}, {"PUT", "/files/over.bin", "12345"}, {"PATCH", "/files/missing", ""}, {"GET", "/files/missing", ""}} {
		got := answer(h, request(c.method, c.path, c.body, true))
		same(t, got, answer(direct, request(c.method, c.path, c.body, true)))
		if got.Code >= 300 && got.Code < 400 {
			t.Fatal("redirect", got.Code)
		}
	}
	r := request("PUT", "/files/event.bin", "1234", true)
	r.Header.Set(events.CauseHeader, "evt_1234567890abcdef")
	r.Header.Set(events.DepthHeader, "2")
	w := answer(h, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.flush(t)
	bus := f.bus.Events()
	if len(bus) != 1 || bus[0].Cause != "evt_1234567890abcdef" || bus[0].Depth != 3 || bus[0].User != "owner-chosen" || bus[0].RequestID != "request-chosen" {
		t.Fatal(bus)
	}
}

// R-E2P7-PAH0
func TestPagesAndStaticRouting(t *testing.T) {
	f := newFixture(t)
	h := web.Handler(f.cfg)
	set, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	ph := identity.Require(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store}))
	for _, path := range []string{"/", "/about", "/tools", "/art_5555555555555555", "/nope", "/files", "/fil%65s/x", "/mcp/", "/events/", "/_appkit"} {
		same(t, answer(h, request("GET", path, "", true)), answer(ph, request("GET", path, "", true)))
	}
	for _, path := range []string{"/_appkit/theme.css", "/_appkit/missing"} {
		same(t, answer(h, request("GET", path, "", true)), answer(identity.Require(page.Static()), request("GET", path, "", true)))
	}
}

// R-UG8X-20VA
func TestMCPRouting(t *testing.T) {
	f := newFixture(t)
	h := web.Handler(f.cfg)
	created := answer(h, request("PUT", "/files/mcp.bin", "bytes", true))
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: causeTransport{http.DefaultTransport}}})
	caller := identity.Caller{UserID: "owner-chosen", Email: "owner@example", RequestID: "mcp-chosen"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	infos, e := client.ListTools(ctx, caller)
	if e != nil {
		t.Fatal(e)
	}
	catalog := tools.Catalog()
	if len(infos) != len(catalog) {
		t.Fatal(infos)
	}
	for i, info := range infos {
		if info.Name != catalog[i].Name || info.Description != catalog[i].Description {
			t.Fatal(info)
		}
	}
	raw, e := json.Marshal(tools.PathArgs{Path: "mcp.bin"})
	if e != nil {
		t.Fatal(e)
	}
	result, e := client.CallTool(ctx, caller, "show", raw)
	if e != nil {
		t.Fatal(e)
	}
	var metadata files.Metadata
	if e = json.Unmarshal(structured(t, result), &metadata); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", srv.URL+"/mcp", nil)
	if metadata.URL != urls.File(urls.Base(r, f.cfg.ServicesPath), "mcp.bin") {
		t.Fatal(metadata)
	}
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_1234567890abcdef", Depth: 4})
	result, e = client.CallTool(ctx, caller, "delete", raw)
	if e != nil || result.IsError() {
		t.Fatal(result, e)
	}
	f.flush(t)
	bus := f.bus.Events()
	if len(bus) != 2 || bus[1].Cause != "evt_1234567890abcdef" || bus[1].Depth != 5 || bus[1].User != caller.UserID || bus[1].RequestID != caller.RequestID {
		t.Fatal(bus)
	}
	if len(f.cfg.MCP.Tools()) != len(catalog) {
		t.Fatal(f.cfg.MCP.Tools())
	}
}

// R-E550-GTYE
func TestNoCookies(t *testing.T) {
	f := newFixture(t)
	h := web.Handler(f.cfg)
	for _, authenticated := range []bool{false, true} {
		for _, path := range []string{"/", "/about", "/tools", "/nope", "/events", "/declarations", "/_appkit/theme.css", "/mcp", "/files/x"} {
			w := answer(h, request("GET", path, "", authenticated))
			if len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatal(path, w.Header())
			}
		}
	}
}

type causeTransport struct{ base http.RoundTripper }

func (tr causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	events.Forward(r.Context(), r)
	return tr.base.RoundTrip(r)
}
func structured(t *testing.T, result mcp.Result) json.RawMessage {
	t.Helper()
	b, e := json.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	var obj map[string]json.RawMessage
	if e = json.Unmarshal(b, &obj); e != nil {
		t.Fatal(e)
	}
	return obj["structuredContent"]
}

// R-E3X4-327P
func TestEveryRouteRequestPair(t *testing.T) {
	f := newFixture(t)
	h := web.Handler(f.cfg)
	for _, authenticated := range []bool{true, false} {
		for _, path := range []string{"/", "/about", "/tools", "/nope", "/events", "/declarations", "/_appkit/theme.css", "/mcp", "/files/x", "/events/"} {
			before := len(f.logs.Events())
			r := request("GET", path, "", authenticated)
			w := answer(h, r)
			f.flush(t)
			logged := f.logs.Events()[before:]
			if len(logged) != 2 || logged[0].Name != "request.started" || logged[1].Name != "request.finished" {
				t.Fatal(path, logged)
			}
			user := ""
			if authenticated {
				user = "owner-chosen"
			}
			for _, e := range logged {
				if e.RequestID != "request-chosen" || e.User != user {
					t.Fatal(path, e)
				}
			}
			if !reflect.DeepEqual(logged[0].Attrs, telemetry.Attrs{"method": "GET", "path": r.URL.Path}) || logged[1].Attrs["status"] != int64(w.Code) {
				t.Fatal(path, logged)
			}
		}
	}
}

type outageBody struct {
	reader *strings.Reader
	reads  int
	bytes  int
	eof    bool
}

func (b *outageBody) Read(p []byte) (int, error) {
	b.reads++
	n, err := b.reader.Read(p)
	b.bytes += n
	b.eof = b.eof || err == io.EOF
	return n, err
}

func (*outageBody) Close() error { return nil }

// R-MVXY-S377 R-MSA9-MRZ4
func TestFailingStoreOnlyRecordsRequestAndToolEvents(t *testing.T) {
	f := newFixture(t)
	a, _, err := f.cfg.Store.Put(context.Background(), store.Draft{Path: "existing.bin", OwnerID: "owner-chosen", Max: 64}, strings.NewReader("retained fixture bytes"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.cfg.Store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	blobNames := func() []string {
		entries, err := os.ReadDir(f.blobs)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	beforeBlobs := blobNames()
	f.handle.SetFailing(true)
	h := web.Handler(f.cfg)
	checkEvents := func(start int, tool bool) {
		t.Helper()
		f.flush(t)
		events := f.logs.Events()[start:]
		want := map[string]int{"request.started": 1, "request.finished": 1}
		if tool {
			want["tool.called"] = 1
		}
		for _, event := range events {
			want[event.Name]--
		}
		for name, count := range want {
			if count != 0 {
				t.Fatalf("event count for %s differs by %d: %#v", name, count, events)
			}
		}
		if len(f.bus.Events()) != 0 {
			t.Fatal("outage emitted bus event", f.bus.Events())
		}
	}
	for _, path := range []string{"/files/existing.bin", "/files/missing.bin", "/", "/" + a.ID, "/art_6666666666666666"} {
		methods := []string{"GET", "HEAD"}
		if strings.HasPrefix(path, "/files/") {
			methods = append(methods, "PUT", "DELETE")
		}
		for _, method := range methods {
			for _, expiry := range []string{"", f.cfg.Now().Add(-time.Second).Format(time.RFC3339)} {
				if method != "PUT" && expiry != "" {
					continue
				}
				start := len(f.logs.Events())
				r := request(method, path, "", true)
				const payload = "outage request bytes"
				body := &outageBody{reader: strings.NewReader(payload)}
				r.Body = body
				if expiry != "" {
					r.Header.Set("X-Artifact-Expires", expiry)
				}
				w := answer(h, r)
				want := store.Unreachable + "\n"
				if method == "HEAD" {
					want = ""
				}
				if w.Code != 503 || w.Body.String() != want || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) {
					t.Fatalf("%s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
				}
				if method == "PUT" {
					if body.bytes != len(payload) || !body.eof {
						t.Fatal("outage PUT did not drain body", body.bytes, body.eof)
					}
				} else if body.reads != 0 {
					t.Fatal("outage read body")
				}
				checkEvents(start, false)
			}
		}
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	for _, c := range []struct {
		name string
		args any
	}{{"list", tools.ListArgs{}}, {"show", tools.PathArgs{Path: "existing.bin"}}, {"show", tools.PathArgs{Path: "missing.bin"}}, {"delete", tools.PathArgs{Path: "existing.bin"}}, {"delete", tools.PathArgs{Path: "missing.bin"}}} {
		start := len(f.logs.Events())
		raw, err := json.Marshal(c.args)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		result, err := client.CallTool(ctx, identity.Caller{UserID: "owner-chosen"}, c.name, raw)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		wire, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(wire, &reply); err != nil {
			t.Fatal(err)
		}
		if !result.IsError() || len(reply.Content) != 1 || reply.Content[0].Type != "text" || reply.Content[0].Text != store.Unreachable || len(structured(t, result)) != 0 {
			t.Fatalf("tool outage: %s", wire)
		}
		checkEvents(start, true)
	}
	f.handle.SetFailing(false)
	if !reflect.DeepEqual(beforeBlobs, blobNames()) {
		t.Fatal("outage changed blob directory")
	}
	after, err := f.cfg.Store.List(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("outage changed records", before, after, err)
	}
	_, file, err := f.cfg.Store.Open(context.Background(), a.Path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(data) != "retained fixture bytes" {
		t.Fatal("outage changed bytes", string(data), err)
	}
}

// R-UUVP-N9RM R-UG8X-20VA
func TestConfiguredImportPlans(t *testing.T) {
	for _, tc := range []struct{ name, entryURL, base string }{
		{"configured", "https://published.example.test", "https://published.example.test"},
		{"trailing slash", "https://published.example.test/", "https://published.example.test"},
		{"no entry", "", ""},
		{"missing file", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
			if tc.name != "missing file" {
				entries := []map[string]any{}
				if tc.entryURL != "" {
					entries = append(entries, map[string]any{"name": urls.Service, "url": tc.entryURL, "description": "", "socket": "", "enabled": true, "mcp": true})
				}
				data, err := json.Marshal(map[string]any{"services": entries})
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(f.cfg.ServicesPath, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			srv := httptest.NewServer(web.Handler(f.cfg))
			defer srv.Close()
			catalog := tools.Catalog()
			caller := identity.Caller{UserID: "caller-chosen", Email: "caller@example", RequestID: "request-chosen"}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			for _, origin := range []struct{ host, scheme string }{{"gateway.example.test", "https"}, {"different.example.test:8123", "http"}} {
				client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: originTransport{origin.host, origin.scheme}}})
				infos, err := client.ListTools(ctx, caller)
				if err != nil {
					t.Fatal(err)
				}
				if len(infos) != len(catalog) {
					t.Fatal(infos)
				}
				for i, info := range infos {
					if info.Name != catalog[i].Name || info.Description != catalog[i].Description {
						t.Fatalf("unexpected catalog: %#v", info)
					}
				}
				args, err := json.Marshal(tools.ImportArgs{Path: "plan.bin"})
				if err != nil {
					t.Fatal(err)
				}
				result, err := client.CallTool(ctx, caller, "import", args)
				if err != nil || result.IsError() {
					t.Fatal(result, err)
				}
				var plan tools.Plan
				if err = json.Unmarshal(structured(t, result), &plan); err != nil {
					t.Fatal(err)
				}
				if plan.URL != tc.base+"/files/plan.bin" {
					t.Fatalf("plan base: %#v", plan)
				}
				// Changing the services file after construction must not register
				// the tools again or change the configured plan base on a later request.
				if err = os.WriteFile(f.cfg.ServicesPath, []byte(`{"services":[{"name":"artifacts","url":"https://later.example.test/","description":"","socket":"","enabled":true,"mcp":true}]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type originTransport struct{ host, scheme string }

func (tr originTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Host = tr.host
	r.Header.Set("X-Forwarded-Proto", tr.scheme)
	return http.DefaultTransport.RoundTrip(r)
}
