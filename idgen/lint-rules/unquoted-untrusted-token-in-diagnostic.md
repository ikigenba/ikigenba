---
description: user-supplied input interpolated raw into a diagnostic, letting whitespace or control characters corrupt the output stream
severity: warning
---
Flag diagnostics that splice attacker- or user-controlled text — argv entries, stdin tokens, request fields, file names, environment values — into a message without quoting or escaping: string concatenation, or `%s`/`%v` where `%q` belongs. A value containing a newline splits one diagnostic into two lines, which breaks anything downstream that counts or parses lines and lets a crafted argument forge what looks like an additional program message. Empty and whitespace-only values become invisible, so `prog: invalid prefix ` gives the user nothing to look at. Terminal escape sequences in the value are rendered by the console. Treat inconsistency within a file as strong evidence: when one message quotes its input with `strconv.Quote` or `%q` and a structurally identical message next to it concatenates raw, flag the raw one.

Do not flag interpolation of values the program itself produced — constants, enum names, numbers, durations, validated identifiers drawn from a closed set. Do not flag structured logging calls that pass the value as a discrete field, since the encoder quotes it. Do not flag messages already using `%q`, `strconv.Quote`, or an explicit escaping helper. Do not flag ordinary prose in an error returned for programmatic handling where no terminal renders it, and do not treat this as a security rule about injection into shells or SQL — those are separate concerns.

```go
// Flagged: token comes from argv; a newline in it forges a second diagnostic line.
_, _ = io.WriteString(stderr, "prog: bad token "+token+": "+err.Error()+"\n")
```

```go
// Spared: the untrusted value is quoted, matching the sibling diagnostics in this file.
fmt.Fprintf(stderr, "prog: bad token %q: %v\n", token, err)
```
