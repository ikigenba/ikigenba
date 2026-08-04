package telemetry

import "time"

// Clock is the source of wall-clock time for the telemetry domain.
type Clock interface {
	Now() time.Time
}

// RealClock reads the process wall clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time {
	return time.Now()
}
