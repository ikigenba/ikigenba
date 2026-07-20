# Phase 16 — Owner-id keying: log fields gain owner_id; identity reads follow the widened Identity

*Realizes design Decision 4 (the MCP tool surface). No dependency on an earlier
pending phase.*

The suite is converting from email-keyed to id-keyed ownership
(`docs/owner-id-design.md`); appkit's chassis has already flipped
`server.Identity` to the five-field shape gated on `X-Owner-Id` (appkit D13).
github stores no owner columns, so this is a **code-only** conversion — no
migration, no schema change, the `internal/db/migrations/` file set is
unchanged.

What changes in `internal/mcp` (and the tests that exercise it):

- `logWrite` emits `owner_id` (from `Identity.OwnerID`) beside the existing
  `owner_email`, `client_id`, `verb`, and target on every write-verb provenance
  line. Both owner fields are logged (Decision 5: keep email, gain id); the
  GitHub request body still carries no owner-identifying field (D3, unchanged).
- Handlers keep reading identity from the `server.Identity` value the transport
  threads; that value is now the widened chassis shape gated on `X-Owner-Id`.
- Tests inject `X-Owner-Id` on every gated request (the appkit gate now refuses
  a request lacking it), keeping `X-Owner-Email` only where a display value is
  asserted, and the write-log tests additionally assert the logged `owner_id`.

No MCP result shape changes (github is bot-only and exposes no owner in tool
results); the health envelope's `owner_id` is chassis-owned (appkit) and not
re-asserted here.

**Done when** the following ids are covered by clearly-named `*_test.go` tests
carrying their `// R-XXXX-XXXX` tag and the suite is green:

- `R-X3XX-6BNN` — a write verb's provenance `slog` line carries `owner_id`
  (from a distinct injected `X-Owner-Id`) beside `owner_email`; a build logging
  only `owner_email` fails.
- `R-EIK7-OGEC` — handlers read caller identity from the request headers
  threaded as the widened `server.Identity` (gated on `X-Owner-Id`), no
  bearer/token parsing of their own.
- `R-EJS4-2851` — each write verb emits exactly one provenance log line
  (`owner_email`, verb, target); the produced GitHub request carries no
  owner-identifying field; a read verb emits none.

and the deterministic checks pass, all from `github/`:

- migrations file set unchanged: `git status --porcelain internal/db/migrations/`
  is empty.
- `GOWORK=off go build ./...` succeeds, `GOWORK=off go vet ./...` is clean,
  `gofmt -l .` is empty, and `GOWORK=off go test ./...` passes with no failures
  and no `SKIP`.
