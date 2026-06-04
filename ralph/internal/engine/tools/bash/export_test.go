package bash

import "time"

// SetTimeoutForTest overrides bashTimeout for the duration of a test
// and returns a restore function. Test-only; not exported in the
// non-test build.
func SetTimeoutForTest(d time.Duration) func() {
	prev := bashTimeout
	bashTimeout = d
	return func() { bashTimeout = prev }
}
