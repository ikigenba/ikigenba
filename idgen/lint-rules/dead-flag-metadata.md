---
description: help text or defaults passed to a parser whose usage output is fully overridden, so the strings can never be displayed
severity: error
---
Flag descriptions, usage strings, or placeholder metadata handed to a flag or argument parser when the program replaces the parser's own help rendering with hand-written text, so the supplied strings are never reachable on any code path. Check both routes to help output — the explicit help request and the parse-error path — since overriding the usage hook usually kills both. Object because the dead strings sit exactly where a reader expects the documentation to live: a maintainer updates them, the change has no effect, and the real help text drifts away from them unnoticed. Recommend either dropping the metadata to make the override explicit, or generating the hand-written block from the registrations so there is one source.

Do not flag a program whose parser help is actually used, including one that overrides the usage hook but still calls the parser's default printer inside it. Do not flag metadata consumed by something other than help rendering — shell completion, a machine-readable option dump, validation, or documentation generation — and do not flag a partially overridden usage that keeps per-option defaults. A deliberately empty description string is a choice, not this smell.

Flagged:

```go
flags.Usage = func() { io.WriteString(out, handWrittenUsage) } // parser help never renders
jobs := flags.Int("j", 1, "number of parallel jobs")          // string is unreachable
format := flags.String("f", "table", "output format")         // so is this one
```

Spared:

```go
flags.Usage = func() {
	io.WriteString(out, header)
	flags.PrintDefaults() // the descriptions below are still rendered
}
jobs := flags.Int("j", 1, "number of parallel jobs")
```
