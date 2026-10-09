package tools_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

// R-U5QU-5TS6
func TestRefusalCopyConstants(t *testing.T) {
	const unreachable string = tools.Unreachable
	const invalidName string = tools.InvalidName
	const nameTaken string = tools.NameTaken
	const noRepository string = tools.NoRepository
	const busy string = tools.Busy
	for _, value := range []string{unreachable, invalidName, nameTaken, noRepository, busy} {
		if value == "" || strings.Contains(value, "\n") {
			t.Fatalf("invalid refusal copy constant: %q", value)
		}
	}
	for _, format := range []string{nameTaken, noRepository, busy} {
		if strings.Count(format, "%s") != 1 || strings.Contains(strings.ReplaceAll(format, "%s", ""), "%") {
			t.Fatalf("refusal format must have exactly one string verb: %q", format)
		}
	}
}
