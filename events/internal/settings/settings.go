// Package settings reads the event bus's startup limits.
package settings

import (
	"math"
	"strconv"
	"time"
)

// Settings holds limits in the units named by its fields.
type Settings struct {
	DrainSeconds, DepthMax, DeliveryTimeoutSeconds, DeliveryAttempts int64
	InflightMax, RetentionDays, DeclarationsSeconds                  int64
}

// Error identifies the first invalid environment value.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Defaults returns the startup limits used for unset and empty variables.
func Defaults() Settings {
	return Settings{DrainSeconds: 5, DepthMax: 8, DeliveryTimeoutSeconds: 5,
		DeliveryAttempts: 10, InflightMax: 4, RetentionDays: 2, DeclarationsSeconds: 60}
}

// Read looks up each setting in diagnostic order and rejects invalid values.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	variables := []struct {
		name, unit string
		field      *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds},
		{"EVENTS_DEPTH_MAX", "levels", &s.DepthMax},
		{"EVENTS_DELIVERY_TIMEOUT_SECONDS", "seconds", &s.DeliveryTimeoutSeconds},
		{"EVENTS_DELIVERY_ATTEMPTS", "attempts", &s.DeliveryAttempts},
		{"EVENTS_INFLIGHT_MAX", "deliveries", &s.InflightMax},
		{"EVENTS_RETENTION_DAYS", "days", &s.RetentionDays},
		{"EVENTS_DECLARATIONS_SECONDS", "seconds", &s.DeclarationsSeconds},
	}
	for _, variable := range variables {
		value, set := lookup(variable.name)
		if !set || value == "" {
			continue
		}
		if !positiveDigits(value) {
			return Settings{}, &Error{Name: variable.name, Value: value, Unit: variable.unit}
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*variable.field = n
	}
	return s, nil
}

func positiveDigits(value string) bool {
	if value[0] < '1' || value[0] > '9' {
		return false
	}
	for i := 1; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func duration(value int64, unit time.Duration) time.Duration {
	if value > math.MaxInt64/int64(unit) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(value) * unit
}

// Drain returns the drain timeout, saturating at the maximum duration.
func (s Settings) Drain() time.Duration { return duration(s.DrainSeconds, time.Second) }

// DeliveryTimeout returns one delivery attempt's timeout.
func (s Settings) DeliveryTimeout() time.Duration {
	return duration(s.DeliveryTimeoutSeconds, time.Second)
}

// Retention returns the retained window in 24-hour days.
func (s Settings) Retention() time.Duration { return duration(s.RetentionDays, 24*time.Hour) }

// DeclarationsInterval returns the interval between declarations refreshes.
func (s Settings) DeclarationsInterval() time.Duration {
	return duration(s.DeclarationsSeconds, time.Second)
}
