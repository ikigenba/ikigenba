# Phase 10 — Declare eventplane's testing facts and prove the declaration

*Realizes design Decision 10 (adopt the suite testing-language contract).*

eventplane's `AGENTS.md` **Tests** section is rewritten to declare, in the
vocabulary of `root project/design/D23.md`, exactly what D10 records:

- the **default-gate test command** (`go test ./...` from `eventplane/`) and
  what "green" means (`go test ./...` and `go vet ./...` exit 0, `gofmt -l .`
  prints nothing);
- the **layers present**: hermetic only — no composed layer (eventplane builds
  no binary), no live layer, no manual runbook;
- the **environmental preconditions beyond the Go toolchain**: the `go` binary
  on `PATH` at test time (the `observe` import-discipline test execs
  `go list -deps`) with a populated module cache — a hard failure when absent,
  never a skip;
- the **GOWORK mode**: workspace mode via the repo-root `go.work`; `GOWORK=off`
  is deliberately **not** set for this tree.

Two tests are added to the module, each tagging its cited id. Natural home: a
new `eventplane/agents_test.go` in a small root-level package, or the existing
`routing` package if a root package is unwanted — either satisfies the tags, so
long as both run under the default gate.

- **R-O1AD-MRKW** — reads the committed `AGENTS.md` from disk (resolved relative
  to the module root, not a fixture copy) and asserts its Tests section declares
  all four facts above: the default-gate command, the layer names present, the
  environmental precondition, and the GOWORK mode. It must fail if any one of
  the four is removed from the file.
- **R-O2IA-0JBL** — walks eventplane's `*_test.go` files, skipping any that
  carry a `live` build constraint, and asserts **zero** occurrences of
  `t.Skip`, `t.Skipf`, or `t.SkipNow`. The needle is assembled from parts at
  runtime so the scan never matches its own source.

No eventplane source outside the test files changes, and `go.mod` gains no
`require` — both tests are stdlib-only, so the no-new-dependency Convention
holds.

**Done when:**

- `R-O1AD-MRKW` is tagged verbatim in an `eventplane/**/*_test.go` file by a
  test that reads the real committed `AGENTS.md` and fails when any declared
  fact is missing.
- `R-O2IA-0JBL` is tagged verbatim in an `eventplane/**/*_test.go` file by a
  test that scans the tree's non-live test sources and fails on any `t.Skip`
  variant.
- The eventplane green bar passes: from `eventplane/`, `go test ./...` and
  `go vet ./...` exit 0 with every package passing, and `gofmt -l .` prints
  nothing.
- `grep -rl 'R-O1AD-MRKW' --include='*_test.go' eventplane/` and the same for
  `R-O2IA-0JBL` each print at least one path.
