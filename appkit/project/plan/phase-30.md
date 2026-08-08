# Phase 30 — Declare appkit's testing facts and prove the declaration

*Realizes design Decision 21 (adopt the suite testing-language contract).*

appkit's `AGENTS.md` **Tests** section is rewritten to declare, in the
vocabulary of `root project/design/D23.md`, exactly what D21 records:

- the **default-gate test command** (`go test ./...` from `appkit/`) and what
  "green" means (`go build ./...`, `go vet ./...`, `gofmt -l .` with no output,
  `go test ./...`);
- the **layers present**: hermetic and composed in the default gate, a manual
  layer in `project/appkit-verification.md`, and **no live layer**;
- the **environmental preconditions beyond the Go toolchain**: the `go` binary
  on `PATH` at test time (the composed boot smoke shells out to `go build`) with
  a populated module cache — a hard failure when absent, never a skip;
- the **GOWORK mode**: workspace mode via the repo-root `go.work` for test and
  vet, plus the isolated `GOWORK=off go build ./...` check.

Two tests are added to the appkit module (natural home: a new
`appkit/agents_test.go` in the root package, alongside the existing
`appkit_test.go`), each tagging its cited id:

- **R-O1AD-MRKW** — reads the committed `AGENTS.md` from disk (relative to the
  module root, not a fixture copy) and asserts its Tests section declares all
  four facts above: the default-gate command, the layer names present, the
  environmental precondition, and the GOWORK mode. It must fail if any one of
  the four is removed from the file.
- **R-O2IA-0JBL** — walks appkit's `*_test.go` files, skipping any that carry a
  `live` build constraint, and asserts **zero** occurrences of `t.Skip`,
  `t.Skipf`, or `t.SkipNow`. The needle is assembled from parts at runtime so
  the scan never matches its own source.

No appkit source outside the test files changes; no Decision's behavior moves.

**Done when:**

- `R-O1AD-MRKW` is tagged verbatim in an `appkit/**/*_test.go` file by a test
  that reads the real committed `AGENTS.md` and fails when any declared fact is
  missing.
- `R-O2IA-0JBL` is tagged verbatim in an `appkit/**/*_test.go` file by a test
  that scans the tree's non-live test sources and fails on any `t.Skip` variant.
- The appkit green bar passes: from `appkit/`, `go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), and `go test ./...` all exit 0 with zero failures.
- The isolated build check passes: from `appkit/`, `GOWORK=off go build ./...`
  exits 0.
- `grep -rl 'R-O1AD-MRKW' --include='*_test.go' appkit/` and the same for
  `R-O2IA-0JBL` each print at least one path.
