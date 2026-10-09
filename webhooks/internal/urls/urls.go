// Package urls builds webhooks' public addresses and auth's from requests and services.
package urls

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Service is the name the services file lists webhooks under.
const Service = "webhooks"

// IngressPrefix is the path every webhook's address begins with.
const IngressPrefix = "/in/"

// Scheme returns the recognized forwarded scheme, defaulting to https.
func Scheme(r *http.Request) string {
	s := strings.ToLower(r.Header.Get("X-Forwarded-Proto"))
	if s == "http" || s == "https" {
		return s
	}
	return "https"
}

func host(r *http.Request) string {
	h := r.Host
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		digits := i+1 < len(h)
		for _, b := range []byte(h[i+1:]) {
			digits = digits && b >= '0' && b <= '9'
		}
		if digits {
			h = h[:i]
		}
	}
	return h
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
	return strings.TrimSuffix(entry.URL, "/")
}

// Base returns webhooks' own address: the services file's, or the request's.
func Base(r *http.Request, servicesPath string) string {
	if s := serviceURL(servicesPath, Service); s != "" {
		return s
	}
	return Scheme(r) + "://" + r.Host
}

// Hook returns the address a sender posts a webhook's deliveries to.
func Hook(base, slug string) string { return base + IngressPrefix + slug }

// Auth returns auth's address: the services file's, or one derived from the request's host.
func Auth(r *http.Request, servicesPath string) string {
	if s := serviceURL(servicesPath, "auth"); s != "" {
		return s
	}
	h := host(r)
	if len(h) > len(Service)+1 && strings.EqualFold(h[:len(Service)+1], Service+".") {
		h = h[len(Service)+1:]
	}
	return Scheme(r) + "://auth." + h
}

// SignIn returns auth's sign-in address, carrying the request's own address to return to.
func SignIn(r *http.Request, servicesPath string) string {
	return Auth(r, servicesPath) + "/?return=" + url.QueryEscape(Scheme(r)+"://"+r.Host+r.URL.RequestURI())
}

type contextKey struct{}

// NewContext carries webhooks' base address to the tools.
func NewContext(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, contextKey{}, base)
}

// FromContext returns the base address NewContext carried, if any.
func FromContext(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(contextKey{}).(string)
	return s, ok
}
