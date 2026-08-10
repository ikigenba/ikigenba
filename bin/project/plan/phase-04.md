# Phase 4 — The changelog gate in `bin/bump`, proven in `bin/bintest`

*Realizes design Decision 8 (`bin/bump` enforces the changelog contract).*

`bin/bump` gains the changelog gate of `root project/design/D28.md`, placed
after the `--dry-run` early exit and before any write: refuse (loud non-zero
exit, `VERSION` unwritten, nothing committed or pushed) when
`<app>/CHANGELOG.md` is absent or its first `## ` heading's version token is
not exactly the version being minted, and on a match widen the path-limited
commit to exactly `<app>/VERSION` + `<app>/CHANGELOG.md`. The refusal message
tells the operator to author the top section for the version `--dry-run`
reports. Bump also gains the inert `BUMP_REPO_ROOT` env override (unset ⇒
behavior unchanged) so tests can point it at a fixture.

`bin/bintest` gains a changelog test file whose tests exec the real `bin/bump`
against `t.TempDir()` fixtures: a git repo on a branch named `main` with a
committed `<app>/VERSION` (plus per-case `CHANGELOG.md` variants) and a local
bare repo wired as `origin`, so the success path's push completes
network-free. No `t.Skip`, no network, no live tag, per Conventions.

**Done when:**

- Each of these ids appears verbatim as a tag comment in
  `bin/bintest/*_test.go` immediately above a genuine test asserting its
  behavior:
  - R-CKLX-X89X — absent `<app>/CHANGELOG.md`: nonzero exit, `VERSION`
    unchanged, no commit.
  - R-CLTU-B00M — first `## ` heading's version token differs from the
    version being minted: nonzero exit, `VERSION` unchanged, no commit.
  - R-CN1Q-ORRB — matching top heading: the single commit contains exactly
    `<app>/VERSION` and `<app>/CHANGELOG.md`.
  - R-CO9N-2JI0 — `--dry-run` prints the next version and writes, commits,
    and pushes nothing, even when the changelog is absent or mismatched.
- `go test ./bintest/...` from `bin/` exits 0 (the tree's green gate).
