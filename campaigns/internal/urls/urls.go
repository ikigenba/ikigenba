// Package urls builds authentication and sibling page addresses.
package urls

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
	if len(host) >= len("campaigns.") && strings.EqualFold(host[:len("campaigns.")], "campaigns.") {
		host = host[len("campaigns."):]
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

// Sibling returns a configured sibling's base address, or empty when absent.
func Sibling(servicesPath, name string) string {
	list, err := services.Read(servicesPath)
	if err != nil {
		return ""
	}
	e, ok := list.Find(name)
	if !ok {
		return ""
	}
	return strings.TrimRight(e.URL, "/")
}

// CandidatePage returns a candidate's page address.
func CandidatePage(base, candidate string) string {
	return siblingPage(base, "/candidates/", candidate)
}

// ProspectListPage returns a prospect list's page address.
func ProspectListPage(base, prospectList string) string {
	return siblingPage(base, "/prospect-lists/", prospectList)
}

// ArtifactPage returns an artifact's page address.
func ArtifactPage(base, artifact string) string { return siblingPage(base, "/", artifact) }
func siblingPage(base, path, id string) string {
	if base == "" || id == "" {
		return ""
	}
	return base + path + url.PathEscape(id)
}
