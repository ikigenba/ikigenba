package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
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
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
)

// R-5P5J-SW2E R-5QDG-6NT3 R-5ST8-Y7AH R-5U15-BZ16
func TestPageAuthFallback(t *testing.T) {
	f := newWebFixture(t)
	var gotUser page.User
	f.cfg.Banner = func(u page.User) page.Banner { gotUser = u; return page.Banner{} }
	h := Handler(f.cfg)
	for _, host := range []struct{ host, space string }{
		{"repos.sbx.ikigenba.dev:443", "sbx.ikigenba.dev"},
		{"repos.sbx.ikigenba.dev", "sbx.ikigenba.dev"},
		{"sbx.ikigenba.dev", "sbx.ikigenba.dev"},
		{"repos.space:00123", "space"}, {"repos.space:", "space"},
		{"repos.space:port", "space:port"}, {"repos.space:12x", "space:12x"},
		{"repos.space:１２", "space:１２"}, {"repos.space:-80", "space:-80"},
		{"repos.space:80:90", "space:80"}, {"repos.space::90", "space:"},
		{"Repos.space:80", "Repos.space"}, {"repos.", "repos."},
		{"repos.:80", "repos."}, {"repos.repos.space:9", "repos.space"},
		{"[::1]:443", "[::1]"}, {"[::1]", "[::1]"}, {"::1", ":"}, {"", ""},
	} {
		for _, proto := range []struct {
			values []string
			scheme string
		}{
			{nil, "https"}, {[]string{""}, "https"}, {[]string{"http"}, "http"},
			{[]string{"https"}, "https"}, {[]string{"HTTPS"}, "https"},
			{[]string{" http"}, "https"}, {[]string{"http "}, "https"},
			{[]string{"http, https"}, "https"}, {[]string{"ftp"}, "https"},
			{[]string{"http", "https"}, "http"}, {[]string{"", "http"}, "https"},
		} {
			for _, email := range [][]string{nil, {""}, {"first@example.test", "second@example.test"}} {
				for _, path := range []string{"/", "/about", "/tools"} {
					for _, method := range []string{"GET", "HEAD"} {
						r := httptest.NewRequest(method, path+"?ignored=value", strings.NewReader("ignored body"))
						r.Host = host.host
						r.Header.Set("X-User-Id", "caller")
						r.Header.Set("X-Request-Id", "fallback")
						r.Header["X-Forwarded-Proto"] = proto.values
						r.Header["X-User-Email"] = email
						got := httptest.NewRecorder()
						h.ServeHTTP(got, r)
						wantEmail := ""
						if len(email) > 0 {
							wantEmail = email[0]
						}
						origin := proto.scheme + "://auth." + host.space
						want := page.User{Email: wantEmail, ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
						if got.Code != 200 || gotUser != want {
							t.Fatalf("%s %s host=%q proto=%q email=%q: status %d user %+v, want %+v", method, path, host.host, proto.values, email, got.Code, gotUser, want)
						}
					}
				}
			}
		}
	}
}

func pageService(name, url, socket string) map[string]any {
	return map[string]any{"name": name, "url": url, "socket": socket, "description": "fixture service", "enabled": true, "mcp": false}
}

func writePageServices(t *testing.T, path string, entries ...map[string]any) {
	t.Helper()
	if entries == nil {
		entries = []map[string]any{}
	}
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	webServices(t, path, string(data))
}

// R-5RLC-KFJS R-5ST8-Y7AH
func TestPageListedAuthEntriesFresh(t *testing.T) {
	f := newWebFixture(t)
	f.cfg.ServicesPath = filepath.Join(f.dir, "services.json")
	var gotUser page.User
	f.cfg.Banner = func(u page.User) page.Banner { gotUser = u; return page.Banner{} }
	h := Handler(f.cfg)
	disabled := pageService("auth", "http://disabled.auth.test/prefix/", "")
	disabled["enabled"] = false
	for _, tc := range []struct {
		entries []map[string]any
		origin  string
	}{
		{[]map[string]any{pageService("auth", "https://auth.first.test", "")}, "https://auth.first.test"},
		{[]map[string]any{pageService("auth", "http://auth.second.test:81/prefix/", "")}, "http://auth.second.test:81/prefix/"},
		{[]map[string]any{disabled}, "http://disabled.auth.test/prefix/"},
		{[]map[string]any{pageService("auth", "https://auth.first.test?q=a&b=two#frag", ""), pageService("auth", "https://ignored.test", "")}, "https://auth.first.test?q=a&b=two#frag"},
		{[]map[string]any{pageService("auth", "", ""), pageService("auth", "https://ignored.test", "")}, "http://auth.space.test"},
		{[]map[string]any{pageService("Auth", "https://ignored.test", "")}, "http://auth.space.test"},
		{[]map[string]any{{"name": "auth", "url": "https://unusable.test"}}, "http://auth.space.test"},
		{nil, "http://auth.space.test"},
	} {
		writePageServices(t, f.cfg.ServicesPath, tc.entries...)
		for _, path := range []string{"/", "/about", "/tools"} {
			r := httptest.NewRequest("GET", path, nil)
			r.Host = "repos.space.test:8080"
			r.Header.Set("X-Forwarded-Proto", "http")
			r.Header.Set("X-User-Id", "user")
			r.Header.Set("X-Request-Id", "listed-auth")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := page.User{ProfileURL: tc.origin + "/", LogoutURL: tc.origin + "/logout"}
			if w.Code != 200 || gotUser != want {
				t.Fatalf("%s listed %v: status %d, user %+v, want %+v", path, tc.entries, w.Code, gotUser, want)
			}
		}
	}
	for _, data := range []string{"not json", `{"services":{}}`, `null`, string([]byte{0xff})} {
		webServices(t, f.cfg.ServicesPath, data)
		for _, path := range []string{"/", "/about", "/tools"} {
			w := webRequest(h, "GET", "http://repos.space.test"+path, "user", "invalid-services", nil)
			if w.Code != 200 || gotUser.ProfileURL != "https://auth.space.test/" || gotUser.LogoutURL != "https://auth.space.test/logout" {
				t.Fatalf("invalid services %q: %d %+v", data, w.Code, gotUser)
			}
		}
	}
	for _, path := range []string{"", filepath.Join(f.dir, "missing.json"), f.dir, f.dir + "/./services.json"} {
		other := newWebFixture(t)
		cfg := other.cfg
		cfg.ServicesPath = path
		cfg.Banner = f.cfg.Banner
		otherHandler := Handler(cfg)
		for _, route := range []string{"/", "/about", "/tools"} {
			w := webRequest(otherHandler, "GET", "http://repos.space.test"+route, "user", "absent-services", nil)
			if w.Code != 200 || gotUser.ProfileURL != "https://auth.space.test/" || gotUser.LogoutURL != "https://auth.space.test/logout" {
				t.Fatalf("unreadable services path %q: %d %+v", path, w.Code, gotUser)
			}
		}
	}
}

// R-5V91-PQRV R-SSBU-08CL R-STJQ-E03A R-5YWQ-V1ZY R-SURM-RRTZ R-SVZJ-5JKO R-SX7F-JBBD R-SYFB-X322
func TestPageExactTemplateRendering(t *testing.T) {
	f := newWebFixture(t)
	f.cfg.ServicesPath = filepath.Join(f.dir, "services.json")
	var renderedBanner page.Banner
	calls := 0
	f.cfg.Banner = func(u page.User) page.Banner {
		calls++
		renderedBanner = page.Banner{Service: "service <&> \"label\"", Version: fmt.Sprintf("render-%d", calls), Home: "https://home.render.test/", Tools: true, Trail: []page.Level{{Name: "old-first", URL: "/old-first"}, {Name: "old-second", URL: "/old-second"}}, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
		if calls%2 == 0 {
			renderedBanner.Services = []page.Service{{Name: "fixture <service>", URL: "https://launcher.example.test/?a=1&b=2", Icon: template.HTML(`<svg><path d="M1 2"/></svg>`), Enabled: true, Current: true}}
		}
		return renderedBanner
	}
	h := Handler(f.cfg)
	for _, published := range []string{"", "https://repos.published.test:8443/prefix/", "http://repos.changed.test/", "https://repos.escaped.test/?a=1&b=2"} {
		writePageServices(t, f.cfg.ServicesPath, pageService("repos", published, ""), pageService("auth", "https://auth.fixture.test/account/", ""))
		for _, path := range []string{"/", "/about", "/tools"} {
			for _, proto := range []string{"http", "https", "HTTPS"} {
				r := httptest.NewRequest("GET", path+"?arbitrary=about", strings.NewReader("arbitrary body <&>"))
				r.Host = "repos.fallback.test:8000"
				r.Header["X-User-Id"] = []string{"caller", "ignored-user"}
				r.Header["X-User-Email"] = []string{"user+<&>@example.test", "ignored@example.test"}
				r.Header.Set("X-Forwarded-Proto", proto)
				r.Header.Set("Authorization", "Bearer arbitrary-value")
				r.Header.Set("Range", "bytes=0-1")
				r.Header.Set("If-None-Match", "*")
				r.Header.Set("X-Request-Id", "exact-render")
				w := httptest.NewRecorder()
				before := calls
				h.ServeHTTP(w, r)
				templates, err := page.Templates().ParseFS(repos.Assets(), "*.html")
				if err != nil {
					t.Fatal(err)
				}
				if templates.Lookup("landing") == nil || templates.Lookup("about") == nil || templates.Lookup("tools") == nil {
					t.Fatal("template set lacks a declared page")
				}
				name := "about"
				expectedBanner := renderedBanner
				expectedBanner.Trail = []page.Level{{Name: "about", URL: "/about"}}
				var data any = map[string]any{"Banner": expectedBanner, "Description": Description}
				if path == "/" {
					name = "landing"
					base := clone.Base(r, f.cfg.ServicesPath)
					data = map[string]any{"Banner": renderedBanner, "ReposURL": base, "Credentials": clone.Guidance(base)}
				}
				if path == "/tools" {
					name = "tools"
					expectedBanner.Trail = []page.Level{{Name: "tools", URL: "/tools"}}
					listed := []Tool{}
					for _, tool := range f.cfg.MCP.Tools() {
						listed = append(listed, Tool{Name: tool.Name, Description: tool.Description})
					}
					data = ToolsData{Banner: expectedBanner, Tools: listed}
				}
				var expected bytes.Buffer
				if err := templates.ExecuteTemplate(&expected, name, data); err != nil {
					t.Fatal(err)
				}
				if calls != before+1 || w.Code != 200 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || !bytes.Equal(w.Body.Bytes(), expected.Bytes()) {
					t.Fatalf("GET %s proto=%q published=%q: calls=%d status=%d headers=%v\nbody=%q\nwant=%q", path, proto, published, calls-before, w.Code, w.Header(), w.Body.String(), expected.String())
				}
			}
		}
	}
}

// R-T0V4-OMJG
func TestPageHeadMatchesGet(t *testing.T) {
	f := newWebFixture(t)
	f.cfg.ServicesPath = filepath.Join(f.dir, "services.json")
	writePageServices(t, f.cfg.ServicesPath, pageService("auth", "http://auth.head.test/prefix/", ""), pageService("repos", "http://repos.head.test/prefix/", ""))
	h := Handler(f.cfg)
	for _, path := range []string{"/", "/about", "/tools"} {
		for _, headers := range []http.Header{{}, {"X-Forwarded-Proto": {"http"}, "X-User-Email": {"user@example.test"}, "Range": {"bytes=0-0"}, "If-None-Match": {"*"}, "Authorization": {"Bearer ignored"}}} {
			var answers []*httptest.ResponseRecorder
			for _, method := range []string{"GET", "HEAD"} {
				r := httptest.NewRequest(method, "http://repos.head.test"+path+"?ignored=true", strings.NewReader("ignored body"))
				r.Header = headers.Clone()
				r.Header.Set("X-User-Id", "user")
				r.Header.Set("X-Request-Id", "head-equivalence")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				answers = append(answers, w)
			}
			get, head := answers[0], answers[1]
			if get.Code != 200 || get.Body.Len() == 0 || head.Code != get.Code || head.Body.Len() != 0 || !reflect.DeepEqual(head.Header(), get.Header()) {
				t.Fatalf("%s: GET %d %v %d bytes, HEAD %d %v %d bytes", path, get.Code, get.Header(), get.Body.Len(), head.Code, head.Header(), head.Body.Len())
			}
		}
	}
}

// R-T231-2EA5
func TestPageMethodRefusals(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	for _, path := range []string{"/", "/about", "/tools"} {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT", "CUSTOM", "get", "head"} {
			r := httptest.NewRequest(method, path+"?ignored=1", strings.NewReader("request payload"))
			r.Header.Set("X-User-Id", "user")
			r.Header.Set("X-User-Email", "ignored@example.test")
			r.Header.Set("Authorization", "Bearer ignored")
			r.Header.Set("X-Forwarded-Proto", "http")
			r.Header.Set("X-Request-Id", "method-refusal")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
				t.Fatalf("%s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
			}
		}
	}
}

// R-T3AX-G60U
func TestPageBannerCallBoundary(t *testing.T) {
	f := newWebFixture(t)
	calls := 0
	var gotUser page.User
	f.cfg.Banner = func(u page.User) page.Banner { calls++; gotUser = u; return page.Banner{} }
	h := Handler(f.cfg)
	for _, path := range []string{"/", "/about", "/tools", "/_appkit/theme.css", "/_appkit/nope.css", "/mcp", "/missing.git/info/refs", "/not-served"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, ids := range [][]string{nil, {""}, {"", "later"}, {"first", "second"}} {
				r := httptest.NewRequest(method, "http://repos.banner.test"+path, strings.NewReader(`{"method":"server/discover"}`))
				r.Header["X-User-Id"] = ids
				r.Header["X-User-Email"] = []string{"first@example.test", "second@example.test"}
				r.Header.Set("X-Request-Id", "banner-boundary")
				r.Header.Set("Content-Type", "application/json")
				before := calls
				h.ServeHTTP(httptest.NewRecorder(), r)
				wantCalls := 0
				if len(ids) > 0 && ids[0] != "" && (path == "/" || path == "/about" || path == "/tools") && (method == "GET" || method == "HEAD") {
					wantCalls = 1
				}
				if calls-before != wantCalls {
					t.Fatalf("%s %s ids=%q: %d banner calls, want %d", method, path, ids, calls-before, wantCalls)
				}
				if wantCalls == 1 && gotUser != (page.User{Email: "first@example.test", ProfileURL: "https://auth.banner.test/", LogoutURL: "https://auth.banner.test/logout"}) {
					t.Fatalf("banner argument: %+v", gotUser)
				}
			}
		}
	}
}

