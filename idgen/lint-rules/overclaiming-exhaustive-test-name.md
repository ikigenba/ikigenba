---
description: tests named every, all, or exhaustive whose table leaves a reachable branch or documented case of the target unexercised
severity: error
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag tests whose name or doc comment asserts completeness — "every", "all", "each", "exhaustive", "the full grammar", "all error cases" — when the table does not in fact cover every case the target defines. Reviewers and future maintainers read such a name as a coverage guarantee and stop looking; a missing enum member, error kind, flag spelling, or rejection reason then goes untested indefinitely, and nobody adds a case when a new one is introduced, because the test's name implies it is already handled.

Check the name against the target's own enumeration: the error values or sentinel wrappings the function can return, the variants of an enum, the branches of a validator, the documented option spellings. A particularly informative outcome is discovering that a case cannot be covered because the corresponding branch is unreachable — say a range check that a preceding length and alphabet check already makes impossible. That is worth reporting as a finding in its own right: either the dead branch goes, or the completeness claim narrows to what the target can actually do.

Do not flag a test whose name describes a category rather than a quantifier ("rejects malformed separators"), even if the table is partial — a narrow name makes an honest claim. Do not flag exhaustiveness over an open-ended domain such as arbitrary strings, where completeness is aspirational and the name is understood as such. Do not demand a case for a branch that is genuinely unreachable; require instead that the discrepancy be resolved. The cheap fix for the common case is to rename the test to what it covers.

```go
// Flagged: named "every boundary", but the target also returns a distinct
// out-of-range error that no case here produces. (Investigation may show that
// branch is unreachable — then the branch, not the table, is the bug.)
func TestParseRejectsEveryMalformedVersion(t *testing.T) {
	invalid := []struct{ name, input string }{
		{"empty", ""},
		{"missing minor", "1."},
		{"letters", "1.a.0"},
		// ... no case yields ErrInvalid with "component out of range"
	}
	// ...
}
```

```go
// Spared: the name claims exactly what the table covers.
func TestParseRejectsMissingAndNonNumericComponents(t *testing.T) {
	// ...
}

// Spared: the completeness claim is backed by a case per documented error.
func TestParseReturnsEveryDocumentedErrorReason(t *testing.T) {
	for _, tc := range []struct {
		name, input, reason string
	}{
		{"bad shape", "1..0", "malformed version"},
		{"out of range", oversizedComponent, "component out of range"},
	} {
		// ...
	}
}
```
