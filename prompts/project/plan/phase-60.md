# Phase 60 — Declare prompts' testing facts in `AGENTS.md` and prove them in the gate

*Realizes design Decision 50 (adopt the suite testing-language contract).*

Two tests land in `cmd/prompts/main_test.go`, beside the existing whole-tree
conformance proofs, and `prompts/AGENTS.md` gains the declaration they check.

**The `AGENTS.md` Tests section** is rewritten to declare, in the contract's
vocabulary: the default-gate command (`go test ./...` from `prompts/`); the
layers present — **hermetic** and **composed**, with the composed tests named as
the boot smokes in `cmd/prompts/main_test.go` and no live layer; that there are
**no environmental preconditions beyond the Go toolchain**; and the GOWORK mode
(workspace for development, `GOWORK=off` for the production build). The existing
import-guard sentence stays. This is the one file outside `project/` this phase
edits that is not a test.

**The doc-truth test** reads `../../AGENTS.md` from disk (the committed bytes,
not an embedded copy) and asserts each of those four declarations is present,
failing with the missing one named.

**The skip scan** walks the `prompts/` tree for `*_test.go` files, skips any file
whose build constraints include `live`, and asserts zero occurrences of
`t.Skip`, `t.Skipf`, or `t.SkipNow`. It assembles the needle from parts at
runtime so the scanning file is not its own first hit, and names the offending
file and line when it fails.

Nothing else changes: no test is renamed, no test moves layer, and no test file
under `internal/` is touched.

**Done when:**

- `R-O1AD-MRKW` — a test in `cmd/prompts/main_test.go` reads the committed
  `prompts/AGENTS.md` from disk and asserts its Tests section declares the
  default-gate command, the layers present in the contract's layer names, the
  absence of environmental preconditions beyond the Go toolchain, and the GOWORK
  mode.
- `R-O2IA-0JBL` — a test in `cmd/prompts/main_test.go` scans every `*_test.go`
  file in the tree outside live-tagged files and asserts zero `t.Skip` /
  `t.Skipf` / `t.SkipNow` occurrences, with the needle assembled from parts.
- The suite is green as design's Conventions define it: from `prompts/`,
  `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...`
  all succeed.
- Both ids appear as tags in `cmd/prompts/main_test.go`:
  `grep -c -E 'R-O1AD-MRKW|R-O2IA-0JBL' cmd/prompts/main_test.go` reports `2`.
- The scan's own claim holds against the tree as it stands:
  `grep -rn -E 't\.Skip(f|Now)?\(' --include='*_test.go' --exclude-dir=project .`
  run from `prompts/` returns no matches.
