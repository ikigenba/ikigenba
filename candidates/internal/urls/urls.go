// Package urls builds addresses of sibling pages.
package urls

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/services"
)

// Base removes trailing slashes from an address.
func Base(u string) string { return strings.TrimRight(u, "/") }

// Scrapes returns the records service address.
func Scrapes(path string) string {
	list, err := services.Read(path)
	if err != nil {
		return ""
	}
	e, ok := list.Find("scrapes")
	if !ok {
		return ""
	}
	return Base(e.URL)
}

// Auth returns the authentication service address.
func Auth(r *http.Request, path string) string {
	list, err := services.Read(path)
	if err == nil {
		if e, ok := list.Find("auth"); ok && e.URL != "" {
			return Base(e.URL)
		}
	}
	scheme := "https"
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") {
		scheme = "http"
	}
	host := r.Host
	if i := strings.LastIndexByte(host, ':'); i >= 0 && decimal(host[i+1:]) {
		host = host[:i]
	}
	if len(host) >= len("candidates.") && strings.EqualFold(host[:len("candidates.")], "candidates.") {
		host = host[len("candidates."):]
	}
	return scheme + "://auth." + host
}
func decimal(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// RowPage returns the address of one records row.
func RowPage(base, run, sourceID string) string {
	if base == "" || run == "" || sourceID == "" {
		return ""
	}
	return base + "/runs/" + run + "/rows/" + url.PathEscape(sourceID)
}

// RunPage returns the address of one records run.
func RunPage(base, run string) string {
	if base == "" || run == "" {
		return ""
	}
	return base + "/runs/" + run
}

// PagePath returns the local address of one kept page.
func PagePath(candidate string, number int64) string {
	return "/candidates/" + candidate + "/pages/" + strconv.FormatInt(number, 10)
}
