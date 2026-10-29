// Package settings reads the service's runtime settings.
package settings

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
)

// WantSeconds describes accepted drain values.
const WantSeconds string = "a positive whole number of seconds"

// Settings holds startup configuration.
type Settings struct {
	DrainSeconds int64
	HostCommand  string
	Sandbox      bool
}

// Error identifies an invalid setting.
type Error struct{ Name, Value, Want string }

func (e *Error) Error() string { return e.Name + " is '" + e.Value + "', not " + e.Want }

// Defaults returns the settings used when variables are absent or empty.
func Defaults() Settings { return Settings{5, "", false} }

// Read validates the drain setting and reads the host selection.
func Read(lookup func(key string) (string, bool)) (Settings, error) {
	s := Defaults()
	if v, ok := lookup("DRAIN_SECONDS"); ok && v != "" {
		valid := v[0] >= '1' && v[0] <= '9'
		for _, b := range []byte(v) {
			if b < '0' || b > '9' {
				valid = false
			}
		}
		if !valid {
			return Settings{}, &Error{"DRAIN_SECONDS", v, WantSeconds}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			n = math.MaxInt64
		}
		s.DrainSeconds = n
	}
	if v, ok := lookup("ACCOUNTS_HOST_COMMAND"); ok {
		s.HostCommand = v
	}
	_, s.Sandbox = lookup("IKIGENBA_SANDBOX")
	return s, nil
}

// Drain converts seconds to a saturating duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}

// HostFields selects the host command, or nil for a new in-memory host.
func (s Settings) HostFields() []string {
	if strings.TrimSpace(s.HostCommand) == "" && s.Sandbox {
		return nil
	}
	return host.Fields(s.HostCommand)
}
