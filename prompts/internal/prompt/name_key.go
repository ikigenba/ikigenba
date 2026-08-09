package prompt

import "strings"

// NameKey slugs a prompt name into its repository key. Runs outside the ASCII
// [a-z0-9] set collapse to one dash, ends are trimmed, and the result is capped
// at 64 bytes. fallbackID is returned when the name contains no alphanumerics.
func NameKey(name, fallbackID string) string {
	var key strings.Builder
	key.Grow(min(len(name), 64))
	pendingDash := false
	for _, r := range name {
		var c byte
		switch {
		case r >= 'a' && r <= 'z':
			c = byte(r)
		case r >= 'A' && r <= 'Z':
			c = byte(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			c = byte(r)
		default:
			if key.Len() > 0 {
				pendingDash = true
			}
			continue
		}
		if pendingDash && key.Len() < 64 {
			key.WriteByte('-')
		}
		pendingDash = false
		if key.Len() < 64 {
			key.WriteByte(c)
		}
		if key.Len() == 64 {
			break
		}
	}
	result := strings.TrimRight(key.String(), "-")
	if result == "" {
		return fallbackID
	}
	return result
}
