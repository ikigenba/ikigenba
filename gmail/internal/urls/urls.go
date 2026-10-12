// Package urls constructs the connector's public addresses.
package urls

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Base removes trailing slashes.
func Base(u string) string { return strings.TrimRight(u, "/") }

// Connect constructs the Google connection page address.
func Connect(base string) string { return base + "/google/connect" }

// ArtifactPage constructs an artifact record address.
func ArtifactPage(base, id string) string {
	if base == "" || id == "" {
		return ""
	}
	return base + "/" + id
}

// ArtifactFile constructs an artifact file address.
func ArtifactFile(base, path string) string {
	if base == "" || path == "" {
		return ""
	}
	return base + "/files/" + path
}

// Auth resolves the authentication service address.
func Auth(r *http.Request, servicesPath string) string {
	if list, err := services.Read(servicesPath); err == nil {
		if e, ok := list.Find("auth"); ok && e.URL != "" {
			return Base(e.URL)
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
	if len(host) >= 6 && strings.EqualFold(host[:6], "gmail.") {
		host = host[6:]
	}
	return scheme + "://auth." + host
}

// MessageLink constructs a Gmail message link.
func MessageLink(message string) string {
	if message == "" {
		return ""
	}
	return "https://mail.google.com/mail/u/0/#all/" + message
}

// DraftLink constructs a Gmail draft link.
func DraftLink(message string) string {
	if message == "" {
		return ""
	}
	return "https://mail.google.com/mail/u/0/#drafts/" + message
}

// DraftPage constructs a local draft page path.
func DraftPage(id string) string { return "/drafts/" + id }

// TransferPage constructs a local transfer page path.
func TransferPage(id string) string { return "/transfers/" + id }
