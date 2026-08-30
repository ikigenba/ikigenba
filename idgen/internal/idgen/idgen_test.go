package idgen

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

func prefixAgreementSample(seed uint64) []string {
	state := seed
	nextRandom := func() uint64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return state
	}
	const validCharacters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	punctuation := []string{"_", "!", ".", "@", "#"}
	nonASCII := []string{"é", "１", "界", "🙂"}

	randomValidRun := func() string {
		length := 1 + int(nextRandom()%16)
		var prefix strings.Builder
		prefix.Grow(length)
		for range length {
			prefix.WriteByte(validCharacters[nextRandom()%62])
		}
		return prefix.String()
	}

	candidates := []string{""}
	for range 32 {
		valid := randomValidRun()
		candidates = append(candidates,
			valid,
			valid+"-",
			valid+punctuation[nextRandom()%5],
			valid+nonASCII[nextRandom()%4],
		)
	}
	return candidates
}

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

func TestTimeOfPrefixAcceptanceAgreesWithValidPrefix(t *testing.T) {
	// R-60YQ-PLJO
	const canonicalBody = "0007-J3LA"
	accepted, rejected := 0, 0
	for _, prefix := range prefixAgreementSample(0x60_626) {
		_, err := TimeOf(prefix + "-" + canonicalBody)
		wantAccepted := ValidPrefix(prefix)
		gotAccepted := err == nil
		if gotAccepted != wantAccepted {
			t.Errorf("TimeOf prefix %q accepted = %t, ValidPrefix = %t (error %v)", prefix, gotAccepted, wantAccepted, err)
		}
		if wantAccepted {
			accepted++
			continue
		}
		rejected++
		if !errors.Is(err, ErrInvalidID) {
			t.Errorf("TimeOf prefix %q error = %v, want an error wrapping ErrInvalidID", prefix, err)
		}
	}
	if accepted == 0 || rejected == 0 {
		t.Fatalf("agreement sample exercised %d accepted and %d rejected prefixes, want both branches", accepted, rejected)
	}
}

func TestEpoch(t *testing.T) {
	// R-HF29-98B6
	got := Epoch()
	want := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if got != want {
		t.Fatalf("Epoch() = %s, want %s", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("Epoch() location = %v, want time.UTC", got.Location())
	}

	file, err := parser.ParseFile(token.NewFileSet(), "idgen.go", nil, 0)
	if err != nil {
		t.Fatalf("parse idgen.go: %v", err)
	}
	foundFunction := false
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Recv == nil && declaration.Name.Name == "Epoch" {
				foundFunction = true
			}
		case *ast.GenDecl:
			if declaration.Tok != token.VAR {
				continue
			}
			for _, spec := range declaration.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range valueSpec.Names {
					if name.Name == "Epoch" {
						t.Fatal("Epoch is declared as an assignable package variable")
					}
				}
			}
		}
	}
	if !foundFunction {
		t.Fatal("Epoch is not declared as a package function")
	}
}

func TestMintAtEpochGoldenVector(t *testing.T) {
	// R-WHEV-1AN5
	want := "R-" + "0007-J3LA"
	if got := MintAt("R", Epoch()); got != want {
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
	instant := Epoch().Add(1143449080754 * time.Millisecond)
	want := "P-" + "0000-0000"
	if got := MintAt("P", instant); got != want {
		t.Fatalf("MintAt(P, instant) = %q, want %q", got, want)
	}
}

func TestMintAtClampsBeforeEpoch(t *testing.T) {
	// R-WIMR-F2DU
	beforeEpoch := Epoch().Add(-time.Nanosecond)
	if got, want := MintAt("R", beforeEpoch), "R-"+"0007-J3LA"; got != want {
		t.Fatalf("MintAt before Epoch = %q, want epoch encoding %q", got, want)
	}
}

func TestMintAtTimeOfRoundTrip(t *testing.T) {
	// R-SJ7P-ALD5
	const (
		seed                         = uint64(1729)
		maxRepresentableOffsetMillis = int64(2_821_109_907_455) // 36^8 - 1, the largest eight-digit base36 value.
		representableOffsetCount     = uint64(2_821_109_907_456)
	)
	state := seed
	nextOffset := func() int64 {
		// xorshift64 is a deterministic PRNG suitable for reproducible sampling.
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return int64(state % representableOffsetCount)
	}
	offsets := []int64{0, 1, maxRepresentableOffsetMillis - 1, maxRepresentableOffsetMillis}
	for range 1000 {
		offsets = append(offsets, nextOffset())
	}
	prefixes := []string{"R", "abc123", "Z9", "Requirement"}
	for _, ms := range offsets {
		instant := Epoch().Add(time.Duration(ms) * time.Millisecond)
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

func TestMintAtEncodingBoundaryBehavior(t *testing.T) {
	const (
		maxRepresentableOffsetMillis = int64(2_821_109_907_455) // 36^8 - 1, the largest eight-digit base36 value.
		encodingPeriodMillis         = maxRepresentableOffsetMillis + 1
		saturatedDurationMillis      = int64(9_223_372_036_854) // floor(MaxInt64 nanoseconds / one millisecond).
	)

	tests := []struct {
		name             string
		instant          time.Time
		wantOffsetMillis int64
	}{
		{
			name:             "last representable offset",
			instant:          Epoch().Add(time.Duration(maxRepresentableOffsetMillis) * time.Millisecond),
			wantOffsetMillis: maxRepresentableOffsetMillis,
		},
		{
			name:             "one past representable offset wraps to epoch",
			instant:          Epoch().Add(time.Duration(encodingPeriodMillis) * time.Millisecond),
			wantOffsetMillis: 0,
		},
		{
			name:             "far future time saturates time.Duration",
			instant:          Epoch().AddDate(1000, 0, 0),
			wantOffsetMillis: saturatedDurationMillis % encodingPeriodMillis,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := MintAt("R", test.instant)
			got, err := TimeOf(id)
			if err != nil {
				t.Fatalf("TimeOf(MintAt(R, %s)) returned error: %v", test.instant, err)
			}
			want := Epoch().Add(time.Duration(test.wantOffsetMillis) * time.Millisecond)
			if !got.Equal(want) {
				t.Fatalf("TimeOf(MintAt(R, %s)) = %s, want %s", test.instant, got, want)
			}
		})
	}
}

func TestTimeOfIgnoresPrefix(t *testing.T) {
	// R-SPB7-7G2M
	body := "OBCA-0VLA"
	prefixes := []string{"R", "S", "SPEC"}
	want := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	for _, prefix := range prefixes {
		got, err := TimeOf(prefix + "-" + body)
		if err != nil {
			t.Fatalf("TimeOf with prefix %q returned error: %v", prefix, err)
		}
		if !got.Equal(want) {
			t.Fatalf("TimeOf with prefix %q = %s, want %s", prefix, got, want)
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
