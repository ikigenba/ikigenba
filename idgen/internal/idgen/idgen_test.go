package idgen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

var _ error = ErrInvalidID

func mustMintAt(t *testing.T, prefix string, instant time.Time) string {
	t.Helper()
	id, err := MintAt(prefix, instant)
	if err != nil {
		t.Fatalf("MintAt(%q, %s) returned unexpected error: %v", prefix, instant, err)
	}
	return id
}

func callMintAt(function func(string, time.Time) (string, error), prefix string, instant time.Time) (string, error) {
	return function(prefix, instant)
}

func callTimeOf(function func(string) (time.Time, error), id string) (time.Time, error) {
	return function(id)
}

func TestValidateAffineMapRejectsNonCoprimeMultiplier(t *testing.T) {
	// R-Y36K-9M90
	deferredPanic := false
	func() {
		defer func() {
			deferredPanic = recover() != nil
		}()
		validateAffineMap(6, 36)
	}()
	if !deferredPanic {
		t.Fatal("validateAffineMap(6, 36) did not panic for a non-coprime multiplier and modulus")
	}
}

func TestTimeOfExportedSignature(t *testing.T) {
	// R-U0TR-IPZM
	want := Epoch()
	got, err := callTimeOf(TimeOf, "R-"+"0007-J3LA")
	if err != nil {
		t.Fatalf("TimeOf through exported signature returned error: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("TimeOf through exported signature = %s, want %s", got, want)
	}
}

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
			prefix.WriteByte(validCharacters[nextRandom()%uint64(len(validCharacters))])
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
	assertEpochValue(t)
	assertEpochDeclaration(t)
}

