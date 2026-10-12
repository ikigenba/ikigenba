// Package settings reads the connector's positive integer configuration.
package settings

import (
	"math"
	"strconv"
	"time"
)

// Settings holds the run's configuration in whole units.
type Settings struct {
	DrainSeconds, TransferMaxBytes, TransferMaxActive, TransferMaxQueued int64
	TransferKeepDays, TransferKeepCount, DraftKeepDays, DraftKeepCount   int64
}

// Defaults returns the settings used for unset and empty variables.
func Defaults() Settings {
	return Settings{5, 52428800, 2, 10, 15, 100, 15, 100}
}

// Error identifies the first invalid variable.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Read reads the eight settings in declaration order, saturating large values.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	variables := []struct {
		name, unit string
		field      *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds},
		{"TRANSFER_MAX_BYTES", "bytes", &s.TransferMaxBytes},
		{"TRANSFER_MAX_ACTIVE", "transfers", &s.TransferMaxActive},
		{"TRANSFER_MAX_QUEUED", "transfers", &s.TransferMaxQueued},
		{"TRANSFER_KEEP_DAYS", "days", &s.TransferKeepDays},
		{"TRANSFER_KEEP_COUNT", "transfers", &s.TransferKeepCount},
		{"DRAFT_KEEP_DAYS", "days", &s.DraftKeepDays},
		{"DRAFT_KEEP_COUNT", "drafts", &s.DraftKeepCount},
	}
	for _, v := range variables {
		value, present := lookup(v.name)
		if !present || value == "" {
			continue
		}
		valid := value[0] >= '1' && value[0] <= '9'
		for i := 1; i < len(value); i++ {
			valid = valid && value[i] >= '0' && value[i] <= '9'
		}
		if !valid {
			return s, &Error{Name: v.name, Value: value, Unit: v.unit}
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		*v.field = n
	}
	return s, nil
}

// Drain returns the drain window, saturated at the largest duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}
