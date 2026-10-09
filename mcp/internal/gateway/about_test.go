package gateway_test

import (
	"bytes"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/page"
	assets "github.com/ikigenba/ikigenba/mcp"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

// R-ZYDU-0HJ1
func TestDescription(t *testing.T) {
	const description string = gateway.Description
	if description == "" || strings.ContainsAny(description, "\"\\") || strings.ContainsFunc(description, unicode.IsControl) {
		t.Fatalf("invalid description %q", description)
	}
}

// R-00TM-S10F R-09CX-GF7A R-ZTI8-HEK9
func TestAboutTemplateData(t *testing.T) {
	set, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	// The positional literal proves the declared fields and their order by use.
	for _, data := range []gateway.AboutData{{}, {basicBanner(page.User{Email: "supplied@example.test"}), "supplied <&> description"}} {
		var body bytes.Buffer
		if err := set.ExecuteTemplate(&body, "about", data); err != nil {
			t.Fatal(err)
		}
	}
}

// R-021J-5SR4 R-039F-JKHT R-SHOE-CVVN
func TestAboutExactlyRendersRequestData(t *testing.T) {
	set, err := page.Templates().ParseFS(assets.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, authURL := range []string{"", "https://accounts.test/base/"} {
		entry := service("auth", "", true, false)
		entry["url"] = authURL
		path := servicesFile(t, []map[string]any{entry})
		for _, trail := range [][]page.Level{nil, {{Name: "old", URL: "/old"}}, {{Name: "first", URL: "/first"}, {Name: "second", URL: "/second"}}} {
			for _, proto := range []string{"", "http", "https", "HTTP"} {
				for _, email := range []string{"", " supplied<&>@example.test "} {
					var users []page.User
					original := page.Banner{
						Service: "supplied-service", Icon: "supplied-icon", Version: "supplied-version", Email: "source@example.test",
						ProfileURL: "https://profile.test/", LogoutURL: "https://profile.test/logout", Home: "https://home.test/", Tools: true, Trail: trail,
						Services: []page.Service{{Name: "supplied-launcher", URL: "https://launcher.test/", Icon: "supplied-launcher-icon", Enabled: true, Current: true}},
					}
					answering := false
					cfg := pageConfig(t, path, func(u page.User) page.Banner {
						if answering {
							users = append(users, u)
						}
						return original
					})
					r := pageRequest("GET", "/about?ignored=a%2Fb&ignored=x+y")
					r.Host = "mcp.space.test:8443"
					r.Header.Set("X-Forwarded-Proto", proto)
					r.Header["X-User-Id"] = []string{"person", ""}
					r.Header.Set("X-User-Email", email)
					h := gateway.Handler(cfg)
					answering = true
					w := answer(h, r)
					answering = false
					scheme := "https"
					if proto == "http" {
						scheme = "http"
					}
					origin := authURL
					if origin == "" {
						origin = scheme + "://auth.space.test"
					}
					wantUser := page.User{Email: email, ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
					if !slices.Contains(users, wantUser) {
						t.Fatalf("banner calls %#v, want %#v", users, wantUser)
					}
					wantBanner := original
					wantBanner.Trail = []page.Level{{Name: "about", URL: "/about"}}
					var expected bytes.Buffer
					if err := set.ExecuteTemplate(&expected, "about", gateway.AboutData{Banner: wantBanner, Description: gateway.Description}); err != nil {
						t.Fatal(err)
					}
					if w.Code != 200 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || w.Body.String() != expected.String() {
						t.Fatalf("about response differs: %d %v", w.Code, w.Header())
					}
				}
			}
		}
	}
}

// R-05P8-B3Z7
func TestAboutHead(t *testing.T) {
	for _, path := range []string{"", servicesFile(t, []map[string]any{service("alpha", "", true, true)})} {
		h := gateway.Handler(pageConfig(t, path, basicBanner))
		get := answer(h, pageRequest("GET", "/about?from=test"))
		head := answer(h, pageRequest("HEAD", "/about?from=test"))
		if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
			t.Fatalf("GET=%d %v HEAD=%d %v %q", get.Code, get.Header(), head.Code, head.Header(), head.Body.String())
		}
	}
}

// R-06X4-OVPW
func TestAboutRejectsOtherMethods(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "CUSTOM"} {
		for _, identity := range []string{"signed", "absent", "empty"} {
			r := pageRequest(method, "/about?ignored=yes")
			setPageIdentity(r, identity)
			w := answer(h, r)
			if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
				t.Fatalf("%s %s: %d %v %q", method, identity, w.Code, w.Header(), w.Body.String())
			}
		}
	}
}

// R-0851-2NGL R-ZC0H-VNKQ
func TestGuestAboutRedirect(t *testing.T) {
	for _, authURL := range []string{"", "https://auth.sbx.ikigenba.dev", "https://accounts.test/base/"} {
		entry := service("auth", "", true, false)
		entry["url"] = authURL
		h := gateway.Handler(pageConfig(t, servicesFile(t, []map[string]any{entry}), func(page.User) page.Banner { t.Fatal("guest called banner"); return page.Banner{} }))
		for _, proto := range []string{"", "http", "https", "HTTP"} {
			for _, target := range []string{"/about", "/about?from=launcher", "/about?a=%2F&b=x+y&b=%26"} {
				for _, method := range []string{"GET", "HEAD"} {
					for _, mode := range []string{"absent", "empty"} {
						r := pageRequest(method, target)
						r.Host = "mcp.sbx.ikigenba.dev"
						r.Header.Set("X-Forwarded-Proto", proto)
						setPageIdentity(r, mode)
						scheme := "https"
						if proto == "http" {
							scheme = "http"
						}
						origin := authURL
						if origin == "" {
							origin = scheme + "://auth.sbx.ikigenba.dev"
						}
						want := origin + "/?return=" + url.QueryEscape(scheme+"://"+r.Host+r.URL.RequestURI())
						w := answer(h, r)
						if w.Code != 302 || !reflect.DeepEqual(w.Header().Values("Location"), []string{want}) || w.Body.Len() != 0 {
							t.Fatalf("%s %s %s: %d %v %q want %s", mode, proto, target, w.Code, w.Header(), w.Body.String(), want)
						}
					}
				}
			}
		}
	}
}