func assertEpochValue(t *testing.T) {
	t.Helper()

	got := Epoch()
	want := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if got != want {
		t.Fatalf("Epoch() = %s, want %s", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("Epoch() location = %v, want time.UTC", got.Location())
	}
}

func assertEpochDeclaration(t *testing.T) {
	t.Helper()

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
	if got := mustMintAt(t, "R", Epoch()); got != want {
		t.Fatalf("MintAt(R, Epoch) = %q, want %q", got, want)
	}
}

func TestMintAtAbsoluteInstantGoldenVector(t *testing.T) {
	// R-SLNI-24UJ
	instant := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	want := "R-" + "OBCA-0VLA"
	if got := mustMintAt(t, "R", instant); got != want {
		t.Fatalf("MintAt(R, instant) = %q, want %q", got, want)
	}
}

func TestMintAtZeroPadsBody(t *testing.T) {
	// R-SMVE-FWL8
	instant := Epoch().Add(1143449080754 * time.Millisecond)
	want := "P-" + "0000-0000"
	if got := mustMintAt(t, "P", instant); got != want {
		t.Fatalf("MintAt(P, instant) = %q, want %q", got, want)
	}
}

func TestMintAtTimeOfRoundTrip(t *testing.T) {
	// R-SJ7P-ALD5
	const (
		seed = uint64(1729)
		// The format has eight base-36 digits, so its independently stated
		// representable offset range is [0, 36^8-1] milliseconds.
		maxRepresentableOffsetMillis = int64(2_821_109_907_455)
	)
	state := seed
	nextOffset := func() int64 {
		// xorshift64 is a deterministic PRNG suitable for reproducible sampling.
		var offset int64
		for range 8 {
			state ^= state << 13
			state ^= state >> 7
			state ^= state << 17
			offset = offset*36 + int64(state%36)
		}
		return offset
	}
	offsets := []int64{0, 1, maxRepresentableOffsetMillis - 1, maxRepresentableOffsetMillis}
	for range 1000 {
		offsets = append(offsets, nextOffset())
	}
	prefixes := []string{"R", "abc123", "Z9", "Requirement"}
	for _, ms := range offsets {
		instant := Epoch().Add(time.Duration(ms) * time.Millisecond)
		for _, prefix := range prefixes {
			id := mustMintAt(t, prefix, instant)
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

func TestMintAtExportedSignature(t *testing.T) {
	// R-FRLW-OBWD
	id, err := callMintAt(MintAt, "R", Epoch())
	if err != nil || id == "" {
		t.Fatalf("MintAt through exported signature = (%q, %v), want a non-empty id and nil error", id, err)
	}
}

func TestErrInvalidIDDeclarationAndIdentity(t *testing.T) {
	// R-AXI2-8KM2
	if ErrInvalidID == nil {
		t.Fatal("ErrInvalidID is nil, want a non-nil error sentinel")
	}
	if wrapped := fmt.Errorf("context: %w", ErrInvalidID); !errors.Is(wrapped, ErrInvalidID) {
		t.Fatalf("errors.Is(%v, ErrInvalidID) = false, want true", wrapped)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "idgen.go", nil, 0)
	if err != nil {
		t.Fatalf("parse idgen.go: %v", err)
	}
	var matchingDeclarations int
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "ErrInvalidID" {
				continue
			}
			matchingDeclarations++
			call, callOK := singleCall(value.Values)
			if !callOK {
				t.Errorf("ErrInvalidID initializer is not a single call to errors.New")
				continue
			}
			selector, selectorOK := call.Fun.(*ast.SelectorExpr)
			if !selectorOK {
				t.Errorf("ErrInvalidID initializer is not a call to errors.New")
				continue
			}
			packageName, packageOK := selector.X.(*ast.Ident)
			if !packageOK || packageName.Name != "errors" || selector.Sel.Name != "New" {
				t.Errorf("ErrInvalidID initializer is not a call to errors.New")
			}
		}
	}
	if matchingDeclarations != 1 {
		t.Fatalf("found %d ErrInvalidID declarations, want exactly 1", matchingDeclarations)
	}
}

func TestErrTimeRangeDeclarationAndIdentity(t *testing.T) {
	// R-FSTT-23N2
	sentinel := ErrTimeRange
	if sentinel == nil || !errors.Is(fmt.Errorf("context: %w", sentinel), ErrTimeRange) {
		t.Fatalf("ErrTimeRange = %v, want a non-nil identity-comparable error sentinel", sentinel)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "idgen.go", nil, 0)
	if err != nil {
		t.Fatalf("parse idgen.go: %v", err)
	}
	var matchingDeclarations int
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "ErrTimeRange" {
				continue
			}
			matchingDeclarations++
			typeName, typeOK := value.Type.(*ast.Ident)
			call, callOK := singleCall(value.Values)
			if !typeOK || typeName.Name != "error" || !callOK {
				t.Errorf("ErrTimeRange declaration must be `var ErrTimeRange error = errors.New(...)`")
				continue
			}
			selector, selectorOK := call.Fun.(*ast.SelectorExpr)
			if !selectorOK {
				t.Errorf("ErrTimeRange declaration must be `var ErrTimeRange error = errors.New(...)`")
				continue
			}
			packageName, packageOK := selector.X.(*ast.Ident)
			if !packageOK || packageName.Name != "errors" || selector.Sel.Name != "New" {
				t.Errorf("ErrTimeRange declaration must be `var ErrTimeRange error = errors.New(...)`")
			}
		}
	}
	if matchingDeclarations != 1 {
		t.Fatalf("found %d ErrTimeRange declarations, want exactly 1", matchingDeclarations)
	}
}

func singleCall(expressions []ast.Expr) (*ast.CallExpr, bool) {
	if len(expressions) != 1 {
		return nil, false
	}
	call, ok := expressions[0].(*ast.CallExpr)
	return call, ok
}

