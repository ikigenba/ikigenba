package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/serving"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
	"github.com/ikigenba/ikigenba/sites/internal/web"
)

// R-UXAG-GEV5 R-QF41-AQY0 R-XPM2-VTHM R-WWFV-811E R-UETF-UB43
// R-WPX4-Q95Y R-WR51-40WN R-WYVN-ZKIS R-WW0M-N3VF R-WX8J-0VM4
// R-QMFF-LDE6 R-XD37-ZW95 R-QJZM-TTWS R-QIRQ-G263
func TestRoutesMatchTheirOwningHandlers(t *testing.T) {
	f := fresh(t)
	configureServices(t, f)
	f.repository(t)
	sha := f.commit(t, "domain routing body")
	site := f.add(t, "blog", store.Public)
	domain := "blog.example.com"
	if _, _, e := f.cfg.Store.Update(context.Background(), site.ID, store.Change{Domain: &domain}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.cfg.Store.Publish(context.Background(), site.ID, sha, ""); e != nil {
		t.Fatal(e)
	}
	if e := f.cfg.Cache.Unpack(context.Background(), site.ID, site.Repo, sha, ""); e != nil {
		t.Fatal(e)
	}
	p, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	sc := serving.Config{Banner: f.cfg.Banner, Pages: p, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Cache: f.cfg.Cache, Telemetry: f.cfg.Telemetry, Rand: f.cfg.Rand}
	registered := f.cfg.MCP.Tools()
	pageTools := make([]pages.Tool, len(registered))
	for i, tool := range registered {
		pageTools[i] = pages.Tool{Name: tool.Name, Description: tool.Description}
	}
	own := map[string]http.Handler{"apex": serving.Apex(sc), "domain": serving.Domains(sc), "site": serving.Sites(sc), "page": pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: p, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Tools: pageTools}), "static": page.Static()}
	for _, test := range []struct {
		host  string
		route string
	}{
		{"", "gateway"}, {"backend", "gateway"}, {"localhost", "gateway"}, {"notes:80", "gateway"},
		{"sites", "gateway"}, {"sites:80", "gateway"}, {"Sites.SBX.ikigenba.dev:8443", "gateway"},
		{"sites.sbx.ikigenba.dev", "gateway"}, {"ikigenba.dev", "apex"}, {"IKIGENBA.dev:443", "apex"},
		{"sites.example.com", "domain"}, {"sites.other.dev", "domain"}, {"sbx.ikigenba.dev", "domain"},
		{"www.ikigenba.dev", "domain"}, {"blog.example.com", "domain"}, {"blog.sbx.ikigenba.dev", "domain"},
		{"sitesx.dev", "domain"}, {"www.sites.dev", "domain"}, {"SİTES.example", "domain"},
		{"backend:80:90", "gateway"}, {"[::1]:80", "gateway"}, {"example.org:80:90", "domain"}, {"[::ffff:192.0.2.1]:80", "domain"},
	} {
		host, route := test.host, test.route
		for _, path := range []string{"/", "/about", "/tools", "/mcp", "/_appkit/theme.css", "/_appkit", "/about/", "/tools/", "/mcp/", "/mcp/tools", "//", "/blog/../x", "/nope?next=/mcp", "/tools?next=/mcp", "/?next=/_appkit/theme.css", "/%74ools", "/%61bout", "/%6dcp", "/_appkit%2ftheme.css"} {
			for _, method := range []string{"GET", "HEAD", "POST", "OPTIONS", "CUSTOM"} {
				for _, user := range []string{"", "user"} {
					request := func() *http.Request {
						r := httptest.NewRequest(method, path, strings.NewReader("fixture-body"))
						r.Host = host
						r.Header.Set("X-Request-Id", "req_0123456789abcdef0123456789abcdef")
						r.Header.Set("Cookie", "ikigenba_visitor=vis_0123456789abcdef")
						r.Header.Set("X-User-Id", user)
						r.Header.Set("X-User-Email", "other@example.invalid")
						r.Header.Set("X-Forwarded-Host", "unrelated.example")
						r.Header.Set("X-Original-URI", "/mcp")
						r.Header.Set("X-Forwarded-Proto", "https")
						return r
					}
					decoded := request().URL.Path
					expected := own["site"]
					switch {
					case route != "gateway":
						expected = own[route]
					case decoded == "/" || decoded == "/about" || decoded == "/tools":
						expected = own["page"]
					case strings.HasPrefix(decoded, "/_appkit/"):
						expected = own["static"]
					case decoded == "/mcp":
						expected = identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							f.cfg.MCP.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), urls.SitesURL(r, f.cfg.ServicesPath))))
						}))
					}
					a, b := httptest.NewRecorder(), httptest.NewRecorder()
					f.h.ServeHTTP(a, request())
					identity.Optional(expected).ServeHTTP(b, request())
					if a.Code != b.Code || a.Body.String() != b.Body.String() || !reflect.DeepEqual(a.Header(), b.Header()) {
						t.Fatalf("%s %s host=%q user=%q: got %d %v want %d %v", method, path, host, user, a.Code, a.Header(), b.Code, b.Header())
					}
				}
			}
		}
	}
	for _, path := range []string{"/", "/about", "/tools", "/mcp", "/_appkit/theme.css", "/nope"} {
		a := f.get(t, "GET", path, "sites", "user", nil)
		b := f.get(t, "GET", path, "sites", "user", map[string]string{"X-User-Email": ""})
		if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Code == 500 {
			t.Fatal("email precondition", path)
		}
	}
}

