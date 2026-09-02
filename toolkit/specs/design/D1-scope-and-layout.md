# D1-scope-and-layout

`toolkit` is a Go library that gives consumers of
`github.com/ikigenba/ikigenba/agentkit` a standard set of local tools — `Bash`,
`Read`, `Write`, `Edit`, `Glob`, `Grep` — as ready-made `agentkit.Tool` values.
It is the sibling agentkit's D0 names for exactly this job: agentkit owns the
conversation loop and the tool seam, toolkit owns the tools. Module path
`github.com/ikigenba/ikigenba/toolkit`, its own `go.mod`, `go 1.26`, no
`go.work`; every command runs from this sub-project directory. It follows the
monorepo's house layout:

```
toolkit/                                (this sub-project; go.mod lives here)
├── AGENTS.md                           spec-driven build contract, gates
├── README.md
├── Makefile                            build test lint llm-lint fmt clean
├── go.mod                              go 1.26; pins agentkit by tag
├── .golangci.yml  .llm-lint.json  lint-rules/
├── specs/design/D<int>-<slug>.md       these documents
├── specs/loops/{gather,build,verify}.md + executable run
└── *.go                                the single package `toolkit`
```

The tools mirror the behavior of the Claude Code harness's own tools of the same
names. Where this design had to choose a behavior, it copied the harness tool
that corresponds most directly; where the harness behavior lives in the harness
rather than the tool — the working directory that persists across Bash calls,
the read-before-write gate on Write and Edit — toolkit deliberately leaves it to
the consumer. Every toolkit tool is stateless across calls.

## One package, six constructors

```go
package toolkit

func Bash(root string) (agentkit.Tool, error)
func Read(root string) (agentkit.Tool, error)
func Write(root string) (agentkit.Tool, error)
func Edit(root string) (agentkit.Tool, error)
func Glob(root string, opts ...GlobOption) (agentkit.Tool, error)
func Grep(root string, opts ...GrepOption) (agentkit.Tool, error)
```

Every constructor takes a **root** directory and returns `(agentkit.Tool,
error)`. The root is explicit — there is no empty-means-cwd sentinel — and it is
validated once, at construction, so a bad root is a constructor error the
consumer sees immediately rather than a per-call failure the model sees. For the
five file tools the root is a **confinement boundary**: every path argument is
resolved against it and must stay inside it. For `Bash` the root is only the
**starting directory** of each command; Bash is not confined and its description
says so. The consumer composes the tools it wants at the call site:

```go
read, err := toolkit.Read(project)
grep, err := toolkit.Grep(project, toolkit.WithSkip(".git"))
conv := agentkit.Config{Tools: []agentkit.Tool{read, grep}}
```

There is no `All` helper, no registry, and no `DeferredGroup` builder: toolkit
hands back tools and stops. Consumers compose it with other siblings themselves.

## Options are per tool and sealed

Options exist only where a tool has a knob. `Glob` and `Grep` share one:
`WithSkip`, extra gitignore-syntax patterns added to the walk filter (D3). Each
tool that takes options has its own option interface, sealed by an unexported
method, so passing an option to a tool it does not apply to is a **compile-time**
error, never a silent no-op. One option value may satisfy several interfaces —
`WithSkip` returns a `SkipOption` that is both a `GlobOption` and a `GrepOption`
— the pattern OpenTelemetry uses for instrument options. Tools without options
(`Bash`, `Read`, `Write`, `Edit`) take no variadic parameter at all; one is added
when an option for that tool is designed.

```go
type GlobOption interface{ applyGlob(*globConfig) }
type GrepOption interface{ applyGrep(*grepConfig) }

// SkipOption is a GlobOption and a GrepOption.
type SkipOption struct{ /* unexported */ }
func WithSkip(patterns ...string) SkipOption
```

## Conventions every tool shares

- **Names**: tool names are TitleCase (`Bash`, `Read`, …); schema field names
  are snake_case (`file_path`, `old_string`, `replace_all`), matching the harness
  tools the model already knows. `Grep` keeps the harness's flag-shaped names
  (`-i`, `-n`, `-A`, `-B`, `-C`).
- **Validation lives in the schema** where the schema can express it: `required`
  fields, numeric floors and ceilings, non-blank strings. agentkit validates
  arguments once before `Call` (its D11); toolkit checks only what a schema cannot
  say (a path escapes the root, a match is not unique).
