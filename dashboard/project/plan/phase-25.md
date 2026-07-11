# Phase 25 — capture a validated same-site `return_to` on the web handshake

*Realizes design Decision 21 (validated `return_to` on the web handshake) — ids
R-XLRL-ZHZT, R-XO7E-R1H7, R-XPFB-4T7W. Depends on no earlier phase (extends the
existing `internal/oauthstate` store and the `internal/server` login handler).*

Three coupled changes, all in `dashboard/`:

- A new forward-only migration (`bin/create-migration dashboard
  add_oauth_state_return_to`) adds a nullable `return_to TEXT` column to
  `oauth_state`, mirroring `005_oauth_state_mcp.sql`.
- `oauthstate.Handshake` gains a `ReturnTo` field; a new `CreateWeb(ctx,
  returnTo)` persists a web handshake carrying it (plain `Create` stays, minting
  empty `return_to`), and `Consume` reads the column back onto the returned
  handshake.
- `handleLogin` reads the `return_to` query parameter, runs it through a
  `safeReturnTo` same-site validator (admits a clean local absolute path; rejects
  empty, non-`/`-leading, protocol-relative `//` or `/\`, any scheme/host,
  backslashes, control bytes → `""`), and mints via `CreateWeb` with the
  validated value.

The MCP-origin login path is untouched.

**Done when:** the suite is green (per design *Conventions*, including
`bin/check-migrations dashboard`) and each id below is covered by a
clearly-named test:

- R-XLRL-ZHZT — a store test on a real temp `modernc.org/sqlite` (appkit-migrated)
  round-trips a non-empty `return_to` through `CreateWeb` → `Consume`, and a plain
  `Create` handshake round-trips `return_to == ""`.
- R-XO7E-R1H7 — an HTTP-level `server` test drives `GET
  /login?return_to=/srv/sites/private/test07/` and confirms that exact path is
  persisted on the minted handshake (read back from the temp DB).
- R-XPFB-4T7W — an HTTP-level `server` test drives `GET /login` with each hostile
  `return_to` (`//evil.com`, `https://evil.com/x`, `/\evil.com`, and a value not
  starting with `/`) and confirms the persisted `return_to` is empty (never the
  off-site value).
