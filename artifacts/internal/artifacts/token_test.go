package artifacts

import (
	"regexp"
	"testing"
)

// R-3EFP-FCGJ
func TestNewTokenHasRandomLowercaseBase32Shape(t *testing.T) {
	first, second := NewToken(), NewToken()
	valid := regexp.MustCompile(`^[a-z2-7]{30}$`)
	if !valid.MatchString(first) || !valid.MatchString(second) {
		t.Fatalf("tokens %q and %q do not have the required shape", first, second)
	}
	if first == second {
		t.Fatalf("two generated tokens are identical: %q", first)
	}
}
