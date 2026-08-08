# Phase 47 — Declare sites' testing facts and prove the declaration

*Realizes design Decision 31 (adopt the suite testing-language contract).*

sites' `AGENTS.md` Tests section is rewritten to declare, in the vocabulary of
`root project/design/D23.md`, the four facts the contract requires: the exact
default-gate test command (`go test ./...` from `sites/`, with green also meaning
clean `go build ./...`, `go vet ./...`, and `gofmt -l .`); the layers this tree
has (**hermetic** and **composed**, and that it has no live layer and no manual
runbook); every environmental precondition beyond the Go toolchain (a
`google-chrome` binary on `PATH`, and the `go` binary on `PATH` in the test
process's environment with the module cache already resolving sites' `replace`
siblings — each a hard failure when absent, never a skip); and the tree's GOWORK
mode (workspace mode through the repo-root `go.work`; the production build forces
`GOWORK=off` and is not part of the gate).

Two tests are added to `sites/` carrying the adopted ids. The doc-truth test
reads the committed `AGENTS.md` from disk — the real file, not a fixture — and
fails if any of the four declarations is missing. The skip-ban scan walks the
tree's `*_test.go` files, excludes any file carrying the `live` build
constraint, and fails on any occurrence of `t.Skip`, `t.Skipf`, or `t.SkipNow`;
it assembles its needle from parts so it can never match its own source.

No shipped source, migration, template, or config changes.

**Done when:**

- `R-O1AD-MRKW` is covered by a named test asserting sites' committed
  `AGENTS.md` Tests section declares the default-gate test command, the layers
  present (hermetic, composed), the `google-chrome` and `go`-on-`PATH`
  preconditions, and the GOWORK mode.
- `R-O2IA-0JBL` is covered by a named test whose source scan over `*_test.go`
  files, excluding `live`-tagged files, finds zero `t.Skip`/`t.Skipf`/
  `t.SkipNow` occurrences.
- Both ids appear verbatim as tags in `*_test.go` files:
  `grep -rl 'R-O1AD-MRKW' --include='*_test.go' --exclude-dir=project .` and the
  same for `R-O2IA-0JBL` each print at least one path.
- The suite is green per design's *Conventions*: `cd sites && go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures.
