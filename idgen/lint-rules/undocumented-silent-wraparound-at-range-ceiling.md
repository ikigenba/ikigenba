---
description: a conversion silently wraps, truncates, or saturates outside its representable range while the doc comment describes only the other boundary
severity: warning
---
Flag conversions and encodings that quietly produce a wrong-but-plausible value once the input leaves the representable range, when the surrounding documentation describes only one end of that range or none of it. The pattern to look for is an exported function whose comment carefully states what happens below a floor ("values before the epoch are represented as the epoch") while the ceiling is enforced only by an implicit `% modulus`, a narrowing cast, or a library call that saturates. Duration and timestamp arithmetic is the common host: `b.Sub(a)` saturates at the maximum representable duration rather than reporting overflow, so two inputs centuries apart can produce the identical result. What makes a reviewer object is not the clamping itself — which may be a deliberate design choice — but that the output is indistinguishable from a correct one: a value that round-trips to a *different* input is a lie, where an honest clamp is merely lossy. Either document the representable range on the exported symbol or return an error outside it.

Do not flag a wraparound that the doc comment, the type, or an adjacent named constant already states — a documented modulus, a function named for its ring, an explicitly bounded counter. Do not flag hash functions, checksums, PRNGs, or any code whose whole purpose is modular arithmetic. Do not flag a narrowing conversion the code has just range-checked, nor arithmetic whose operands are provably bounded by a check a few lines above; if the bound exists but is unstated, prefer the "unchecked" rules for that instead. Do not flag deliberate saturation in a metrics or display path where an approximate large value is the point.

```go
// Flagged: the comment covers only the floor; above ~modulus the id decodes to a different instant.
// Encode returns a code for t. Instants before Epoch are represented as Epoch.
func Encode(t time.Time) string {
	ms := int64(t.Sub(Epoch) / time.Millisecond) // saturates for far-future t
	return format(ms % modulus)                  // wraps, silently, into a valid-looking code
}
```

```go
// Spared: the range is stated and enforced, so out-of-range input is reported rather than aliased.
// Encode returns a code for t, which must lie in [Epoch, Epoch+MaxSpan).
// Instants before Epoch are represented as Epoch; later instants return ErrOutOfRange.
func Encode(t time.Time) (string, error) {
	if t.Sub(Epoch) >= MaxSpan {
		return "", fmt.Errorf("%w: %s is past %s", ErrOutOfRange, t, Epoch.Add(MaxSpan))
	}
	...
}
```
