# Phase 30 — owner-id alignment (no migration, no schema)

*Realizes design Decision 12 (chassis MCP transport / health identity keys) and
2 (route wiring / identity gate) and 1 (landing ignores identity) — text/harness
true-ups only, no new behavior of dropbox's own. Depends on no earlier pending
phase (appkit's owner-id gate flip, appkit D13, is already landed in the
codebase).*

dropbox's slice of the suite-wide owner-id conversion
(`docs/owner-id-design.md`, suite plan phase 8). dropbox stores **no** owner
columns and keys **no** domain data on the owner (single-tenant): the identity
gate on `POST /mcp` and the `owner_id`/`owner_email`/`client_id` keys in the
`health` envelope are **chassis-owned** (appkit D9/D13), so this conversion mints
**no** dropbox requirement id and adds **no** migration. It is a code-comment and
test-harness true-up that makes dropbox's own source name the `X-Owner-Id`-keyed
contract already in force.

Observable end state:

- `cmd/dropbox/main.go` names the id-keyed identity contract: its header-trust
  comment and its `Health` reporter comment reference `X-Owner-Id` (the stable
  owner key) and note the chassis `health` tool adds `owner_id` beside
  `owner_email`/`client_id` — no comment still presents `X-Owner-Email` as *the*
  injected identity key.
- Tests that drive the identity-gated `POST /mcp` handler (`internal/mcp`'s
  request helpers; `cmd/dropbox`'s MCP-driving requests, if any) inject
  `X-Owner-Id` with `X-Client-Id`; display owner headers
  (`X-Owner-Email`/`-Name`/`-Picture`) are set only where a test asserts a
  display value. The loopback `/content`/`/list` guard tests (keyed on
  `X-Forwarded-Proto` only, D23) are unchanged.
- No file is added to `internal/db/migrations/`; no schema changes.

## Done when

Deterministic exit conditions (this is a structural/alignment phase — it carries
no `R-XXXX-XXXX` id of its own; the gate and health owner keys are proven by
appkit's own ids):

- **No new migration.** `internal/db/migrations/` contains exactly its six
  committed files and nothing more:
  `ls -1 dropbox/internal/db/migrations | wc -l` prints `6`.
- **Governed source names the id-keyed contract.**
  `grep -q 'X-Owner-Id' dropbox/cmd/dropbox/main.go` succeeds (the
  composition-root comments name the stable owner-id key).
- **Gated-route tests inject the id.**
  `grep -q 'X-Owner-Id' dropbox/internal/mcp/tools_test.go` succeeds (the
  `POST /mcp` request helpers carry `X-Owner-Id`).
- **Suite green**, per design's Conventions: `cd dropbox && go build ./...`,
  `cd dropbox && go vet ./...`, `cd dropbox && gofmt -l .` (no output), and
  `cd dropbox && go test ./...` all succeed with zero failures.
