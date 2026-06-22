// Package page provides cursor-pagination primitives shared by wiki stores.
package page

import (
	"encoding/base64"
	"strconv"
	"strings"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Params is a cursor-pagination request.
type Params struct {
	Limit  int
	Cursor string
}

// ResolvedLimit returns Limit clamped to [1, MaxLimit], with zero using DefaultLimit.
func (p Params) ResolvedLimit() int {
	switch {
	case p.Limit == 0:
		return DefaultLimit
	case p.Limit < 0:
		return 1
	case p.Limit > MaxLimit:
		return MaxLimit
	default:
		return p.Limit
	}
}

// EncodeCursor packs ordered key components into one opaque token.
func EncodeCursor(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(strconv.Itoa(len(part)))
		b.WriteByte(':')
		b.WriteString(part)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(b.String()))
}

// DecodeCursor reverses EncodeCursor; ok=false on a malformed token.
func DecodeCursor(token string) (parts []string, ok bool) {
	if token == "" {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, false
	}
	s := string(raw)
	for len(s) > 0 {
		i := strings.IndexByte(s, ':')
		if i <= 0 {
			return nil, false
		}
		n, err := strconv.Atoi(s[:i])
		if err != nil || n < 0 {
			return nil, false
		}
		s = s[i+1:]
		if len(s) < n {
			return nil, false
		}
		parts = append(parts, s[:n])
		s = s[n:]
	}
	return parts, true
}
