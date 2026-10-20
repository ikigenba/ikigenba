// Package settings reads the service's runtime settings.
package settings

import (
	"math"
	"net/url"
	"strconv"
	"time"
)

// WantSeconds describes accepted drain values.
const WantSeconds string = "a positive whole number of seconds"

// WantURL describes accepted address values.
const WantURL string = "an absolute http or https URL with a host and printable ASCII without spaces"

// Settings holds the service's startup configuration.
type Settings struct {
	DrainSeconds                   int64
	DefaultDestination, LinkPrefix string
}

// Error identifies the first invalid setting.
type Error struct{ Name, Value, Want string }

func (e *Error) Error() string { return e.Name + " is '" + e.Value + "', not " + e.Want }

// Defaults returns the settings used when variables are absent or empty.
func Defaults() Settings {
	return Settings{5, "https://michaelgreenly.dev/", "https://michaelgreenly.dev/c/?t="}
}

// Read validates settings in their declared order.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	if v, ok := lookup("DRAIN_SECONDS"); ok && v != "" {
		valid := v[0] >= '1' && v[0] <= '9'
		for _, b := range []byte(v) {
			if b < '0' || b > '9' {
				valid = false
			}
		}
		if !valid {
			return Settings{}, &Error{"DRAIN_SECONDS", v, WantSeconds}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		s.DrainSeconds = n
	}
	for _, item := range []struct {
		name  string
		field *string
	}{
		{"CAMPAIGNS_DEFAULT_DESTINATION", &s.DefaultDestination},
		{"CAMPAIGNS_LINK_PREFIX", &s.LinkPrefix},
	} {
		v, ok := lookup(item.name)
		if !ok || v == "" {
			continue
		}
		if !validURL(v) {
			return Settings{}, &Error{item.name, v, WantURL}
		}
		*item.field = v
	}
	return s, nil
}
func validURL(v string) bool {
	for _, b := range []byte(v) {
		if b < 0x21 || b > 0x7e {
			return false
		}
	}
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

// Drain converts seconds to a saturating duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}
