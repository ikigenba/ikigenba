package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/tools"
	"github.com/ikigenba/ikigenba/cron/internal/trail"
)

func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func require(t *testing.T, ok bool, why string) {
	t.Helper()
	if !ok {
		t.Fatal(why)
	}
}
func templates(t *testing.T) *template.Template {
	t.Helper()
	// R-D7V6-ODWX R-ZC7P-WVO7
	ts, err := page.Templates().ParseFS(cron.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"landing", "about", "tools", "notfound"} {
		require(t, ts.Lookup(n) != nil, "missing template "+n)
	}
	return ts
}
func rendered(t *testing.T, ts *template.Template, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := ts.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func loaded(t *testing.T) *pages.Set {
	t.Helper()
	s, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	require(t, s != nil, "nil set")
	return s
}

func TestPublicDataAndTemplateExecution(t *testing.T) {
	// R-CUGA-GWRA R-468J-UWBQ R-CWW3-8G8O R-CY3Z-M7ZD
	equal(t, pages.ServiceName, "cron")
	const description string = pages.Description
	require(t, description != "", "empty description")
	load := pages.Load
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	write := s.Write
	// R-CZBV-ZZQ2 R-D1RO-RJ7G R-D2ZL-5AY5 R-D47H-J2OU
	row := pages.TriggerRow{"crn_example", "slug", "@hourly", "owner@example.test", "active", true, "stamp", "minute", "next", "nextminute"}
	equal(t, row.ID, "crn_example")
	equal(t, row.Mine, true)
	equal(t, row.NextText, "nextminute")
	b := page.Banner{Service: "cron", Release: "test-release", Commit: "test-commit", Email: "a<&@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}
	data := []struct {
		name       string
		zero, full any
	}{
		{"landing", pages.LandingData{}, pages.LandingData{b, []pages.TriggerRow{row}}},
		{"about", pages.AboutData{}, pages.AboutData{b, "<description>"}},
		// R-ZQUI-I4KJ R-ZS2E-VWB8
		{"tools", pages.ToolsData{}, pages.ToolsData{b, []pages.Tool{{"example", "<description>"}}}},
		{"notfound", pages.NoticeData{}, pages.NoticeData{b}},
	}
	ts := templates(t)
	// R-ZDFM-ANEW R-ZENI-OF5L R-ZFVF-26WA R-ZH3B-FYMZ
	for _, d := range data {
		for _, v := range []any{d.zero, d.full} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				for _, status := range []int{200, 404} {
					w := httptest.NewRecorder()
					w.Header()["X-Keep"] = []string{"one", "two"}
					w.Header()["Content-Type"] = []string{"old", "older"}
					write(w, httptest.NewRequest(method, "/", nil), status, d.name, v)
					equal(t, w.Code, status)
					equal(t, w.Header(), http.Header{"X-Keep": []string{"one", "two"}, "Content-Type": []string{"text/html; charset=utf-8"}})
					want := rendered(t, ts, d.name, v)
					if method == "HEAD" {
						want = ""
					}
					equal(t, w.Body.String(), want)
				}
			}
		}
	}
	// R-CLC3-O8LH
	t.Chdir(t.TempDir())
	for range 2 {
		require(t, loaded(t) != nil, "load outside checkout failed")
	}
}

type fixture struct {
	db        *db.DB
	store     *store.Store
	scheduler *scheduler.Scheduler
	cfg       pages.Config
	banner    page.Banner
	path      string
	triggers  []store.Trigger
}

var now = time.Date(2026, 10, 5, 9, 32, 0, 0, time.UTC)

