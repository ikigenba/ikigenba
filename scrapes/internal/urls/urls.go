// Package urls resolves the authentication service address.
package urls

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Auth resolves the authentication service address.
func Auth(r *http.Request, servicesPath string) string {
	if list, err := services.Read(servicesPath); err == nil {
		if e, ok := list.Find("auth"); ok && e.URL != "" {
			return strings.TrimRight(e.URL, "/")
		}
	}
	scheme := "https"
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") {
		scheme = "http"
	}
	host := r.Host
	if i := strings.LastIndexByte(host, ':'); i >= 0 && i+1 < len(host) {
		decimal := true
		for _, c := range host[i+1:] {
			if c < '0' || c > '9' {
				decimal = false
			}
		}
		if decimal {
			host = host[:i]
		}
	}
	if len(host) >= 8 && strings.EqualFold(host[:8], "scrapes.") {
		host = host[8:]
	}
	return scheme + "://auth." + host
}
