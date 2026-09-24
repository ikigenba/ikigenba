// Package apps provides service discovery, manifests and app operations.
package apps

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

// Database describes an app's database storage.
type Database struct {
	Engine string
	Path   string
}

// Manifest describes the capabilities declared by an installed app.
type Manifest struct {
	App      string
	Default  bool
	Secrets  []string
	Env      map[string]string
	Database *Database
}

// Timeouts are the space-wide drain and systemd stop periods in seconds.
type Timeouts struct {
	DrainSeconds int64
	StopSeconds  int64
}

// ReadTimeouts reads and validates the space-wide app timing settings.
func ReadTimeouts(store config.Store) (Timeouts, error) {
	drain, err := readSeconds(store, "apps.drain_seconds", 5)
	if err != nil {
		return Timeouts{}, err
	}
	stop, err := readSeconds(store, "apps.stop_seconds", 10)
	if err != nil {
		return Timeouts{}, err
	}
	if stop <= drain {
		return Timeouts{}, fmt.Errorf("apps.stop_seconds (%d) is not greater than apps.drain_seconds (%d)", stop, drain)
	}
	return Timeouts{DrainSeconds: drain, StopSeconds: stop}, nil
}

func readSeconds(store config.Store, key string, fallback int64) (int64, error) {
	value, err := store.Get(key)
	if errors.Is(err, config.ErrNotSet) {
		return fallback, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", key, err)
	}
	if value == "" {
		return fallback, nil
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return 0, invalidSeconds(key, value)
		}
	}
	trimmed := strings.TrimLeft(value, "0")
	if trimmed == "" {
		return 0, invalidSeconds(key, value)
	}
	seconds, parseErr := strconv.ParseInt(trimmed, 10, 64)
	if parseErr != nil || seconds < 1 || seconds > 9223372036 {
		return 0, invalidSeconds(key, value)
	}
	return seconds, nil
}

func invalidSeconds(key, value string) error {
	return fmt.Errorf("%s is not a positive whole number of seconds: '%s'", key, value)
}

// Service describes an app discovered on the host.
type Service struct {
	Name          string
	Manifest      *Manifest
	ManifestError error
}

var reservedNames = map[string]struct{}{
	"host":              {},
	"deploy":            {},
	"backup-host":       {},
	"backup-services":   {},
	"renew-certificate": {},
}

type unusableAppNameError struct {
	name string
}

func (failure *unusableAppNameError) Error() string {
	return fmt.Sprintf("unusable app name %q", failure.name)
}

// ValidateName reports whether name can safely identify an app.
func ValidateName(name string) error {
	if len(name) == 0 || len(name) > 63 || !isASCIIAlphanumeric(name[0]) || !isASCIIAlphanumeric(name[len(name)-1]) {
		return &unusableAppNameError{name: name}
	}

	lower := make([]byte, len(name))
	for i := range len(name) {
		character := name[i]
		if !isASCIIAlphanumeric(character) && character != '-' {
			return &unusableAppNameError{name: name}
		}
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		lower[i] = character
	}
	if _, reserved := reservedNames[string(lower)]; reserved {
		return &unusableAppNameError{name: name}
	}

	return nil
}

func isASCIIAlphanumeric(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}
