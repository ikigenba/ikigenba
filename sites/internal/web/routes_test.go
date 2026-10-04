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

// R-UXAG-GEV5 R-WL1J-7676 R-XPM2-VTHM R-WNHB-YPOK R-WOP8-CHF9
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
	for _, host := range []string{"sites", "sites:80", "Sites.sbx.ikigenba.dev", "sites.sbx.ikigenba.dev:8443", "ikigenba.dev", "ikigenba.dev:443", "sitesx.dev", "www.sites.dev", ""} {
		apex := host != "sites" && host != "sites:80" && !strings.HasPrefix(strings.ToLower(host), "sites.")
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

// R-WTKT-VKE1
func TestToolRegistration(t *testing.T) {
	f := fresh(t)
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "sites"; f.h.ServeHTTP(w, r) }))
	defer a.Close()
	other := mcp.NewServer(mcp.ServerConfig{Name: "sites", Version: "fixture", Telemetry: f.cfg.Telemetry})
	tools.Register(other, tools.Config{Store: f.cfg.Store, Cache: f.cfg.Cache, Limits: f.cfg.Limits, Telemetry: f.cfg.Telemetry})
	b := httptest.NewServer(telemetry.Middleware(f.cfg.Telemetry, identity.Require(other)))
	defer b.Close()
	get := func(endpoint string) []mcp.ToolInfo {
		client := mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint})
		list, err := client.ListTools(context.Background(), identity.Caller{UserID: "user", RequestID: "req_123456789abcdef0123456789abcdef0"})
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	got, want := get(a.URL+"/mcp"), get(b.URL)
	if len(got) != 7 || !reflect.DeepEqual(got, want) {
		t.Fatalf("registration %v != %v", got, want)
	}
}

// R-XP0T-GDM1 R-X0W8-66U7
func TestCatalogRefusalAndRecovery(t *testing.T) {
	f := fresh(t)
	if err := f.cfg.Store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"sites", "ikigenba.dev"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, path := range []string{"/", "/blog/", "/mcp"} {
				if host == "sites" && (path == "/mcp" || method == "POST") {
					continue
				}
				r := f.get(t, method, path, host, "user", nil)
				if r.Code != 503 || len(r.Header().Values("Content-Type")) != 1 || r.Header().Get("Content-Type") != "text/plain; charset=utf-8" || (method == "HEAD" && r.Body.Len() != 0) || (method != "HEAD" && r.Body.String() != "cannot reach the catalog; try again later\n") {
					t.Fatalf("catalog %s %s %s: %d %s", host, method, path, r.Code, r.Body.String())
				}
			}
		}
	}
	if r := f.get(t, "GET", "/about", "sites", "user", nil); r.Code != 200 {
		t.Fatal("handler stopped after catalog refusal", r.Code)
	}
}

// R-WL1J-7676 R-WOP8-CHF9
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
