---
description: expected values computed by invoking the same function or constant the assertion is meant to verify
severity: error
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag assertions whose expected value is produced by calling the very function, method, or constant under test, so the comparison reduces to `f(x) == f(x)` and holds for any implementation of `f`. This is most common when a test exercises a caller (a CLI entry point, a handler, a wrapper) and builds its expectation by calling the same collaborator the caller calls with the same arguments — the test then pins the argument plumbing but cannot detect any change in the collaborator's behavior, while its name promises otherwise. It also appears when output is asserted against the production constant that produced it, or when a loop constructs the whole expected payload by re-running the production encoder.

Do not flag a test whose expectation is an independent literal, even when that literal duplicates a value that also exists in production source — an independently declared copy of a usage string, a version string, or a golden payload is the correct pattern, not a violation. Do not flag golden values shared through an exported constant or fixture when that constant is itself pinned against a literal by some other test in the suite; the value has an independent anchor, and requiring every caller-level test to re-hardcode it would be noise. Do not flag round-trip properties (`decode(encode(x)) == x`), which deliberately compose two functions and are falsifiable. Do not flag test setup that uses the production API to *construct inputs* — only assertions that use it to construct *expectations*. Do not flag a test that calls the function under test twice to assert determinism or idempotence, provided the test says so; that is a different rule.

```go
// Flagged: Run produces its digest by calling Sum on these same bytes, so this
// asserts Sum(data) == Sum(data).
func TestRunPrintsDigest(t *testing.T) {
	data := []byte("known input\n")
	var out bytes.Buffer
	Run(bytes.NewReader(data), &out)
	if got, want := out.String(), Sum(data)+"\n"; got != want {
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
// Spared: the input is fixed, so the exact output is knowable and written as a
// literal. A change in the hash now fails this test.
func TestRunPrintsDigest(t *testing.T) {
	data := []byte("known input\n")
	var out bytes.Buffer
	Run(bytes.NewReader(data), &out)
	if got, want := out.String(), "d1b0a4c8e2f97316\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// Spared: round-trip property, falsifiable for any broken encoding.
func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	cfg := Config{Name: "a", Limit: 3}
	got, err := Unmarshal(Marshal(cfg))
	if err != nil || got != cfg {
		t.Fatalf("Unmarshal(Marshal(cfg)) = %v, %v; want %v", got, err, cfg)
	}
}
```
