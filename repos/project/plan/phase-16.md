# Phase 16 — Rename the bot identity `ikibot` → `ikigenba` across source

*Realizes the renamed form of design Decisions 3 (intake / loop suppression), 5
(session branch naming), 6 (issue protocol / check gate), and 7 (session_start
branch validation) — a mechanical identifier rename that carries no new
Verification id. Depends on Phase 03, Phase 04, Phase 05, and Phase 07.*

The deployed GitHub App authenticates as `ikigenba[bot]`, so the design was
rewritten in place to state `ikigenba` as the single current identity: the bot
login default (D03), the `ikigenba/…` branch namespace (D05/D06/D07), and the
`.ikigenba/check` gate file (D06). This phase makes the source satisfy that
design — a pure token rename, no behavior shape changes, no schema/migration
change.

Observable end state, the four governed surfaces now read `ikigenba`:

- `internal/repos/intake.go` — `defaultBotLogin = "ikigenba[bot]"`, so an unset
  `REPOS_BOT_LOGIN` suppresses deliveries from the real bot account.
- `internal/mcp/tools.go` — `session_start` branch validation accepts
  `ikigenba/*` and rejects anything outside it (`branch must match ikigenba/*`).
- `internal/runner/runner.go` — the framing prompt names `.ikigenba/check`, the
  check-gate path is `<worktree>/.ikigenba/check`, and session branches are
  `ikigenba/session-<id>` (manual) and `ikigenba/issue-<N>[.k]` (issue).
- `internal/repos/events.go` — the doc-comment example branches read
  `ikigenba/…`.

Every test that asserts these constants moves with them (the intake default
fixture, the `session_start` validation cases, the runner branch/check-path
cases, the events and mcp fixtures), so the existing D03/D05/D06/D07 behaviors
stay proven under the renamed constants. `REPOS_BOT_LOGIN` remains an override;
only its default changes, so the box needs no override once this ships.

Out of scope (not loop-built source): `AGENTS.md` / `CLAUDE.md`'s `@ikibot`
prose is a hand-maintained doc handled as a separate named-file edit, and the
frozen `project/plan/` history legitimately keeps `ikibot` as the record of what
earlier phases built.

**Done when:**

- `grep -rIn 'ikibot' internal cmd` run from `repos/` prints nothing — every
  governed source and test occurrence has become `ikigenba` (a
  `project/`-excluded grep, so the append-only plan history is untouched and the
  check is reachable, not self-referential).
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `go test ./...` exit 0, `gofmt -l .` prints nothing, all from `repos/`) — in
  particular the intake loop-suppression test (R-ESK5-4RWJ) now judges the
  default against `ikigenba[bot]`, and the `session_start` validation, check
  gate, and PR-head branch tests (D06/D07) pass against `ikigenba/*` and
  `.ikigenba/check`.
