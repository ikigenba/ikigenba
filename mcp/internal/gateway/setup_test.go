package gateway_test

import (
	"bytes"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"text/template"

	"github.com/ikigenba/ikigenba/appkit/page"
	assets "github.com/ikigenba/ikigenba/mcp"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

func setPageIdentity(r *http.Request, mode string) {
	switch mode {
	case "absent":
		r.Header.Del("X-User-Id")
	case "empty":
		r.Header["X-User-Id"] = []string{"", "later"}
	}
	r.Header.Set("X-User-Email", "guest@example.test")
}

// R-ZASL-HVU1 R-ZC0H-VNKQ
func TestGuestConnectRedirect(t *testing.T) {
	for _, authURL := range []string{"", "https://accounts.test/base/"} {
		entry := service("auth", "", true, false)
		entry["url"] = authURL
		path := servicesFile(t, []map[string]any{entry})
		h := gateway.Handler(pageConfig(t, path, func(page.User) page.Banner { t.Fatal("guest called banner"); return page.Banner{} }))
		for _, proto := range []string{"", "http", "https", "HTTP"} {
			for _, host := range []string{"mcp.sbx.ikigenba.dev", "mcp.space.test:8443", "mcp.mcp.space.test:", "space.test:word", "mcp."} {
				for _, target := range []string{"/", "/?from=launcher", "/?a=%2F&b=x+y&b=%26"} {
					for _, method := range []string{"GET", "HEAD"} {
						for _, mode := range []string{"absent", "empty"} {
							r := pageRequest(method, target)
							r.Host = host
							r.Header.Set("X-Forwarded-Proto", proto)
							setPageIdentity(r, mode)
							scheme := "https"
							if proto == "http" {
								scheme = "http"
							}
							spaces := map[string]string{"mcp.sbx.ikigenba.dev": "sbx.ikigenba.dev", "mcp.space.test:8443": "space.test", "mcp.mcp.space.test:": "mcp.space.test", "space.test:word": "space.test:word", "mcp.": "mcp."}
							auth := authURL
							if auth == "" {
								auth = scheme + "://auth." + spaces[host]
							}
							want := auth + "/?return=" + url.QueryEscape(scheme+"://"+host+r.URL.RequestURI())
							w := answer(h, r)
							if w.Code != 302 || !reflect.DeepEqual(w.Header().Values("Location"), []string{want}) || w.Body.Len() != 0 {
								t.Fatalf("%s %s %s %s: %d %v %q want %s", mode, proto, host, target, w.Code, w.Header(), w.Body.String(), want)
							}
						}
					}
				}
			}
		}
	}
}

// R-YUXW-IV70 R-YW5S-WMXP
func TestSetupTemplateSet(t *testing.T) {
	set, err := template.ParseFS(assets.Assets(), "setup.txt", "setup.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"instructions", "installer"} {
		if set.Lookup(name) == nil {
			t.Fatalf("missing %s", name)
		}
		var out bytes.Buffer
		data := struct{ Space, Origin, Endpoint, TokenURL, Variable, Server string }{"example.test", "https://mcp.example.test", "https://mcp.example.test/mcp", "https://auth.example.test/", "IKIGENBA_TOKEN_EXAMPLE_TEST", "ikigenba-example-test"}
		if err := set.ExecuteTemplate(&out, name, data); err != nil {
			t.Fatal(err)
		}
	}
}

// R-YSI3-RBPM R-UPQA-OWBF R-YYLL-O6F3 R-Z11E-FPWH R-YQ2A-ZS88
// R-UQY7-2O24 R-US63-GFST R-Z74W-CKLY R-UTDZ-U7JI R-Z29A-THN6
// R-UNAH-XCU1 R-UOIE-B4KQ
func TestSetupExactlyRendersRequestData(t *testing.T) {
	set, err := template.ParseFS(assets.Assets(), "setup.txt", "setup.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, authURL := range []string{"", "http://accounts.test/base/?quoted='&other=<value>"} {
		entry := service("auth", "", true, false)
		entry["url"] = authURL
		h := gateway.Handler(pageConfig(t, servicesFile(t, []map[string]any{entry}), func(page.User) page.Banner { t.Fatal("setup called banner"); return page.Banner{} }))
		for _, proto := range []string{"", "http", "https", "HTTP", "https, http"} {
			for _, host := range []string{"mcp.space.test:8443", "mcp.mcp.space.test:", "space.test:word", "mcp.", "mcp.space<&>.test:8443"} {
				scheme := "https"
				if proto == "http" {
					scheme = "http"
				}
				spaces := map[string]string{"mcp.space.test:8443": "space.test", "mcp.mcp.space.test:": "mcp.space.test", "space.test:word": "space.test:word", "mcp.": "mcp.", "mcp.space<&>.test:8443": "space<&>.test"}
				auth := authURL
				if auth == "" {
					auth = scheme + "://auth." + spaces[host]
				}
				origin := scheme + "://" + host
				endpoint, tokenURL := origin+"/mcp", auth+"/"
				names := map[string][2]string{
					"space.test":      {"IKIGENBA_TOKEN_SPACE_TEST", "ikigenba-space-test"},
					"mcp.space.test":  {"IKIGENBA_TOKEN_MCP_SPACE_TEST", "ikigenba-mcp-space-test"},
					"space.test:word": {"IKIGENBA_TOKEN_SPACE_TEST_WORD", "ikigenba-space-test-word"},
					"mcp.":            {"IKIGENBA_TOKEN_MCP_", "ikigenba-mcp-"},
					"space<&>.test":   {"IKIGENBA_TOKEN_SPACE____TEST", "ikigenba-space----test"},
				}
				space := spaces[host]
				variable, server := names[space][0], names[space][1]
				data := map[string]any{"Space": space, "Origin": origin, "Endpoint": endpoint, "TokenURL": tokenURL, "Variable": variable, "Server": server}
				for _, route := range []struct{ path, name string }{{"/setup.txt", "instructions"}, {"/setup.sh", "installer"}} {
					var expected bytes.Buffer
					if err := set.ExecuteTemplate(&expected, route.name, data); err != nil {
						t.Fatal(err)
					}
					for _, mode := range []string{"signed", "absent", "empty"} {
						r := pageRequest("GET", route.path+"?unused=yes")
						r.Host = host
						r.Header.Set("X-Forwarded-Proto", proto)
						setPageIdentity(r, mode)
						w := answer(h, r)
						if w.Code != 200 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != expected.String() {
							t.Fatalf("%s %s %s %s: %d %v body differs", route.path, mode, proto, host, w.Code, w.Header())
						}
						if !strings.Contains(w.Body.String(), endpoint) || !strings.Contains(w.Body.String(), tokenURL) {
							t.Fatal("addresses absent")
						}
						if route.name == "instructions" {
							command := "curl -fsSL " + origin + "/setup.sh | bash"
							found := false
							lines := strings.Split(w.Body.String(), "\n")
							for _, line := range lines[1 : len(lines)-1] {
								if strings.TrimLeft(line, " ") == command {
									found = true
								}
							}
							if !found {
								t.Fatal("fetch command absent as its own line")
							}
							for _, text := range []string{variable, server, "${XDG_CONFIG_HOME:-~/.config}/ikigenba/" + space + "/"} {
								if !strings.Contains(w.Body.String(), text) {
									t.Fatalf("missing %q", text)
								}
							}
						} else {
							if !strings.HasPrefix(w.Body.String(), "#!/usr/bin/env bash\n") {
								t.Fatal("installer shebang absent")
							}
							for _, value := range []string{space, endpoint, tokenURL, variable, server} {
								if !strings.Contains(w.Body.String(), "'"+value+"'") {
									t.Fatalf("missing single-quoted value %q", value)
								}
							}
						}
						headRequest := r.Clone(r.Context())
						headRequest.Method = "HEAD"
						head := answer(h, headRequest)
						if head.Code != w.Code || !reflect.DeepEqual(head.Header(), w.Header()) || head.Body.Len() != 0 {
							t.Fatal("HEAD differs from GET")
						}
					}
				}
			}
		}
	}
}

// R-Z3H7-79DV
func TestSetupRejectsOtherMethods(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, path := range []string{"/setup.txt", "/setup.sh"} {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "CUSTOM"} {
			for _, mode := range []string{"signed", "absent", "empty"} {
				r := pageRequest(method, path)
				setPageIdentity(r, mode)
				w := answer(h, r)
				if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
					t.Fatalf("%s %s %s: %d %v %q", method, path, mode, w.Code, w.Header(), w.Body.String())
				}
			}
		}
	}
}

