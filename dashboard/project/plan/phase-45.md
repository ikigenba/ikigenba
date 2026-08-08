# Phase 45 — Declare the dashboard's testing facts and prove them in the gate

*Realizes design Decision 37 (testing-language conformance).*

The dashboard's `AGENTS.md` currently declares its whole test suite as
`- Unit: go test ./...` — which is false in the contract's terms (the suite
includes a boot smoke that compiles and runs a real binary) and silent about the
facts `root project/design/D23.md` requires every tree to state. This phase makes
the declaration true and makes it a checked fact rather than a comment.

**The `AGENTS.md` Tests section** is rewritten to declare, in the contract's
vocabulary, exactly the four facts D37 records:

- the **default-gate test command** — `go test ./...`, inside the green bar
  (`go build ./...`, `go vet ./...`, `gofmt -l .` silent, `go test ./...`);
- the **layers present** — `hermetic`, `composed`, and `manual`, and **no `live`
  layer**: hermetic for the package suites and the shipped-file guards
  (`etc/nginx.conf`, `etc/manifest.env`), composed for the boot smoke in
  `cmd/dashboard/main_test.go`, manual for the interactive Google/GitHub sign-in
  and the live apex routing exercised at deploy time;
- the **environmental preconditions beyond the Go toolchain** — none;
- the **GOWORK mode** — workspace (the production build's `GOWORK=off` is
  `bin/ship`'s, not the gate's).

**The two tests** land in `cmd/dashboard/docs_test.go`, beside the existing D6
doc-truth test and following its read-from-disk pattern:

- the **doc-truth test** reads the committed `dashboard/AGENTS.md` from disk and
  asserts its Tests section declares each of the four facts above, so a future
  edit that drops or contradicts one fails the gate;
- the **skip-ban source scan** walks the `dashboard/` tree's `*_test.go` files
  from disk, excludes any file carrying the `live` build constraint, and fails on
  any occurrence of `t.Skip`, `t.Skipf`, or `t.SkipNow`. The needle is assembled
  from parts at runtime so the scan can never match its own source — a scan that
  matches itself is vacuous.

No source, schema, or config outside `AGENTS.md` and that one test file changes.
The tree is already skip-free, so the scan passes on arrival; it exists to keep it
that way.

**Done when:**

- `R-O1AD-MRKW` is tagged by a genuine test in `cmd/dashboard/docs_test.go` that
  reads the committed `dashboard/AGENTS.md` from disk and asserts the Tests
  section declares the default-gate test command, the layers present (hermetic,
  composed, manual; no live), that there are no environmental preconditions beyond
  the Go toolchain, and the GOWORK mode (workspace).
- `R-O2IA-0JBL` is tagged by a genuine test in `cmd/dashboard/docs_test.go` that
  scans the tree's `*_test.go` files excluding live-tagged ones and asserts zero
  occurrences of `t.Skip`, `t.Skipf`, and `t.SkipNow`, with the needle assembled
  from parts.
- The green bar passes: `cd dashboard && go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), and `go test ./...` all succeed with zero failures.
- Both ids appear verbatim as tags in `dashboard/*_test.go`:
  `grep -rhoE 'R-(O1AD-MRKW|O2IA-0JBL)' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l`
  prints `2`.
