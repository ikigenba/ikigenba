# Phase 17 — MCP tests inject `X-Owner-Id` under the flipped appkit gate

*Realizes design Decision 2 (route wiring / identity gate) and 1 (landing
handler) — identity-substrate alignment only; no new ids. Depends on no earlier
pending phase.*

This is notify's slice of the suite-wide owner-id conversion
(`docs/owner-id-design.md`, plan phase 12): a **test/identity-alignment
conversion only**. notify keys on nothing — it has no owner-scoped tables, so
there is **no migration, no schema change, and no owner column**. The `send`
handler ignores `Identity` (`_ server.Identity`), the landing page ignores
identity, and the crm/prompts consumer handlers read no owner from event
payloads (only webhooks embeds an owner, which notify does not consume). So no
non-test Go source changes.

The one change: appkit's shipped chassis (its Decision 13) flipped the identity
gate to refuse `401` on an empty `X-Owner-Id` and to stop gating on
`X-Owner-Email`. notify's `/mcp` route is wrapped in `rt.RequireIdentity`
(D2), so every gated JSON-RPC test that drives `/mcp` must now present
`X-Owner-Id`. The shared `rpc` test helper in `internal/mcp/tools_test.go`
today injects only `X-Owner-Email` + `X-Client-Id`; it gains `X-Owner-Id`
(keeping `X-Owner-Email` as a display header). This re-greens the whole gated
MCP surface — `tools/list`, `tools/call`, and the `send` happy/validation/
upstream paths — without changing any behavioral assertion. No `R-XXXX-XXXX`
id is added or retired; every notify id remains realized by its existing
tagged test, now transiting the flipped gate.

**Done when:**

- The gated MCP test helper injects `X-Owner-Id`:
  `grep -c 'X-Owner-Id' internal/mcp/tools_test.go` is `>= 1`.
- No owner-id migration or schema change was introduced: the migrations
  directory file-set is unchanged — `ls internal/db/migrations/*.sql | wc -l`
  is exactly `2` (`001_schema_migrations.sql`, `002_feed_offset.sql`).
- The suite is green (design *Conventions*): `cd notify && go build ./...`,
  `cd notify && go vet ./...`, `cd notify && gofmt -l .` (no output), and
  `cd notify && go test ./...` all succeed with zero failures — in particular
  the gated MCP tests that carry R-4IBU-MT7Z, R-A918-YY6H, R-AA95-CPX6,
  R-ACOY-49EK, and R-ADWU-I159 pass through `rt.RequireIdentity` under the
  `X-Owner-Id` gate.
