package page

import "testing"

func TestCursorCodecRoundTripsPartsAndRejectsMalformedToken(t *testing.T) {
	// R-1C7R-ES1A
	tests := [][]string{
		{"alpha", "subject-1"},
		{"", "contains:colon", "unicode 東京"},
		{"line\nbreak", "slash/and space"},
	}
	for _, parts := range tests {
		token := EncodeCursor(parts...)
		got, ok := DecodeCursor(token)
		if !ok {
			t.Fatalf("DecodeCursor(%q) ok = false, want true", token)
		}
		if len(got) != len(parts) {
			t.Fatalf("DecodeCursor(%q) len = %d, want %d", token, len(got), len(parts))
		}
		for i := range parts {
			if got[i] != parts[i] {
				t.Fatalf("DecodeCursor(%q)[%d] = %q, want %q", token, i, got[i], parts[i])
			}
		}
	}

	for _, token := range []string{"not base64!", EncodeCursor("alpha") + "=", "MTA6c2hvcnQ"} {
		if parts, ok := DecodeCursor(token); ok {
			t.Fatalf("DecodeCursor(%q) = %v, true; want malformed", token, parts)
		}
	}
}

func TestParamsResolvedLimitClampsToContract(t *testing.T) {
	// R-19RY-N8JW
	tests := map[string]struct {
		in   int
		want int
	}{
		"default":  {in: 0, want: DefaultLimit},
		"one":      {in: 1, want: 1},
		"negative": {in: -7, want: 1},
		"max":      {in: MaxLimit, want: MaxLimit},
		"over max": {in: MaxLimit + 1, want: MaxLimit},
	}
	for name, tt := range tests {
		if got := (Params{Limit: tt.in}).ResolvedLimit(); got != tt.want {
			t.Fatalf("%s: ResolvedLimit(%d) = %d, want %d", name, tt.in, got, tt.want)
		}
	}
}
