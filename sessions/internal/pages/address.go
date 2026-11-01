package pages

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

var trailingPort = regexp.MustCompile(`:[0-9]+$`)

func scheme(r *http.Request) string {
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") {
		return "http"
	}
	return "https"
}

// Auth derives the auth address from the services file or the request.
func Auth(r *http.Request, servicesPath string) string {
	if entries, err := services.Read(servicesPath); err == nil {
		if auth, ok := entries.Find("auth"); ok && auth.URL != "" {
			return strings.TrimRight(auth.URL, "/")
		}
	}
	host := trailingPort.ReplaceAllString(r.Host, "")
	if strings.HasPrefix(strings.ToLower(host), "sessions.") {
		host = host[len("sessions."):]
	}
	return scheme(r) + "://auth." + host
}

// SignIn preserves the complete request address as auth's return parameter.
func SignIn(r *http.Request, servicesPath string) string {
	return Auth(r, servicesPath) + "/?return=" + url.QueryEscape(scheme(r)+"://"+r.Host+r.RequestURI)
}
