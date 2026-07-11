# Phase 26 — the web callback returns you to `return_to`, or `/` by default

*Realizes design Decision 22 (callback replay of `return_to`) — ids R-XQN7-IKYL,
R-XRV3-WCPA. Depends on Phase 25 (`Handshake.ReturnTo` persisted and read back by
`Consume`).*

`callbackWeb` takes the consumed `handshake` (already in hand in
`handleCallback`) and redirects to `handshake.ReturnTo` when it is non-empty,
else to the apex home `/`. It re-validates nothing (the value was validated at
capture, D21) and constructs no new target. Session minting, the audit events,
and the session cookie are unchanged, and the MCP-origin branch (`callbackMCP`)
is untouched.

**Done when:** the suite is green (per design *Conventions*) and each id below is
covered by a clearly-named HTTP-level `server`-package test asserting the
callback's `Location` header (driven through the real `/login` → callback route
against a temp DB):

- R-XQN7-IKYL — a web sign-in whose handshake carries
  `return_to=/srv/sites/private/test07/` completes with a 302 to **that path**.
- R-XRV3-WCPA — a web sign-in whose handshake carries an empty `return_to`
  redirects to **`/`**, and an MCP-origin callback still redirects to its
  `MCPRedirectURI` (default preserved, MCP path unaffected).
