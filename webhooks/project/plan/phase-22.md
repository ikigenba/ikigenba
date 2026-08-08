# Phase 22 — Declare webhooks' testing facts and prove the declaration

*Realizes design Decision 20 (adopt the suite testing-language contract).*

webhooks' `AGENTS.md` Tests section is rewritten to declare, in the vocabulary of
`root project/design/D23.md`, the four facts the contract requires: the exact
default-gate test command (`go test ./...` from `webhooks/`, with green also
meaning clean `go build ./...`, `go vet ./...`, and `gofmt -l .`); the layers this
tree has (**hermetic** and **composed**, and that it has no live layer and no
tree-local manual runbook — the `internal/e2e` package name being an informal
alias, not a layer); every environmental precondition beyond the Go toolchain
(the `go` binary on `PATH` in the test process's environment with the module
cache already resolving webhooks' `replace` siblings, and a POSIX `bash` with
`grep` for the non-test-source guard — each a hard failure when absent, never a
skip); and the tree's GOWORK mode (workspace mode through the repo-root
`go.work`; the production build forces `GOWORK=off` and is not part of the gate).

Two tests are added to `webhooks/` carrying the adopted ids. The doc-truth test
reads the committed `AGENTS.md` from disk — the real file, not a fixture — and
fails if any of the four declarations is missing. The skip-ban scan walks the
tree's `*_test.go` files, excludes any file carrying the `live` build
constraint, and fails on any occurrence of `t.Skip`, `t.Skipf`, or `t.SkipNow`;
it assembles its needle from parts so it can never match its own source.

No shipped source, migration, template, or config changes. D7 and D8 were
rewritten in place to describe the substrates the gate actually runs; their
existing tests already assert those substrates and need no edit.

**Done when:**

- `R-O1AD-MRKW` is covered by a named test asserting webhooks' committed
  `AGENTS.md` Tests section declares the default-gate test command, the layers
  present (hermetic, composed), the `go`-on-`PATH` and `bash`/`grep`
  preconditions, and the GOWORK mode.
- `R-O2IA-0JBL` is covered by a named test whose source scan over `*_test.go`
  files, excluding `live`-tagged files, finds zero `t.Skip`/`t.Skipf`/
  `t.SkipNow` occurrences.
- Both ids appear verbatim as tags in `*_test.go` files:
  `grep -rl 'R-O1AD-MRKW' --include='*_test.go' --exclude-dir=project .` and the
  same for `R-O2IA-0JBL` each print at least one path.
- No test file references the `:8080` dev front door:
  `grep -rn '8080' --include='*_test.go' --exclude-dir=project .` prints
  nothing.
- The suite is green per design's *Conventions*: `cd webhooks && go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures.
