// Package settings reads the space's drain deadline and the retention window.
package settings

import (
	"math"
	"strconv"
	"time"
)

// Settings holds the drain deadline in seconds and the retention window in days.
type Settings struct{ DrainSeconds, RetentionDays int64 }

// Error describes a value that is not a positive whole number.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Defaults returns the values used when the environment supplies none.
func Defaults() Settings { return Settings{DrainSeconds: 5, RetentionDays: 2} }

// Read reads DRAIN_SECONDS and WEBHOOKS_RETENTION_DAYS through lookup, in that order.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	for _, v := range []struct {
		name, unit string
		field      *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds},
		{"WEBHOOKS_RETENTION_DAYS", "days", &s.RetentionDays},
	} {
		value, present := lookup(v.name)
		if !present || value == "" {
			continue
		}
		valid := value[0] >= '1' && value[0] <= '9'
		for i := 1; i < len(value); i++ {
			valid = valid && value[i] >= '0' && value[i] <= '9'
		}
		if !valid {
			return Settings{}, &Error{Name: v.name, Value: value, Unit: v.unit}
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*v.field = n
	}
	return s, nil
}

func duration(value int64, unit time.Duration) time.Duration {
	if value > math.MaxInt64/int64(unit) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(value) * unit
}

// Drain returns the drain deadline, saturating at the largest duration.
func (s Settings) Drain() time.Duration { return duration(s.DrainSeconds, time.Second) }

// Retention returns the retention window in 24-hour days, saturating at the largest duration.
func (s Settings) Retention() time.Duration { return duration(s.RetentionDays, 24*time.Hour) }
