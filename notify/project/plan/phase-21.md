# Phase 21 — Declare notify's testing facts and prove them in the gate

*Realizes design Decision 20 (testing-language conformance).*

notify's `AGENTS.md` currently declares its whole test suite as
`- Unit: go test ./...` — which is false in the contract's terms (the suite
includes a boot smoke that compiles and runs a real binary, and an event chain that
runs a real outbox, feed, and consumer loop) and silent about the facts
`root project/design/D23.md` requires every tree to state. This phase makes the
declaration true and makes it a checked fact rather than a comment.

**The `AGENTS.md` Tests section** is rewritten to declare, in the contract's
vocabulary, exactly the four facts D20 records:

- the **default-gate test command** — `go test ./...`, inside the green bar
  (`go build ./...`, `go vet ./...`, `gofmt -l .` silent, `go test ./...`);
- the **layers present** — `hermetic` and `composed`, and **no `live` layer and no
  `manual` layer**: hermetic for the push, MCP, db, consumer, and web package
  suites (the mock ntfy server is an `httptest` listener on `127.0.0.1`, never
  ntfy.sh) and the shipped-file guards (`etc/nginx.conf`, `etc/manifest.env`, the
  loopback guard), composed for the boot smoke in `cmd/notify/main_test.go`;
- the **environmental preconditions beyond the Go toolchain** — none, and in
  particular no test reads or changes behavior on `NTFY_TOPIC` / `NTFY_API_KEY`;
- the **GOWORK mode** — workspace (the production build's `GOWORK=off` is
  `bin/ship notify`'s, not the gate's).

**The two tests** land in a new `cmd/notify/docs_test.go`, in the package that
already owns notify's read-from-disk assertions over shipped artifacts:

- the **doc-truth test** reads the committed `notify/AGENTS.md` from disk and
  asserts its Tests section declares each of the four facts above, so a future edit
  that drops or contradicts one fails the gate;
- the **skip-ban source scan** walks the `notify/` tree's `*_test.go` files from
  disk, excludes any file carrying the `live` build constraint, and fails on any
  occurrence of `t.Skip`, `t.Skipf`, or `t.SkipNow`. The needle is assembled from
  parts at runtime so the scan can never match its own source — a scan that matches
  itself is vacuous.

No source, schema, or config outside `AGENTS.md` and that one new test file
changes. The tree is already skip-free, so the scan passes on arrival; it exists to
keep it that way. Adding a live layer against real ntfy.sh later is a separate
Decision and phase, not part of this one.

**Done when:**

- `R-O1AD-MRKW` is tagged by a genuine test in `cmd/notify/docs_test.go` that reads
  the committed `notify/AGENTS.md` from disk and asserts the Tests section declares
  the default-gate test command, the layers present (hermetic and composed; no
  live, no manual), that there are no environmental preconditions beyond the Go
  toolchain, and the GOWORK mode (workspace).
- `R-O2IA-0JBL` is tagged by a genuine test in `cmd/notify/docs_test.go` that scans
  the tree's `*_test.go` files excluding live-tagged ones and asserts zero
  occurrences of `t.Skip`, `t.Skipf`, and `t.SkipNow`, with the needle assembled
  from parts.
- The green bar passes: `cd notify && go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), and `go test ./...` all succeed with zero failures.
- Both ids appear verbatim as tags in `notify/*_test.go`:
  `grep -rhoE 'R-(O1AD-MRKW|O2IA-0JBL)' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l`
  prints `2`.
