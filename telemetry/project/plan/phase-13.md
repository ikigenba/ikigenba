# Phase 13 — Create `telemetry/AGENTS.md` and prove its testing declarations in the gate

*Realizes design Decision 10 (adopt the suite testing-language contract).*

telemetry is the only deployable service in the suite with **no `AGENTS.md`**.
This phase creates it and lands the two tests that keep it true.

**`telemetry/AGENTS.md`** is a new file at the tree root, following the shape
every sibling service uses:

- what the service is — the suite's forensic record store under
  `/srv/telemetry/`, an appkit binary over SQLite, neither an event-plane
  producer nor a consumer, no web surface, no token logic (nginx is the sole
  trust boundary), module path `telemetry`;
- how changes are made — through the `project/` spec, direction-gated, the same
  paragraph the sibling trees carry;
- the layout — `cmd/telemetry` (composition root), `internal/record`,
  `internal/db`, `internal/ingest`, `internal/retention`, `internal/mcp`,
  `internal/e2e`, `etc/`, `project/`;
- a **Tests** section carrying the four declarations the contract requires: the
  default-gate command `go test ./...` run from `telemetry/` (green also meaning
  clean `go build ./...` and `go vet ./...`); the layers present — **hermetic**
  and **composed**, the composed tests being `internal/e2e/` (the real composed
  service over a loopback port, including restart survival) and the boot smoke in
  `cmd/telemetry/main_test.go` (the real binary against a temporary install
  tree), with **no live layer**; **no environmental preconditions beyond the Go
  toolchain**; and the GOWORK mode — telemetry's own `telemetry/go.work` for
  development, `GOWORK=off` for the production build;
- versioning — the committed `telemetry/VERSION` file, advanced with
  `bin/bump telemetry`, shipped with `bin/ship telemetry`.

**Two tests land in `cmd/telemetry/main_test.go`**, beside D9's layout and
manifest proofs. The doc-truth test reads `../../AGENTS.md` from disk (the
committed bytes, not an embedded copy) and asserts each of the four declarations
is present, naming the missing one on failure. The skip scan walks the
`telemetry/` tree for `*_test.go` files, excludes any file whose build
constraints include `live`, and asserts zero occurrences of `t.Skip`, `t.Skipf`,
or `t.SkipNow`; it assembles the needle from parts at runtime so the scanning
file is not its own first hit, and names the offending file and line on failure.

No existing test is renamed, moved, or re-layered; no schema, migration, or
runtime behavior changes.

**Done when:**

- `telemetry/AGENTS.md` exists at the tree root with a Tests section.
- `R-O1AD-MRKW` — a test in `cmd/telemetry/main_test.go` reads the committed
  `telemetry/AGENTS.md` from disk and asserts its Tests section declares the
  default-gate command, the layers present in the contract's layer names, the
  absence of environmental preconditions beyond the Go toolchain, and the GOWORK
  mode.
- `R-O2IA-0JBL` — a test in `cmd/telemetry/main_test.go` scans every `*_test.go`
  file in the tree outside live-tagged files and asserts zero `t.Skip` /
  `t.Skipf` / `t.SkipNow` occurrences, with the needle assembled from parts.
- The suite is green as design's Conventions define it: from `telemetry/`,
  `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0 with no
  failures.
- Both ids appear as tags in `cmd/telemetry/main_test.go`:
  `grep -c -E 'R-O1AD-MRKW|R-O2IA-0JBL' cmd/telemetry/main_test.go` reports `2`.
- The scan's own claim holds against the tree as it stands:
  `grep -rn -E 't\.Skip(f|Now)?\(' --include='*_test.go' --exclude-dir=project .`
  run from `telemetry/` returns no matches.
