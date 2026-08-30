---
description: one function performing argument parsing, validation, execution, and output formatting with natural extraction seams
severity: warning
---
Flag a single function that carries several distinct phases end to end — constructing and registering its inputs, dispatching among modes, validating, doing the work, and formatting the output — where the phases have visible seams. The seams are concrete: local variables that are live across exactly one stretch and dead afterward, blocks separated by blank lines or comment headers, and nested loops or conditionals whose bodies would read as early returns inside a helper. Say which phases the function is carrying and where the boundaries fall, and recommend extracting them so the entrypoint is left as wiring that a reader can take in at once.

Do not flag a long function that is one cohesive phase: a state machine, an exhaustive switch over a token or message type, a table of cases, a decoder loop, or generated code. Do not flag a function that is long only because of straight-line initialization or per-error wrapping with no phase boundary to cut on, and do not flag a short function merely because it does two things. Do not flag an entrypoint that is already only wiring — a long parameter list or a long list of helper calls is not this smell.

Flagged:

```go
func Run(args []string, out io.Writer) int {
	// phase 1: build and register every option, ten calls
	// phase 2: dispatch help, version, and mode
	// phase 3: two validations with their own diagnostics
	// phase 4: the work, as a nested loop
	// phase 5: format and write each result
}
```

Spared:

```go
func Run(args []string, out io.Writer) int {
	opts, code, ok := parseOptions(args, out)
	if !ok {
		return code
	}
	if err := opts.validate(); err != nil {
		return reportUsage(out, err)
	}
	return execute(opts, out)
}
```
