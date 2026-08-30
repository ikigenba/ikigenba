---
description: a bare literal repeated for a quantity the codebase has already named as a constant
severity: warning
---
Flag a literal that restates a value the project has already given a name to, especially when the literal appears in several files while the named constant is barely used. This is worse than a plain magic number: the name exists, so a maintainer changing the constant reasonably believes they have changed the quantity everywhere, and the surviving literals fail silently rather than at compile time. Look for a structural quantity — a field width, a chunk size, a split point, a version, a limit — appearing as a slice bound in one place, a length comparison in another, and a repetition count inside a regular expression or format string in a third. Literals embedded in strings and patterns are the highest-value catch, since no compiler or type checker will ever connect them to the constant. Report the sites collectively and name the constant they should be using.

Do not flag literals that merely happen to share a value with an unrelated constant — the test is whether they denote the *same quantity*, not whether the numbers match. Do not flag structural literals with no independent meaning: `0`, `1`, and `-1` as loop bounds or offsets, `2` in a halving, indices into a fixed pair. Do not flag a literal in a golden vector, a wire-format fixture, or a serialization compatibility test, where the point is to pin the value independently of the constant that produces it — treating those as duplication turns the test tautological. Do not flag a literal where using the constant is impossible or worse, such as a struct tag or a constant expression in a context the language forbids, unless the code could reasonably build the string from the constant instead.

```go
// Flagged: the 4-4 split appears as bare literals in three files while
// bodyDigits, the constant that governs it, is referenced once.
const bodyDigits = 8

return prefix + "-" + body[:4] + "-" + body[4:]
...
if len(part) != 4 { return false }
...
regexp.MustCompile(`^[A-Z]+-[0-9A-Z]{4}-[0-9A-Z]{4}$`)
```

```go
// Spared: the split point is named and the pattern is built from it, so one
// edit moves every use.
const (
	bodyDigits = 8
	groupWidth = bodyDigits / 2
)

return prefix + "-" + body[:groupWidth] + "-" + body[groupWidth:]
...
regexp.MustCompile(fmt.Sprintf(`^[A-Z]+-[0-9A-Z]{%d}-[0-9A-Z]{%d}$`, groupWidth, groupWidth))
```

```go
// Spared: a golden vector deliberately pins the expected shape independently.
want := "R-0007-J3LA"
```
