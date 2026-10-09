// Package urls builds links to the authentication service.
package urls

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Scheme returns the authenticated proxy's supported scheme.
func Scheme(r *http.Request) string {
	if r.Header.Get("X-Forwarded-Proto") == "http" {
		return "http"
	}
	return "https"
}

// AuthProfile returns the authentication profile address.
func AuthProfile(r *http.Request, servicesPath string) string { return auth(r, servicesPath) + "/" }

// AuthLogout returns the authentication sign-out address.
func AuthLogout(r *http.Request, servicesPath string) string {
	return auth(r, servicesPath) + "/logout"
}

func auth(r *http.Request, path string) string {
	if entries, err := services.Read(path); err == nil {
		if entry, ok := entries.Find("auth"); ok && entry.URL != "" {
			return entry.URL
		}
	}
	host := r.Host
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		digits := true
		for _, c := range host[i+1:] {
			if c < '0' || c > '9' {
				digits = false
			}
		}
		if digits {
			host = host[:i]
		}
	}
	if len(host) > len("prompts.") && strings.EqualFold(host[:len("prompts.")], "prompts.") {
		host = host[len("prompts."):]
	}
	return Scheme(r) + "://auth." + host
}
