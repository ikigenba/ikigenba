# Phase 11 — Absolute state root (cwd-independent worktree paths)

*Realizes design Decision 4 (repo lifecycle & git custody), slice
R-C9CO-ODYU. Depends on Phase 02 and Phase 03.*

The service resolves its state root to an absolute path so a session's git
worktree is created and later inspected at the same location regardless of any
git invocation's working directory. A package-level `ResolveStateRoot(getenv)`
in `internal/repos` reads `REPOS_STATE_DIR` (default `state`) and returns it
via `filepath.Abs`; the composition root (`cmd/repos/spec.go`) calls it once
and hands the absolute result to `NewGit` (joined with `repos`) and to
`runner.Config.StateRoot`, replacing the inline relative default. Nothing below
the composition root re-reads the env or holds a relative root. No schema
change, no new migration, no product-visible change: sessions that previously
died at `git inspect commits: chdir …: no such file or directory` now build,
inspect, and push their worktree.

**Done when:**

- R-C9CO-ODYU is covered by a clearly-named test: `ResolveStateRoot` with
  `getenv` returning `""` yields an absolute path equal to `<cwd>/state`
  (`filepath.IsAbs` true); and, driving the real `git` binary from the resolved
  root, a `WorktreeAdd` followed by `HasCommits` succeeds when the process
  working directory is changed to a temp dir different from the canonical clone
  between creation and inspection — a test that reproduces the relative-root
  regression (nested worktree, inspection miss) fails, so the resolver is what
  the assertion turns on.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `go test ./...` exit 0, `gofmt -l .` prints nothing, all from `repos/`).
- Live acceptance (e2e proof, not a minted id): with the fix deployed to
  `int.ikigenba.com`, re-triggering the `execute` label on `ikigenba/agentrepl`
  issue #1 drives a session that builds its worktree, runs the agent, produces
  a commit, and opens a real pull request — the final agent-workspace handoff
  that previously failed now completes.
