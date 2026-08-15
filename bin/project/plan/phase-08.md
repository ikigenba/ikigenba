# Phase 8 — `bin/ship` refuses a red tree: the lint gate at release time

*Realizes design Decision 9 (`bin/lint` enforces the suite lint contract) —
the ship-gate slice. Depends on Phase 7.*

Wire the release-time refusal per D9: `bin/ship <svc>` runs `bin/lint <svc>`
immediately after its clone of `main` and before any build, with
`LINT_REPO_ROOT` pointed at the clone so the judged source is exactly the
build source. A non-zero lint aborts ship with no bundle produced and no box
contact; `--dry-run` runs the gate too. The `bin/bintest` test drives the
real `bin/ship` to its lint refusal against a fixture clone source, with no
network and no real build.

**Done when:** R-X5WI-N72P is tagged in `bin/bintest/*_test.go` above a
genuine test of the refusal (nonzero exit, no bundle staged) and of success
once the tree is clean at its tier, and the tree is green
(`go test ./bintest/...` from `bin/` exits 0).