// R-T4IT-TXRJ
func TestPagesAndSharedFilesNeverConnectToServices(t *testing.T) {
	f := newWebFixture(t)
	dir, err := os.MkdirTemp("", "repos-pages-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f.cfg.ServicesPath = filepath.Join(f.dir, "services.json")
	writePageServices(t, f.cfg.ServicesPath, pageService("auth", "https://auth.sockets.test", socket), pageService("repos", "https://repos.sockets.test", socket), pageService("dummy", "https://dummy.sockets.test", socket), pageService("telemetry", "", socket))
	h := Handler(f.cfg)
	for _, path := range []string{"/", "/about", "/tools", "/_appkit/", "/_appkit/theme.css", "/_appkit/launcher.js", "/_appkit/feedback.js", "/_appkit/JetBrainsMono.woff2", "/_appkit/nope.css"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, user := range []string{"", "caller"} {
				r := httptest.NewRequest(method, path, strings.NewReader("ignored payload"))
				r.Header.Set("X-User-Id", user)
				r.Header.Set("X-User-Email", "user@example.test")
				r.Header.Set("X-Request-Id", "no-socket")
				r.Header.Set("Authorization", "Bearer ignored")
				h.ServeHTTP(httptest.NewRecorder(), r)
				raw, err := ln.SyscallConn()
				if err != nil {
					t.Fatal(err)
				}
				var acceptErr error
				if err := raw.Control(func(fd uintptr) {
					accepted, _, e := syscall.Accept4(int(fd), syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
					acceptErr = e
					if e == nil {
						_ = syscall.Close(accepted)
					}
				}); err != nil {
					t.Fatal(err)
				}
				if acceptErr != syscall.EAGAIN && acceptErr != syscall.EWOULDBLOCK {
					t.Fatalf("%s %s user=%q connected to a listed service: %v", method, path, user, acceptErr)
				}
			}
		}
	}
}

// R-T5QQ-7PI8
func TestPagesAndSharedFilesIgnoreFailingCatalogAndRemovedRepositories(t *testing.T) {
	f := newWebFixture(t)
	seed := f.repo(t, "caller", "private-notes")
	root := filepath.Dir(f.cfg.Store.Dir(seed.ID))
	h := Handler(f.cfg)
	type answer struct {
		method, path, user string
		response           *httptest.ResponseRecorder
	}
	var answers []answer
	for _, path := range []string{"/", "/about", "/tools", "/_appkit/", "/_appkit/theme.css", "/_appkit/launcher.js", "/_appkit/feedback.js", "/_appkit/InterVariable.woff2", "/_appkit/InterVariable-Italic.woff2", "/_appkit/JetBrainsMono.woff2", "/_appkit/OFL.txt", "/_appkit/TABLER-LICENSE.txt", "/_appkit/nope.css"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, user := range []string{"", "caller"} {
				answers = append(answers, answer{method, path, user, webRequest(h, method, path, user, "catalog-independent", strings.NewReader("ignored payload"))})
			}
		}
	}
	f.db.SetFailing(true)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("repository root remains: %v", err)
	}
	for _, before := range answers {
		after := webRequest(h, before.method, before.path, before.user, "catalog-independent", strings.NewReader("ignored payload"))
		if after.Code != before.response.Code || !reflect.DeepEqual(after.Header(), before.response.Header()) || !bytes.Equal(after.Body.Bytes(), before.response.Body.Bytes()) {
			t.Fatalf("%s %s user=%q changed after catalog close/removal: before %d %v %d bytes, after %d %v %d bytes", before.method, before.path, before.user, before.response.Code, before.response.Header(), before.response.Body.Len(), after.Code, after.Header(), after.Body.Len())
		}
	}
}

