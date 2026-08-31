---
description: writes carrying the program's real output discard their error, so an i/o failure still reports success
severity: error
include: ["**/*.go"]
---
Flag writes that carry a program's primary result — the data a caller or user invoked it to obtain — whose error is explicitly thrown away, most often via `_, _ = w.Write(...)`, `_, _ = io.WriteString(...)`, `_, _ = fmt.Fprintf(...)`, or an unchecked `Flush`/`Close` on a buffered writer. The tell is that the surrounding function goes on to return success (nil, a zero exit code, an "ok" status) on a path where nothing was actually delivered. This is not an errcheck finding: assigning to blank identifiers silences mechanical linters, which is precisely why a human has to catch it. The realistic failure is not exotic — a full disk, or `prog | head -5` closing the pipe so every later write returns EPIPE — and the result is a truncated answer reported as a complete one. Flag the loop or function as a whole rather than every write inside it, and prefer flagging the site that decides the return value.

Do not flag discarded writes of diagnostics, usage text, logs, or progress chatter: there is nowhere useful to report a failure to report, and propagating it would be noise. Do not flag a discarded write when the same function already returns a failure for that path on other grounds, or when the writer is a `bytes.Buffer`, `strings.Builder`, or `httptest.ResponseRecorder` whose `Write` cannot fail. Do not flag test helpers. A single checked write at the end (`if _, err := w.Write(buf); err != nil`) satisfies the rule even if the code built its output with many discarded appends to an in-memory buffer first.

```go
// Flagged: every result line's write error is discarded, yet the function reports success.
func emit(w io.Writer, records []Record) int {
	for _, r := range records {
		_, _ = io.WriteString(w, r.String()+"\n")
	}
	return exitSuccess
}
```

```go
// Spared: the result is buffered, and both the write and the flush decide the outcome.
func emit(w io.Writer, records []Record) int {
	out := bufio.NewWriter(w)
	for _, r := range records {
		if _, err := io.WriteString(out, r.String()+"\n"); err != nil {
			return exitFailure
		}
	}
	if err := out.Flush(); err != nil {
		return exitFailure
	}
	return exitSuccess
}
```

```go
// Spared: diagnostics have no meaningful failure path of their own.
_, _ = io.WriteString(stderr, "prog: "+err.Error()+"\n")
```
