# Phase 27 — Teardown: remove the v1 development plane and reduce the composition root

*Realizes design Decision 1 (composition root, structurally — its ids land in
Phase 38 once the v2 surfaces exist). No pending-phase dependency.*

repos v2 is a git custodian: it runs no agents, touches no GitHub, and consumes
no feed. This phase deletes everything that served the v1 development plane and
leaves a green, minimal service — chassis boot, the landing page, `/feed`, and
an MCP endpoint with only the chassis tools — that the following phases build
on. Nothing new is added here.

What is gone afterward:

- The packages `internal/runner/` and `internal/tools/` (the confined agent
  session engine), and their tests.
- In `internal/repos/`: `intake.go`, `protocol.go`, `ghpeer.go`, `reaper.go`,
  `git.go` (the clone/worktree custody), the session and repo-clone halves of
  `store.go`, `service.go`, `types.go`, and `events.go`, together with every
  test file for them.
- In `internal/db/`: the feed-consumer engine test and the `feed_offset` drift
  guard (repos consumes nothing). The committed migration files are **not**
  touched — they are immutable, and the v2 schema arrives as a new migration in
  Phase 28.
- In `internal/mcp/`: the v1 tool table, leaving the package wired to the
  chassis with no domain tools.
- In `cmd/repos/spec.go`: `Consumers`, the session-engine env reads, the model
  validation, the GitHub peer construction, and the `Workers`/`Health` bodies
  that counted sessions — replaced by a health reporter that reports zero
  repositories over the existing store handle.
- `go.mod`: the `github.com/ikigenba/agentkit` requirement and any provider
  dependency it pulled, with `go mod tidy` run.
- `etc/manifest.env`: the `CONSUMES=webhooks` line and the four session-engine
  extras.
- `.envrc`: the `ANTHROPIC_API_KEY` and `REPOS_GITHUB_ORG` export lines
  (`source_up` and `REPOS_STATE_DIR` stay).
- `AGENTS.md`: rewritten to describe the v2 service — no agent sessions, no
  GitHub, no consumer — so the tree's own description stops contradicting it.

`cmd/repos/nginx_test.go` and `cmd/repos/web_test.go` are **kept**: D10's
fragment and landing ids are still current and must stay green through this
phase.

**Done when:** all of the following, run from `repos/`, hold —

- `go build ./...`, `go vet ./...`, and `go test ./...` each exit 0, and
  `gofmt -l .` prints nothing.
- `test ! -e internal/runner && test ! -e internal/tools` succeeds.
- `grep -rIlE 'agentkit|GitHubPeer|HTTPTokenSource|REPOS_GITHUB_ORG|REPOS_PROVIDER|REPOS_MODEL|REPOS_SESSION_TTL|REPOS_MAX_SESSIONS|ANTHROPIC_API_KEY|webhooks|worktree' --exclude-dir=project --exclude-dir=.git --exclude-dir=state . ; test $? -eq 1` — the grep finds no file (exit status 1).
- `grep -c 'CONSUMES' etc/manifest.env` prints `0`.
- `go list -m all | grep -c agentkit` prints `0`.