// R-WTKT-VKE1 R-UETF-UB43
func TestToolRegistration(t *testing.T) {
	f := fresh(t)
	other := mcp.NewServer(mcp.ServerConfig{Name: "sites", Version: "fixture", Telemetry: f.cfg.Telemetry})
	tools.Register(other, tools.Config{Store: f.cfg.Store, Cache: f.cfg.Cache, Limits: f.cfg.Limits, Telemetry: f.cfg.Telemetry})
	b := httptest.NewServer(telemetry.Middleware(f.cfg.Telemetry, identity.Require(other)))
	defer b.Close()
	get := func(t *testing.T, endpoint string) []mcp.ToolInfo {
		t.Helper()
		client := mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint})
		list, err := client.ListTools(context.Background(), identity.Caller{UserID: "user", RequestID: "req_123456789abcdef0123456789abcdef0"})
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	want := get(t, b.URL)
	for _, host := range []string{"", "backend", "localhost", "notes:80", "sites", "sites:80", "Sites.sbx.ikigenba.dev", "sites.sbx.ikigenba.dev:8443"} {
		t.Run(host, func(t *testing.T) {
			a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = host; f.h.ServeHTTP(w, r) }))
			defer a.Close()
			got := get(t, a.URL+"/mcp")
			if len(got) != 7 || !reflect.DeepEqual(got, want) {
				t.Fatalf("registration %v != %v", got, want)
			}
		})
	}
}

