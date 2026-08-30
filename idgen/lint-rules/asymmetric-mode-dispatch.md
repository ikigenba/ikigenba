---
description: a dispatcher delegating one branch to a named function while inlining its sibling branch
severity: warning
---
Flag a function that chooses among peer modes or operations and handles one of them by delegating to a named helper while writing a sibling branch's body inline. Equally important alternatives then read at different altitudes: one is a single word, the other is a dozen lines of loop and condition, and nothing in the code says they are the same kind of thing. Object because the asymmetry hides the structure a reader needs, the inline branch accretes further logic precisely because it is already there, and changes to the two modes produce diffs that cannot be compared. Recommend extracting the inline branch to a sibling of the existing helper so the dispatcher reads as dispatch.

Do not flag branches that are genuinely not peers: a guard clause, an early return for empty input, an error path, or a short-circuit for a degenerate case is subordinate to the main path, not parallel to it. Do not flag a branch whose inline body is one or two statements, where extraction would buy indirection and nothing else. Do not flag dispatchers in which every branch is inline and comparably short, and do not flag a branch inlined because it needs several locals from the enclosing scope that a helper would have to take as parameters — though a large parameter list there is often itself the signal that the extraction is wanted.

Flagged:

```go
if *list {
	return runList(archive, stdout, stderr) // one mode: named
}
// the sibling mode: a dozen lines inline
var written int64
for _, name := range members {
	f, err := os.Open(name)
	if err != nil {
		fmt.Fprintf(stderr, "open %s: %v\n", name, err)
		return exitFailure
	}
	...
}
return exitSuccess
```

Spared:

```go
if *list {
	return runList(archive, stdout, stderr)
}
return runPack(archive, members, stdout, stderr)
```
