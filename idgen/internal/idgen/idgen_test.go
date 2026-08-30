package idgen

import (
	"errors"
	"testing"
	"time"
)

func TestValidPrefix(t *testing.T) {
	// R-5ZQU-BTSZ
	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{name: "uppercase letter", prefix: "R", want: true},
		{name: "lowercase letters", prefix: "requirement", want: true},
		{name: "digits", prefix: "0123456789", want: true},
		{name: "mixed alphanumeric", prefix: "AbC123z", want: true},
		{name: "empty", prefix: "", want: false},
		{name: "space", prefix: "A B", want: false},
		{name: "tab", prefix: "A\tB", want: false},
		{name: "separator", prefix: "A-B", want: false},
		{name: "punctuation", prefix: "A_B", want: false},
		{name: "non-ASCII letter", prefix: "Spéc", want: false},
		{name: "non-ASCII digit", prefix: "A１", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidPrefix(test.prefix); got != test.want {
				t.Fatalf("ValidPrefix(%q) = %t, want %t", test.prefix, got, test.want)
			}
		})
	}
}

func TestMintAtEpochGoldenVector(t *testing.T) {
	// R-SKFL-OD3U
	want := "R-" + "0007-J3LA"
	if got := MintAt("R", Epoch); got != want {
		t.Fatalf("MintAt(R, Epoch) = %q, want %q", got, want)
	}
}

func TestMintAtAbsoluteInstantGoldenVector(t *testing.T) {
	// R-SLNI-24UJ
	instant := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	want := "R-" + "OBCA-0VLA"
	if got := MintAt("R", instant); got != want {
		t.Fatalf("MintAt(R, instant) = %q, want %q", got, want)
	}
}

func TestMintAtZeroPadsBody(t *testing.T) {
	// R-SMVE-FWL8
	instant := Epoch.Add(1143449080754 * time.Millisecond)
	want := "P-" + "0000-0000"
	if got := MintAt("P", instant); got != want {
		t.Fatalf("MintAt(P, instant) = %q, want %q", got, want)
	}
}

func TestMintAtClampsBeforeEpoch(t *testing.T) {
	// R-SO3A-TOBX
	beforeEpoch := Epoch.Add(-time.Nanosecond)
	if got, want := MintAt("R", beforeEpoch), MintAt("R", Epoch); got != want {
		t.Fatalf("MintAt before Epoch = %q, want epoch encoding %q", got, want)
	}
}

func TestMintAtTimeOfRoundTrip(t *testing.T) {
	// R-SJ7P-ALD5
	const seed = uint64(1729)
	state := seed
	nextOffset := func() int64 {
		// xorshift64 is a deterministic PRNG suitable for reproducible sampling.
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return int64(state % uint64(modulus))
	}
	prefixes := []string{"R", "abc123", "Z9", "Requirement"}
	for i := 0; i < 1000; i++ {
		ms := nextOffset()
		instant := Epoch.Add(time.Duration(ms) * time.Millisecond)
		for _, prefix := range prefixes {
			id := MintAt(prefix, instant)
			got, err := TimeOf(id)
			if err != nil {
				t.Fatalf("TimeOf(MintAt(%q, offset %dms)) returned error: %v", prefix, ms, err)
			}
			want := instant.Truncate(time.Millisecond).UTC()
			if !got.Equal(want) {
				t.Fatalf("TimeOf(MintAt(%q, offset %dms)) = %s, want %s", prefix, ms, got, want)
			}
		}
	}
}

func TestTimeOfIgnoresPrefix(t *testing.T) {
	// R-SPB7-7G2M
	body := "OBCA-0VLA"
	prefixes := []string{"R", "S", "SPEC"}
	var decoded time.Time
	for i, prefix := range prefixes {
		got, err := TimeOf(prefix + "-" + body)
		if err != nil {
			t.Fatalf("TimeOf with prefix %q returned error: %v", prefix, err)
		}
		if i == 0 {
			decoded = got
			continue
		}
		if !got.Equal(decoded) {
			t.Fatalf("TimeOf with prefix %q = %s, want %s", prefix, got, decoded)
		}
	}
}

func TestTimeOfRejectsEveryNonCanonicalGrammarBoundary(t *testing.T) {
	// R-SQJ3-L7TB
	invalid := map[string]string{
		"empty":                   "",
		"missing prefix":          "-0000-0000",
		"missing prefix and dash": "0000-0000",
		"prefix punctuation":      "A_B-0000-0000",
		"prefix non-ASCII":        "Spéc-0000-0000",
		"missing dash":            "X0000-0000",
		"extra dash":              "X-0000-0000-",
		"misplaced dash":          "X-000-0-0000",
		"short first body part":   "X-000-0000",
		"long first body part":    "X-00000-0000",
		"short second body part":  "X-0000-000",
		"long second body part":   "X-0000-00000",
		"lowercase first part":    "X-00a0-0000",
		"lowercase second part":   "X-0000-00z0",
		"body punctuation":        "X-00_0-0000",
		"body non-ASCII":          "X-0000-00É0",
		"embedded space":          "X-00 0-0000",
		"leading space":           " X-0000-0000",
		"trailing space":          "X-0000-0000 ",
		"leading material":        "!X-0000-0000",
		"trailing material":       "X-0000-0000!",
		"newline":                 "X-0000-0000\n",
	}

	for name, id := range invalid {
		t.Run(name, func(t *testing.T) {
			_, err := TimeOf(id)
			if !errors.Is(err, ErrInvalidID) {
				t.Fatalf("TimeOf(%q) error = %v, want an error wrapping ErrInvalidID", id, err)
			}
		})
	}
}

func TestTimeOfNeverPanicsForArbitraryInput(t *testing.T) {
	// R-SRQZ-YZK0
	state := uint64(0x5eed)
	nextRandom := func() uint64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return state
	}
	inputs := []string{
		"",
		"-",
		"--",
		"X-0000-0000",
		"X-ZZZZ-ZZZZ",
		"X-0000-0000\x00",
		string([]byte{0xff, 0xfe, 0xfd}),
	}
	for i := 0; i < 20000; i++ {
		length := int(nextRandom() % 257)
		value := make([]byte, length)
		for j := range value {
			value[j] = byte(nextRandom() & 0xff)
		}
		inputs = append(inputs, string(value))
	}

	for i, id := range inputs {
		got, err := TimeOf(id)
		switch {
		case err == nil:
			if got.Location() != time.UTC || got.Nanosecond()%int(time.Millisecond) != 0 {
				t.Fatalf("input %d returned invalid successful time %s", i, got)
			}
		case errors.Is(err, ErrInvalidID):
			// Every rejected input must use the package's documented sentinel.
		default:
			t.Fatalf("input %d returned unclassified error: %v", i, err)
		}
	}
}

func TestValidateAffineMapPanicsWhenNotInvertible(t *testing.T) {
	// R-SSYW-CRAP
	validateAffineMap(multiplier, modulus)

	deferred := false
	func() {
		defer func() {
			if recover() != nil {
				deferred = true
			}
		}()
		validateAffineMap(6, 9)
	}()
	if !deferred {
		t.Fatal("validateAffineMap(6, 9) did not panic for a non-coprime pair")
	}
}
