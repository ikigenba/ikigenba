---
description: inequality or set-membership assertions on values a deterministic fake makes exactly predictable
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag assertions that settle for a bound or a membership check when the test's own setup makes the exact answer knowable. With an injected fake clock, a fixed seed, and a fixed input, the number of sleeps, the elapsed virtual time, the number of output lines, and the retry count are all determined — so `>= 4ms` is a strictly weaker claim than `== 4ms`, and it stays green if a bug makes the code wait four seconds. The same weakness shows up as asserting that each produced value belongs to some expected set without also asserting how many values were produced or that they are distinct, which passes when the code emits the same value repeatedly.

The judgment is about determinism, not about the operator: an inequality is a smell only when nothing in the test can vary. Look for a fake with a fully scripted response sequence, a frozen clock, or a literal input table feeding a comparison that leaves slack. Where a lower bound reflects the actual requirement ("at least N-1 milliseconds apart"), the exact assertion still belongs beside it, or the requirement should be restated as the equality the implementation guarantees.

Do not flag bounds on quantities that genuinely vary: wall-clock durations, allocation counts, goroutine counts, floating-point results needing a tolerance, or anything measured against a real external system. Do not flag membership assertions over unordered results where order truly is unspecified — but do expect a count assertion alongside them. Do not flag a deliberately loose bound documented as a smoke check against pathological behavior when an exact assertion elsewhere pins the value.

```go
// Flagged: with an advancing fake clock the totals are exactly 4ms; as written
// this passes if the code sleeps four virtual seconds. And membership alone
// accepts four identical outputs.
if got := clock.totalSleep(); got < 4*time.Millisecond {
	t.Errorf("virtual advance = %s, want at least %s", got, 4*time.Millisecond)
}
for i, b := range collectedBatches(t, out) {
	if _, ok := allowedSizes[len(b)]; !ok {
		t.Errorf("batch[%d] size %d was never requested", i, len(b))
	}
}
```

```go
// Spared: the deterministic fake makes every quantity exact, and the count and
// distinctness of the outputs are asserted too.
if got, want := clock.totalSleep(), 4*time.Millisecond; got != want {
	t.Errorf("virtual advance = %s, want %s", got, want)
}
batches := collectedBatches(t, out)
if len(batches) != 5 {
	t.Fatalf("batch count = %d, want 5", len(batches))
}
for i, b := range batches {
	if got, want := len(b), batchSize; got != want {
		t.Errorf("batch[%d] size = %d, want %d", i, got, want)
	}
}
```
