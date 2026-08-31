---
description: a validity check made impossible by validation earlier in the same path, often with an error message that misdescribes it
severity: error
---
Flag a guard whose condition cannot be true given what the code has already established a few lines above: a range check on a value whose type or prior parse bounds it more tightly, an `ok`/`err` branch on a helper that cannot fail for the inputs this call site can produce, a re-validation of characters an earlier validator already restricted. Two things make this worth a human's attention. It misinforms: the next reader takes the guard as evidence that the condition occurs, and reasons about the rest of the function under a false assumption. And it usually drags along a message or error path that was never exercised, so its wording drifts free of the truth — the classic case is one message covering two disjuncts of an `||` where each disjunct means something different and neither can fire. Prefer deleting the guard and stating the established invariant in a comment, or moving the check to the boundary where the value is actually untrusted. When several impossible conditions are combined, say which ones and why each cannot hold.

Do not flag defense in depth at a trust boundary — validating what arrived over a network, from a file, from another process, from user input, or across a public API — even when a caller inside the codebase already validated it; the guard is there for the callers you cannot see. Do not flag a check that is unreachable only under current call sites but guards an exported function against future ones. Do not flag checks documented as guarding a future refactor, an invariant a reviewer is asked to preserve, or a panic-avoidance backstop stated as such in a comment. Do not flag assertions in tests, `default` branches over an enum or type switch, or the impossible-error arms of interfaces whose contract permits failure even if this implementation never does. Only flag when you can state the specific earlier line that makes the condition impossible.

```go
// Flagged: validHexDigits has already restricted every byte to the alphabet
// parseHex accepts, so ok is always true; and six hex digits cannot reach
// 1<<24, so the range test is dead too — while the message describes only one of them.
if !validHexDigits(body) {
	return Color{}, fmt.Errorf("%w: non-canonical format", ErrInvalidColor)
}
n, ok := parseHex(body)
if !ok || n >= 1<<24 {
	return Color{}, fmt.Errorf("%w: value out of range", ErrInvalidColor)
}
```

```go
// Spared: the invariant is stated rather than re-checked, and the reader is told why.
// validHexDigits above restricts the alphabet, so parsing cannot fail here, and
// six hex digits max out one below 1<<24.
n := parseHex(body)
```

```go
// Spared: defense in depth on an exported entry point, whose callers are unknown.
func ParseColor(s string) (Color, error) {
	if len(s) > maxColorLen {
		return Color{}, ErrInvalidColor
	}
	...
}
```
