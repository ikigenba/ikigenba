package quote

import "testing"

func TestArgAndField(t *testing.T) {
	// R-E3EZ-SOD5 R-E4MW-6G3U R-XGG4-ZUCK R-XHO1-DM39
	for _, tc := range []struct {
		in, arg, field string
	}{
		{"plain", "plain", "plain"},
		{"it's\\\n\t\r", "it\\'s\\\\\\n\\t\\r", "it's\\\\\\n\\t\\r"},
		{string([]byte{0, 0x1b, 0x7f, 0xff}), `\x00\x1b\x7f\xff`, `\x00\x1b\x7f\xff`},
		{"é\u202e\U000e0001", "é\\u202e\\U000e0001", "é\\u202e\\U000e0001"},
		{string([]byte{0xe2, 0x82}), `\xe2\x82`, `\xe2\x82`},
	} {
		if got := Arg(tc.in); got != tc.arg {
			t.Errorf("Arg(%q) = %q, want %q", tc.in, got, tc.arg)
		}
		if got := Field(tc.in); got != tc.field {
			t.Errorf("Field(%q) = %q, want %q", tc.in, got, tc.field)
		}
	}
}
