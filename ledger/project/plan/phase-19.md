# Phase 19 — Declare ledger's testing facts and prove them in the gate

*Realizes design Decision 18 (testing-language conformance).*

ledger's `AGENTS.md` currently declares `- Unit: go test ./...` plus the isolated
build check. The first line is false in the contract's terms (the suite includes a
boot smoke that compiles and runs a real binary) and the section is silent about
the layers and preconditions `root project/design/D23.md` requires every tree to
state. This phase makes the declaration true and makes it a checked fact rather
than a comment.

**The `AGENTS.md` Tests section** is rewritten to declare, in the contract's
vocabulary, exactly the facts D18 records:

- the **default-gate test command** — `go test ./...`, inside the green bar
  (`go build ./...`, `go vet ./...`, `gofmt -l .` silent, `go test ./...`);
- the **layers present** — `hermetic` and `composed`, and **no `live` layer and no
  `manual` layer**: hermetic for the domain, db, MCP, and web package suites and
  the shipped-file guards (`etc/nginx.conf`, `etc/manifest.env`, the loopback
  guard), composed for the boot smoke in `cmd/ledger/main_test.go`;
- the **environmental preconditions beyond the Go toolchain** — none;
- the **GOWORK mode** — workspace for the gate, **plus** the retained isolated
  build check `GOWORK=off go build ./...`, which mirrors the production build's
  resolution and compiles without running anything.

**The two tests** land in a new `cmd/ledger/docs_test.go`, in the package that
already owns ledger's read-from-disk assertions over shipped artifacts:

- the **doc-truth test** reads the committed `ledger/AGENTS.md` from disk and
  asserts its Tests section declares each of the facts above — including **both**
  GOWORK facts, since a declaration naming only one would be incomplete on the tree
  whose distinguishing fact it is;
- the **skip-ban source scan** walks the `ledger/` tree's `*_test.go` files from
  disk, excludes any file carrying the `live` build constraint, and fails on any
  occurrence of `t.Skip`, `t.Skipf`, or `t.SkipNow`. The needle is assembled from
  parts at runtime so the scan can never match its own source — a scan that matches
  itself is vacuous.

No source, schema, or config outside `AGENTS.md` and that one new test file
changes. The tree is already skip-free, so the scan passes on arrival; it exists to
keep it that way.

**Done when:**

- `R-O1AD-MRKW` is tagged by a genuine test in `cmd/ledger/docs_test.go` that reads
  the committed `ledger/AGENTS.md` from disk and asserts the Tests section declares
  the default-gate test command, the layers present (hermetic and composed; no
  live, no manual), that there are no environmental preconditions beyond the Go
  toolchain, and the GOWORK mode — the workspace gate and the `GOWORK=off` build
  check.
- `R-O2IA-0JBL` is tagged by a genuine test in `cmd/ledger/docs_test.go` that scans
  the tree's `*_test.go` files excluding live-tagged ones and asserts zero
  occurrences of `t.Skip`, `t.Skipf`, and `t.SkipNow`, with the needle assembled
  from parts.
- The green bar passes: `cd ledger && go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), and `go test ./...` all succeed with zero failures.
- The isolated build check still passes: `cd ledger && GOWORK=off go build ./...`
  exits `0`.
- Both ids appear verbatim as tags in `ledger/*_test.go`:
  `grep -rhoE 'R-(O1AD-MRKW|O2IA-0JBL)' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l`
  prints `2`.
