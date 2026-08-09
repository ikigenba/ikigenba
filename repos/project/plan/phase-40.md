# Phase 40 — Compose the state root from IKIGENBA_ROOT so production boots with no per-app env var

*Realizes design Decision 15 (env-channel conformance — state-root resolution
slice: R-QX3N-8GNH, R-QYBJ-M8E6, R-QZJG-004V) and the D14 boot-smoke update it
implies.*

The composition root (`cmd/repos/spec.go`) stops hard-requiring
`REPOS_STATE_DIR` and resolves the state root in the D15 order: an explicit
`REPOS_STATE_DIR` wins (resolved absolute); else `IKIGENBA_ROOT` composes
`filepath.Join(root, "repos", "state")`; else `serve` fails at startup with an
error naming both variables, exiting nonzero before any state directory is
created. The composed install-layout boot smoke in `cmd/repos/main_test.go`
drops `REPOS_STATE_DIR` from the child environment so the real binary proves
composition end to end: green health, git root created at
`<IKIGENBA_ROOT>/repos/state/git`. Dev behavior is unchanged (`.envrc` and
`bin/start` set the override); no migration, no schema change, no manifest
change.

**Done when:**

- R-QX3N-8GNH is covered: a hermetic `cmd/repos` test with an injected
  environment map proves the override wins when both variables are set and is
  resolved absolute when set alone.
- R-QYBJ-M8E6 is covered: the composed boot smoke supplies `IKIGENBA_ROOT`
  and no `REPOS_STATE_DIR`, reaches a green health check, and asserts the git
  root exists under `<IKIGENBA_ROOT>/repos/state/`.
- R-QZJG-004V is covered: a hermetic test with both variables unset proves the
  serve path fails with an error naming both `REPOS_STATE_DIR` and
  `IKIGENBA_ROOT` and creates no state directory.
- The suite is green per design Conventions: from `repos/`,
  `go build ./...`, `go vet ./...`, `gofmt -l .` silent, `go test ./...`.
