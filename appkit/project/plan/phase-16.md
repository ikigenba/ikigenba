# Phase 16 — Identity keys on `X-Owner-Id`: widened `server.Identity`, hard gate flip, id-carrying transport and health

*Realizes design Decision 13 (identity contract) and the revised slices of
Decision 8 (transport identity hand-off, R-DHJA-J13E) and Decision 9 (health
identity fields, R-DIR6-WSU3).*

`server.Identity` (`appkit/server/middleware.go`) gains `OwnerID`, `OwnerName`,
and `OwnerPicture` beside the existing `OwnerEmail`/`ClientID`; the identity
gate (`requireIdentityHeaders`) refuses exactly when `X-Owner-Id` is empty
(unchanged 401 + `WWW-Authenticate` challenge + `unauthorized` body) and no
longer gates on `X-Owner-Email`. The MCP transport's identity paths
(`appkit/mcp` — the handler hand-off and the `identityFromRequest` fallback)
populate all five fields from the request headers. The `health` tool's result
and declared `outputSchema` (`appkit/mcp`) carry `owner_id` beside
`owner_email`/`client_id`. Existing appkit tests that inject only
`X-Owner-Email` are updated to inject `X-Owner-Id` (keeping email where the
assertion wants display data); the old email-gate behavior id R-MG78-T8RU is
gone from design, so its tag disappears from the tests with this phase.

**Done when:**

- R-DDVL-DPVB — a request without `X-Owner-Id` is refused `401` with the
  bearer challenge and the inner handler not invoked, even when
  `X-Owner-Email` is present.
- R-DF3H-RHM0 — a request with `X-Owner-Id` but no `X-Owner-Email` reaches
  the inner handler.
- R-DGBE-59CP — the handler-visible `Identity` carries all five header values
  verbatim in the corresponding fields.
- R-DHJA-J13E — the identity passed to an MCP Tool `Handler` carries all five
  header values in the corresponding fields.
- R-DIR6-WSU3 — `tools/call health` returns `owner_id` equal to the request's
  `X-Owner-Id`, and the `health` descriptor's `outputSchema` declares
  `owner_id`.
- Each id above appears verbatim as a tag in a `*_test.go` file; no test
  carries the retired tag R-MG78-T8RU
  (`grep -rn "R-MG78-T8RU" --include='*_test.go' .` returns nothing).
- The suite is green per design Conventions: `go test ./...` passes and
  `GOWORK=off go build ./...` succeeds in `appkit/`.
