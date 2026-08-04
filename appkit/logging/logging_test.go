package logging_test

import (
	"strings"
	"testing"

	"appkit/logging"
)

// R-18EC-NBCU
func TestNewULID_UsesCrockfordAlphabet(t *testing.T) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	sawZero, sawOne := false, false
	for range 500 {
		id := logging.NewULID()
		if len(id) != 26 {
			t.Fatalf("len(NewULID()) = %d for %q, want 26", len(id), id)
		}
		for _, char := range id {
			if !strings.ContainsRune(alphabet, char) {
				t.Fatalf("NewULID() = %q contains non-Crockford character %q", id, char)
			}
			sawZero = sawZero || char == '0'
			sawOne = sawOne || char == '1'
		}
	}
	if !sawZero || !sawOne {
		t.Fatalf("500 ULIDs saw digit 0 = %t, digit 1 = %t; want both", sawZero, sawOne)
	}
}
