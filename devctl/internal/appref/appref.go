package appref

import (
	"fmt"
	"strings"
)

var reservedNames = map[string]struct{}{
	"host":              {},
	"deploy":            {},
	"backup-host":       {},
	"backup-services":   {},
	"renew-certificate": {},
}

// ValidName reports whether name is a usable application name.
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	if !asciiLowerOrDigit(name[0]) || !asciiLowerOrDigit(name[len(name)-1]) {
		return false
	}
	for i := 1; i < len(name)-1; i++ {
		if !asciiLowerOrDigit(name[i]) && name[i] != '-' {
			return false
		}
	}
	_, reserved := reservedNames[name]
	return !reserved
}

// ValidVersion reports whether version is a v-prefixed semantic version.
func ValidVersion(version string) bool {
	if len(version) < 2 || version[0] != 'v' {
		return false
	}

	version = version[1:]
	coreAndPrerelease, metadata, hasMetadata := strings.Cut(version, "+")
	if hasMetadata && (!validIdentifiers(metadata, false) || strings.Contains(metadata, "+")) {
		return false
	}

	core, prerelease, hasPrerelease := strings.Cut(coreAndPrerelease, "-")
	if hasPrerelease && !validIdentifiers(prerelease, true) {
		return false
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if !validDecimal(part) {
			return false
		}
	}
	return true
}

// VersionForTag extracts app's version from a checkout tag.
func VersionForTag(app, tag string) (string, bool) {
	if !ValidName(app) {
		return "", false
	}
	version, found := strings.CutPrefix(tag, app+"/")
	if !found || !ValidVersion(version) {
		return "", false
	}
	return version, true
}

// ParseFile extracts the app and version from a release archive basename.
func ParseFile(name string) (app, version string, err error) {
	const suffix = ".tar.xz"
	if strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, suffix) {
		return "", "", fmt.Errorf("invalid app archive name %q", name)
	}

	stem := strings.TrimSuffix(name, suffix)
	for i := 0; i < len(stem); i++ {
		if stem[i] != '-' {
			continue
		}
		candidateApp, candidateVersion := stem[:i], stem[i+1:]
		if !ValidName(candidateApp) || !ValidVersion(candidateVersion) {
			continue
		}
		if app != "" {
			return "", "", fmt.Errorf("ambiguous app archive name %q", name)
		}
		app, version = candidateApp, candidateVersion
	}
	if app == "" {
		return "", "", fmt.Errorf("invalid app archive name %q", name)
	}
	return app, version, nil
}

func validDecimal(value string) bool {
	if value == "" || len(value) > 1 && value[0] == '0' {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func validIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for identifier := range strings.SplitSeq(value, ".") {
		if identifier == "" {
			return false
		}
		numeric := true
		for i := range len(identifier) {
			b := identifier[i]
			if !asciiAlphaNumeric(b) && b != '-' {
				return false
			}
			if b < '0' || b > '9' {
				numeric = false
			}
		}
		if rejectNumericLeadingZero && numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func asciiLowerOrDigit(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

func asciiAlphaNumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
