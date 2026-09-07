---
description: sweeps bounding their input range with the same constant the implementation uses, guaranteeing the boundary is never crossed
severity: error
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag tests that derive the range of inputs they explore from a constant belonging to the implementation — a modulus, a capacity, a buffer size, a maximum length, a page size. Such a sweep is pinned to whatever the implementation currently believes its own limits are, so it can never reach the boundary where behavior changes, and it silently follows the constant if someone edits it. If the constant is wrong, the test explores the wrong domain and still passes. The most damaging version wraps a value modulo the implementation's own modulus before feeding it in, which makes overflow or wrap-around structurally untestable by that sweep: the test proves the function is correct exactly where it was already going to be correct.

The signal is a package-internal constant appearing in the generator or in the loop bound rather than in an assertion. Note that this is compatible with a test being otherwise excellent — a round-trip property is a good property; the objection is to the domain it runs over.

Do not flag a test that *asserts* against an implementation constant while generating from an independent range, which is often the right way to check a documented limit. Do not flag exported constants that are part of the public contract and pinned to a literal elsewhere in the suite. Do not flag helper code sizing a fixture buffer or preallocating a slice; the rule is about the input domain, not about allocation. The fix is to state the domain in the test's own terms (literal bounds drawn from the specification) and to add explicit cases at the limit, one past the limit, and at whatever saturation the surrounding types impose.

```go
// Flagged: the sweep can never leave the range where round-tripping holds,
// so wrap-around past the implementation's own limit is untestable here.
func TestRoundTrip(t *testing.T) {
	for i := 0; i < 1000; i++ {
		seq := next() % maxSeq
		got, err := Unpack(Pack(seq))
		if err != nil || got != seq {
			t.Fatalf("seq %d: %d, %v", seq, got, err)
		}
	}
}
```

```go
// Spared: the domain comes from the specification, and the limits are named
// explicitly so a change in the implementation constant fails the test.
func TestRoundTrip(t *testing.T) {
	const maxRepresentable = 281474976710655 // spec: 2^48 - 1
	cases := []uint64{0, 1, maxRepresentable - 1, maxRepresentable}
	for i := 0; i < 1000; i++ {
		cases = append(cases, next()%(maxRepresentable+1))
	}
	for _, seq := range cases {
		got, err := Unpack(Pack(seq))
		if err != nil || got != seq {
			t.Errorf("seq %d: %d, %v", seq, got, err)
		}
	}
}
```
