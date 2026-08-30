---
description: property or sweep tests whose generator overwhelmingly produces inputs rejected at the first guard, leaving the claimed domain unexercised
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag randomized sweeps and property tests whose generator almost never produces an input that survives the parser's or validator's first guard, so the thousands of iterations all exercise one early rejection path and the interesting code is never reached. Uniformly random byte strings fed to a structured-format parser are the archetype: the probability of accidentally producing a well-formed token is effectively zero, so a sweep advertised as covering the whole input space actually covers "input is not shaped like a token" ten thousand times. The same defect appears with random integers aimed at a narrow valid range, random maps aimed at a schema, and random strings aimed at anything with a required prefix, length, or separator.

The evidence is structural — compare what the generator can emit against what the first guard in the code under test accepts — and it is usually confirmable from a coverage report showing the deeper branches uncovered despite a large iteration count. A reviewer objects because the test's cost and its name both claim a breadth it does not have, and because it crowds out the targeted cases someone would otherwise write.

Do not flag sweeps that deliberately target the rejection path and say so in the test name; hammering a validator with garbage is legitimate when that is the stated contract. Do not flag coverage-guided fuzz targets, whose engine evolves inputs past the guards over time. Do not flag a generator that mixes strategies — some proportion of well-formed values, some near-misses, some garbage — even if the garbage share is large. The fix is to generate near-valid inputs: mutate a known-good value one byte or one field at a time, or build values from the format's own grammar with a slightly widened alphabet or length.

```go
// Flagged: random bytes essentially never contain five separators between
// two-digit hex groups, so every iteration stops at the shape check and
// the parser body is never entered.
func TestParseMACNeverPanics(t *testing.T) {
	for i := 0; i < 20000; i++ {
		buf := make([]byte, rnd()%257)
		for j := range buf {
			buf[j] = byte(rnd() & 0xff)
		}
		if _, err := ParseMAC(string(buf)); err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("iteration %d: unclassified error: %v", i, err)
		}
	}
}
```

```go
// Spared: mutations of a well-formed value reach the parser body, so the
// sweep covers the domain its name claims.
func TestParseMACNeverPanics(t *testing.T) {
	const seed = "12:34:56:78:9A:BC"
	for i := 0; i < 20000; i++ {
		buf := []byte(seed)
		buf[rnd()%uint64(len(buf))] = byte(rnd() & 0xff)
		addr := string(buf)
		if _, err := ParseMAC(addr); err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("ParseMAC(%q): unclassified error: %v", addr, err)
		}
	}
}
```
