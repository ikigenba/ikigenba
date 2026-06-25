# Phase 5 — MCP tool surface (the four owner tools)

*Realizes design Decision 6 (MCP tool surface). Depends on Phase 1 (Store) and Phase 2 (Service Create/Rotate).*

Build the owner-facing management surface in `internal/mcp/`: the JSON-RPC `Handler`
served on the gated `POST /mcp` route (the gating itself is wired by the
composition root in Phase 6 via `rt.RequireIdentity`). `NewHandler` takes the
`*webhooks.Service`, the version/service strings, the `baseURL`
(`TrimSuffix(rt.ResourceID(), "mcp")`, trailing slash), the health closure, and
the `outbox.Registry` for the standard reflection tool. Tool names are bare verbs
(`toolPrefix == ""`).

Four domain tools, each scoped to the authenticated `OwnerEmail` (from
`X-Owner-Email`, guaranteed by `RequireIdentity`), alongside the suite-standard
`health` and `reflection` tools:

- `create {name?}` → `{name, trigger_url, secret}` (secret show-once;
  `trigger_url = baseURL + "in/" + name`); errors `duplicate` /
  `validation`(`field:"name"`).
- `list {}` → `[{name, created_at, last_triggered_at}]`, no secret/hash, owner-scoped.
- `delete {name}` → `{deleted:true}`; not-owned or missing → `not_found`.
- `rotate {name}` → `{name, trigger_url, secret}` (new show-once secret, same URL);
  not-owned or missing → `not_found`.

Sentinels map to the suite's closed error envelope (`ErrNameTaken`→`duplicate`,
`ErrInvalidName`→`validation`+`field:"name"`, `ErrNotFound`→`not_found`, else
`internal`). Owner comes only from the authenticated identity, never tool args.

End state: `cd webhooks && go build ./... && go vet ./... && go test ./...` green,
with the handler driven over `httptest` via JSON-RPC `tools/call` against a
real-SQLite `Service`, identity supplied as `X-Owner-Email`.

**Done when:** design D6's Verification ids are each covered by a genuine
handler+real-DB test and the suite is green —
- R-5Z8J-Y0YP — `create` (no name) → `trigger_url == baseURL+"in/"+name`, `secret`
  begins `ms_wh_`, persisted owned by the caller;
- R-60GG-BSPE — `list` returns exactly the caller's webhooks (name/created_at/
  last_triggered_at, no secret/hash), excluding another caller's;
- R-61OC-PKG3 — owner's `delete{name}` removes it from that owner's `list`;
- R-62W9-3C6S — `rotate{name}` returns a new one-time secret with the same
  `trigger_url`, differing from the `create` secret;
- R-6445-H3XH — `delete`/`rotate` by a non-owner each return `not_found` and mutate
  nothing (webhook still exists, original secret still verifies);
- R-65C1-UVO6 — duplicate name → envelope `code=="duplicate"`; invalid name →
  `code=="validation"` with `field=="name"`.