// R-SZN8-AUSR R-SYFB-X322: Compare the page to the client-visible tool list,
// including tools added after handler construction and after an earlier request.
func TestToolsPageMatchesMCPListAndLateRegistration(t *testing.T) {
	f := newWebFixture(t)
	banner := page.Banner{Service: "fixture-tools-service", Version: "fixture-tools-version", Email: "fixture@example.test", Home: "https://home.fixture.test/", Tools: true, Trail: []page.Level{{Name: "replace-me", URL: "/replace-me"}}}
	f.cfg.Banner = func(page.User) page.Banner { return banner }
	h := Handler(f.cfg)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	for _, extra := range []string{"", "fixture_zebra", "fixture_alpha"} {
		if extra != "" {
			mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: extra, Description: "Fixture " + extra + " supplied description <&>.\nsecond line", Effect: mcp.Read,
				Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
		}
		registered := f.cfg.MCP.Tools()
		if extra != "" && (len(registered) < 7 || registered[len(registered)-1].Name != extra) {
			t.Fatalf("late tool order: %+v", registered)
		}
		wantBanner := banner
		wantBanner.Trail = []page.Level{{Name: "tools", URL: "/tools"}}
		data := ToolsData{Banner: wantBanner}
		for _, tool := range registered {
			data.Tools = append(data.Tools, Tool{Name: tool.Name, Description: tool.Description})
		}
		if extra == "fixture_alpha" {
			listed, err := client.ListTools(t.Context(), identity.Caller{UserID: "fixture-caller", RequestID: "fixture-list"})
			if err != nil {
				t.Fatal(err)
			}
			wireTools := make([]Tool, len(listed))
			for i, tool := range listed {
				wireTools[i] = Tool{Name: tool.Name, Description: tool.Description}
			}
			if !reflect.DeepEqual(data.Tools, wireTools) {
				t.Fatalf("registered tools differ from client list: %+v, %+v", data.Tools, wireTools)
			}
			data.Tools = wireTools
		}
		templates, err := page.Templates().ParseFS(repos.Assets(), "*.html")
		if err != nil {
			t.Fatal(err)
		}
		var expected bytes.Buffer
		if err := templates.ExecuteTemplate(&expected, "tools", data); err != nil {
			t.Fatal(err)
		}
		got := webRequest(h, "GET", "/tools", "fixture-caller", "fixture-page", nil)
		if got.Code != 200 || !bytes.Equal(got.Body.Bytes(), expected.Bytes()) {
			t.Fatalf("tools list differs: status=%d\nbody=%s\nwant=%s", got.Code, got.Body.String(), expected.String())
		}
	}
}
