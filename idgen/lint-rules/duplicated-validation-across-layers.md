---
description: the same input rule implemented independently in two layers or packages, with no shared predicate
severity: error
---
Flag a validation rule — a character class, a lexical grammar, a range check, a required-field check — implemented more than once for the same concept in different layers or packages, with no shared constant or exported predicate linking the copies. The copies are often written by different mechanisms (a regular expression in one place, a hand-rolled character loop in another), which hides the duplication from a plain text search. Object because the two can drift silently and asymmetrically: widening the rule at the outer boundary produces values the inner layer then rejects, and the failure surfaces far from the edit that caused it. Recommend that the layer which owns the format own the rule, exporting one predicate the other layer calls, so there is a single place to widen or narrow it.

Do not flag defense in depth where each layer checks a genuinely different rule — an interface layer checking that a value is present and non-empty while the core checks the full grammar is two rules, not one duplicated. Do not flag a trust boundary re-validating untrusted input that an inner layer also validates for its own contract, when both call the same shared predicate. Do not flag validations that merely look alike because they use similar syntax for different fields, generated code that mirrors a hand-written definition, or a schema declaration paired with the runtime check it describes.

Flagged:

```go
// package cli
var validLabel = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// package format — same rule, second implementation, no shared source of truth
func validLabel(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isAlphanumeric(s[i]) {
			return false
		}
	}
	return len(s) > 0
}
```

Spared:

```go
// package cli — boundary check: presence only
if label == "" {
	return errMissingLabel
}

// package format — owns and exports the grammar; cli defers to it for the rule itself
func ValidLabel(s string) bool { ... }
```
