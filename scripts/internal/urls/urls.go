// Package urls builds the auth addresses used by script pages.
package urls

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Scheme returns the forwarded scheme accepted by scripts.
func Scheme(r *http.Request) string {
	if r.Header.Get("X-Forwarded-Proto") == "http" {
		return "http"
	}
	return "https"
}

func auth(r *http.Request, path string) string {
	entries, err := services.Read(path)
	if err == nil {
		if e, ok := entries.Find("auth"); ok && e.URL != "" {
			return e.URL
		}
	}
	space := r.Host
	if i := strings.LastIndexByte(space, ':'); i >= 0 {
		digits := true
		for _, c := range space[i+1:] {
			if c < '0' || c > '9' {
				digits = false
				break
			}
		}
		if digits {
			space = space[:i]
		}
	}
	if len(space) > 8 && strings.EqualFold(space[:8], "scripts.") {
		space = space[8:]
	}
	return Scheme(r) + "://auth." + space
}

// AuthProfile returns the auth profile address, rereading the services file.
func AuthProfile(r *http.Request, servicesPath string) string { return auth(r, servicesPath) + "/" }

// AuthLogout returns the auth logout address, rereading the services file.
func AuthLogout(r *http.Request, servicesPath string) string {
	return auth(r, servicesPath) + "/logout"
}
