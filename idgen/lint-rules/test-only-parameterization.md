---
description: parameters or indirection on a helper whose only non-constant caller is a test, adding generality production never exercises
severity: error
---
Flag a function whose production call sites all pass the same fixed values — package constants, a single literal, one global — where the only varying arguments come from tests. The parameters exist so the test can drive the helper in isolation, but what they buy is a test of the helper's generality rather than of the behavior that ships: the case the test constructs can never occur, and the case that can occur is exercised only incidentally. Awkward parameter names invented to avoid shadowing the constants that are always passed are a reliable tell. Recommend either reading the fixed values directly and testing through the real entry point, or keeping the parameter because a second production caller genuinely needs it.

Do not flag dependency-injection seams that substitute a real collaborator — a clock, a filesystem, a random source, a network or database client, a logger, an output stream. There the production and test implementations are genuinely different implementations of one contract, and the seam is the design. Do not flag a parameter with more than one distinct production call site, a naturally general function driven by a table-driven test (a parser, an encoder, a pure computation), or a documented option on a public API meant for callers outside the module.

Flagged:

```go
// Called once in production, always with these two package constants.
func validateRingShape(ringSlots, ringReplicas int) { ... }

func init() { validateRingShape(slots, replicas) }

// The only caller that varies the arguments:
func TestValidateRingShape(t *testing.T) { validateRingShape(6, 9) }
```

Spared:

```go
// A real injected collaborator: production and tests supply different implementations.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

func Poll(interval time.Duration, clock Clock) error { ... }
```
