---
description: an exported function trusts an input constraint it neither documents nor checks, while a caller package re-implements that constraint separately
severity: warning
include: ["**/*.go"]
---
Flag exported functions that depend on a constraint on their arguments — an allowed character set, a non-empty field, a range, an already-normalized path — where the constraint is neither checked in the function nor stated in its doc comment, and is instead enforced by a caller in another package. Two symptoms make this concrete and worth flagging. First, the constraint is duplicated: the caller re-expresses it in its own idiom (a regexp against the callee's hand-written character loop, say), so the two definitions can drift apart with nothing to catch it. Second, the package becomes internally inconsistent — its constructor happily emits values its own parser rejects, so `Parse(Format(x))` fails for inputs the API never warned about. Both are invisible to a linter and obvious to a reviewer reading the two packages together. The fix is either a doc comment stating the obligation, a check inside the callee, or exporting one validator that both sides call.

Do not flag unexported helpers whose callers are all in the same file or package and visibly guarded. Do not flag a documented precondition, even an unchecked one — "s must be valid UTF-8" in the doc comment discharges this rule. Do not flag hot-path functions where the check is deliberately hoisted to a boundary and the doc comment says so. Do not flag arguments whose type already encodes the constraint (a validated named type, an enum, a `*regexp.Regexp`), and do not demand defensive checks on internal invariants that no external caller can violate.

```go
// Flagged: name must be token characters for ParseEntry to accept the result, but WriteEntry
// neither says so nor checks, and the only enforcement lives in a regexp inside the caller's package.
func WriteEntry(name, value string) string {
	return name + "=" + escape(value)
}
```

```go
// Spared: the obligation is stated, and the validator both sides use is exported.
// WriteEntry renders one entry. name must satisfy ValidName; otherwise the
// result is not accepted by ParseEntry.
func WriteEntry(name, value string) string {
	return name + "=" + escape(value)
}

func ValidName(name string) bool { ... }
```
