---
description: an infrastructure failure and a bad-input failure collapse into one status and one undifferentiated message, hiding that the work was truncated
severity: error
---
Flag handlers that give an I/O or transport failure — a read that died mid-stream, a broken connection, a scanner error, a timeout — the same outcome and the same wording as a malformed-input failure, so neither the user nor a calling script can tell them apart. The two mean opposite things: bad input means the tool did its whole job and rejected some items, while a read error means an unknown remainder of the input was never seen at all, and any partial output already emitted is silently incomplete. Watch for the shape where both paths set one shared `failed` flag or return the same exit code, and where the I/O branch prints only the bare error with no statement that processing stopped early. Related: a `bufio.Scanner` whose error is reported verbatim surfaces as `bufio.Scanner: token too long` for an over-long line, naming neither the input nor a remedy. Distinguish the two outcomes, and say explicitly that the stream ended early.

Do not flag a deliberate, documented mapping — an HTTP handler that answers 400 for both because the client cannot act on the difference, or a CLI whose contract genuinely defines a single non-zero code. Do not flag when the two paths share a code but emit clearly different messages that name the truncation. Do not flag a read error on an optional or best-effort source the code is documented to skip. Do not flag aggregation helpers that collect heterogeneous errors while preserving each one for the caller to inspect.

```go
// Flagged: a mid-stream read failure and a malformed token set the same flag and print alike,
// so a truncated run is indistinguishable from a complete one with rejects.
if err := scanner.Err(); err != nil {
	_, _ = io.WriteString(stderr, "prog: "+err.Error()+"\n")
	failed = true
}
```

```go
// Spared: the truncation is named and given its own outcome.
if err := scanner.Err(); err != nil {
	fmt.Fprintf(stderr, "prog: reading input stopped after %d tokens: %v\n", processed, err)
	return exitIOError
}
```
