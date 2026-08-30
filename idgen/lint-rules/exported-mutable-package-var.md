---
description: package-level state exported as a var when the value is a frozen constant no importer should mutate
severity: warning
include: ["**/*.go"]
---
Flag exported package-level `var` declarations whose values are effectively constants: epochs and other fixed reference points, lookup and translation tables, frozen format parameters, default instances. Any importer can assign to them, changing behavior for the entire process from an unrelated package, and nothing in the owning package can detect or guard it. Watch particularly for values that cannot be `const` in Go — `time.Time`, slices, maps, structs, compiled regular expressions — which land in a `var` by default rather than by decision. Recommend an unexported var behind an exported accessor, and for slices and maps an accessor that returns a copy, so the contract is enforced rather than merely documented.

Do not flag sentinel errors declared as `var ErrX = errors.New(...)`: the idiom requires a package var because callers compare identity through `errors.Is`. Do not flag vars deliberately exported as seams — a `var timeNow = time.Now` substituted in tests, a version string stamped by the linker at build time — nor vars that are genuinely intended as caller-tunable configuration and documented as such. Do not flag unexported package vars, which are already confined to the package that owns them.

Flagged:

```go
// Any importer can move the epoch and shift every timestamp computed from it.
var Epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
```

Spared:

```go
// Sentinel error: the exported var is the idiom, and errors.Is depends on it.
var ErrInvalid = errors.New("invalid input")

// Frozen value read through an accessor; callers cannot reassign it.
var epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func Epoch() time.Time { return epoch }
```
