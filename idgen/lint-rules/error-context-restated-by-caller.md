---
description: caller-side message text repeats a phrase the wrapped error already carries, producing doubled context like "invalid id x: invalid id: ..."
severity: error
---
**Parked.** This rule is promoted to error severity but deliberately left out
of the `enable` allowlist in `.llm-lint.json`. Two runs against deepseek-v4-flash
returned no findings on `internal/cli/cli.go`, which renders
`idgen: invalid id <token>: invalid id: non-canonical format` — this rule's own
Flagged shape. Rewriting the rule for per-file judging did not move it. Re-enable
once llm-lint supports a per-rule model override, and judge this one with a
stronger model.

Flag places where a caller builds a message around an error whose own text already contains the same words. Each layer is supposed to add the context only it knows — the file name, the token, the request id — and let the inner error supply the rest; when the caller also restates the inner error's category, the rendered message stutters. Read the sentinel or the constructor that produced the error and compare it to the literal the caller prepends: if a sentinel is `errors.New("invalid id")` and the caller writes `"prog: invalid id " + token + ": " + err.Error()`, the user sees `prog: invalid id x: invalid id: non-canonical format`. The same smell appears in wrapping: `fmt.Errorf("failed to open config: %w", err)` where `err` is already `open /etc/app.conf: no such file or directory` yields a doubled "open". Prefer the caller contributing the identifier alone and letting `%w`/`%v` render the reason.

To find these, do not skim for repeated words. Enumerate every message the file renders to a user — each `Fprintf`, `WriteString`, or `Errorf` that embeds an error — and for each one write out the **full rendered line**, substituting what the inner error contributes. Then read that line aloud as a user would see it and ask whether it names the same fault twice. The stutter is obvious in the rendered string and nearly invisible in the source, because the two halves are written by different code.

**Judging the inner error's text.** The wording usually comes from elsewhere, and you may be looking at only one file. Use the strongest evidence available, in this order: the sentinel or constructor if it is in view; otherwise the error value's own name, which in Go conventionally spells its message — `ErrInvalidID` renders as `invalid id`, `ErrNotFound` as `not found`, `ErrPermissionDenied` as `permission denied`. A sentinel named for the same fault the caller's literal states is evidence of the stutter, not a reason to abstain: an identifier imported from a sibling package in this same module is as visible as one declared beside you, so flag it and name the inference you made. Abstain only for an error whose wording you genuinely cannot infer — one from a third-party dependency whose text is neither shown nor implied by its name.

Do not flag a caller that adds a genuinely different noun even if some word repeats incidentally — "reading manifest: invalid id" over an "invalid id" sentinel is legitimate layering, since the caller names the operation and the inner error names the fault. Do not flag a deliberate rephrasing that replaces rather than repeats the inner text, and do not flag messages that merely share a common prefix like the program name.

```go
// Flagged: ErrInvalidID's text is already "invalid id", so the rendered line says it twice.
var ErrInvalidID = errors.New("invalid id")
...
_, _ = io.WriteString(stderr, "prog: invalid id "+token+": "+err.Error()+"\n")
```

```go
// Flagged: the sentinel is declared in another package, so only its name is in
// view here — and `ErrInvalidID` spells "invalid id", the same phrase the
// caller prepends. Rendered: "prog: invalid id x: invalid id: non-canonical
// format". Flag it and say the name is what the reading rests on.
import "example.com/proj/internal/ids" // declares ErrInvalidID

if _, err := ids.TimeOf(token); err != nil {
	_, _ = io.WriteString(stderr, "prog: invalid id "+token+": "+err.Error()+"\n")
}
```

```go
// Spared: the caller adds only what it knows — the offending token — and the error supplies the reason.
fmt.Fprintf(stderr, "prog: %q: %v\n", token, err)
```
