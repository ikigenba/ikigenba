---
description: fields on test doubles or table cases that are populated but never asserted on or consumed
severity: warning
include: ["**/*_test.go", "**/*_test.py", "**/*.test.ts", "**/*.test.js", "**/*_test.rb"]
---
Flag struct fields on test doubles and table-test case structs that are written but never read. A fake that records a call log nobody inspects, a case struct with a `wantErr` or `skip` column no branch consults, a recorded-arguments slice that only ever grows — each one advertises a guarantee the suite does not provide. A reader scanning the fake sees `reported []time.Time` and reasonably assumes some test checks what the collaborator was asked; none does. Compilers and standard analyzers do not catch this, because the field is assigned, and assignment is a use.

Two failure modes are worth separating in the report. A dead recording field on a fake usually means an assertion was deleted or never written, and the fix is either to assert on it or to drop the bookkeeping. A dead column in a table often means the cases disagree about what they are testing — one row was copied from a table where the column mattered — and it is a hint that the table is mixing unrelated behaviors and should be split.

Do not flag fields required to satisfy an interface or to construct a value, even if unread. Do not flag fields read by only some rows of a table when that is a deliberate optional column with a meaningful zero value — the objection is to a column no row reads. Do not flag fields consumed by a shared helper elsewhere in the package rather than at the call site; check the whole package before reporting. Do not flag recorded state that a `t.Cleanup` or teardown assertion inspects.

```go
// Flagged: `reported` accumulates every observation and no test ever reads it.
type backwardClock struct {
	now      time.Time
	reported []time.Time
	sleeps   []time.Duration
}

func (c *backwardClock) Now() time.Time {
	c.reported = append(c.reported, c.now)
	return c.now
}

// Flagged: no case body branches on `wantErr`.
tests := []struct {
	name    string
	args    []string
	wantErr bool
}{
	{name: "zero count", args: []string{"-n", "0"}, wantErr: true},
	{name: "negative count", args: []string{"-n", "-3"}, wantErr: true},
}
```

```go
// Spared: the recording exists because a test asserts against it.
type recordingClock struct {
	now      time.Time
	reported []time.Time
}

func TestMintsOnlyAtObservedInstants(t *testing.T) {
	clock := &recordingClock{now: start}
	ids := mint(t, clock, 4)
	for i, at := range decodedTimes(t, ids) {
		if !slices.ContainsFunc(clock.reported, at.Equal) {
			t.Errorf("output[%d] at %s was never reported by Now", i, at)
		}
	}
}
```
