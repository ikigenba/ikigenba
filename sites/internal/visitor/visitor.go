// Package visitor handles anonymous browser identifiers and view metadata.
package visitor

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	// CookieName names the visitor cookie.
	CookieName = "ikigenba_visitor"
	// IDPrefix identifies visitor ids.
	IDPrefix = "vis_"
	// MaxAge is the cookie lifetime in seconds.
	MaxAge = 34560000
)

// ValidID reports whether s is a lowercase visitor id.
func ValidID(s string) bool {
	if len(s) != len(IDPrefix)+16 || !strings.HasPrefix(s, IDPrefix) {
		return false
	}
	for _, c := range s[len(IDPrefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Mint reads eight bytes and formats a visitor id.
func Mint(r io.Reader) (string, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", err
	}
	return IDPrefix + hex.EncodeToString(b[:]), nil
}

// FromRequest returns the first visitor cookie if it is valid.
func FromRequest(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || !ValidID(c.Value) {
		return "", false
	}
	return c.Value, true
}

// Secure reports whether the forwarded scheme is HTTPS.
func Secure(r *http.Request) bool {
	s := r.Header.Get("X-Forwarded-Proto")
	if len(s) != 5 {
		return false
	}
	for i, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != "https"[i] {
			return false
		}
	}
	return true
}

// Cookie builds the visitor Set-Cookie header in its specified order.
func Cookie(id string, secure bool) string {
	s := CookieName + "=" + id + "; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax"
	if secure {
		s += "; Secure"
	}
	return s
}

// ReferrerHost returns the parsed referrer's host, without credentials.
func ReferrerHost(r *http.Request) string {
	if r.Referer() == "" {
		return ""
	}
	u, err := url.Parse(r.Referer())
	if err != nil {
		return ""
	}
	return u.Host
}
