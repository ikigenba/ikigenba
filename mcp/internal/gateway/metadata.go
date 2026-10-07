package gateway

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/ikigenba/ikigenba/appkit/services"
)

const metadataPath = "/.well-known/oauth-protected-resource"

func serveMetadata(w http.ResponseWriter, r *http.Request, entries services.List) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	auth := requestScheme(r) + "://auth." + requestSpace(r.Host)
	if entry, found := entries.Find("auth"); found && entry.URL != "" {
		if u, err := url.Parse(entry.URL); err == nil && u.Scheme != "" && u.Host != "" {
			auth = u.Scheme + "://" + u.Host
		}
	}
	document := struct {
		Resource               string   `json:"resource"`
		AuthorizationServers   []string `json:"authorization_servers"`
		BearerMethodsSupported []string `json:"bearer_methods_supported"`
	}{requestScheme(r) + "://" + r.Host + "/mcp", []string{auth}, []string{"header"}}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(document)
	}
}
