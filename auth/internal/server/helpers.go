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

// onSpaceOrigin accepts only the serialized origins that can drive logout.
func onSpaceOrigin(origin, requestHost string) bool {
	if isLocalRequest(requestHost) {
		const prefix = "http://"
		if len(origin) < len(prefix) || !asciiEqualFold(origin[:len(prefix)], prefix) {
			return false
		}
		hostPort := origin[len(prefix):]
		const localhost = "localhost"
		if len(hostPort) < len(localhost) || !asciiEqualFold(hostPort[:len(localhost)], localhost) {
			return false
		}
		rest := hostPort[len(localhost):]
		if rest == "" {
			return true
		}
		if len(rest) < 2 || rest[0] != ':' || rest[1] == '0' {
			return false
		}
		port := 0
		for i := 1; i < len(rest); i++ {
			if rest[i] < '0' || rest[i] > '9' {
				return false
			}
			port = port*10 + int(rest[i]-'0')
			if port > 65535 {
				return false
			}
		}
		return port > 0
	}

	const prefix = "https://"
	if len(origin) <= len(prefix) || !asciiEqualFold(origin[:len(prefix)], prefix) {
		return false
	}
	host := origin[len(prefix):]
	if strings.ContainsAny(host, "/?#@:") {
		return false
	}
	requestSpace := space(requestHost)
	if asciiEqualFold(host, requestSpace) {
		return true
	}
	if len(host) <= len(requestSpace)+1 || host[len(host)-len(requestSpace)-1] != '.' {
		return false
	}
	return asciiEqualFold(host[len(host)-len(requestSpace):], requestSpace)
}

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ac, bc := a[i], b[i]
		if ac >= 'A' && ac <= 'Z' {
			ac += 'a' - 'A'
		}
		if bc >= 'A' && bc <= 'Z' {
			bc += 'a' - 'A'
		}
		if ac != bc {
			return false
		}
	}
	return true
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
