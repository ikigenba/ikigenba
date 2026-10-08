// Package settings reads the space's drain deadline.
package settings

import (
	"math"
	"strconv"
)

// Settings holds the space's drain deadline in seconds.
type Settings struct{ DrainSeconds int64 }

// Error describes a value that is not a positive whole number.
type Error struct{ Name, Value, Unit string }

func (e *Error) Error() string {
	return e.Name + " is '" + e.Value + "', not a positive whole number of " + e.Unit
}

// Defaults returns the deadline used when the space supplies none.
func Defaults() Settings { return Settings{DrainSeconds: 5} }

// Read reads DRAIN_SECONDS through lookup.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	value, present := lookup("DRAIN_SECONDS")
	if !present || value == "" {
		return Defaults(), nil
	}
	valid := value[0] >= '1' && value[0] <= '9'
	for i := 1; i < len(value); i++ {
		valid = valid && value[i] >= '0' && value[i] <= '9'
	}
	if !valid {
		return Settings{}, &Error{Name: "DRAIN_SECONDS", Value: value, Unit: "seconds"}
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		seconds = math.MaxInt64
	}
	return Settings{DrainSeconds: seconds}, nil
}
