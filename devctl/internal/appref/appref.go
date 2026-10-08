package appref

var reservedNames = map[string]struct{}{
	"host":              {},
	"deploy":            {},
	"snapshots":         {},
	"seed":              {},
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

func asciiLowerOrDigit(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}
