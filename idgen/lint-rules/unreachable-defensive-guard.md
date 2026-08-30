---
description: a validity check made impossible by validation earlier in the same path, often with an error message that misdescribes it
severity: warning
---
Flag a guard whose condition cannot be true given what the code has already established a few lines above: a range check on a value whose type or prior parse bounds it more tightly, an `ok`/`err` branch on a helper that cannot fail for the inputs this call site can produce, a re-validation of characters an earlier validator already restricted. Two things make this worth a human's attention. It misinforms: the next reader takes the guard as evidence that the condition occurs, and reasons about the rest of the function under a false assumption. And it usually drags along a message or error path that was never exercised, so its wording drifts free of the truth — the classic case is one message covering two disjuncts of an `||` where each disjunct means something different and neither can fire. Prefer deleting the guard and stating the established invariant in a comment, or moving the check to the boundary where the value is actually untrusted. When several impossible conditions are combined, say which ones and why each cannot hold.

Do not flag defense in depth at a trust boundary — validating what arrived over a network, from a file, from another process, from user input, or across a public API — even when a caller inside the codebase already validated it; the guard is there for the callers you cannot see. Do not flag a check that is unreachable only under current call sites but guards an exported function against future ones. Do not flag checks documented as guarding a future refactor, an invariant a reviewer is asked to preserve, or a panic-avoidance backstop stated as such in a comment. Do not flag assertions in tests, `default` branches over an enum or type switch, or the impossible-error arms of interfaces whose contract permits failure even if this implementation never does. Only flag when you can state the specific earlier line that makes the condition impossible.

```go
// Flagged: validBodyPart has already restricted every byte to the same alphabet
// decode accepts, so ok is always true; and eight base-36 digits cannot reach
// modulus, so the range test is dead too — while the message describes only one of them.
if !validBodyPart(parts[1]) || !validBodyPart(parts[2]) {
	return time.Time{}, fmt.Errorf("%w: non-canonical format", ErrInvalidID)
}
n, ok := decodeBase36(parts[1] + parts[2])
if !ok || n >= modulus {
	return time.Time{}, fmt.Errorf("%w: body out of range", ErrInvalidID)
}
```

```go
// Spared: the invariant is stated rather than re-checked, and the reader is told why.
// validBodyPart above restricts the alphabet, so decoding cannot fail here, and
// bodyDigits base-36 digits max out one below modulus.
n := decodeBase36(parts[1] + parts[2])
```

```go
// Spared: defense in depth on an exported entry point, whose callers are unknown.
func Decode(id string) (time.Time, error) {
	if len(id) > maxIDLen {
		return time.Time{}, ErrInvalidID
	}
	...
}
```
