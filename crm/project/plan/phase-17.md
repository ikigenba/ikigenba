# Phase 17 — Owner-id gate alignment: inject `X-Owner-Id` in the MCP test harness

*Realizes design Decision 2 (route wiring / gate substrate) and the design
Testing strategy's MCP-surface note. Depends on no pending crm phase — appkit's
shipped owner-id gate flip (D13) is already in the codebase.*

This is crm's slice of the suite-wide owner-id conversion (`docs/owner-id-plan.md`
phase 10): a **test/identity alignment only — no owner columns, no schema change,
no migration**. crm's domain rows are not owner-scoped (its MCP handlers take
`server.Identity` but ignore it), so nothing about crm's storage or verbs
changes. The only delta is that appkit's identity gate now keys on `X-Owner-Id`
alone (appkit D13): the `/mcp` mount is `rt.RequireIdentity`-gated, so a test
request carrying only `X-Owner-Email` is now refused `401` and every MCP test
that flows through the shared harness goes red.

The single fix site is the `rpc` helper in `crm/internal/mcp/tools_test.go`
(the one live-request seam every `tools/*` test uses): it must set `X-Owner-Id`
on the request so the chassis gate admits it. `X-Owner-Email` stays injected
(the health-envelope smoke asserts `owner_email` as a display value) alongside
`X-Client-Id`. No new crm requirement id is minted: the gate's own behavior
(401 on empty `X-Owner-Id`, id-only admission) is proven by the chassis
(appkit D13, `R-DDVL-DPVB` / `R-DF3H-RHM0` / `R-DGBE-59CP`) and must not be
re-proved here. The existing crm MCP ids that run through this harness
(D9–D13, D19: `R-PDZ7-HTAN`, `R-PF73-VL1C`, `R-PGF0-9CS1`, `R-PIUT-0W9F`,
`R-PK2P-EO04`, `R-PLAL-SFQT`, `R-PMII-67HI`, `R-MW1X-S9EV`, `R-5Y60-E30A`,
`R-5ZDW-RUQZ`, `R-60LT-5MHO`, `R-61TP-JE8D`, `R-631L-X5Z2`, `R-65HE-OPGG`,
`R-8IP7-FWJ5`) keep their behavior unchanged and simply return to green under
the fixed harness; their tags and assertions are untouched.

**Done when** (deterministic):
- The `rpc` harness injects the gated key:
  `grep -q 'Header.Set("X-Owner-Id"' crm/internal/mcp/tools_test.go` succeeds.
- No schema/migration change: `crm/internal/db/migrations/` still holds exactly
  its four committed files and nothing else —
  `ls -1 crm/internal/db/migrations/` prints exactly
  `001_schema_migrations.sql`, `002_crm.sql`, `003_outbox.sql`,
  `20260712160534_outbox_routing.sql` (four lines, no new file).
- The suite is green (design *Conventions*): `cd crm && go build ./...`,
  `cd crm && go vet ./...`, `cd crm && gofmt -l .` (no output), and
  `cd crm && go test ./...` all succeed with zero failures.
