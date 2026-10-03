// Package settings reads repos' positive whole-number environment settings.
package settings

import "math"

// Settings holds the limits and drain interval in the units their names state.
type Settings struct {
	DrainSeconds, ReadSlots, WriteSlots, QueueLength, QueueSeconds int64
	OperationSeconds, PushMaxBytes, RepoMaxBytes, MaintenanceHours int64
}

// Defaults returns the settings used for an unset or empty variable.
func Defaults() Settings {
	return Settings{
		DrainSeconds: 5, ReadSlots: 8, WriteSlots: 2, QueueLength: 16,
		QueueSeconds: 30, OperationSeconds: 600, PushMaxBytes: 104857600,
		RepoMaxBytes: 1073741824, MaintenanceHours: 24,
	}
}

// Error identifies an invalid setting and preserves its supplied value.
type Error struct {
	Name, Value, Unit string
}

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Read reads all nine variables and reports the first invalid one.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	fields := []struct {
		name, unit string
		value      *int64
	}{
		{"DRAIN_SECONDS", "seconds", &s.DrainSeconds},
		{"READ_SLOTS", "operations", &s.ReadSlots},
		{"WRITE_SLOTS", "operations", &s.WriteSlots},
		{"QUEUE_LENGTH", "operations", &s.QueueLength},
		{"QUEUE_SECONDS", "seconds", &s.QueueSeconds},
		{"OPERATION_SECONDS", "seconds", &s.OperationSeconds},
		{"PUSH_MAX_BYTES", "bytes", &s.PushMaxBytes},
		{"REPO_MAX_BYTES", "bytes", &s.RepoMaxBytes},
		{"MAINTENANCE_HOURS", "hours", &s.MaintenanceHours},
	}
	var first error
	for _, field := range fields {
		v, set := lookup(field.name)
		if !set || v == "" {
			continue
		}
		n, valid := positive(v)
		if !valid {
			if first == nil {
				first = &Error{Name: field.name, Value: v, Unit: field.unit}
			}
			continue
		}
		*field.value = n
	}
	return s, first
}

func positive(v string) (int64, bool) {
	if v[0] < '1' || v[0] > '9' {
		return 0, false
	}
	var n int64
	for i := range len(v) {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
		d := int64(v[i] - '0')
		if n > (math.MaxInt64-d)/10 {
			n = math.MaxInt64
		} else {
			n = n*10 + d
		}
	}
	return n, true
}
