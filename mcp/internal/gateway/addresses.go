package gateway

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

type addresses struct{ Origin, Endpoint, TokenURL, Server string }

func requestScheme(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme != "http" && scheme != "https" {
		return "https"
	}
	return scheme
}

func requestAddresses(r *http.Request, entries services.List) addresses {
	scheme := requestScheme(r)
	origin := scheme + "://" + r.Host
	space := requestSpace(r.Host)
	auth := scheme + "://auth." + space
	if entry, found := entries.Find("auth"); found && entry.URL != "" {
		auth = entry.URL
	}
	var server strings.Builder
	for _, c := range space {
		switch {
		case c >= 'A' && c <= 'Z':
			server.WriteRune(c - 'A' + 'a')
		case c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
			server.WriteRune(c)
		default:
			server.WriteByte('-')
		}
	}
	return addresses{Origin: origin, Endpoint: origin + "/mcp", TokenURL: auth + "/", Server: server.String()}
}
