# Phase 26 — Declare repos' testing facts and prove the declaration

*Realizes design Decision 16 (adopt the suite testing-language contract).*

repos' `AGENTS.md` Tests section — today a single line, `Unit: go test ./...` —
is rewritten to declare, in the vocabulary of `root project/design/D23.md`, the
four facts the contract requires: the exact default-gate test command
(`go test ./...` from `repos/`, with green also meaning clean `go build ./...`,
`go vet ./...`, and `gofmt -l .`); the layers this tree has (**hermetic** and
**composed**, and that it has no live layer and no tree-local manual runbook);
every environmental precondition beyond the Go toolchain (the **real `git`
binary** on `PATH`, which the never-mocked git-custody tests require, and the
`go` binary on `PATH` in the test process's environment with the module cache
already resolving repos' `replace` siblings and the pinned `agentkit` module —
each a hard failure when absent, never a skip); and the tree's GOWORK mode
(workspace mode through the repo-root `go.work`; the production build forces
`GOWORK=off` and is not part of the gate).

The `git` precondition is the substantive addition: it is stated today only
inside this design's *Conventions*, so a box set up from `AGENTS.md` alone would
fail the custody tests with no explanation.

Two tests are added to `repos/` carrying the adopted ids. The doc-truth test
reads the committed `AGENTS.md` from disk — the real file, not a fixture — and
fails if any of the four declarations is missing. The skip-ban scan walks the
tree's `*_test.go` files, excludes any file carrying the `live` build
constraint, and fails on any occurrence of `t.Skip`, `t.Skipf`, or `t.SkipNow`;
it assembles its needle from parts so it can never match its own source.

No shipped source, migration, template, or config changes.

**Done when:**

- `R-O1AD-MRKW` is covered by a named test asserting repos' committed
  `AGENTS.md` Tests section declares the default-gate test command, the layers
  present (hermetic, composed), both preconditions (the real `git` binary and
  the `go` binary), and the GOWORK mode.
- `R-O2IA-0JBL` is covered by a named test whose source scan over `*_test.go`
  files, excluding `live`-tagged files, finds zero `t.Skip`/`t.Skipf`/
  `t.SkipNow` occurrences.
- Both ids appear verbatim as tags in `*_test.go` files:
  `grep -rl 'R-O1AD-MRKW' --include='*_test.go' --exclude-dir=project .` and the
  same for `R-O2IA-0JBL` each print at least one path.
- The suite is green per design's *Conventions*: `cd repos && go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures.
