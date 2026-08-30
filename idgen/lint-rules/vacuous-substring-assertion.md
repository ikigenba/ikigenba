---
description: containment assertions whose needle is already a substring of other expected content, so they pass regardless of the behavior under test
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag substring or containment assertions that cannot fail because the needle also occurs inside neighboring content the output is independently required to have. The classic case is a loop asserting that help text documents every short option spelling, where each short spelling is a prefix of its own long spelling — deleting every short flag from the output leaves the assertions green. The same shape appears when checking for a field name that is a substring of another field name, an error code embedded in a longer code, or a key that also appears in an unrelated part of a serialized payload. A reviewer objects because the test's name states a coverage claim the assertion does not make, and because it is usually redundant beside an exact-equality assertion elsewhere in the same file.

To judge this, read the needle against the rest of the expected output, not against the API being called. Do not flag `Contains` in general: containment is the correct assertion when the output has genuinely variable parts (a timestamp, a path, a stack trace) and only a fragment is stable. Do not flag a needle that appears exactly once in the expected content — that assertion is falsifiable. Do not flag containment used deliberately as a loose check on a diagnostic message whose exact wording is not part of the contract. When the needle is subsumed but the surrounding test also asserts exact equality on the same buffer, prefer reporting the redundancy: the containment loop should be deleted, not repaired. When no exact-equality assertion exists, the fix is a delimiter-aware check (match the whole token, or a line-anchored pattern) rather than a bare substring.

```go
// Flagged: "-j" is a substring of "--jobs", "-f" of "--format", "-h" of
// "--help". Only "-V" is a real check, because "--version" holds "-v" lowercase.
// Removing every short flag from the usage text keeps this test green.
func TestUsageMentionsEveryOption(t *testing.T) {
	out, _, _ := run([]string{"--help"})
	for _, opt := range []string{"-j", "--jobs", "-f", "--format", "-h", "--help", "-V", "--version"} {
		if !strings.Contains(out, opt) {
			t.Errorf("usage does not mention %q: %q", opt, out)
		}
	}
}
```

```go
// Spared: the needle is matched as a whole token at a line boundary, so a
// missing short spelling actually fails.
func TestUsageMentionsEveryOption(t *testing.T) {
	out, _, _ := run([]string{"--help"})
	for _, opt := range []string{"-j", "-f", "-h", "-V"} {
		re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(opt) + `[,\s]`)
		if !re.MatchString(out) {
			t.Errorf("usage does not document short option %q: %q", opt, out)
		}
	}
}

// Spared: the stable fragment is checked because the rest of the line varies.
func TestErrorNamesTheOffendingToken(t *testing.T) {
	_, stderr, _ := run([]string{"--config", "no_such_file"})
	if !strings.Contains(stderr, "no_such_file") {
		t.Errorf("stderr = %q, want the rejected token named", stderr)
	}
}
```
