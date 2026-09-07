---
description: a conversion silently wraps, truncates, or saturates outside its representable range while the doc comment describes only the other boundary
severity: error
---
Flag conversions and encodings that quietly produce a wrong-but-plausible value once the input leaves the representable range, when the surrounding documentation describes only one end of that range or none of it. The pattern to look for is an exported function whose comment carefully states what happens below a floor ("values before the epoch are represented as the epoch") while the ceiling is enforced only by an implicit `% modulus`, a narrowing cast, or a library call that saturates. Duration and timestamp arithmetic is the common host: `b.Sub(a)` saturates at the maximum representable duration rather than reporting overflow, so two inputs centuries apart can produce the identical result. What makes a reviewer object is not the clamping itself — which may be a deliberate design choice — but that the output is indistinguishable from a correct one: a value that round-trips to a *different* input is a lie, where an honest clamp is merely lossy. Either document the representable range on the exported symbol or return an error outside it.

Do not flag a wraparound that the doc comment, the type, or an adjacent named constant already states — a documented modulus, a function named for its ring, an explicitly bounded counter. Do not flag hash functions, checksums, PRNGs, or any code whose whole purpose is modular arithmetic. Do not flag a narrowing conversion the code has just range-checked, nor arithmetic whose operands are provably bounded by a check a few lines above; if the bound exists but is unstated, prefer the "unchecked" rules for that instead. Do not flag deliberate saturation in a metrics or display path where an approximate large value is the point.

```go
// Flagged: the comment covers only the floor; past 2106 the stamp aliases to a different instant.
// Stamp returns a record timestamp for t. Instants before 1970 are represented as 0.
func Stamp(t time.Time) uint32 {
	s := t.Unix()
	if s < 0 {
		s = 0
	}
	return uint32(s) // wraps, silently, into a valid-looking stamp
}
```

```go
// Spared: the range is stated and enforced, so out-of-range input is reported rather than aliased.
// Stamp returns a record timestamp for t, which must lie in [1970, MaxStamp].
// Instants before 1970 are represented as 0; later instants return ErrOutOfRange.
func Stamp(t time.Time) (uint32, error) {
	if t.Unix() > MaxStamp {
		return 0, fmt.Errorf("%w: %s is past %s", ErrOutOfRange, t, time.Unix(MaxStamp, 0))
	}
	...
}
```
