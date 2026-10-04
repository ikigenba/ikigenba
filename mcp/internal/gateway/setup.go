package gateway

import (
	"bytes"
	"net/http"
	"strings"
	"text/template"

	"github.com/ikigenba/ikigenba/appkit/services"
	assets "github.com/ikigenba/ikigenba/mcp"
)

var setupTemplates = template.Must(template.ParseFS(assets.Assets(), "setup.txt", "setup.sh"))

type setupData struct {
	Space, Origin, Endpoint, TokenURL, Variable, Server string
}

func requestScheme(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme != "http" && scheme != "https" {
		return "https"
	}
	return scheme
}

func requestAddresses(r *http.Request, entries services.List) setupData {
	scheme := requestScheme(r)
	origin := scheme + "://" + r.Host
	auth := scheme + "://auth." + requestSpace(r.Host)
	if entry, found := entries.Find("auth"); found && entry.URL != "" {
		auth = entry.URL
	}
	space := requestSpace(r.Host)
	var variable, server strings.Builder
	variable.WriteString("IKIGENBA_TOKEN_")
	server.WriteString("ikigenba-")
	for _, c := range space {
		switch {
		case c >= 'a' && c <= 'z':
			variable.WriteRune(c - 'a' + 'A')
			server.WriteRune(c)
		case c >= 'A' && c <= 'Z':
			variable.WriteRune(c)
			server.WriteRune(c - 'A' + 'a')
		case c >= '0' && c <= '9':
			variable.WriteRune(c)
			server.WriteRune(c)
		default:
			variable.WriteByte('_')
			server.WriteByte('-')
		}
	}
	return setupData{Space: space, Origin: origin, Endpoint: origin + "/mcp", TokenURL: auth + "/", Variable: variable.String(), Server: server.String()}
}

func serveSetup(w http.ResponseWriter, r *http.Request, entries services.List) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := "instructions"
	if r.URL.Path == "/setup.sh" {
		name = "installer"
	}
	var body bytes.Buffer
	if err := setupTemplates.ExecuteTemplate(&body, name, requestAddresses(r, entries)); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
