// Package settings reads the service's startup configuration.
package settings

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Settings holds startup settings in read order.
type Settings struct {
	DrainSeconds, KeepRuns int64
	FMCSAStates            []string
	FMCSAMinPowerUnits     int64
	FMCSAStatus            string
}

// Error identifies a refused setting.
type Error struct{ Name, Value, Want string }

func (e *Error) Error() string { return e.Name + " is '" + e.Value + "', not " + e.Want }

// Configuration refusal copy.
const (
	WantSeconds    = "a positive whole number of seconds"
	WantRuns       = "a positive whole number of runs"
	WantStates     = "comma-separated two-letter uppercase state codes"
	WantPowerUnits = "a non-negative whole number of power units"
	WantStatus     = "one uppercase status letter"
)

// Defaults returns fresh default settings.
func Defaults() Settings { return Settings{5, 2, []string{"MN", "WI"}, 5, "A"} }

// Drain returns the bounded drain duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}
func number(v string, zero bool) bool {
	if v == "0" {
		return zero
	}
	if v == "" || v[0] == '0' {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func capital(v string, n int) bool {
	if len(v) != n {
		return false
	}
	for _, c := range v {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// Read validates and reads the five settings, stopping at the first refusal.
func Read(lookup func(string) (string, bool)) (Settings, error) {
	s := Defaults()
	keys := []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS"}
	wants := []string{WantSeconds, WantRuns, WantStates, WantPowerUnits, WantStatus}
	for i, k := range keys {
		v, ok := lookup(k)
		if !ok || v == "" {
			continue
		}
		valid := false
		switch i {
		case 0, 1, 3:
			valid = number(v, i == 3)
		case 2:
			valid = true
			for _, code := range strings.Split(v, ",") {
				if !capital(code, 2) {
					valid = false
				}
			}
		case 4:
			valid = capital(v, 1)
		}
		if !valid {
			return Settings{}, &Error{k, v, wants[i]}
		}
		switch i {
		case 0, 1, 3:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				n = math.MaxInt64
			}
			switch i {
			case 0:
				s.DrainSeconds = n
			case 1:
				s.KeepRuns = n
			case 3:
				s.FMCSAMinPowerUnits = n
			}
		case 2:
			s.FMCSAStates = strings.Split(v, ",")
		case 4:
			s.FMCSAStatus = v
		}
	}
	return s, nil
}
