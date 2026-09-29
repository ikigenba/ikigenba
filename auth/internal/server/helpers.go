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

func returnAuthority(value string) (string, bool) {
	for i := range len(value) {
		if value[i] <= 0x20 || value[i] == 0x7f || value[i] == '\\' {
			return "", false
		}
	}
	prefix := 0
	if len(value) >= 7 && asciiEqualFold(value[:7], "http://") {
		prefix = 7
	}
	if len(value) >= 8 && asciiEqualFold(value[:8], "https://") {
		prefix = 8
	}
	if prefix == 0 {
		return "", false
	}
	// Splitting must retain an empty authority rather than skipping it.
	end := strings.IndexAny(value[prefix:], "/?#")
	raw := value[prefix:]
	if end >= 0 {
		raw = raw[:end]
	}
	host, port, hasPort := strings.Cut(raw, ":")
	if host == "" {
		return "", false
	}
	for i := range len(host) {
		c := host[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return "", false
		}
	}
	if hasPort {
		for i := range len(port) {
			if port[i] < '0' || port[i] > '9' {
				return "", false
			}
		}
	}
	return raw, true
}

func inSpace(returnURL, requestHost string) bool {
	authority, ok := returnAuthority(returnURL)
	if !ok {
		return false
	}
	host, _, _ := strings.Cut(authority, ":")
	requestSpace := stripNumericPort(space(requestHost))
	return requestSpace != "" && (asciiEqualFold(host, requestSpace) || len(host) >= len(requestSpace)+1 && host[len(host)-len(requestSpace)-1] == '.' && asciiEqualFold(host[len(host)-len(requestSpace):], requestSpace))
}

func returnQuery(query string) string {
	for _, pair := range strings.Split(query, "&") {
		if strings.Contains(pair, ";") {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(name)
		if err != nil {
			continue
		}
		value, err = url.QueryUnescape(value)
		if err == nil && name == "return" {
			return value
		}
	}
	return ""
}

func stripNumericPort(host string) string {
	colon := strings.LastIndexByte(host, ':')
	if colon < 0 || colon == len(host)-1 {
		return host
	}
	for i := colon + 1; i < len(host); i++ {
		if host[i] < '0' || host[i] > '9' {
			return host
		}
	}
	return host[:colon]
}

func apexName(host string) string {
	labels := strings.Split(stripNumericPort(host), ".")
	if len(labels) > 2 {
		labels = labels[len(labels)-2:]
	}
	return strings.Join(labels, ".")
}

func percentEncode(value string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(value) {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}
