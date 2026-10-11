// Package urls builds service addresses.
package urls

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Service provides the declared connection contract.
const Service = "connections"

// CrossOrigin provides the declared connection contract.
const CrossOrigin = "request origin does not match connections"

// Scheme provides the declared connection contract.
func Scheme(r *http.Request) string {
	s := strings.ToLower(r.Header.Get("X-Forwarded-Proto"))
	if s == "http" || s == "https" {
		return s
	}
	return "https"
}

// configured provides the declared connection contract.
func configured(path, name string) string {
	list, err := services.Read(path)
	if err != nil {
		return ""
	}
	e, ok := list.Find(name)
	if !ok {
		return ""
	}
	return strings.TrimSuffix(e.URL, "/")
}

// Configured provides the declared connection contract.
func Configured(path string) string { return configured(path, Service) }

// Callback provides the declared connection contract.
func Callback(callbackURL, servicesPath string) string {
	if callbackURL != "" {
		return callbackURL
	}
	return Configured(servicesPath)
}

// Connect provides the declared connection contract.
func Connect(base, provider string) string { return base + "/" + provider + "/connect" }

// Auth provides the declared connection contract.
func Auth(r *http.Request, servicesPath string) string {
	if list, err := services.Read(servicesPath); err == nil {
		if e, ok := list.Find("auth"); ok {
			return strings.TrimSuffix(e.URL, "/")
		}
	}
	host := r.Host
	if i := strings.LastIndexByte(host, ':'); i >= 0 && i < len(host)-1 {
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
	if strings.HasPrefix(strings.ToLower(host), Service+".") {
		host = host[len(Service)+1:]
	}
	return Scheme(r) + "://auth." + host
}

// SameOrigin provides the declared connection contract.
func SameOrigin(r *http.Request) bool {
	v := r.Header.Values("Origin")
	return len(v) == 1 && v[0] == Scheme(r)+"://"+r.Host
}
