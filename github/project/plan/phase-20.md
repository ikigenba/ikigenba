# Phase 20 — Declare github's testing facts and prove the declaration

*Realizes design Decision 14 (adopt the suite testing-language contract).*

github's `AGENTS.md` Tests section is rewritten to declare, in the vocabulary of
`root project/design/D23.md`, the four facts the contract requires: the exact
default-gate test command (`GOWORK=off go test ./...` from `github/`, with green
also meaning clean `GOWORK=off go build ./...`, `GOWORK=off go vet ./...`, and
`gofmt -l .`); the layers this tree has (**hermetic** and **composed** in the
gate — the composed member being the install-layout boot smoke in
`cmd/github/main_test.go` that builds and runs the real binary — plus a
**manual** layer whose committed runbook is `project/github-verification.md`,
where D2's `R-DMUT-QF4A` is proven by the operator out of gate; and that there is
no live layer); the one environmental precondition beyond the Go toolchain (the
`go` binary on `PATH` in the test process's environment, with the module cache
already resolving github's `replace` siblings — a hard failure when absent,
never a skip); and the tree's GOWORK mode (**`GOWORK=off`**, mirroring the
production build).

Two tests are added to `github/` carrying the adopted ids. The doc-truth test
reads the committed `AGENTS.md` from disk — the real file, not a fixture — and
fails if any of the four declarations is missing. The skip-ban scan walks the
tree's `*_test.go` files, excludes any file carrying the `live` build
constraint, and fails on any occurrence of `t.Skip`, `t.Skipf`, or `t.SkipNow`;
it assembles its needle from parts so it can never match its own source.

No shipped source, migration, template, or config changes, and no change to
`project/github-verification.md` — the runbook already carries the positive
check, the bad-key negative check, and the recording location the contract asks
for.

**Done when:**

- `R-O1AD-MRKW` is covered by a named test asserting github's committed
  `AGENTS.md` Tests section declares the default-gate test command, the layers
  present (hermetic, composed, manual with its runbook named), the `go`-on-`PATH`
  precondition, and the `GOWORK=off` mode.
- `R-O2IA-0JBL` is covered by a named test whose source scan over `*_test.go`
  files, excluding `live`-tagged files, finds zero `t.Skip`/`t.Skipf`/
  `t.SkipNow` occurrences.
- Both ids appear verbatim as tags in `*_test.go` files:
  `grep -rl 'R-O1AD-MRKW' --include='*_test.go' --exclude-dir=project .` and the
  same for `R-O2IA-0JBL` each print at least one path.
- The suite is green per design's *Conventions*: `GOWORK=off go build ./...`,
  `GOWORK=off go vet ./...`, `gofmt -l .` (no output), and
  `GOWORK=off go test ./...` all succeed with zero failures and no `SKIP`, from
  `github/`.
