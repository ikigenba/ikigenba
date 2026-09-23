---
description: short and long option aliases registered by repeating the default value and help text at each call
severity: error
---
Flag option registration in which a short form and a long form of the same option are registered by two separate calls that each restate the same default value and the same description. Each option's default then exists in two places with nothing tying them together, so changing one and missing the other gives an option whose behavior depends on which spelling the user typed — a defect the type checker cannot see and a test that exercises only one spelling will not catch. Count the repetition: five options registered this way means ten call sites and ten duplicated literals. Recommend one small helper that takes the name, the alias, the default, and the usage string and performs both registrations, so each literal appears once.

Do not flag a parser that natively accepts several names in a single registration call, which is the fix rather than the smell. Do not flag options registered once with no alias, and do not flag two registrations that deliberately differ — a deprecated spelling with a different default, an alias that binds a different variable, or a hidden alias with its own description. Repetition of the option name itself across the pair is unavoidable and is not the objection; the duplicated default and description are.

Flagged:

```go
jobs := flags.Int("j", 1, "number of parallel jobs")
flags.IntVar(jobs, "jobs", 1, "number of parallel jobs")
format := flags.String("f", "table", "output format")
flags.StringVar(format, "format", "table", "output format")
```

Spared:

```go
jobs := intFlag(flags, "j", "jobs", 1, "number of parallel jobs")
format := stringFlag(flags, "f", "format", "table", "output format")
```
