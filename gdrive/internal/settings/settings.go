// Package settings reads the process's positive integer limits.
package settings

import (
	"math"
	"strconv"
	"time"
)

// Settings holds the process limits.
type Settings struct{ DrainSeconds, TransferMaxBytes, TransferMaxActive, TransferMaxQueued, TransferKeepDays, TransferKeepCount int64 }

// Error identifies a refused environment value.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Defaults returns the manifest defaults and the space drain default.
func Defaults() Settings { return Settings{5, 1073741824, 2, 10, 15, 100} }

// Drain returns the saturated drain duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}

// Read reads and validates limits in their declared order.
func Read(lookup func(string) (string, bool)) (Settings, error) {
	s := Defaults()
	fields := []struct {
		name, unit string
		value      *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds}, {"TRANSFER_MAX_BYTES", "bytes", &s.TransferMaxBytes}, {"TRANSFER_MAX_ACTIVE", "transfers", &s.TransferMaxActive}, {"TRANSFER_MAX_QUEUED", "transfers", &s.TransferMaxQueued}, {"TRANSFER_KEEP_DAYS", "days", &s.TransferKeepDays}, {"TRANSFER_KEEP_COUNT", "transfers", &s.TransferKeepCount},
	}
	for _, f := range fields {
		v, ok := lookup(f.name)
		if !ok || v == "" {
			continue
		}
		valid := v[0] != '0'
		for _, c := range []byte(v) {
			if c < '0' || c > '9' {
				valid = false
			}
		}
		if !valid {
			return Settings{}, &Error{f.name, v, f.unit}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*f.value = n
	}
	return s, nil
}
