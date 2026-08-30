---
description: expected values computed by invoking the same function or constant the assertion is meant to verify
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag assertions whose expected value is produced by calling the very function, method, or constant under test, so the comparison reduces to `f(x) == f(x)` and holds for any implementation of `f`. This is most common when a test exercises a caller (a CLI entry point, a handler, a wrapper) and builds its expectation by calling the same collaborator the caller calls with the same arguments — the test then pins the argument plumbing but cannot detect any change in the collaborator's behavior, while its name promises otherwise. It also appears when output is asserted against the production constant that produced it, or when a loop constructs the whole expected payload by re-running the production encoder.

Do not flag a test whose expectation is an independent literal, even when that literal duplicates a value that also exists in production source — an independently declared copy of a usage string, a version string, or a golden payload is the correct pattern, not a violation. Do not flag golden values shared through an exported constant or fixture when that constant is itself pinned against a literal by some other test in the suite; the value has an independent anchor, and requiring every caller-level test to re-hardcode it would be noise. Do not flag round-trip properties (`decode(encode(x)) == x`), which deliberately compose two functions and are falsifiable. Do not flag test setup that uses the production API to *construct inputs* — only assertions that use it to construct *expectations*. Do not flag a test that calls the function under test twice to assert determinism or idempotence, provided the test says so; that is a different rule.

```go
// Flagged: Run mints by calling Encode with these same arguments, so this
// asserts Encode(prefix, at) == Encode(prefix, at).
func TestRunMintsWithPrefix(t *testing.T) {
	at := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	Run(nil, &out, fixedClock{now: at})
	if got, want := out.String(), Encode("R", at)+"\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// Flagged: compared against the production constant that generated the output.
func TestVersionFlag(t *testing.T) {
	out, _, _ := run([]string{"--version"})
	if out != version+"\n" {
		t.Errorf("stdout = %q, want %q", out, version+"\n")
	}
}
```

```go
// Spared: the clock is fixed, so the exact output is knowable and written as a
// literal. A change in the encoder now fails this test.
func TestRunMintsWithPrefix(t *testing.T) {
	at := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	Run(nil, &out, fixedClock{now: at})
	if got, want := out.String(), "R-8QJ2-11VC\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// Spared: round-trip property, falsifiable for any broken encoding.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	at := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	got, err := Decode(Encode("R", at))
	if err != nil || !got.Equal(at) {
		t.Fatalf("Decode(Encode(at)) = %v, %v; want %v", got, err, at)
	}
}
```
