// Package settings reads and validates sites' startup configuration.
package settings

import (
	"math"
	"strconv"
)

// Settings holds startup limits and the unresolved repository directory.
type Settings struct {
	DrainSeconds, SiteMaxBytes, OperationSeconds int64
	ReposDir                                     string
}

// Defaults returns the service's default configuration.
func Defaults() Settings {
	return Settings{DrainSeconds: 5, SiteMaxBytes: 268435456, OperationSeconds: 600, ReposDir: "../repos/state/repos"}
}

// Error identifies the first unacceptable numeric setting.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Read validates numeric settings and preserves REPOS_DIR verbatim.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	for _, field := range []struct {
		name, unit string
		dst        *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds},
		{"SITE_MAX_BYTES", "bytes", &s.SiteMaxBytes},
		{"OPERATION_SECONDS", "seconds", &s.OperationSeconds},
	} {
		v, set := lookup(field.name)
		if !set || v == "" {
			continue
		}
		valid := v[0] >= '1' && v[0] <= '9'
		for i := range len(v) {
			if v[i] < '0' || v[i] > '9' {
				valid = false
			}
		}
		if !valid {
			return Settings{}, &Error{Name: field.name, Value: v, Unit: field.unit}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*field.dst = n
	}
	if v, set := lookup("REPOS_DIR"); set && v != "" {
		s.ReposDir = v
	}
	return s, nil
}
