# Phase 37 — Adopt agentkit v0.7.0 and its toolkit for the six standard sandbox tools

*Realizes design Decision 1 (module dependency) and 5 (built-in sandbox tools).*

Bump `prompts/go.mod` (and `go.sum`) to `github.com/ikigenba/agentkit v0.7.0`
and rewrite `prompts/internal/tools/` so the six standard coding tools —
`Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep` — come from
`github.com/ikigenba/agentkit/toolkit` instead of local implementations. End
state:

- `tools.All(sandboxRoot, sourcePortAllowed, share)` remains the single
  constructor and still returns exactly thirteen tools: `toolkit.All(sandboxRoot)`
  followed by the seven prompts-specific tools (`Fetch` + the six `File*`).
  The runner is untouched.
- The local implementations of the six standard tools (and their now-unused
  input structs, helpers, and name constants) are deleted from
  `internal/tools/tools.go`; the sandbox confinement helpers remain, used by
  `Fetch.dest_path`, `FileGet.dest_path`, and `FilePut.source_path`.
- Tests that exercised the deleted local implementations' internals are
  deleted with them (toolkit's own tests cover that behavior); the
  count/name test (R-F5X1-XH6C) is kept, the escape test (R-K1UK-T5K6) is
  rescoped to the three prompts-owned sandbox-path arguments, and a new test
  (R-GNY2-Y47H) proves the toolkit variants are wired: `Bash` on a nonzero
  exit returns a nil error with output ending in the `[exit status N]` marker.
- The framing prompt is deliberately unchanged (settled in D5).

**Done when:**

- `go build ./...` and `gofmt -l .` are clean from `prompts/`, and
  `go test ./...` is green (design Conventions).
- `grep -n 'agentkit v0.7.0' go.mod` matches exactly one line, and
  `grep -c 'agentkit v0.6.0' go.mod` reports 0.
- R-F5X1-XH6C, R-GNY2-Y47H, and R-K1UK-T5K6 each appear verbatim as a tag in
  a `*_test.go` file under `internal/tools/` (`--exclude-dir=project`), and no
  other `R-` id previously realized in the package is lost
  (`go test ./internal/tools/` green includes them).