// R-QL7J-7LNH R-XMS7-WNA2
func TestCatalogRefusalAndRecovery(t *testing.T) {
	f := fresh(t)
	f.add(t, "private", "private")
	public := f.add(t, "public", store.Public)
	domain := "blog.example.com"
	if _, _, e := f.cfg.Store.Update(context.Background(), public.ID, store.Change{Domain: &domain}); e != nil {
		t.Fatal(e)
	}
	configureServices(t, f)
	baseline := f.get(t, "GET", "/about", "sites", "user", nil)
	landing := f.get(t, "GET", "/", "sites", "user", nil)
	after := paging.Token{Tool: "sites"}
	before := paging.Token{Tool: "sites-before"}
	paths := []string{
		"/",
		"/?page=",
		"/?page=&page=bogus",
		"/?page=" + after.Encode(""),
		"/?page=" + after.Encode("between-sites"),
		"/?page=" + after.Encode("between-sites") + "&page=bogus&limit=1",
		"/?page=" + before.Encode("between-sites"),
		"/?page=" + before.Encode("private"),
		"/?page=last",
		"/?page=last&page=bogus",
		"/blog", "/blog/", "/private/", "/mcp", "/about",
	}
	f.db.SetFailing(true)
	for _, host := range []string{"sites", "ikigenba.dev", "blog.example.com", "absent.example.com"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE", "OPTIONS", "CUSTOM"} {
			for _, path := range paths {
				for _, user := range []string{"", "user"} {
					if host != "sites" && host != "ikigenba.dev" && method != "GET" && method != "HEAD" {
						continue
					}
					if host == "sites" && (path == "/mcp" || path == "/about" || method != "GET" && method != "HEAD" || strings.SplitN(path, "?", 2)[0] == "/" && user == "") {
						continue
					}
					r := f.get(t, method, path, host, user, nil)
					if r.Code != 503 || len(r.Header().Values("Content-Type")) != 1 || r.Header().Get("Content-Type") != "text/plain; charset=utf-8" || (method == "HEAD" && r.Body.Len() != 0) || (method != "HEAD" && r.Body.String() != store.Unreachable+"\n") {
						t.Fatalf("catalog %s %s %s user=%q: %d %s", host, method, path, user, r.Code, r.Body.String())
					}
				}
			}
		}
	}
	if r := f.get(t, "GET", "/about", "sites", "user", nil); r.Code != 200 || r.Body.String() != baseline.Body.String() || !reflect.DeepEqual(r.Header(), baseline.Header()) {
		t.Fatal("catalog failure changed about", r.Code)
	}
	f.db.SetFailing(false)
	if r := f.get(t, "GET", "/", "sites", "user", nil); r.Code != landing.Code || r.Body.String() != landing.Body.String() || !reflect.DeepEqual(r.Header(), landing.Header()) {
		t.Fatal("handler did not recover", r.Code)
	}
}

// R-UETF-UB43 R-QIRQ-G263
func TestUnicodeHostsAreDomainRequests(t *testing.T) {
	f := fresh(t)
	for _, host := range []string{"SİTES.example", "SİTES.example:80"} {
		r := f.get(t, "GET", "/mcp", host, "", nil)
		if r.Code != http.StatusNotFound {
			t.Fatalf("Unicode host %q: status %d, want domain 404", host, r.Code)
		}
		if r.Header().Get("Set-Cookie") != "" {
			t.Fatal("domain minted a visitor")
		}
	}
}

// R-QJZM-TTWS R-QMFF-LDE6
func TestRedirectsMatchTheirOwningHandlers(t *testing.T) {
	f := fresh(t)
	configureServices(t, f)
	site := f.add(t, "public", store.Public)
	if _, err := f.cfg.Store.SetApex(context.Background(), site.ID); err != nil {
		t.Fatal(err)
	}
	f.add(t, "private", store.Private)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := serving.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Cache: f.cfg.Cache, Telemetry: f.cfg.Telemetry, Rand: f.cfg.Rand}
	for _, host := range []string{"ikigenba.dev", "sites"} {
		owner := serving.Apex(cfg)
		paths := []string{"/", "/about", "/tools", "/mcp", "/_appkit/theme.css", "/tools/", "/blog/../x", "//"}
		if host == "sites" {
			owner = serving.Sites(cfg)
			paths = []string{"/private", "/private/"}
		}
		for _, path := range paths {
			for _, method := range []string{"GET", "HEAD", "POST", "CUSTOM"} {
				request := func() *http.Request {
					r := httptest.NewRequest(method, path+"?next=/tools", strings.NewReader("redirect-fixture"))
					r.Host = host
					r.Header.Set("X-Request-Id", "req_0123456789abcdef0123456789abcdef")
					r.Header.Set("X-Forwarded-Proto", "https")
					return r
				}
				got, want := httptest.NewRecorder(), httptest.NewRecorder()
				f.h.ServeHTTP(got, request())
				identity.Optional(owner).ServeHTTP(want, request())
				if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) {
					t.Fatalf("redirect %s %s host=%q: got %d %v, want %d %v", method, path, host, got.Code, got.Header(), want.Code, want.Header())
				}
			}
		}
	}
}

