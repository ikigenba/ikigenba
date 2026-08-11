package artifacts

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidateFilename rejects names that cannot safely represent one flat blob.
func ValidateFilename(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("filename must not be empty")
	case !utf8.ValidString(name):
		return fmt.Errorf("filename must be valid UTF-8")
	case len(name) > 128:
		return fmt.Errorf("filename must be at most 128 bytes")
	case strings.Contains(name, "/"):
		return fmt.Errorf("filename must not contain slash")
	case strings.Contains(name, `\`):
		return fmt.Errorf("filename must not contain backslash")
	case strings.ContainsRune(name, 0):
		return fmt.Errorf("filename must not contain NUL")
	case name == "." || name == "..":
		return fmt.Errorf("filename must not be dot or dot-dot")
	default:
		return nil
	}
}
