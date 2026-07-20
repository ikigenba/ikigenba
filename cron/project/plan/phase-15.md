# Phase 15 — Owner-id test/identity alignment (no schema, no owner columns)

*Realizes design Decision 10 (MCP tool-table harness) — no ids. Depends on no
earlier pending phase (the appkit identity flip, suite phase 1, is already
shipped and is simply the current chassis).*

cron's slice of the suite-wide owner-id conversion (`docs/owner-id-design.md`,
suite phase 13) is a **pure test/identity alignment**: cron stores **no owner
column**, keys every schedule on `name` (never on the caller), reads `Identity`
for **no** logic, and renders **no** owner display. It therefore ships **no
migration and no schema change**, and mints **no** requirement id.

The only code touch is the MCP behavioral harness. Update
`cron/internal/mcp/tools_test.go` so it injects the caller identity through the
**`X-Owner-Id`** header — the stable owner key the shipped appkit gate keys on
(D10's harness note) — in place of relying on `X-Owner-Email` as the identity
substrate. `X-Client-Id` stays. Because the harness drives the assembled
`appkit/mcp` handler directly (not through `rt.RequireIdentity`) and cron's tools
ignore `Identity`, the assertions are unchanged; this only aligns the harness
with the current identity contract. No production Go source, migration, or schema
changes.

**Done when:**

- `internal/mcp/tools_test.go` injects the identity via `X-Owner-Id`:
  `grep -c 'X-Owner-Id' cron/internal/mcp/tools_test.go` is `≥ 1`, and the test
  no longer sets `X-Owner-Email` as its identity header
  (`grep -c 'X-Owner-Email' cron/internal/mcp/tools_test.go` is `0`).
- The migrations file-set is **unchanged** — `internal/db/migrations/` holds
  exactly `001_schema_migrations.sql`, `002_crontab.sql`, `003_outbox.sql`, and
  `20260712160651_outbox_routing.sql` (four files; `ls internal/db/migrations/ |
  wc -l` is `4`), with no new or edited migration.
- The suite is green: `cd cron && go build ./...`, `cd cron && go vet ./...`,
  `cd cron && gofmt -l .` (no output), and `cd cron && go test ./...` all succeed
  with zero failures.
