---
description: a format or template hard-coding a timezone, unit, or currency suffix that nothing structurally guarantees the value carries
severity: warning
---
Flag output that asserts a value's frame of reference in literal text — a trailing `Z` or `UTC` in a timestamp layout, a `+00:00`, a `ms`/`s`/`KB` unit suffix, a currency symbol or code — where the correctness of that label depends on a conversion the format string cannot see. The common shape is a single expression that happens to convert and format together, so the label is true today by adjacency rather than by construction: move the formatting into a helper, format a value that arrived from elsewhere, or drop the conversion during a refactor, and the code emits a confidently mislabeled value that no test on the current call path will catch. Timestamps are the worst case because the output stays well-formed and plausible while silently shifting by hours. Prefer a layout that renders the actual offset (so a non-UTC value shows itself), a formatting helper that performs the conversion it advertises, or a type that can only hold values in the declared frame.

Do not flag a literal suffix on a value whose type or origin makes the frame unconditional — a duration type whose unit is fixed by the language, a field documented and enforced as canonical at its constructor, an amount carried in a currency-tagged type. Do not flag a layout that renders the offset dynamically rather than asserting one. Do not flag test helpers or golden expectations that state the intended frame on purpose. Do not flag log lines and human-facing chatter where an approximate label is harmless; reserve this for values that are parsed, stored, compared, or exchanged. A conversion in the immediately preceding statement, tied to the same variable, is enough to spare the site — the concern is a label with no conversion in view, or one separated from it by a function boundary.

```go
// Flagged: the layout promises UTC, but only the .UTC() sitting inside this one
// expression makes that true; any refactor that separates them lies silently.
_, _ = io.WriteString(out, instant.UTC().Format("2006-01-02T15:04:05.000Z")+"\n")

// Flagged: the "ms" is asserted by the caller, not by anything about d.
fmt.Fprintf(w, "took %d ms\n", d)
```

```go
// Spared: the conversion is the helper's stated job, so the label cannot drift
// away from it.
func formatUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Spared: the offset is rendered, so a non-UTC value is visible rather than mislabeled.
t.Format(time.RFC3339Nano)

// Spared: the unit comes from the value's own type.
fmt.Fprintf(w, "took %s\n", d) // time.Duration prints its unit
```
