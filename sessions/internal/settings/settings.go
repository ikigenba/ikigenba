// Package settings reads the service's process settings.
package settings

import (
	"math"
	"strconv"
	"time"
)

// WantSeconds describes the accepted drain setting.
const WantSeconds = "a positive decimal integer of seconds"

// Settings holds the settings read at process start.
type Settings struct {
	DrainSeconds int64
}

// Error identifies a refused setting and the expected form.
type Error struct {
	Name, Value, Want string
}

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not " + e.Want
}

// Defaults returns the settings used when no values are supplied.
func Defaults() Settings { return Settings{DrainSeconds: 5} }

// Read reads the drain setting through lookup.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	v, ok := lookup("DRAIN_SECONDS")
	if !ok || v == "" {
		return s, nil
	}
	valid := v[0] >= '1' && v[0] <= '9'
	for i := 1; i < len(v); i++ {
		valid = valid && v[i] >= '0' && v[i] <= '9'
	}
	if !valid {
		return Settings{}, &Error{Name: "DRAIN_SECONDS", Value: v, Want: WantSeconds}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		n = math.MaxInt64
	}
	s.DrainSeconds = n
	return s, nil
}

// Drain returns the drain interval, saturating at the duration limit.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}
