package gateway

import (
	"bytes"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	assets "github.com/ikigenba/ikigenba/mcp"
)

var connectTemplates = template.Must(page.Templates().ParseFS(assets.Assets(), "*.html"))
var appkitStatic = page.Static()

type connectService struct {
	Name, Description, URL, Reason string
	Available                      bool
}

type connectData struct {
	Banner   page.Banner
	Endpoint string
	Services []connectService
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
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	origin := scheme + "://auth." + requestSpace(r.Host)
	if entry, found := entries.Find("auth"); found && entry.URL != "" {
		origin = entry.URL
	}
	u := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
	data := connectData{Banner: cfg.Banner(u), Endpoint: scheme + "://" + r.Host + "/mcp"}
	seen := make(map[string]bool)
	var names []string
	for _, entry := range entries {
		if entry.Name == ServiceName || seen[entry.Name] {
			continue
		}
		seen[entry.Name] = true
		first, _ := entries.Find(entry.Name)
		if first.MCP {
			names = append(names, entry.Name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		entry, _ := entries.Find(name)
		service := connectService{Name: name, Description: entry.Description, URL: data.Endpoint + "/" + name, Available: entry.Enabled}
		if !entry.Enabled {
			service.Reason = "disabled"
		}
		data.Services = append(data.Services, service)
	}
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
