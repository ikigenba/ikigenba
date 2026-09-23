---
description: a zero value that is also a legal input is used to mean "not set", with correctness propped up by a second redundant guard
severity: error
include: ["**/*.go"]
---
Flag variables initialized to their zero value to mean "nothing yet" when that zero is itself a value the domain can legitimately produce — `0` for a Unix timestamp, an epoch offset, a price, an index, a count that may legally be zero; `""` for a name that may be empty; a zero `time.Time` where the clock may be arbitrary. The reliable tell is a second condition doing the real work nearby: a separate `first`/`i > 0` test, or a length check, that keeps the sentinel from being compared on the first pass. Two mechanisms then encode one state, and deleting either — an easy thing to do while refactoring, since each looks redundant in isolation — silently changes behavior for the inputs where the zero is real. Prefer a pointer, an `ok` companion boolean, an option type, or restructuring so the first iteration does not take the comparison at all.

Do not flag a zero value that is genuinely outside the domain — a zero id where ids start at 1, a nil slice, a zero-length buffer — nor accumulators where zero is the correct identity element. Do not flag a documented sentinel constant with a name (`const noDeadline = 0`). Do not flag `time.Time`'s zero used with `IsZero()`, which is the idiomatic absence test for that type. Do not flag when the guard and the sentinel are the same expression rather than two independent conditions.

```go
// Flagged: 0 is a real byte offset; only the separate i > 0 guard makes the
// first comparison safe, and either half looks removable on its own.
var previousOffset int64
for i, rec := range records {
	if i > 0 && rec.Offset <= previousOffset {
		return fmt.Errorf("record %d out of order", i)
	}
	previousOffset = rec.Offset
}
```

```go
// Spared: absence is represented distinctly, so one condition carries the whole meaning.
var previous *int64
for i, rec := range records {
	if previous != nil && rec.Offset <= *previous {
		return fmt.Errorf("record %d out of order", i)
	}
	current := rec.Offset
	previous = &current
}
```
