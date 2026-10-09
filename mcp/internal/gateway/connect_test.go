package gateway_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	assets "github.com/ikigenba/ikigenba/mcp"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func pageConfig(t *testing.T, path string, banner func(page.User) page.Banner) gateway.Config {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _ := handlerTelemetry(t, nil)
	return gateway.Config{ServicesPath: path, Banner: banner, MCP: gateway.NewServer("test", writer), Telemetry: writer}
}

func basicBanner(u page.User) page.Banner {
	return page.Banner{Service: gateway.ServiceName, Version: "test", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
}

func servicesFile(t *testing.T, entries []map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "services.json")
	writeServices(t, path, entries)
	return path
}

func writeServices(t *testing.T, path string, entries []map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func service(name, description string, enabled, mcp bool) map[string]any {
	return map[string]any{"name": name, "description": description, "enabled": enabled, "mcp": mcp, "url": "", "socket": ""}
}

func pageRequest(method, path string) *http.Request {
	r := httptest.NewRequest("GET", "https://mcp.space.test:8443"+path, strings.NewReader("arbitrary body"))
	r.Method = method
	r.Header.Set("X-User-Id", "person")
	r.Header.Set("X-User-Email", "person@example.test")
	return r
}

func answer(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// R-ZTI8-HEK9 R-ZVY1-8Y1N
func TestGatewayTemplateSet(t *testing.T) {
	templates, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	if templates.Lookup("connect") == nil || templates.Lookup("about") == nil {
		t.Fatal("gateway template absent")
	}
	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, "connect", map[string]any{"Banner": basicBanner(page.User{}), "Endpoint": "https://mcp.example.test/mcp", "Server": "example-test"}); err != nil {
		t.Fatal(err)
	}
}

// R-SNRW-9QL4 R-RIX1-NUV0 R-SBKW-G166 R-SCSS-TSWV R-SE0P-7KNK R-SF8L-LCE9 R-SHOE-CVVN
func TestConnectExactlyRendersRequestData(t *testing.T) {
	for _, proto := range []string{"", "http", "https", "HTTP", "http, https", " https"} {
		for _, host := range []string{"mcp.space.test:8443", "mcp.space.test:", "mcp.", "space.test:word", "mcp.mcp.space.test", "mcp.space<&>.test:8443"} {
			for _, authURL := range []string{"", "https://accounts.test/base/"} {
				t.Run(proto+"/"+host+"/"+authURL, func(t *testing.T) {
					first := service("zeta", "<b> &amp; text", false, true)
					entries := []map[string]any{first, service("alpha", "enabled", true, true), service("zeta", "ignored duplicate", true, true), service("mcp", "gateway", true, true), service("other", "not MCP", true, false)}
					auth := service("auth", "authentication", true, false)
					auth["url"] = authURL
					entries = append(entries, auth)
					path := servicesFile(t, entries)
					var users []page.User
					cfg := pageConfig(t, path, func(u page.User) page.Banner { users = append(users, u); return basicBanner(u) })
					r := pageRequest("GET", "/?query=ignored")
					r.Host = host
					r.Header.Set("X-Forwarded-Proto", proto)
					r.Header.Set("X-User-Email", "  caller<&>@example.test ")
					w := answer(gateway.Handler(cfg), r)
					scheme := "https"
					if proto == "http" {
						scheme = "http"
					}
					spaces := map[string]string{"mcp.space.test:8443": "space.test", "mcp.space.test:": "space.test", "mcp.": "mcp.", "space.test:word": "space.test:word", "mcp.mcp.space.test": "mcp.space.test", "mcp.space<&>.test:8443": "space<&>.test"}
					origin := authURL
					if origin == "" {
						origin = scheme + "://auth." + spaces[host]
					}
					u := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
					if !slices.Contains(users, u) {
						t.Fatalf("banner calls: %#v want %#v", users, u)
					}
					endpoint := scheme + "://" + host + "/mcp"
					data := map[string]any{"Banner": basicBanner(u), "Endpoint": endpoint, "Server": map[string]string{"mcp.space.test:8443": "space-test", "mcp.space.test:": "space-test", "mcp.": "mcp-", "space.test:word": "space-test-word", "mcp.mcp.space.test": "mcp-space-test", "mcp.space<&>.test:8443": "space----test"}[host]}
					set, err := page.Templates().ParseFS(assets.Assets(), "*.html")
					if err != nil {
						t.Fatal(err)
					}
					var expected bytes.Buffer
					if err := set.ExecuteTemplate(&expected, "connect", data); err != nil {
						t.Fatal(err)
					}
					if w.Code != 200 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || w.Body.String() != expected.String() {
						t.Fatalf("response differs: status=%d headers=%v\n%s\nwant\n%s", w.Code, w.Header(), w.Body.String(), expected.String())
					}
				})
			}
		}
	}
}

// R-SOZS-NIBT
func TestConnectHEADMatchesGET(t *testing.T) {
	h := gateway.Handler(pageConfig(t, servicesFile(t, []map[string]any{service("alpha", "description", true, true)}), basicBanner))
	get := answer(h, pageRequest("GET", "/"))
	head := answer(h, pageRequest("HEAD", "/"))
	if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
		t.Fatalf("GET=%d %v HEAD=%d %v %q", get.Code, get.Header(), head.Code, head.Header(), head.Body.String())
	}
}

