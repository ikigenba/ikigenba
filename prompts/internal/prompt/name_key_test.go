package prompt

import (
	"strings"
	"testing"
)

func TestNameKeySlugsCapsAndFallsBack(t *testing.T) {
	// R-RDFP-IOO1
	long := strings.Repeat("a", 63) + "!" + strings.Repeat("b", 136)
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{"words", "Daily Digest", "id-1", "daily-digest"},
		{"collapse and trim", "  bills/AWS — 2026!  ", "id-2", "bills-aws-2026"},
		{"cap and retrim", long, "id-3", strings.Repeat("a", 63)},
		{"empty slug", "—— ——", "01K123FALLBACK", "01K123FALLBACK"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NameKey(test.input, test.fallback)
			if got != test.want {
				t.Fatalf("NameKey(%q, %q) = %q, want %q", test.input, test.fallback, got, test.want)
			}
			if len(got) > 64 || strings.HasSuffix(got, "-") {
				t.Fatalf("NameKey(%q) = %q (%d bytes), want at most 64 bytes and no trailing dash", test.input, got, len(got))
			}
		})
	}
}
