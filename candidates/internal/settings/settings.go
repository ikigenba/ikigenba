// Package settings reads the service's bounded runtime settings.
package settings

import (
	"math"
	"strconv"
	"time"
)

// WantSeconds is the explanation used for invalid drain settings.
const WantSeconds string = "a positive whole number of seconds"

// WantPasses is the explanation used for invalid retention settings.
const WantPasses string = "a positive whole number of passes"

// Settings holds accepted drain and retention values.
type Settings struct {
	DrainSeconds, KeepPasses int64
	RouteOrigin, MapsAPIKey  string
}

// Error identifies the first rejected setting.
type Error struct{ Name, Value, Want string }

func (e *Error) Error() string { return e.Name + " is '" + e.Value + "', not " + e.Want }

// DefaultRouteOrigin is the address used when no route origin is configured.
const DefaultRouteOrigin = "Sandstone, MN"

// Defaults returns the settings used for absent values.
func Defaults() Settings { return Settings{5, 20, DefaultRouteOrigin, ""} }

// Read validates the numeric settings through the supplied environment lookup.
func Read(lookup func(string) (string, bool)) (Settings, error) {
	s := Defaults()
	for _, item := range []struct {
		name, want string
		value      *int64
	}{{"DRAIN_SECONDS", WantSeconds, &s.DrainSeconds}, {"CANDIDATES_KEEP_PASSES", WantPasses, &s.KeepPasses}} {
		v, ok := lookup(item.name)
		if !ok || v == "" {
			continue
		}
		valid := v[0] >= '1' && v[0] <= '9'
		for _, b := range []byte(v) {
			if b < '0' || b > '9' {
				valid = false
			}
		}
		if !valid {
			return Settings{}, &Error{item.name, v, item.want}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*item.value = n
	}
	if origin, ok := lookup("CANDIDATES_ROUTE_ORIGIN"); ok && origin != "" {
		s.RouteOrigin = origin
	}
	if key, ok := lookup("GOOGLE_MAPS_API_KEY"); ok {
		s.MapsAPIKey = key
	}
	return s, nil
}

// Drain converts seconds to a saturating duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}