// R-YIQW-P5S2
func TestConnectRejectsOtherMethods(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "CUSTOM"} {
		for _, identity := range []string{"signed", "absent", "empty"} {
			r := pageRequest(method, "/")
			setPageIdentity(r, identity)
			w := answer(h, r)
			if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
				t.Fatalf("%s: %d %v %q", method, w.Code, w.Header(), w.Body.String())
			}
		}
	}
}

// R-ZDAR-FFEL R-ZEIN-T75A R-ZX5X-MPSC
func TestUnknownPathsReturnExact404(t *testing.T) {
	const bodyCopy string = gateway.NotFound
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, path := range []string{"/_appkit", "/assets/", "/assets/connect.html", "/logout", "/index.html", "/setup", "/setup.txt", "/setup.sh", "/.well-known", "/.well-known/", "/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resourcex", "/about/", "/about/x", "/setup.txt/", "/setup.sh/", "/setup.txt/x", "/setup.sh/x", "//", "/nope/", "/x/../", "/./", "/x/%2e%2e/", "/%61ssets/theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST", "OPTIONS"} {
			for _, identity := range []string{"signed", "absent", "empty"} {
				r := pageRequest(method, path+"?ignored=yes")
				setPageIdentity(r, identity)
				w := answer(h, r)
				body := bodyCopy
				if method == "HEAD" {
					body = ""
				}
				if w.Code != 404 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != body || w.Header().Get("Location") != "" {
					t.Fatalf("%s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
				}
			}
		}
	}
}

// R-SSNH-STJW R-R3W9-7NFO
func TestNonMCPRoutesNeitherSetCookiesNorContactBackends(t *testing.T) {
	dir, err := os.MkdirTemp("", "mcp-page-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	socket := filepath.Join(dir, "backend.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
	})
	entry := service("alpha", "backend", true, true)
	entry["socket"] = socket
	h := gateway.Handler(pageConfig(t, servicesFile(t, []map[string]any{entry}), basicBanner))
	for _, path := range []string{"/", "/about", "/setup.txt", "/setup.sh", "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp/a,,b", "/_appkit/theme.css", "/_appkit/../theme.css", "/logout", "/assets/connect.html", "/unknown"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, user := range []string{"person", ""} {
				r := pageRequest(method, path)
				r.Header.Set("X-User-Id", user)
				w := answer(h, r)
				if len(w.Header().Values("Set-Cookie")) != 0 {
					t.Fatalf("cookie on %s %s", method, path)
				}
				conn, err := ln.SyscallConn()
				if err != nil {
					t.Fatal(err)
				}
				var acceptErr error
				var accepted int
				if err := conn.Control(func(fd uintptr) {
					accepted, _, acceptErr = syscall.Accept4(int(fd), syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
				}); err != nil {
					t.Fatal(err)
				}
				if acceptErr == nil {
					_ = syscall.Close(accepted)
					t.Fatalf("backend contacted on %s %s", method, path)
				}
				if !errors.Is(acceptErr, syscall.EAGAIN) {
					t.Fatalf("accept: %v", acceptErr)
				}
			}
		}
	}
}

// R-6P2M-PC2C
func TestConnectServerNames(t *testing.T) {
	var banner page.Banner
	h := gateway.Handler(pageConfig(t, "", func(u page.User) page.Banner { banner = basicBanner(u); return banner }))
	templates, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ host, server string }{
		{"mcp.sbx.ikigenba.dev", "sbx-ikigenba-dev"},
		{"mcp.sbx.ikigenba.dev:443", "sbx-ikigenba-dev"},
		{"mcp.SBX.Ikigenba.Dev", "sbx-ikigenba-dev"},
		{"MCP.sbx.ikigenba.dev", "mcp-sbx-ikigenba-dev"},
		{"mcp.wip-mcp.localhost:7403", "wip-mcp-localhost"},
		{"mcp.Az09_-é界.test:12", "az09-----test"},
		{"mcp.a\xff\xfe.test", "a---test"},
		{"mcp.mcp.A.Test:", "mcp-a-test"},
		{"mcp.a.test:port", "a-test-port"},
		{"mcp.", "mcp-"},
	} {
		for _, proto := range []string{"http", "https", "HTTPS", ""} {
			r := pageRequest("GET", "/")
			r.Host = tc.host
			r.Header.Set("X-Forwarded-Proto", proto)
			body := answer(h, r).Body.String()
			scheme := "https"
			if proto == "http" {
				scheme = "http"
			}
			data := map[string]any{
				"Banner":   banner,
				"Endpoint": scheme + "://" + tc.host + "/mcp",
				"Server":   tc.server,
			}
			var expected bytes.Buffer
			if err := templates.ExecuteTemplate(&expected, "connect", data); err != nil {
				t.Fatal(err)
			}
			if body != expected.String() {
				t.Fatalf("host %q: render differs for server %q", tc.host, tc.server)
			}
		}
	}
}