func TestSetupReadsRewrittenAuth(t *testing.T) {
	path := servicesFile(t, []map[string]any{})
	h := gateway.Handler(pageConfig(t, path, basicBanner))
	for _, route := range []string{"/setup.txt", "/setup.sh"} {
		writeServices(t, path, []map[string]any{})
		before := answer(h, pageRequest("GET", route))
		auth := service("auth", "", true, false)
		auth["url"] = "https://new-auth.test"
		writeServices(t, path, []map[string]any{auth})
		after := answer(h, pageRequest("GET", route))
		if !strings.Contains(before.Body.String(), "https://auth.space.test/") || !strings.Contains(after.Body.String(), "https://new-auth.test/") || strings.Contains(after.Body.String(), "https://auth.space.test/") {
			t.Fatal("setup auth not read afresh")
		}
	}
}

// R-UNAH-XCU1 R-UOIE-B4KQ
func TestSetupNamesNormalizeEachSpaceCharacter(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, tc := range []struct{ host, variable, server string }{
		{"mcp.sbx.ikigenba.dev", "IKIGENBA_TOKEN_SBX_IKIGENBA_DEV", "ikigenba-sbx-ikigenba-dev"},
		{"mcp.wip-mcp.localhost:7403", "IKIGENBA_TOKEN_WIP_MCP_LOCALHOST", "ikigenba-wip-mcp-localhost"},
		{"mcp.Az09_-é界.test:12", "IKIGENBA_TOKEN_AZ09_____TEST", "ikigenba-az09-----test"},
		{"mcp.a\xff\xfe.test", "IKIGENBA_TOKEN_A___TEST", "ikigenba-a---test"},
		{"MCP.A.Test", "IKIGENBA_TOKEN_MCP_A_TEST", "ikigenba-mcp-a-test"},
		{"mcp.mcp.A.Test:", "IKIGENBA_TOKEN_MCP_A_TEST", "ikigenba-mcp-a-test"},
		{"mcp.a.test:port", "IKIGENBA_TOKEN_A_TEST_PORT", "ikigenba-a-test-port"},
		{"mcp.", "IKIGENBA_TOKEN_MCP_", "ikigenba-mcp-"},
	} {
		r := pageRequest("GET", "/setup.sh")
		r.Host = tc.host
		body := answer(h, r).Body.String()
		for _, value := range []string{tc.variable, tc.server} {
			if !strings.Contains(body, "'"+value+"'") {
				t.Fatalf("host %q: missing name %q", tc.host, value)
			}
		}
	}
}
