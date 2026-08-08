# Phase 02 — `bin/AGENTS.md` and the testing-declaration tests

*Realizes design Decision 7 (the testing-language contract). Depends on no
pending phase.*

`bin/` gains the tree doc it has never had, carrying the testing declaration the
suite contract requires, and `bin/bintest` gains one test file that keeps that
declaration honest and proves the default gate cannot skip.

What gets built:

- **`bin/AGENTS.md`** — a new committed tree doc: what `bin/` is (repo-root
  operator scripts plus the `bintest` proof tier), that it is spec-governed by
  `bin/project/` so its scripts are not hand-edited, that it is not versioned,
  and a **Tests** section declaring, in the contract's vocabulary, the
  default-gate test command `go test ./bin/bintest/...` run from the repo root;
  the layers present — `hermetic` for `bin/bintest`, `manual` for the bash
  orchestration tier, with no composed and no live layer; that there is **no**
  environmental precondition beyond the Go toolchain; and the GOWORK mode,
  workspace (via the repo-root `go.work`).
- **`bin/bintest/testing_contract_test.go`** — a new test file carrying both
  cited ids. Both are ordinary hermetic file reads resolved from the package
  directory's repo root, the same resolution D5 already uses for scripts; no
  build tags, no fixtures standing in for the real files.

**Done when:**

- `bin/AGENTS.md` exists and declares the four facts. Checked structurally from
  the repo root, `project/`-excluded, with exact match counts — each of the
  following prints `1`:
  - `grep -c -F 'go test ./bin/bintest/...' bin/AGENTS.md`
  - `grep -c -F 'GOWORK' bin/AGENTS.md` (the workspace-mode declaration)
  - `grep -ci -F 'hermetic' bin/AGENTS.md`
  - `grep -ci -F 'manual' bin/AGENTS.md`
- `R-O1AD-MRKW` — a test reads the committed `bin/AGENTS.md` from disk (not a
  fixture, not an embedded copy) and asserts its Tests section declares the gate
  command, the layer names present, the absence of extra preconditions, and the
  workspace GOWORK mode; deleting any one of those facts from `AGENTS.md` fails
  the test.
- `R-O2IA-0JBL` — a test scans every `*_test.go` under `bin/bintest`, excluding
  files whose build constraints include `live`, and asserts **zero** occurrences
  of `t.Skip`, `t.Skipf`, `t.SkipNow`; the needle is assembled from parts at
  runtime so the scanning file does not match itself (verified by the scan
  passing while that file is in range).
- This tree is green: `go test ./bin/bintest/...` from the repo root exits 0.
- Both ids appear verbatim as tags in `bin/bintest/*_test.go`:
  `grep -l 'R-O1AD-MRKW' bin/bintest/*_test.go` and the same for
  `R-O2IA-0JBL` each print exactly 1 path.
