package artifacts

import (
	"strings"
	"testing"
)

// R-3FNL-T478
func TestValidateFilenameUsesByteLengthAndRejectsUnsafeNames(t *testing.T) {
	valid128 := strings.Repeat("é", 64)
	if err := ValidateFilename(valid128); err != nil {
		t.Fatalf("128-byte UTF-8 filename rejected: %v", err)
	}

	cases := []struct {
		name string
		want string
	}{
		{strings.Repeat("x", 129), "128 bytes"},
		{"", "empty"},
		{string([]byte{0xff}), "UTF-8"},
		{"directory/file", "slash"},
		{`directory\file`, "backslash"},
		{"nul\x00byte", "NUL"},
		{".", "dot"},
		{"..", "dot"},
		{strings.Repeat("é", 65), "128 bytes"},
	}
	for _, tc := range cases {
		original := tc.name
		err := ValidateFilename(tc.name)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateFilename(%q) error = %v, want rule containing %q", tc.name, err, tc.want)
		}
		if tc.name != original {
			t.Errorf("ValidateFilename altered %q to %q", original, tc.name)
		}
	}
}
