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

var connectTemplates = template.Must(page.Templates().ParseFS(assets.Assets(), "*.html"))
var appkitStatic = page.Static()

type connectData struct {
	Banner   page.Banner
	Endpoint string
	SetupURL string
}

func serveAssets(w http.ResponseWriter, r *http.Request) {
	appkitStatic.ServeHTTP(w, r)
}

func gatewayNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte("not found\n"))
	}
}

func serveConnect(w http.ResponseWriter, r *http.Request, cfg Config, entries services.List) {
	if r.URL.Path != "/" {
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
	data := connectData{Banner: cfg.Banner(u), Endpoint: addresses.Endpoint, SetupURL: addresses.Origin + "/setup.txt"}
	var body bytes.Buffer
	if err := connectTemplates.ExecuteTemplate(&body, "connect", data); err != nil {
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
