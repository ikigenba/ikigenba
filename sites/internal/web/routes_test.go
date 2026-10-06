package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/serving"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

// R-UXAG-GEV5 R-3NN6-FZEC R-XPM2-VTHM R-WNHB-YPOK R-WOP8-CHF9
// R-WPX4-Q95Y R-WR51-40WN R-WUSQ-9C4Q R-WW0M-N3VF R-WX8J-0VM4
// R-WYGF-ENCT R-XD37-ZW95
func TestRoutesMatchTheirOwningHandlers(t *testing.T) {
	f := fresh(t)
	p, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	sc := serving.Config{Banner: f.cfg.Banner, Pages: p, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Cache: f.cfg.Cache, Telemetry: f.cfg.Telemetry, Rand: f.cfg.Rand}
	own := map[string]http.Handler{"apex": serving.Apex(sc), "site": serving.Sites(sc), "page": pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: p, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store}), "static": page.Static()}
	for _, test := range []struct {
		host string
		apex bool
	}{
		{"", false}, {"backend", false}, {"localhost", false}, {"notes:80", false},
		{"sites", false}, {"sites:80", false}, {"Sites.sbx.ikigenba.dev", false}, {"sites.sbx.ikigenba.dev:8443", false},
		{"SITES.example", false}, {"sites.", false}, {"backend:80:90", false}, {"[::1]:80", false},
		{"ikigenba.dev", true}, {"ikigenba.dev:443", true}, {"wip.localhost:7400", true},
		{"sitesx.dev", true}, {"www.sites.dev", true}, {"example.org:80:90", true}, {"[::ffff:192.0.2.1]:80", true},
	} {
		host, apex := test.host, test.apex
		for _, path := range []string{"/", "/about", "/mcp", "/_appkit/theme.css", "/_appkit", "/about/", "/mcp/", "/mcp/tools", "//", "/blog/../x", "/nope?next=/mcp"} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				for _, user := range []string{"", "user"} {
					request := func() *http.Request {
						r := httptest.NewRequest(method, path, nil)
						r.Host = host
						r.Header.Set("X-Request-Id", "req_0123456789abcdef0123456789abcdef")
						r.Header.Set("X-User-Id", user)
						r.Header.Set("X-User-Email", "other@example.invalid")
						return r
					}
					expected := own["site"]
					switch {
					case apex:
						expected = own["apex"]
					case path == "/" || path == "/about":
						expected = own["page"]
					case strings.HasPrefix(path, "/_appkit/"):
						expected = own["static"]
					case path == "/mcp":
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
	for _, path := range []string{"/", "/about", "/mcp", "/_appkit/theme.css", "/nope"} {
		a := f.get(t, "GET", path, "sites", "user", nil)
		b := f.get(t, "GET", path, "sites", "user", map[string]string{"X-User-Email": ""})
		if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Code == 500 {
			t.Fatal("email precondition", path)
		}
	}
}

// R-WTKT-VKE1 R-3NN6-FZEC
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

// R-XQFX-1YI5 R-XMS7-WNA2
func TestCatalogRefusalAndRecovery(t *testing.T) {
	f := fresh(t)
	f.add(t, "private", "private")
	baseline := f.get(t, "GET", "/about", "sites", "user", nil)
	landing := f.get(t, "GET", "/", "sites", "user", nil)
	f.db.SetFailing(true)
	for _, host := range []string{"sites", "ikigenba.dev"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE", "OPTIONS", "CUSTOM"} {
			for _, path := range []string{"/", "/blog", "/blog/", "/private/", "/mcp", "/about"} {
				for _, user := range []string{"", "user"} {
					if host == "sites" && (path == "/mcp" || path == "/about" || method != "GET" && method != "HEAD" || path == "/" && user == "") {
						continue
					}
					r := f.get(t, method, path, host, user, nil)
					if r.Code != 503 || len(r.Header().Values("Content-Type")) != 1 || r.Header().Get("Content-Type") != "text/plain; charset=utf-8" || (method == "HEAD" && r.Body.Len() != 0) || (method != "HEAD" && r.Body.String() != "cannot reach the catalog; try again later\n") {
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

// R-3NN6-FZEC R-WOP8-CHF9
func TestUnicodeHostsAreApexRequests(t *testing.T) {
	f := fresh(t)
	for _, host := range []string{"SİTES.example", "SİTES.example:80"} {
		r := f.get(t, "GET", "/mcp", host, "", nil)
		if r.Code != http.StatusNotFound {
			t.Fatalf("Unicode host %q: status %d, want apex 404", host, r.Code)
		}
		if r.Header().Get("Set-Cookie") != "" {
			t.Fatal("apex minted a visitor")
		}
	}
}
