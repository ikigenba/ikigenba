package web

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
)

type landingData struct {
	Banner      page.Banner
	ReposURL    string
	Credentials clone.Credentials
}

type aboutData struct {
	Banner      page.Banner
	Description string
}

// Tool is the name and description of one registered MCP tool.
type Tool struct {
	Name, Description string
}

// ToolsData supplies the tools page's banner and registered tools.
type ToolsData struct {
	Banner page.Banner
	Tools  []Tool
}

func newPages(cfg Config) http.Handler {
	templates := template.Must(page.Templates().ParseFS(repos.Assets(), "*.html"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		banner := cfg.Banner(bannerUser(r, cfg.ServicesPath))
		var name string
		var data any
		switch r.URL.Path {
		case "/":
			name = "landing"
			base := clone.Base(r, cfg.ServicesPath)
			data = landingData{Banner: banner, ReposURL: base, Credentials: clone.Guidance(base)}
		case "/about":
			name = "about"
			banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
			data = aboutData{Banner: banner, Description: Description}
		case "/tools":
			name = "tools"
			banner.Trail = []page.Level{{Name: "tools", URL: "/tools"}}
			registered := cfg.MCP.Tools()
			listed := make([]Tool, len(registered))
			for i, tool := range registered {
				listed[i] = Tool{Name: tool.Name, Description: tool.Description}
			}
			data = ToolsData{Banner: banner, Tools: listed}
		}
		var body bytes.Buffer
		if err := templates.ExecuteTemplate(&body, name, data); err != nil {
			panic(err)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body.Bytes())
		}
	})
}

func bannerUser(r *http.Request, servicesPath string) page.User {
	origin := ""
	if listed, err := services.Read(servicesPath); err == nil {
		if auth, found := listed.Find("auth"); found {
			origin = auth.URL
		}
	}
	if origin == "" {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme != "http" && scheme != "https" {
			scheme = "https"
		}
		space := r.Host
		if colon := strings.LastIndexByte(space, ':'); colon >= 0 && strings.Trim(space[colon+1:], "0123456789") == "" {
			space = space[:colon]
		}
		if strings.HasPrefix(space, "repos.") && len(space) > len("repos.") {
			space = space[len("repos."):]
		}
		origin = scheme + "://auth." + space
	}
	return page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
}
