package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	// SessionCookieName carries the opaque browser session identifier.
	SessionCookieName = "ikigenba_session"

	// HeaderUserID conveys the authenticated user id to nginx.
	HeaderUserID = "X-User-Id"
	// HeaderUserEmail conveys the authenticated user email to nginx.
	HeaderUserEmail = "X-User-Email"
)

const localHost = "localhost:3001"

// space returns the space a request host belongs to. A request to an auth
// subdomain removes exactly its leading auth. label; a local request has no
// space.
func space(host string) string {
	host = hostWithoutPort(host)
	if strings.HasPrefix(host, "auth.") {
		return strings.TrimPrefix(host, "auth.")
	}
	return host
}

func hostWithoutPort(host string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		return name
	}
	return host
}

func isLocalRequest(host string) bool {
	return host == localHost
}

func redirectURI(host string) string {
	if isLocalRequest(host) {
		return "http://localhost:3001/login/google/callback"
	}
	return "https://auth." + space(host) + "/login/google/callback"
}

func ownOrigin(host string) string {
	if isLocalRequest(host) {
		return "http://" + localHost
	}
	return "https://auth." + space(host)
}

func cookieForHost(host, value string, expire bool) *http.Cookie {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if !isLocalRequest(host) {
		cookie.Domain = space(host)
	}
	if expire {
		cookie.MaxAge = -1
	}
	return cookie
}

func inSpace(returnURL, requestHost string) bool {
	u, err := url.Parse(returnURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	requestSpace := space(requestHost)
	return u.Hostname() == requestSpace || strings.HasSuffix(u.Hostname(), "."+requestSpace)
}
