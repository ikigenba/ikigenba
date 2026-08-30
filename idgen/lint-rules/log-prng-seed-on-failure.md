---
description: seeded randomized tests that fail without reporting the seed or the generated input needed to reproduce the failure
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag randomized or seeded sweeps whose failure output does not carry enough information to reproduce the failure: no seed, and no printout of the generated value that failed. A message like `iteration 4713 failed` sends the reader back to the source to find the seed, then to re-derive four thousand PRNG steps by hand before they can even see the input. Deterministic seeding makes the run reproducible in principle; only reporting makes it reproducible in practice, and the two are routinely confused. The problem is worse for a randomly seeded test, where the failing input is gone the moment the process exits.

Flag both halves: a failure message that omits the offending input, and a test that never surfaces its seed. Printing the concrete input is usually sufficient on its own and is the better fix, because it survives changes to the generator; printing the seed matters for sweeps where the failure depends on accumulated state. An index into the iteration space is not a substitute for either.

Do not flag tests whose inputs are a fixed literal table — there is nothing to reproduce, and the case name identifies the row. Do not flag native fuzz targets whose harness already records and persists the failing corpus entry. Do not flag a sweep that prints the input on failure but not the seed, when each iteration is independent of the ones before it. Do not demand logging on the success path; the seed belongs in the failure message, or behind a `t.Log` that only matters when the test is verbose.

```go
// Flagged: the failing input exists only inside the generator's state, and
// neither it nor the seed appears in the message.
func TestDecodeNeverPanics(t *testing.T) {
	state := uint64(0x5eed)
	for i := 0; i < 20000; i++ {
		id := randomString(&state)
		if _, err := Decode(id); err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("input %d returned unclassified error: %v", i, err)
		}
	}
}
```

```go
// Spared: the failure names the seed and the exact input, so the reader can
// re-run it directly.
func TestDecodeNeverPanics(t *testing.T) {
	const seed = uint64(0x5eed)
	state := seed
	for i := 0; i < 20000; i++ {
		id := randomString(&state)
		if _, err := Decode(id); err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("seed %#x, iteration %d: Decode(%q) returned unclassified error: %v", seed, i, id, err)
		}
	}
}
```
