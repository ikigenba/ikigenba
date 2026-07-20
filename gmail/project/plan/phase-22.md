# Phase 22 — Owner-id identity alignment (tests inject `X-Owner-Id`; health asserts `owner_id`)

*Realizes design Decision 1 (landing ignores identity), 2 (route wiring / the
id-keyed `RequireIdentity` gate) and 10 (MCP surface / chassis health envelope) —
currency touch-ups only, no new or changed Verification ids. No dependency on an
earlier pending phase (appkit's D13 id-keyed gate is already the shipped
chassis).*

This is gmail's slice of the suite-wide owner-id conversion
(`docs/owner-id-design.md`, plan phase 14). gmail keys on **nothing**: its
tables (`sync_state`, `outbox`) carry no owner columns, and every MCP tool
handler ignores its `server.Identity` argument. So this phase ships **no
migration and no schema change** — it aligns the test corpus and one display
assertion to the id-keyed identity contract:

- **The MCP test driver injects `X-Owner-Id`.** `internal/mcp/tools_test.go`'s
  `rpc` helper (the single request builder every `tools/list` and `tools/call`
  test flows through) sets `X-Owner-Id` as the caller's stable key, representing
  an authenticated caller under the new contract. It **keeps** `X-Owner-Email`
  and `X-Client-Id` on the request, because the health-envelope test asserts
  those display fields.
- **The health-envelope test asserts `owner_id`.** `TestHealth_Envelope`
  (`internal/mcp/tools_test.go`) additionally asserts the chassis health
  envelope's `owner_id` equals the injected `X-Owner-Id`, alongside its existing
  `owner_email`/`client_id` assertions (the chassis health tool gained `owner_id`
  in appkit's flip; D10).

Left deliberately unchanged: the D20 attachment-guard test
(`internal/gmail/attachment_test.go`, `R-8Q5R-R9T8`) keeps injecting
`X-Owner-Email` as the *ignored, caller-asserted* identity header — its point is
that the loopback guard keys on `X-Forwarded-Proto` alone and no longer admits on
a caller identity header, which is the exact discriminator against the retired
email predicate. The D4 nginx fragment already forwards all four owner headers
(`X-Owner-Id` included) and its tests already assert them; no change here.

## Done when

- `internal/mcp/tools_test.go`'s `rpc` helper injects the caller's id key:
  `grep -q 'X-Owner-Id' internal/mcp/tools_test.go` exits 0.
- `TestHealth_Envelope` asserts the health envelope's `owner_id`:
  `grep -q 'owner_id' internal/mcp/tools_test.go` exits 0.
- No owner columns / no migration added — the migrations directory file-set is
  **unchanged** (exactly the five committed files):
  `ls internal/db/migrations/ | sort` prints exactly
  `001_schema_migrations.sql`, `002_gmail.sql`, `003_outbox.sql`,
  `20260712185110_outbox_routing_columns.sql`,
  `20260712190007_outbox_routing.sql` — `ls internal/db/migrations/ | wc -l`
  is `5`.
- The suite is green (design *Conventions*): `cd gmail && go build ./...`,
  `cd gmail && go vet ./...`, `cd gmail && gofmt -l .` (no output), and
  `cd gmail && go test ./...` all succeed with zero failures.
