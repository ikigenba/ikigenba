package valid

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrPath indicates a malformed or oversized backup restore path.
var ErrPath = errors.New("invalid restore path")

// Restore path bounds include the quotes and escaping in the JSON encoding.
const (
	MaxRestorePath     = 512
	MaxDatabasePath    = 256
	MaxRestoresPerHost = 10
)

// RestorePath checks a relative path or the empty request for a whole folder.
func RestorePath(p string) error {
	if p == "" {
		return nil
	}
	return validatePath(p, MaxRestorePath)
}

// DatabasePath checks a path under the account's data folder.
func DatabasePath(p string) error {
	first, _, _ := strings.Cut(p, "/")
	if first != "data" {
		return ErrPath
	}
	return validatePath(p, MaxDatabasePath)
}

func validatePath(p string, limit int) error {
	if len(p) > limit-2 || !utf8.ValidString(p) {
		return ErrPath
	}
	for i := range len(p) {
		if p[i] < 0x20 || p[i] == 0x7f {
			return ErrPath
		}
	}
	for part := range strings.SplitSeq(p, "/") {
		if part == "" || part == "." || part == ".." {
			return ErrPath
		}
	}
	encoded, _ := json.Marshal(p)
	if len(encoded) > limit {
		return ErrPath
	}
	return nil
}