// R-WYVN-ZKIS
func TestToolsPageUsesMCPDescriptions(t *testing.T) {
	f := fresh(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "sites"
		f.h.ServeHTTP(w, r)
	}))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	registered, err := client.ListTools(context.Background(), identity.Caller{UserID: "user", RequestID: "req_0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	pageTools := make([]pages.Tool, len(registered))
	for i, tool := range registered {
		pageTools[i] = pages.Tool{Name: tool.Name, Description: tool.Description}
	}
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	owner := identity.Optional(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Tools: pageTools}))
	want := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/tools", nil)
	r.Host = "sites"
	r.Header.Set("X-User-Id", "user")
	owner.ServeHTTP(want, r)
	got := f.get(t, "GET", "/tools", "sites", "user", nil)
	if got.Code != http.StatusOK || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) {
		t.Fatalf("tools page differs from tools/list: status %d", got.Code)
	}
}

func configureServices(t *testing.T, f *fixture) {
	t.Helper()
	path := filepath.Join(f.root, "services.json")
	writeServices(t, path, "https://sites.sbx.ikigenba.dev")
	f.cfg.ServicesPath = path
	f.cfg.MCP = newServer(f.cfg.Telemetry)
	f.h = web.Handler(f.cfg)
}
func writeServices(t *testing.T, path, base string) {
	t.Helper()
	b, e := json.Marshal(map[string]any{"services": []any{map[string]any{"name": "sites", "url": base, "enabled": true, "mcp": true, "description": "fixture", "socket": "/missing"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}

// R-QF41-AQY0 R-UETF-UB43 R-QMFF-LDE6
func TestRoutingReclassifiesHostsWhenServicesChanges(t *testing.T) {
	f := fresh(t)
	configureServices(t, f)
	site := f.add(t, "blog", store.Public)
	if _, e := f.cfg.Store.SetApex(context.Background(), site.ID); e != nil {
		t.Fatal(e)
	}
	for _, q := range []struct {
		base, host string
		status     int
	}{
		{"https://sites.sbx.ikigenba.dev", "ikigenba.dev", 302},
		{"https://Sites.SBX.ikigenba.dev:8443/", "IKIGENBA.dev:443", 302},
		{"https://sites.sbx.ikigenba.dev", "sites.other.dev", 405},
		{"https://sites.sbx.ikigenba.dev", "sites.sbx.ikigenba.dev", 500},
		{"http://sites.wip.localhost:7400", "localhost", 500},
		{"http://sites.wip.localhost:7400", "wip.localhost", 405},
		{"https://sites.sbx.example.co.uk", "example.co.uk", 302},
		{"https://sites.sbx.example.co.uk", "co.uk", 405},
		{"https://sites.localhost", "example.com", 405},
		{"sites.sbx.ikigenba.dev", "sites.example.com", 500},
		{"https://sites.", "sites.example.com", 500},
		{"https://sitesx.dev", "sites.example.com", 500},
		{"https://www.sites.dev", "sites.example.com", 500},
		{"https://sites.sbx.ikigenba.dev", "backend", 500},
		{"https://sites.sbx.ikigenba.dev", "sites:80", 500},
	} {
		writeServices(t, f.cfg.ServicesPath, q.base)
		r := f.get(t, "POST", "/mcp?route=/_appkit/theme.css", q.host, "", map[string]string{"X-Forwarded-Host": "sites.sbx.ikigenba.dev"})
		if r.Code != q.status {
			t.Fatalf("base=%q host=%q: %d want %d", q.base, q.host, r.Code, q.status)
		}
	}
	original := f.cfg.ServicesPath
	for _, path := range []string{"", filepath.Join(f.root, "missing"), original} {
		if path == original {
			if e := os.WriteFile(path, []byte(`{"services":[]}`), 0600); e != nil {
				t.Fatal(e)
			}
		}
		f.cfg.ServicesPath = path
		f.cfg.MCP = newServer(f.cfg.Telemetry)
		f.h = web.Handler(f.cfg)
		for _, q := range []struct {
			host   string
			status int
		}{{"sites.sbx.ikigenba.dev", 500}, {"sites.example.com", 500}, {"ikigenba.dev", 405}, {"wip.localhost:7400", 405}} {
			r := f.get(t, "POST", "/mcp", q.host, "", nil)
			if r.Code != q.status {
				t.Fatalf("path=%q host=%q status=%d", path, q.host, r.Code)
			}
		}
	}
}
