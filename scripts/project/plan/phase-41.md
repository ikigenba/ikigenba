# Phase 41 — `describe` teaches the git-backed model

*Realizes design Decision 26 (its new slice: R-2ZVO-S8YU). Depends on Phase 37
and Phase 38.*

`internal/mcp/describe.go`'s `describeText` is revised in place so an authoring
agent learns what a script now is: a repository whose root `main.py` is the
entrypoint with modules beside it, `create`/`update` as commits, a run as a
checkout pinned to the commit it reports as `repo_sha`, the
`SUITE_REPO_KEY`/`SUITE_REPO_SHA`/`SUITE_GIT_TOKEN` environment, the
`repos:push/<kind>/<name>` trigger shape, and merging only through an explicit
`suite.mcp("repos", "merge", …)`. The existing sections stay truthful; the
one-stored-body framing goes.

**Done when:** the suite is green and this id is covered by a genuine test:

- R-2ZVO-S8YU — the `describe` text names `main.py` as the entrypoint, states
  the pinned checkout and `repo_sha`, names `SUITE_GIT_TOKEN` and
  `SUITE_REPO_SHA`, shows a `repos:push/` filter, states that merging happens
  only through an explicit `suite.mcp("repos", "merge", …)` call, and no longer
  describes the script as a single stored body — while `R-IOUA-M8K1`'s existing
  `suite`-module assertions still pass.
