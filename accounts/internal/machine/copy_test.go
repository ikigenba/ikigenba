package machine_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

func TestRedactedCopy(t *testing.T) {
	// R-MHHY-URV4
	const text string = machine.Redacted
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
	if len(text) == 0 || strings.ContainsAny(text, "\r\n") ||
		strings.IndexByte(alphabet, text[0]) >= 0 || strings.IndexByte(alphabet, text[len(text)-1]) >= 0 {
		t.Fatalf("credential redaction marker has invalid bytes: %q", text)
	}
}
