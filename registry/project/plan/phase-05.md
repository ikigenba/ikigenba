# Phase 5 — Declare registry's testing facts and prove the declaration

*Realizes design Decision 4 (adopt the suite testing-language contract).*

registry's `AGENTS.md` **Tests** section is rewritten to declare, in the
vocabulary of `root project/design/D23.md`, exactly what D4 records:

- the **default-gate test command** (`GOWORK=off go test ./...` from
  `registry/`) and what "green" means (`GOWORK=off go build ./...` succeeds and
  `GOWORK=off go test ./...` passes with no failures and no skips);
- the **layers present**: hermetic only — no composed layer (registry builds no
  binary), no live layer, no manual runbook;
- the **environmental preconditions beyond the Go toolchain**: none — the
  package is pure, imports only the standard library, and its tests spawn no
  subprocess and reach no network address;
- the **GOWORK mode**: `GOWORK=off` forced, which is what proves the module
  resolves standalone with zero third-party dependencies.

Two tests are added to the existing package-local test home (`registry/`,
package `registry` — a new `agents_test.go` beside `registry_test.go`), each
tagging its cited id:

- **R-O1AD-MRKW** — reads the committed `AGENTS.md` from disk (resolved relative
  to the module root, not a fixture copy) and asserts its Tests section declares
  all four facts above: the default-gate command, the layer names present, the
  no-preconditions statement, and the `GOWORK=off` mode. It must fail if any one
  of the four is removed from the file.
- **R-O2IA-0JBL** — walks registry's `*_test.go` files, skipping any that carry
  a `live` build constraint, and asserts **zero** occurrences of `t.Skip`,
  `t.Skipf`, or `t.SkipNow`. The needle is assembled from parts at runtime so
  the scan never matches its own source.

Both tests are stdlib-only, so the zero-third-party-dependency Convention and
D1's `go list -deps` check hold unchanged. No non-test registry source changes.

**Done when:**

- `R-O1AD-MRKW` is tagged verbatim in a `registry/*_test.go` file by a test that
  reads the real committed `AGENTS.md` and fails when any declared fact is
  missing.
- `R-O2IA-0JBL` is tagged verbatim in a `registry/*_test.go` file by a test that
  scans the tree's non-live test sources and fails on any `t.Skip` variant.
- The registry green bar passes: from `registry/`, `GOWORK=off go build ./...`
  exits 0 and `GOWORK=off go test ./...` passes with no failures and no skips.
- `grep -rl 'R-O1AD-MRKW' --include='*_test.go' registry/` and the same for
  `R-O2IA-0JBL` each print at least one path.
