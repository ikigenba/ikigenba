package web_test

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	assets "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

func TestPageRenderingAndMethods(t *testing.T) {
	// R-DPCF-MKZW R-DQKC-0CQL R-DRS8-E4HA R-DT04-RW7Z R-DU81-5NYO R-DWNT-X7G2 R-DXVQ-AZ6R R-RJ0T-YY1W
	f := newFixture(t)
	calls := 0
	var user page.User
	f.cfg.Banner = func(u page.User) page.Banner {
		calls++
		user = u
		return page.Banner{Service: "chosen-service<&", Icon: template.HTML("supplied-icon"), Release: "chosen-release<&", Commit: "chosen-commit<&", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL,
			Home: "https://home.example/?q=<&", Tools: true,
			Trail: []page.Level{{Name: "discarded-trail", URL: "/discarded"}, {Name: "discarded-child", URL: "/discarded/child"}}}
	}
	cfg := freshServer(t, f, f.cfg)
	h := web.Handler(cfg)
	registered := cfg.MCP.Tools()
	ts := httptest.NewServer(h)
	defer ts.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: ts.URL + "/mcp", HTTPClient: ts.Client()})
	listed, err := client.ListTools(context.Background(), identity.Caller{UserID: "reader"})
	if err != nil || len(listed) != len(registered) || len(registered) != 4 {
		t.Fatal(listed, registered, err)
	}
	for i, tool := range registered {
		if listed[i].Name != tool.Name || listed[i].Description != tool.Description {
			t.Fatal(listed, registered)
		}
	}
	templates := template.Must(page.Templates().ParseFS(assets.Assets(), "*.html"))
	pageRequest := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://another.example"+path+"?ignored=1", strings.NewReader("supplied body"))
		r.Header["X-User-Id"] = []string{"u", "other"}
		r.Header.Set("X-User-Email", "caller<&@example")
		r.Header.Set("X-Forwarded-Proto", "http")
		r.Header.Set("X-Original-URL", "/missing")
		r.Header.Set("If-None-Match", "*")
		r.Header.Set("Range", "bytes=0-1")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	for _, tc := range []struct{ path, name string }{{"/", "landing"}, {"/about", "about"}, {"/tools", "tools"}} {
		before := calls
		get := pageRequest("GET", tc.path)
		if calls != before+1 || user != (page.User{Email: "caller<&@example", ProfileURL: "http://auth.another.example/", LogoutURL: "http://auth.another.example/logout"}) {
			t.Fatal(calls, user)
		}
		banner := f.cfg.Banner(user)
		calls--
		banner.Trail = nil
		if tc.path != "/" {
			banner.Trail = []page.Level{{Name: tc.name, URL: tc.path}}
		}
		var expected bytes.Buffer
		var data any = struct{ Banner page.Banner }{banner}
		if tc.name == "about" {
			data = struct {
				Banner      page.Banner
				Description string
			}{banner, web.Description}
		}
		if tc.name == "tools" {
			pageTools := make([]web.Tool, len(registered))
			for i, tool := range registered {
				pageTools[i] = web.Tool{Name: tool.Name, Description: tool.Description}
			}
			data = web.ToolsData{Banner: banner, Tools: pageTools}
		}
		if err := templates.ExecuteTemplate(&expected, tc.name, data); err != nil {
			t.Fatal(err)
		}
		if get.Code != 200 || !reflect.DeepEqual(get.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || get.Body.String() != expected.String() {
			t.Fatal(get.Code, get.Header(), get.Body.String())
		}
		before = calls
		head := pageRequest("HEAD", tc.path)
		if calls != before+1 || head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
			t.Fatal(head)
		}
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CUSTOM"} {
			before = calls
			out := request(h, method, tc.path, "u")
			if out.Code != 405 || !reflect.DeepEqual(out.Header().Values("Allow"), []string{"GET, HEAD"}) || out.Body.Len() != 0 || calls != before {
				t.Fatal(out, calls)
			}
		}
		r := httptest.NewRequest("GET", "http://example"+tc.path, nil)
		r.Header.Set("X-User-Id", "u")
		empty := r.Clone(r.Context())
		empty.Header.Set("X-User-Email", "")
		a, b := httptest.NewRecorder(), httptest.NewRecorder()
		h.ServeHTTP(a, r)
		h.ServeHTTP(b, empty)
		equalResponse(t, a, b)
		if a.Code == 500 {
			t.Fatal("email required")
		}
	}
	for _, tc := range []struct{ method, path, user string }{{"GET", "/", ""}, {"GET", "/about", ""}, {"GET", "/tools", ""}, {"HEAD", "/tools", ""}, {"GET", "/missing", "u"}, {"GET", "/_appkit/theme.css", "u"}, {"GET", "/mcp", "u"}, {"GET", "/ingest", "u"}} {
		before := calls
		request(h, tc.method, tc.path, tc.user)
		if calls != before {
			t.Fatal("banner called", tc)
		}
	}
}

