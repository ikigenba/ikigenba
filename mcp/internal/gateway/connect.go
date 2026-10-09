package gateway

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	assets "github.com/ikigenba/ikigenba/mcp"
)

// NotFound is the body of the gateway's 404 response.
const NotFound string = "not found\n"

var gatewayTemplates = template.Must(page.Templates().ParseFS(assets.Assets(), "*.html"))
var appkitStatic = page.Static()

type connectData struct {
	Banner   page.Banner
	Endpoint string
	Server   string
}

func serveAssets(w http.ResponseWriter, r *http.Request) {
	appkitStatic.ServeHTTP(w, r)
}

func gatewayNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(NotFound))
	}
}

func servePage(w http.ResponseWriter, r *http.Request, cfg Config, entries services.List) {
	if r.URL.Path != "/" && r.URL.Path != "/about" {
		gatewayNotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	addresses := requestAddresses(r, entries)
	if r.Header.Get("X-User-Id") == "" {
		w.Header().Set("Location", addresses.TokenURL+"?return="+url.QueryEscape(addresses.Origin+r.URL.RequestURI()))
		w.WriteHeader(http.StatusFound)
		return
	}
	origin := strings.TrimSuffix(addresses.TokenURL, "/")
	u := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: addresses.TokenURL, LogoutURL: origin + "/logout"}
	banner := cfg.Banner(u)
	name := "connect"
	var data any = connectData{Banner: banner, Endpoint: addresses.Endpoint, Server: addresses.Server}
	if r.URL.Path == "/about" {
		name = "about"
		banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
		data = AboutData{Banner: banner, Description: Description}
	}
	var body bytes.Buffer
	if err := gatewayTemplates.ExecuteTemplate(&body, name, data); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func requestSpace(host string) string {
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
		digits := true
		for _, c := range host[colon+1:] {
			if c < '0' || c > '9' {
				digits = false
			}
		}
		if digits {
			host = host[:colon]
		}
	}
	if strings.HasPrefix(host, "mcp.") && len(host) > 4 {
		host = host[4:]
	}
	return host
}
