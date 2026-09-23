---
description: the same input rule implemented independently in two places — other layers, other packages, or two stages of one path — with no shared predicate
severity: error
---
Flag a validation rule — a character class, a lexical grammar, a range check, a required-field check — implemented more than once for the same concept, with no shared constant or predicate linking the copies. "More than once" counts every independent encoding of the rule, wherever the copies sit: two packages, two layers, or **two functions in the same file**. A very common same-file shape is a validate-then-consume pair: a predicate checks the grammar, and the consumer that runs next (a decoder, a parser, a converter) re-derives the same grammar internally — for example by switching on the same character ranges the predicate already accepted, or by carrying its own "invalid character" branch for input the predicate guaranteed cannot arrive. That consumer's re-check is a second implementation of the rule, and it counts even though it looks like harmless defensiveness.

The copies need not be textually similar; they are usually written by different mechanisms — a regular expression in one place, a hand-rolled character loop in another, a `switch` over byte ranges in a third — which hides the duplication from a plain text search. To find them, do not compare text: enumerate each validation or character-classification decision the code makes, state the rule it enforces in words ("byte is 0-9 or a-z", "value fits in N digits", "field is non-empty"), and flag any rule stated twice by independent code.

Object because the copies drift silently and asymmetrically: widening the rule in one place produces values the other still rejects, and the failure surfaces far from the edit that caused it. Recommend that the code which owns the format own the rule once — one predicate, one classification table, or one shared constant — that every other site calls, so there is a single place to widen or narrow it. For a validate-then-consume pair, either the consumer calls the shared predicate/table, or the validator is the consumer's own error path (validate by attempting the conversion), leaving one encoding of the grammar.

Do not flag defense in depth where each check enforces a genuinely different rule — presence checked at the boundary while the core checks the full grammar is two rules, not one duplicated. Do not flag re-validation of untrusted input when both sites call the same shared predicate. Do not flag validations that merely look alike because they use similar syntax for different fields, generated code that mirrors a hand-written definition, or a schema declaration paired with the runtime check it describes.

Flagged (across packages):

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

Flagged (same file, validate-then-consume):

```go
// Parse checks the grammar once...
func Parse(s string) (Value, error) {
	if !validToken(s) { // rule: bytes are 0-9 or a-z
		return 0, ErrBadToken
	}
	return decode(s), nil
}

// ...and decode implements the identical character classification a second time.
func decode(s string) Value {
	var v Value
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9': // same rule, re-derived
			v = v*36 + Value(c-'0')
		case c >= 'a' && c <= 'z': // same rule, re-derived
			v = v*36 + Value(c-'a'+10)
		}
	}
	return v
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