func TestMintAtRejectsOutsideRepresentableWindow(t *testing.T) {
	// R-FU1P-FVDR
	ceiling := Epoch().Add(time.Duration(modulus) * time.Millisecond)
	tests := []struct {
		name      string
		instant   time.Time
		wantError bool
	}{
		{name: "before epoch", instant: Epoch().Add(-time.Nanosecond), wantError: true},
		{name: "epoch", instant: Epoch()},
		{name: "final representable millisecond", instant: ceiling.Add(-time.Millisecond)},
		{name: "exact ceiling", instant: ceiling, wantError: true},
		{name: "after ceiling", instant: ceiling.Add(time.Nanosecond), wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, err := MintAt("R", test.instant)
			if test.wantError {
				if id != "" || !errors.Is(err, ErrTimeRange) {
					t.Fatalf("MintAt(R, %s) = (%q, %v), want no id and an error wrapping ErrTimeRange", test.instant, id, err)
				}
				return
			}
			if err != nil || id == "" {
				t.Fatalf("MintAt(R, %s) = (%q, %v), want a non-empty id and nil error", test.instant, id, err)
			}
			got, decodeErr := TimeOf(id)
			if decodeErr != nil || !got.Equal(test.instant) {
				t.Fatalf("TimeOf(%q) = (%s, %v), want (%s, nil)", id, got, decodeErr, test.instant)
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
		"bare separator":          "-",
		"double separator":        "--",
		"trailing null byte":      "X-0000-0000\x00",
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
	const seed = uint64(0x5eed)
	state := seed
	nextRandom := func() uint64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return state
	}

	// A uniformly-random byte string is rejected at TimeOf's first shape guard
	// with probability ~1, so a sweep built only from garbage exercises the shape
	// check 20 000 times and never enters the base-36 decode / timestamp
	// reconstruction path. To back the "arbitrary input" claim with real coverage
	// we build well-formed ids from the format's own minter, mutate known-good
	// values one byte at a time, and mix in pure garbage so the shape-rejection
	// path stays covered too.

	// wellFormedID builds a canonical id straight from the format's own minter,
	// so it always survives the shape guard and drives the decode + timestamp
	// path. The millisecond offset is sampled across the representable window;
	// sweeping it spreads the encoded body across the base-36 space.
	const maxRepresentableOffsetMillis = uint64(2_821_109_907_455)
	prefixes := []string{"R", "S", "SPEC", "abc123", "Z9", "Requirement"}
	wellFormedID := func() string {
		prefix := prefixes[nextRandom()%uint64(len(prefixes))]
		ms := int64(nextRandom() % (maxRepresentableOffsetMillis + 1))
		return mustMintAt(t, prefix, Epoch().Add(time.Duration(ms)*time.Millisecond))
	}

	// nearMissID mutates one byte of a well-formed id to an arbitrary value.
	// Many mutations land on the body and remain shaped like a token, so the
	// parser body is still reached; others break the shape and exercise the
	// rejection path — both from a value that started life inside the grammar.
	nearMissID := func() string {
		buf := []byte(wellFormedID())
		buf[nextRandom()%uint64(len(buf))] = byte(nextRandom() & 0xff)
		return string(buf)
	}

	// garbage is a uniformly-random byte string of arbitrary length, kept to
	// hold coverage of the shape-rejection path.
	garbage := func() string {
		length := int(nextRandom() % 257)
		value := make([]byte, length)
		for j := range value {
			value[j] = byte(nextRandom() & 0xff)
		}
		return string(value)
	}

	// decodedOK reports whether TimeOf accepted id. Whatever the input, TimeOf
	// must never panic and must land in exactly one of two classified outcomes:
	// a UTC, millisecond-aligned time, or an error wrapping ErrInvalidID.
	decodedOK := func(context string, id string) bool {
		got, err := TimeOf(id)
		switch {
		case err == nil:
			if got.Location() != time.UTC || got.Nanosecond()%int(time.Millisecond) != 0 {
				t.Fatalf("seed %#x, %s %q: TimeOf returned invalid successful time %s", seed, context, id, got)
			}
			return true
		case errors.Is(err, ErrInvalidID):
			return false
		default:
			t.Fatalf("seed %#x, %s %q: TimeOf returned unclassified error: %v", seed, context, id, err)
			return false
		}
	}

	// Hand-picked shape-rejection edges: never panic, always classified. The
	// structured rejection cases here are also enumerated by
	// TestTimeOfRejectsEveryNonCanonicalGrammarBoundary.
	for _, id := range []string{
		"",
		"-",
		"--",
		"X-0000-0000",
		"X-ZZZZ-ZZZZ",
		"X-0000-0000\x00",
		string([]byte{0xff, 0xfe, 0xfd}),
	} {
		decodedOK("edge", id)
	}

	// Every canonical id from the minter must survive the shape guard and
	// round-trip through the decode + timestamp path. Asserting this per input
	// makes the deep-path coverage exact — the sweep cannot silently collapse
	// into re-testing shape rejection.
	for i := 0; i < 20000; i++ {
		id := wellFormedID()
		if !decodedOK("well-formed", id) {
			t.Fatalf("seed %#x, well-formed input %d: TimeOf rejected canonical minted id %q", seed, i, id)
		}
	}

	// Near-miss mutations and pure garbage: never panic, always classified,
	// whether or not the mutated value still parses.
	for i := 0; i < 20000; i++ {
		decodedOK("near-miss", nearMissID())
		decodedOK("garbage", garbage())
	}
}
