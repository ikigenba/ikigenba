// Package urls builds the public addresses handed to artifacts' callers.
package urls

import (
	"context"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Service and FilesPrefix identify artifacts and its byte route.
const (
	Service     = "artifacts"
	FilesPrefix = "/files/"
)

// Scheme returns the forwarded scheme, defaulting to HTTPS.
func Scheme(r *http.Request) string {
	switch strings.ToLower(r.Header.Get("X-Forwarded-Proto")) {
	case "http":
		return "http"
	default:
		return "https"
	}
}

func serviceURL(path, name string) (string, bool) {
	list, err := services.Read(path)
	if err != nil {
		return "", false
	}
	entry, ok := list.Find(name)
	return strings.TrimRight(entry.URL, "/"), ok
}

// Configured returns the published artifacts URL without a request fallback.
func Configured(servicesPath string) string {
	base, _ := serviceURL(servicesPath, Service)
	return base
}

// Base returns the published artifacts URL or the request origin.
func Base(r *http.Request, servicesPath string) string {
	if base, ok := serviceURL(servicesPath, Service); ok {
		return base
	}
	return Scheme(r) + "://" + r.Host
}

// File returns the address of an artifact path.
func File(base, path string) string { return base + FilesPrefix + path }

// Auth returns the published or inferred auth URL.
func Auth(r *http.Request, servicesPath string) string {
	if base, ok := serviceURL(servicesPath, "auth"); ok {
		return base
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

type contextKey struct{}

// NewContext carries the public artifacts base on a context.
func NewContext(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, contextKey{}, base)
}

// FromContext reads the public artifacts base from a context.
func FromContext(ctx context.Context) (string, bool) {
	base, ok := ctx.Value(contextKey{}).(string)
	return base, ok
}
