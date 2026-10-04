package gateway

import (
	"bytes"
	"net/http"
	"text/template"

	"github.com/ikigenba/ikigenba/appkit/services"
	assets "github.com/ikigenba/ikigenba/mcp"
)

var setupTemplates = template.Must(template.ParseFS(assets.Assets(), "setup.txt", "setup.sh"))

type setupData struct {
	Origin, Endpoint, TokenURL string
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
	return setupData{Origin: origin, Endpoint: origin + "/mcp", TokenURL: auth + "/"}
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
