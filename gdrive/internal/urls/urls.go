// Package urls builds service page addresses.
package urls

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Base removes trailing slashes from a service address.
func Base(url string) string { return strings.TrimRight(url, "/") }

// Connect returns the Google connection page address.
func Connect(base string) string { return base + "/google/connect" }

// ArtifactPage returns an artifact page when its host and id exist.
func ArtifactPage(base, id string) string {
	if base == "" || id == "" {
		return ""
	}
	return base + "/" + id
}

var port = regexp.MustCompile(`:[0-9]+$`)

// Auth returns the auth service address for the banner.
func Auth(r *http.Request, servicesPath string) string {
	if list, err := services.Read(servicesPath); err == nil {
		if entry, ok := list.Find("auth"); ok && entry.URL != "" {
			return Base(entry.URL)
		}
	}
	scheme := "https"
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") {
		scheme = "http"
	}
	host := port.ReplaceAllString(r.Host, "")
	if strings.HasPrefix(strings.ToLower(host), "gdrive.") {
		host = host[len("gdrive."):]
	}
	return scheme + "://auth." + host
}
