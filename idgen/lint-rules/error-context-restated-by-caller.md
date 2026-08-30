---
description: caller-side message text repeats a phrase the wrapped error already carries, producing doubled context like "invalid id x: invalid id: ..."
severity: warning
---
Flag places where a caller builds a message around an error whose own text already contains the same words. Each layer is supposed to add the context only it knows — the file name, the token, the request id — and let the inner error supply the rest; when the caller also restates the inner error's category, the rendered message stutters. Read the sentinel or the constructor that produced the error and compare it to the literal the caller prepends: if a sentinel is `errors.New("invalid id")` and the caller writes `"prog: invalid id " + token + ": " + err.Error()`, the user sees `prog: invalid id x: invalid id: non-canonical format`. The same smell appears in wrapping: `fmt.Errorf("failed to open config: %w", err)` where `err` is already `open /etc/app.conf: no such file or directory` yields a doubled "open". Prefer the caller contributing the identifier alone and letting `%w`/`%v` render the reason.

Do not flag a caller that adds a genuinely different noun even if some word repeats incidentally — "reading manifest: invalid id" over an "invalid id" sentinel is legitimate layering, since the caller names the operation and the inner error names the fault. Do not flag when you cannot see the inner error's text (an error from a third-party package whose wording you are guessing at); require the sentinel or constructor to be visible in the file or package under review. Do not flag a deliberate rephrasing that replaces rather than repeats the inner text, and do not flag messages that merely share a common prefix like the program name.

```go
// Flagged: ErrInvalidID's text is already "invalid id", so the rendered line says it twice.
var ErrInvalidID = errors.New("invalid id")
...
_, _ = io.WriteString(stderr, "prog: invalid id "+token+": "+err.Error()+"\n")
```

```go
// Spared: the caller adds only what it knows — the offending token — and the error supplies the reason.
fmt.Fprintf(stderr, "prog: %q: %v\n", token, err)
```
