// Package apps provides service discovery, manifests and app operations.
package apps

import "fmt"

// Database describes an app's database storage.
type Database struct {
	Engine string
	Path   string
}

// Manifest describes the capabilities declared by an installed app.
type Manifest struct {
	App      string
	Port     int
	Default  bool
	Secrets  []string
	Env      map[string]string
	Database *Database
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
