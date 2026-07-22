package sites

import (
	"regexp"
	"testing"
)

func TestNewTokenIsRandomValidBase32Slug(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-z2-7]{30}$`)
	first := NewToken()
	second := NewToken()
	// R-H7WX-8DDO
	if !pattern.MatchString(first) || ValidateSlug(first) != nil {
		t.Fatalf("NewToken() = %q, want valid 30-character base32 slug", first)
	}
	if first == second {
		t.Fatalf("successive NewToken calls both returned %q", first)
	}
	positions := make([]map[byte]bool, 30)
	for i := range positions {
		positions[i] = map[byte]bool{}
	}
	for i := 0; i < 200; i++ {
		token := NewToken()
		if !pattern.MatchString(token) || ValidateSlug(token) != nil {
			t.Fatalf("NewToken() = %q, want valid 30-character base32 slug", token)
		}
		for position := range token {
			positions[position][token[position]] = true
		}
	}
	for position, values := range positions {
		if len(values) <= 1 {
			t.Fatalf("position %d only produced %v over 200 tokens", position, values)
		}
	}
}
