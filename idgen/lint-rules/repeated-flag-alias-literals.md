---
description: short and long option aliases registered by repeating the default value and help text at each call
severity: warning
---
Flag option registration in which a short form and a long form of the same option are registered by two separate calls that each restate the same default value and the same description. Each option's default then exists in two places with nothing tying them together, so changing one and missing the other gives an option whose behavior depends on which spelling the user typed — a defect the type checker cannot see and a test that exercises only one spelling will not catch. Count the repetition: five options registered this way means ten call sites and ten duplicated literals. Recommend one small helper that takes the name, the alias, the default, and the usage string and performs both registrations, so each literal appears once.

Do not flag a parser that natively accepts several names in a single registration call, which is the fix rather than the smell. Do not flag options registered once with no alias, and do not flag two registrations that deliberately differ — a deprecated spelling with a different default, an alias that binds a different variable, or a hidden alias with its own description. Repetition of the option name itself across the pair is unavoidable and is not the objection; the duplicated default and description are.

Flagged:

```go
count := flags.Int("n", 1, "number of items to emit")
flags.IntVar(count, "number", 1, "number of items to emit")
prefix := flags.String("p", "R", "identifier prefix")
flags.StringVar(prefix, "prefix", "R", "identifier prefix")
```

Spared:

```go
count := intFlag(flags, "n", "number", 1, "number of items to emit")
prefix := stringFlag(flags, "p", "prefix", "R", "identifier prefix")
```