func freshServer(t *testing.T, f *fixture, cfg web.Config) web.Config {
	t.Helper()
	cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: "test-version", Telemetry: f.w})
	return cfg
}
func TestAuthOriginsReadPerRequest(t *testing.T) {
	// R-RRK4-NC8R R-RSS1-13ZG R-RTZX-EVQ5 R-RV7T-SNGU R-RWFQ-6F7J
	f := newFixture(t)
	var got page.User
	f.cfg.Banner = func(u page.User) page.Banner {
		got = u
		return page.Banner{Service: web.ServiceName, Release: "test-release", Commit: "test-commit"}
	}
	f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
	h := web.Handler(freshServer(t, f, f.cfg))
	for _, tc := range []struct{ host, proto, origin string }{{"telemetry.sbx.ikigenba.dev:443", "https", "https://auth.sbx.ikigenba.dev"}, {"telemetry.sbx.ikigenba.dev", "", "https://auth.sbx.ikigenba.dev"}, {"telemetry.sbx.ikigenba.dev", "HTTPS", "https://auth.sbx.ikigenba.dev"}, {"sbx.ikigenba.dev", "", "https://auth.sbx.ikigenba.dev"}, {"telemetry.sbx.ikigenba.dev", "http", "http://auth.sbx.ikigenba.dev"}, {"telemetry.", "invalid", "https://auth.telemetry."}, {"telemetry.site:abc", "https", "https://auth.site:abc"}, {"telemetry.site:", "https", "https://auth.site"}} {
		for _, path := range []string{"/", "/about", "/tools"} {
			r := httptest.NewRequest("GET", "http://example"+path, nil)
			r.Host = tc.host
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			r.Header.Set("X-User-Id", "u")
			r.Header.Set("X-User-Email", "caller@example")
			h.ServeHTTP(httptest.NewRecorder(), r)
			want := page.User{Email: "caller@example", ProfileURL: tc.origin + "/", LogoutURL: tc.origin + "/logout"}
			if got != want {
				t.Fatal(got, want)
			}
		}
	}
	for _, origin := range []string{"http://auth.custom/path/", "https://another.example", ""} {
		body := `{"services":[{"name":"auth","group":"platform","url":"` + origin + `","description":"auth","socket":"/unused","enabled":true,"mcp":false}]}`
		if err := os.WriteFile(f.cfg.ServicesPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		request(h, "GET", "/", "u")
		want := origin
		if want == "" {
			want = "https://auth.example"
		}
		if got.ProfileURL != want+"/" || got.LogoutURL != want+"/logout" || got.Email != "" {
			t.Fatal(got)
		}
	}
	for _, body := range []string{`not json`, `{}`, `{"services":[]}`} {
		if err := os.WriteFile(f.cfg.ServicesPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		request(h, "GET", "/about", "u")
		if got.ProfileURL != "https://auth.example/" {
			t.Fatal(got)
		}
	}
}
func TestPagesDoNotConnectToServices(t *testing.T) {
	// R-DZ3M-OQXG
	f := newFixture(t)
	dir, err := os.MkdirTemp("", "telemetry-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(dir, "s")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	}()
	f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
	body := `{"services":[{"name":"auth","group":"platform","url":"https://auth.example","description":"auth","socket":"` + path + `","enabled":true,"mcp":false}]}`
	if err := os.WriteFile(f.cfg.ServicesPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	h := web.Handler(freshServer(t, f, f.cfg))
	for _, path := range []string{"/", "/about", "/tools"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CUSTOM"} {
			for _, user := range []string{"", "u"} {
				request(h, method, path, user)
			}
		}
	}
	file, err := listener.File()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := unixNonblockingNoPending(file); err != nil {
		t.Fatal(err)
	}
}

func unixNonblockingNoPending(file *os.File) error {
	fd := int(file.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		return err
	}
	accepted, _, err := syscall.Accept(fd)
	if err == nil {
		_ = syscall.Close(accepted)
		return errors.New("page connected to service")
	}
	if !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
		return err
	}
	return nil
}
