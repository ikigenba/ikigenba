---
description: fakes whose behavior branches on an internal invocation counter, silently changing meaning when the caller's call pattern shifts
severity: error
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag test doubles that decide what to return by counting how many times they have been called — "fail on the third call", "step the clock backward on the second `Now`", "return the short page on call four". The counter encodes an assumption about the caller's internal call pattern that nothing states and nothing checks. When someone refactors the code under test to consult the collaborator one extra time, the scripted event lands in a different iteration, the test keeps passing, and it now exercises a scenario nobody chose. That is the worst failure mode a double can have: green, and testing something other than its name.

The signal is a mutable counter field on the fake that is read in a conditional inside the fake's own method, rather than only being read by assertions in the test. Counting calls for later assertion is a different thing and is fine. The fix is to make the schedule explicit and caller-independent: hand the fake a queue of responses the test writes out in full, trigger the special behavior on a *condition of the input* rather than on a tally, or expose a method the test calls at a chosen point to arm the behavior.

Do not flag counters used purely for assertions (`if clock.nowCalls != 0`), which is how a test proves work was or was not done. Do not flag a fake that returns a scripted sequence the test declares literally, even though position in that sequence is inherently ordinal — the difference is that the sequence is visible at the call site and its length is asserted. Do not flag retry or backoff tests where "fail the first N attempts, then succeed" is precisely the contract under test, since there the call count *is* the specification.

```go
// Flagged: hard-codes that the second Now() is the one to perturb. One extra
// clock read anywhere in the caller moves the backward step and the test
// silently starts covering a different scenario.
type backwardClock struct {
	now      time.Time
	nowCalls int
}

func (c *backwardClock) Now() time.Time {
	c.nowCalls++
	if c.nowCalls == 2 {
		c.now = c.now.Add(-3 * time.Millisecond)
	}
	return c.now
}
```

```go
// Spared: the test writes the whole schedule out, so what the caller sees is
// readable at the call site and independent of how often it asks.
type scriptedClock struct {
	readings []time.Time
	i        int
}

func (c *scriptedClock) Now() time.Time {
	at := c.readings[min(c.i, len(c.readings)-1)]
	c.i++
	return at
}

// Spared: counting for assertion only.
type fakeClock struct {
	now      time.Time
	nowCalls int
}

func (c *fakeClock) Now() time.Time {
	c.nowCalls++
	return c.now
}
```
