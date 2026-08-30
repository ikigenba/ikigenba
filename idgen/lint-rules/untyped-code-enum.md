---
description: a closed set of related integer or string constants left untyped, so functions carrying them expose an unconstrained type
severity: warning
include: ["**/*.go"]
---
Flag a group of related constants that form a closed set — exit codes, status or result codes, kinds, states, categories — declared without a named type, together with the functions that return or accept them as a bare `int` or `string`. The signature then says nothing: any integer is assignable, so a count, a length, or an index can be returned where a code was meant and the compiler will accept it, and no tool can check that a switch covers the set. Recommend declaring a named type, giving the constants that type, and using it in every signature that carries one, converting only at the boundary that demands the underlying type.

Do not flag constants whose type is dictated by an external contract — a value written to a wire format, a struct tag, or an argument to an API that takes the primitive — provided the conversion happens at that boundary and the internal plumbing is typed. Do not flag a single standalone constant, constants that are quantities rather than a closed set (buffer sizes, limits, retry counts, timeouts), sets already declared with a named type, or constants generated from an external schema.

Flagged:

```go
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

func Run(args []string) int { ... } // an int: indistinguishable from a count
```

Spared:

```go
type exitCode int

const (
	exitSuccess exitCode = 0
	exitFailure exitCode = 1
	exitUsage   exitCode = 2
)

func Run(args []string) exitCode { ... }

// conversion confined to the boundary that requires a plain int
os.Exit(int(Run(os.Args[1:])))
```