- **Errors are in-band**: a `Call` error becomes an `IsError` tool result the
  model reads and reacts to (agentkit D12). Error text is therefore written for the
  model: it names the argument and the offending value.
- **Every result is capped** at 30,000 characters. The cap is toolkit's own
  constant (agentkit refuses to export one, D17) and is unexported. A truncated
  result ends with an explicit marker so the model knows it saw a prefix.
- **Root confinement**: a path argument may be absolute or relative. A relative
  path is joined to the root. The result is cleaned and its longest existing
  prefix is resolved through symlinks; the resolved path must equal the root or
  lie beneath it. The existing-prefix rule is what lets `Write` create a file that
  does not exist yet while still refusing a symlink that points outside.

## Dependencies

- `github.com/ikigenba/ikigenba/agentkit` — the tool seam. Pinned by tag.
- `github.com/boyter/gocodewalker` — the gitignore-aware tree walk behind
  `Glob` and `Grep` (D3). Chosen because nested `.gitignore` semantics are hard to
  get right and this library is validated against a shared gitignore test suite.
- `github.com/bmatcuk/doublestar/v4` — `**` glob matching for `Glob`'s pattern
  and `Grep`'s `glob` filter. The standard `filepath.Match` has no `**`.

## REQUIREMENTS

- R-C1N8-ZFDG: The module MUST be `github.com/ikigenba/ikigenba/toolkit` with `go 1.26` in its own `go.mod` and no `go.work`, and it MUST require `github.com/ikigenba/ikigenba/agentkit v0.1.0`, `github.com/boyter/gocodewalker v1.5.1`, and `github.com/bmatcuk/doublestar/v4 v4.10.0`.
- R-C2V5-D745: Package `toolkit` MUST export `func Bash(root string) (agentkit.Tool, error)`.
- R-C431-QYUU: Package `toolkit` MUST export `func Read(root string) (agentkit.Tool, error)`.
- R-C5AY-4QLJ: Package `toolkit` MUST export `func Write(root string) (agentkit.Tool, error)`.
- R-C6IU-IIC8: Package `toolkit` MUST export `func Edit(root string) (agentkit.Tool, error)`.
- R-C7QQ-WA2X: Package `toolkit` MUST export `func Glob(root string, opts ...GlobOption) (agentkit.Tool, error)`.
- R-C8YN-A1TM: Package `toolkit` MUST export `func Grep(root string, opts ...GrepOption) (agentkit.Tool, error)`.
- R-CA6J-NTKB: Package `toolkit` MUST export the interfaces `GlobOption` and `GrepOption`, each sealed by an unexported method so no type outside `toolkit` can satisfy them.
- R-CBEG-1LB0: Package `toolkit` MUST export `type SkipOption` and `func WithSkip(patterns ...string) SkipOption`, and `SkipOption` MUST satisfy both `GlobOption` and `GrepOption`.
- R-CCMC-FD1P: Package `toolkit` MUST NOT export any identifier other than `Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`, `GlobOption`, `GrepOption`, `SkipOption`, and `WithSkip`.
- R-CDU8-T4SE: The tools' `Name()` values MUST be exactly `Bash`, `Read`, `Write`, `Edit`, `Glob`, and `Grep` respectively, and every property name in every tool's `Schema()` MUST be lowercase snake_case or one of the `Grep` flag names `-i`, `-n`, `-A`, `-B`, `-C`.
- R-CF25-6WJ3: Every constructor MUST return a non-nil error, and a nil tool, when `root` is empty, does not exist, or is not a directory after symlink resolution.
- R-CGA1-KO9S: Every constructor MUST accept a relative `root`, resolving it against the process working directory at construction time, so that a later change of the process working directory does not move the tool's root.
- R-CHHX-YG0H: A path argument given to `Read`, `Write`, `Edit`, `Glob`, or `Grep` MUST be resolved by joining a relative path to the root, cleaning it, and resolving symlinks on its longest existing prefix; a resolved path that is neither the root nor beneath the root MUST make `Call` return an error whose text contains the argument name and the path as given.
- R-EUUW-QDX3: Every `Call` result string MUST contain at most 30,000 characters (Unicode code points) of tool output; when the output would be longer, the tool MUST return exactly its first 30,000 characters followed by a newline and the marker `[output truncated: showing 30000 of N characters]`, where N is the untruncated length, and nothing else.
- R-CL5N-3R8K: Every `Call` MUST return `("", err)` with a non-nil `err` on failure and `(text, nil)` on success; a tool MUST NOT return both a non-empty string and a non-nil error.
