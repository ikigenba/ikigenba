# Phase 1 — The bintest library-dependency conformance checks

*Realizes design Decision 6 (bintest proves `root project/design/D22.md`).*

One new test file in the existing `bin/bintest` Go module implementing the four
contract checks over the repo's committed module files: in-repo libraries
required at `v0.0.0` with matching relative replaces, plain-literal module
paths (parsed-vs-raw-bytes agreement), one repo-wide agentkit pin, and a
replace-free `go.work`. Discovery walks the repo root for every committed
`go.mod` (excluding `testdata/`); facts come from `go mod edit -json` /
`go work edit -json`, never raw-text grep. No new module, no new runner.

**Ordering note.** These checks assert repo-wide state that other trees'
pending work establishes: the telemetry tree's plain-literal `go.mod` fix and
the repos tree's agentkit pin alignment, plus the operator's authorized
`go.work` replace removal. Run this phase **after** those land — until then the
checks fail honestly against a nonconforming repo.

**Done when:**

- R-3R5W-79JK — a bintest test tagged with this id asserts every committed
  `go.mod` requiring `appkit`/`eventplane`/`registry` requires it at exactly
  `v0.0.0` with a `replace` to the relative sibling path, and fails on a tagged
  in-repo require or a missing/absolute replace.
- R-3SDS-L1A9 — a bintest test tagged with this id asserts every module path in
  every committed `go.mod` appears verbatim in the raw file bytes (no
  escape-carrying quoted module strings survive).
- R-3TLO-YT0Y — a bintest test tagged with this id asserts the set of distinct
  `github.com/ikigenba/agentkit` require versions across all committed `go.mod`
  files has size ≤ 1.
- R-3UTL-CKRN — a bintest test tagged with this id asserts the committed
  `go.work` parses with an empty/absent `Replace` list.
- `go test ./...` in `bin/bintest` exits 0.
