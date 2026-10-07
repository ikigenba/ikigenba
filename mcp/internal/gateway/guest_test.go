package gateway_test

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
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
