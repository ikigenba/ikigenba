// Package urls builds sites' public addresses from requests and services.
package urls

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Scheme returns the recognized forwarded scheme, defaulting to https.
func Scheme(r *http.Request) string {
	s := asciiLower(r.Header.Get("X-Forwarded-Proto"))
	if s == "http" || s == "https" {
		return s
	}
	return "https"
}

// SitesURL returns the published sites address or the request address.
func SitesURL(r *http.Request, servicesPath string) string {
	if s := serviceURL(servicesPath, "sites"); s != "" {
		return strings.TrimSuffix(s, "/")
	}
	return Scheme(r) + "://" + r.Host
}

// SiteURL joins the base and slug without normalizing either.
func SiteURL(base, slug string) string { return base + "/" + slug + "/" }

// ApexBase returns the sites address for a request on the apex host.
func ApexBase(r *http.Request, servicesPath string) string {
	if s := serviceURL(servicesPath, "sites"); s != "" {
		return strings.TrimSuffix(s, "/")
	}
	return Scheme(r) + "://sites." + r.Host
}

// AuthProfile returns auth's profile address.
func AuthProfile(r *http.Request, servicesPath string) string { return authBase(r, servicesPath) + "/" }

// AuthLogout returns auth's sign-out address.
func AuthLogout(r *http.Request, servicesPath string) string {
	return authBase(r, servicesPath) + "/logout"
}

// SignIn returns auth's sign-in address including the escaped return address.
func SignIn(r *http.Request) string {
	scheme := Scheme(r)
	return scheme + "://auth." + stripSites(r.Host) + "/?return=" + url.QueryEscape(scheme+"://"+r.Host+r.RequestURI)
}

type contextKey struct{}

// NewContext carries a request's sites base address.
func NewContext(ctx context.Context, sitesURL string) context.Context {
	return context.WithValue(ctx, contextKey{}, sitesURL)
}

// FromContext retrieves the closest sites address in the context ancestry.
func FromContext(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(contextKey{}).(string)
	return s, ok
}

func serviceURL(path, name string) string {
	list, err := services.Read(path)
	if err != nil {
		return ""
	}
	entry, ok := list.Find(name)
	if !ok {
		return ""
	}
	return entry.URL
}

func authBase(r *http.Request, path string) string {
	if s := serviceURL(path, "auth"); s != "" {
		return s
	}
	host := r.Host
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		digits := true
		for j := i + 1; j < len(host); j++ {
			if host[j] < '0' || host[j] > '9' {
				digits = false
				break
			}
		}
		if digits {
			host = host[:i]
		}
	}
	return Scheme(r) + "://auth." + stripSites(host)
}

func stripSites(host string) string {
	if len(host) > 6 && asciiLower(host[:6]) == "sites." {
		return host[6:]
	}
	return host
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
