---
description: tests comparing several outputs of the same function against each other with no independently known expected value
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag tests that call the code under test two or more times and assert only that the results agree, when the correct result is knowable and cheap to state. Such a test proves consistency, not correctness: a regression that makes every call wrong in the same way leaves it green. It usually shows up as "this input is ignored" or "these aliases behave alike" tests, which loop over variants, stash the first result, and compare the rest to it. The name promises a behavioral guarantee; the body only rules out disagreement.

The signal is a stashed first result used as the expectation for later iterations, often with a `if i == 0 { first = got; continue }` branch inside the loop — which is also per-case branching that makes the first iteration mean something different from the others, and typically mislabels a second observed value as `want` in the failure message. Consistency assertions are worth keeping; they just need one anchor. Flag when no iteration is compared against a literal or an independently derived value.

Do not flag consistency checks where the correct value is genuinely unavailable to the test — output containing a real timestamp, a random identifier, or a host-dependent path — provided the invariant itself is the contract. Do not flag differential tests that compare two *different* implementations (a fast path against a reference, an optimized encoder against a naive one), which are falsifiable by construction. Do not flag a test that anchors one case against a literal and compares the remaining cases to it; that is the recommended fix.

```go
// Flagged: all three prefixes could decode to the wrong instant and this passes.
// The `i == 0` branch also makes the first iteration a different test.
func TestDecodeIgnoresPrefix(t *testing.T) {
	var first time.Time
	for i, prefix := range []string{"R", "S", "SPEC"} {
		got, err := Decode(prefix + "-" + body)
		if err != nil {
			t.Fatalf("Decode with prefix %q: %v", prefix, err)
		}
		if i == 0 {
			first = got
			continue
		}
		if !got.Equal(first) {
			t.Fatalf("prefix %q = %s, want %s", prefix, got, first)
		}
	}
}
```

```go
// Spared: anchored on a known instant, so every prefix is checked for
// correctness and consistency falls out.
func TestDecodeIgnoresPrefix(t *testing.T) {
	want := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	for _, prefix := range []string{"R", "S", "SPEC"} {
		got, err := Decode(prefix + "-" + body)
		if err != nil {
			t.Fatalf("Decode with prefix %q: %v", prefix, err)
		}
		if !got.Equal(want) {
			t.Errorf("prefix %q = %s, want %s", prefix, got, want)
		}
	}
}
```
