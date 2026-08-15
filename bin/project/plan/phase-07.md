# Phase 7 — `bin/lint` + the committed tier configs, proven in `bin/bintest`

*Realizes design Decision 9 (`bin/lint` enforces the suite lint contract) —
the runner and config slice; the ship-gate slice is Phase 8.*

Build the lint gate's tooling half:

- **`bin/lint.d/cheap.yml`** and **`bin/lint.d/strict.yml`** — the two
  complete golangci-lint v2 configs realizing the contract's
  (`root project/design/D30.md`) rule lists and thresholds verbatim, findings
  uncapped, analyzers excluding `_test\.go` via per-linter path rules so
  gofumpt still covers test files.
- **`bin/lint`** — the bash runner per D9: version gate first (pinned
  golangci-lint, refuse anything else before linting), `LINT_REPO_ROOT` env
  seam (inert when unset), configs resolved script-relative from
  `bin/lint.d/`, module set from the target root's `go.work`, tier from
  `<tree>/.lint-tier` (absent = `off`, report-only; invalid content and
  unknown tree are loud errors), sequential no-argument scoreboard, exit
  semantics per the contract.
- **`bin/bintest` tests** tagging the eight runner-side ids below, each
  execing the real `bin/lint` against `t.TempDir()` fixture repo roots (own
  `go.work`, tiny modules authored with/without specific findings,
  `.lint-tier` variants); the version-mismatch test uses a `PATH`-prepended
  stub `golangci-lint` reporting a wrong version. The real pinned
  golangci-lint on `PATH` is a stated precondition (Conventions); its absence
  fails loudly, never skips.

**Done when:** each of these ids is tagged in `bin/bintest/*_test.go` above a
genuine test of its behavior — R-WW5B-L155, R-WXD7-YSVU, R-WYL4-CKMJ,
R-WZT0-QCD8, R-X10X-443X, R-X28T-HVUM, R-X3GP-VNLB, R-X4OM-9FC0 — and the
tree is green (`go test ./bintest/...` from `bin/` exits 0).
