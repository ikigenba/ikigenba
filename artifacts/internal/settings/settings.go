// Package settings reads the service's positive integer settings.
package settings

import (
	"math"
	"strconv"
	"time"
)

// Settings holds the startup limits.
type Settings struct{ DrainSeconds, ArtifactMaxBytes, ImportSeconds int64 }

// Error identifies an invalid setting.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Defaults returns the limits used for absent values.
func Defaults() Settings { return Settings{5, 1073741824, 600} }

// Read validates each value in startup order.
func Read(lookup func(string) (string, bool)) (Settings, error) {
	s := Defaults()
	for _, item := range []struct {
		name, unit string
		target     *int64
	}{{"DRAIN_SECONDS", "seconds", &s.DrainSeconds}, {"ARTIFACT_MAX_BYTES", "bytes", &s.ArtifactMaxBytes}, {"IMPORT_SECONDS", "seconds", &s.ImportSeconds}} {
		value, ok := lookup(item.name)
		if !ok || value == "" {
			continue
		}
		valid := value[0] != '0'
		for _, c := range []byte(value) {
			if c < '0' || c > '9' {
				valid = false
			}
		}
		if !valid {
			return Settings{}, &Error{item.name, value, item.unit}
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*item.target = n
	}
	return s, nil
}
func duration(seconds int64) time.Duration {
	if seconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds) * time.Second
}

// Drain returns the bounded shutdown duration.
func (s Settings) Drain() time.Duration { return duration(s.DrainSeconds) }

// Import returns the bounded import duration.
func (s Settings) Import() time.Duration { return duration(s.ImportSeconds) }
