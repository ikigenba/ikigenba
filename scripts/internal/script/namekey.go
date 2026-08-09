package script

import (
	"context"
	"fmt"
	"strings"
)

const maxNameKeyBytes = 48

// slugify derives the human-readable portion of a script repository key.
func slugify(name, scriptID string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	b.Grow(min(len(lower), maxNameKeyBytes))
	dash := false
	for _, r := range lower {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 && b.Len() < maxNameKeyBytes {
				b.WriteByte('-')
			}
			if b.Len() < maxNameKeyBytes {
				b.WriteByte(byte(r))
			}
			dash = false
			continue
		}
		dash = true
	}
	key := strings.Trim(b.String(), "-")
	if key == "" {
		return "script-" + strings.ToLower(scriptID)
	}
	return key
}

// deriveNameKey finds the first service-wide free key. excludeID allows an
// existing script to retain its own key while its metadata is updated.
func (s *Store) deriveNameKey(ctx context.Context, excludeID, name string) (string, error) {
	base := slugify(name, excludeID)
	for n := 1; ; n++ {
		candidate := base
		if n > 1 {
			suffix := fmt.Sprintf("-%d", n)
			stem := strings.TrimRight(base[:min(len(base), maxNameKeyBytes-len(suffix))], "-")
			candidate = stem + suffix
		}
		var count int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM scripts WHERE name_key = ? AND id <> ?`,
			candidate, excludeID,
		).Scan(&count); err != nil {
			return "", fmt.Errorf("script: derive name key: %w", err)
		}
		if count == 0 {
			return candidate, nil
		}
	}
}