func setup(t *testing.T, populated bool) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	dir := t.TempDir()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "cron.db"), Migrations: cron.Migrations(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	random := make([]byte, 1024)
	for i := range random {
		random[i] = byte(i)
	}
	st := store.New(d, store.Config{Now: func() time.Time { return now.Add(-24 * time.Hour) }, Rand: bytes.NewReader(random)})
	f := &fixture{db: d, store: st, banner: page.Banner{Service: "cron", Release: "test-release", Commit: "test-commit", Home: "/sentinel-home", Tools: true, Email: "source@example.test", ProfileURL: "/sentinel-profile", LogoutURL: "/sentinel-logout", Trail: []page.Level{{Name: "source", URL: "/source"}}}, path: filepath.Join(dir, "services.json")}
	if populated {
		for _, draft := range []store.Draft{
			{Slug: "weekly_digest", When: "@weekly", OwnerID: "u_7f3a9c21", OwnerEmail: "mira@example.test"},
			{Slug: "nightly_backup", When: "0 2 * * *", OwnerID: "u_2b8e1d04", OwnerEmail: "noah@example.test"},
			{Slug: "month_end", When: "0 0 31 * *", OwnerID: "u_2b8e1d04", OwnerEmail: "noah@example.test"},
			{Slug: "hourly", When: "@hourly", OwnerID: "u_7f3a9c21", OwnerEmail: "mira@example.test"},
			{Slug: "never", When: "0 0 31 2 *", OwnerID: "u_2b8e1d04", OwnerEmail: "noah@example.test"},
		} {
			x, e := st.Create(context.Background(), draft)
			if e != nil {
				t.Fatal(e)
			}
			if x.Slug == "weekly_digest" {
				if _, e = st.SetStatus(context.Background(), x.ID, store.Paused); e != nil {
					t.Fatal(e)
				}
			}
			if x.Slug == "hourly" {
				if _, e = st.SetLastFired(context.Background(), x.ID, time.Date(2026, 10, 5, 11, 0, 0, 0, time.FixedZone("east", 7200))); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	w := telemetry.New(telemetry.Config{Service: "cron", Sink: &telemetry.Capture{}, Stderr: &bytes.Buffer{}, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test complete") })
	em := events.New(events.Config{Service: "cron", Sink: &events.Capture{}, Stderr: &bytes.Buffer{}, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Sleep: func(context.Context, time.Duration) {}, Telemetry: w, Emits: trail.Emits()})
	t.Cleanup(func() { em.Shutdown(context.Background()) })
	sch, err := scheduler.Start(context.Background(), scheduler.Config{Store: st, Events: em, Telemetry: w, Now: func() time.Time { return now }, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: bytes.NewReader(make([]byte, 1024))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sch.Stop)
	f.scheduler = sch
	f.triggers, err = st.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// R-ZIB7-TQDO R-ZJJ4-7I4D
	srv := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w})
	tools.Register(srv, tools.Config{Store: st, Scheduler: sch})
	f.cfg = pages.Config{func(page.User) page.Banner { return f.banner }, loaded(t), f.path, st, sch, srv}
	return f
}
func request(method, path, user, email, host, proto string) *http.Request {
	r := httptest.NewRequest(method, "https://cron.example.test"+path, strings.NewReader("arbitrary body"))
	r.Host = host
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	r.Header.Set("X-User-Email", email)
	r.Header.Set("X-Forwarded-Proto", proto)
	r.Header.Set("X-Other", "irrelevant")
	return r
}
func answer(cfg pages.Config, r *http.Request) *httptest.ResponseRecorder {
	// R-DDYO-L8ME
	w := httptest.NewRecorder()
	identity.Require(pages.Handler(cfg)).ServeHTTP(w, r)
	return w
}
func expectedBanner(f *fixture, path string) page.Banner {
	// R-ZTAB-9O1X
	b := f.banner
	b.Trail = nil
	if path == "/about" {
		b.Trail = []page.Level{{Name: "about", URL: "/about"}}
	}
	if path == "/tools" {
		b.Trail = []page.Level{{Name: "tools", URL: "/tools"}}
	}
	return b
}
func expectedRows(f *fixture, user string) []pages.TriggerRow {
	// R-DK26-I3BV R-DLA2-VV2K
	rows := make([]pages.TriggerRow, 0, len(f.triggers))
	for _, x := range f.triggers {
		r := pages.TriggerRow{ID: x.ID, Slug: x.Slug, When: x.When, Owner: x.OwnerEmail, Status: x.Status, Mine: x.OwnerID == user}
		if !x.LastFired.IsZero() {
			r.LastFired = x.LastFired.UTC().Format(time.RFC3339)
			r.LastFiredText = x.LastFired.UTC().Format("2006-01-02 15:04")
		}
		if n, ok := f.scheduler.Next(x.ID); ok {
			r.Next = n.UTC().Format(time.RFC3339)
			r.NextText = n.UTC().Format("2006-01-02 15:04")
		}
		rows = append(rows, r)
	}
	return rows
}
func TestLandingAndAboutExactBodies(t *testing.T) {
	// R-ZPMM-4CTU R-ZKR0-L9V2
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprint(populated), func(t *testing.T) {
			f := setup(t, populated)
			ts := templates(t)
			for _, user := range []string{"u_7f3a9c21", "u_2b8e1d04"} {
				for _, path := range []string{"/", "/about"} {
					var seen []page.User
					f.cfg.Banner = func(u page.User) page.Banner { seen = append(seen, u); return f.banner }
					w := answer(f.cfg, request("GET", path+"?arbitrary=query", user, "MiRa+tag@example.test", "cron.sbx.example.test:443", "HTTPS"))
					equal(t, w.Code, 200)
					equal(t, w.Header(), http.Header{"Content-Type": []string{"text/html; charset=utf-8"}})
					want := rendered(t, ts, "about", pages.AboutData{Banner: expectedBanner(f, "/about"), Description: pages.Description})
					if path == "/" {
						want = rendered(t, ts, "landing", pages.LandingData{Banner: expectedBanner(f, "/"), Triggers: expectedRows(f, user)})
					}
					equal(t, w.Body.String(), want)
					require(t, len(seen) > 0, "banner not called")
					for _, u := range seen {
						equal(t, u, page.User{Email: "MiRa+tag@example.test", ProfileURL: "https://auth.sbx.example.test/", LogoutURL: "https://auth.sbx.example.test/logout"})
					}
				}
			}
		})
	}
}
func writeServices(t *testing.T, path, url string) {
	t.Helper()
	data := fmt.Sprintf(`{"services":[{"name":"auth","url":%q,"description":"auth","socket":"/unused","enabled":true,"mcp":false}]}`, url)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestBannerUserAndFreshServices(t *testing.T) {
	// R-DF6K-Z0D3 R-DGEH-CS3S R-DHMD-QJUH
	f := setup(t, false)
	cases := []struct{ host, proto, base string }{
		{"cron.sbx.example.test:443", "https", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "HTTPS", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "Http", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "https, http", "https://auth.sbx.example.test"},
		{"sbx.example.test", "https", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "http", "http://auth.sbx.example.test"},
		{"cron.", "http", "http://auth.cron."},
		{"cron.sbx.example.test:abc", "http", "http://auth.sbx.example.test:abc"},
		{"cron.sbx.example.test:", "http", "http://auth.sbx.example.test"},
	}
	var got page.User
	f.cfg.Banner = func(u page.User) page.Banner { got = u; return f.banner }
	for _, servicesMode := range []string{"missing", "empty-path", "malformed", "no-auth", "empty-url", "custom", "rewritten"} {
		f.cfg.ServicesPath = f.path
		switch servicesMode {
		case "empty-path":
			f.cfg.ServicesPath = ""
		case "malformed":
			if e := os.WriteFile(f.path, []byte("bad"), 0600); e != nil {
				t.Fatal(e)
			}
		case "no-auth":
			if e := os.WriteFile(f.path, []byte(`{"services":[]}`), 0600); e != nil {
				t.Fatal(e)
			}
		case "empty-url":
			writeServices(t, f.path, "")
		case "custom":
			writeServices(t, f.path, "http://accounts.example.test/prefix/")
		case "rewritten":
			writeServices(t, f.path, "https://replacement.example.test")
		}
		for _, c := range cases {
			for _, path := range []string{"/", "/about", "/tools", "/nope"} {
				base := c.base
				if servicesMode == "custom" {
					base = "http://accounts.example.test/prefix/"
				}
				if servicesMode == "rewritten" {
					base = "https://replacement.example.test"
				}
				w := answer(f.cfg, request("GET", path, "caller", "Email<&@example.test", c.host, c.proto))
				require(t, w.Code == 200 || w.Code == 404, "page failed")
				equal(t, got, page.User{Email: "Email<&@example.test", ProfileURL: base + "/", LogoutURL: base + "/logout"})
			}
		}
	}
}
func TestRoutesMethodsFailuresAndReadOnly(t *testing.T) {
	// R-ZLYW-Z1LR R-ZOEP-QL35 R-ZN6T-CTCG R-49W9-07JT R-DTTD-K99F R-CRFL-L3AY R-CSNH-YV1N
	f := setup(t, true)
	ts := templates(t)
	before := append([]store.Trigger(nil), f.triggers...)
	next := map[string]time.Time{}
	has := map[string]bool{}
	for _, x := range before {
		next[x.ID], has[x.ID] = f.scheduler.Next(x.ID)
	}
	for _, path := range []string{"/", "/about", "/tools", "/nope", "/hourly", "/hourly/", "/about/", "/tools/", "/mcp/", "/_appkit", "//"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			var healthy *httptest.ResponseRecorder
			for _, failing := range []bool{false, true} {
				f.db.SetFailing(failing)
				w := answer(f.cfg, request(method, path+"?anything=yes", "viewer", "viewer@example.test", "cron.example.test", "https"))
				equal(t, w.Header().Values("Set-Cookie"), []string(nil))
				equal(t, w.Header().Values("Location"), []string(nil))
				switch {
				case path != "/" && path != "/about" && path != "/tools":
					equal(t, w.Code, 404)
					equal(t, w.Header().Values("Allow"), []string(nil))
					equal(t, w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"})
					want := rendered(t, ts, "notfound", pages.NoticeData{Banner: expectedBanner(f, path)})
					if method == "HEAD" {
						want = ""
					}
					equal(t, w.Body.String(), want)
				case method != "GET" && method != "HEAD":
					equal(t, w.Code, 405)
					equal(t, w.Header().Values("Allow"), []string{"GET, HEAD"})
					equal(t, w.Body.String(), "")
				case path == "/" && failing:
					equal(t, w.Code, 503)
					equal(t, w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"})
					want := store.Unreachable + "\n"
					if method == "HEAD" {
						want = ""
					}
					equal(t, w.Body.String(), want)
				default:
					equal(t, w.Code, 200)
					equal(t, w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"})
				}
				if healthy == nil {
					healthy = w
				} else if path != "/" || (method != "GET" && method != "HEAD") {
					equal(t, w.Code, healthy.Code)
					equal(t, w.Header(), healthy.Header())
					equal(t, w.Body.String(), healthy.Body.String())
				}
				if method == "HEAD" {
					get := answer(f.cfg, request("GET", path+"?anything=yes", "viewer", "viewer@example.test", "cron.example.test", "https"))
					equal(t, w.Code, get.Code)
					equal(t, w.Header(), get.Header())
					equal(t, w.Body.String(), "")
				}
			}
		}
	}
	f.db.SetFailing(false)
	after, err := f.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	equal(t, after, before)
	for _, x := range before {
		n, ok := f.scheduler.Next(x.ID)
		equal(t, n, next[x.ID])
		equal(t, ok, has[x.ID])
	}
}
func TestConcurrentPages(t *testing.T) {
	// R-DXH2-PKHI
	f := setup(t, true)
	h := identity.Require(pages.Handler(f.cfg))
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := "/"
			switch i % 3 {
			case 0:
				path = "/about"
			case 1:
				path = "/tools"
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request("GET", path, "viewer", "viewer@example.test", "cron.example.test", "https"))
			if w.Code != 200 {
				t.Errorf("status %d", w.Code)
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatal("concurrent page requests did not finish")
	}
}

func TestLandingPrivateData(t *testing.T) {
	// R-4NOH-AV0L R-ET6G-NLRK
	f := setup(t, true)
	f.banner.Home = "https://home.example.test/"
	body := answer(f.cfg, request("GET", "/", "viewer", "viewer@example.test", "cron.example.test", "https")).Body.String()
	equal(t, body, rendered(t, templates(t), "landing", pages.LandingData{Banner: expectedBanner(f, "/"), Triggers: expectedRows(f, "viewer")}))
	for _, x := range f.triggers {
		require(t, !strings.Contains(body, x.OwnerID), "owner id exposed")
		require(t, !strings.Contains(body, x.Created.UTC().Format(time.RFC3339)), "creation stamp exposed")
		require(t, !strings.Contains(body, x.Created.UTC().Format("2006-01-02 15:04")), "creation text exposed")
	}
}

func TestToolsExactBodiesAndFreshRegistration(t *testing.T) {
	// R-ZUI7-NFSM R-ZWY0-EZA0 R-ZTAB-9O1X
	f := setup(t, false)
	ts := templates(t)
	registered := f.cfg.MCP
	for _, server := range []string{"empty", "custom", "cron"} {
		if server != "cron" {
			capture := &telemetry.Capture{}
			writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: capture, Stderr: &bytes.Buffer{}, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Sleep: func(context.Context, time.Duration) {}})
			t.Cleanup(func() { writer.Shutdown(context.Background(), "test complete") })
			f.cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: writer})
		} else {
			f.cfg.MCP = registered
		}
		for step := range 2 {
			if server == "custom" && step == 1 {
				for _, tool := range []pages.Tool{{Name: "zulu", Description: "First supplied tool.\n\nFull description <&> for a model."}, {Name: "alpha", Description: "Second supplied tool."}} {
					mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: tool.Name, Description: tool.Description, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
				}
			}
			expected := make([]pages.Tool, 0)
			for _, tool := range f.cfg.MCP.Tools() {
				expected = append(expected, pages.Tool{Name: tool.Name, Description: tool.Description})
			}
			for _, failing := range []bool{false, true} {
				f.db.SetFailing(failing)
				var seen []page.User
				f.cfg.Banner = func(u page.User) page.Banner { seen = append(seen, u); return f.banner }
				w := answer(f.cfg, request("GET", "/tools?arbitrary=query", "viewer", "supplied<&@example.test", "cron.example.test", "http"))
				equal(t, w.Code, 200)
				equal(t, w.Header(), http.Header{"Content-Type": []string{"text/html; charset=utf-8"}})
				equal(t, w.Body.String(), rendered(t, ts, "tools", pages.ToolsData{Banner: expectedBanner(f, "/tools"), Tools: expected}))
				require(t, len(seen) > 0, "banner source not called")
				for _, user := range seen {
					equal(t, user, page.User{Email: "supplied<&@example.test", ProfileURL: "http://auth.example.test/", LogoutURL: "http://auth.example.test/logout"})
				}
			}
		}
	}
}
