// Package pages builds authentication and sibling page addresses.
package pages

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

func scheme(r *http.Request) string {
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") {
		return "http"
	}
	return "https"
}

// Auth returns the authentication service's base address.
func Auth(r *http.Request, servicesPath string) string {
	list, err := services.Read(servicesPath)
	if err == nil {
		if e, ok := list.Find("auth"); ok && e.URL != "" {
			return strings.TrimRight(e.URL, "/")
		}
	}
	host := r.Host
	if i := strings.LastIndexByte(host, ':'); i >= 0 && decimal(host[i+1:]) {
		host = host[:i]
	}
	if len(host) >= len("ledger.") && strings.EqualFold(host[:len("ledger.")], "ledger.") {
		host = host[len("ledger."):]
	}
	return scheme(r) + "://auth." + host
}
func decimal(s string) bool {
	if s == "" {
		return false
	}
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

// SignIn returns an authentication address carrying the original request address.
func SignIn(r *http.Request, servicesPath string) string {
	return Auth(r, servicesPath) + "/?return=" + url.QueryEscape(scheme(r)+"://"+r.Host+r.RequestURI)
}
