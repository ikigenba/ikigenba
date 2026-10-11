// Package settings validates the environment read at startup.
package settings

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Settings contains the drain interval and explicit callback origin.
type Settings struct {
	DrainSeconds int64
	CallbackURL  string
}

// Error describes an invalid positive integer setting.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// OriginError describes an invalid origin setting.
type OriginError struct{ Name, Value string }

func (e *OriginError) Error() string { return e.Name + " is '" + e.Value + "', not an origin" }

// MissingError identifies the first absent secret.
type MissingError struct{ Name string }

func (e *MissingError) Error() string { return e.Name + " is not set" }

// Defaults returns the startup defaults.
func Defaults() Settings { return Settings{DrainSeconds: 5} }

// Drain returns the saturating duration in seconds.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}
func digits(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// IsOrigin reports whether v is a canonical HTTP origin.
func IsOrigin(v string) bool {
	defaultPort := 80
	var host string
	switch {
	case strings.HasPrefix(v, "http://"):
		host = strings.TrimPrefix(v, "http://")
	case strings.HasPrefix(v, "https://"):
		host = strings.TrimPrefix(v, "https://")
		defaultPort = 443
	default:
		return false
	}
	if h, p, ok := strings.Cut(host, ":"); ok {
		host = h
		if !digits(p) {
			return false
		}
		port, err := strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 || port == defaultPort {
			return false
		}
	}
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	for i := range len(host) {
		c := host[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// Read reads settings once each, in validation order.
func Read(lookup func(string) (string, bool)) (Settings, error) {
	s := Defaults()
	if v, ok := lookup("DRAIN_SECONDS"); ok && v != "" {
		if !digits(v) {
			return Settings{}, &Error{"DRAIN_SECONDS", v, "seconds"}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		s.DrainSeconds = n
	}
	if v, ok := lookup("IKIGENBA_CALLBACK_URL"); ok {
		if !IsOrigin(v) {
			return Settings{}, &OriginError{"IKIGENBA_CALLBACK_URL", v}
		}
		s.CallbackURL = v
	}
	return s, nil
}

// Secrets reads named secrets in order, preserving their bytes.
func Secrets(lookup func(string) (string, bool), names []string) (map[string]string, error) {
	m := make(map[string]string, len(names))
	for _, name := range names {
		v, ok := lookup(name)
		if !ok || v == "" {
			return nil, &MissingError{name}
		}
		m[name] = v
	}
	return m, nil
}
