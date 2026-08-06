# Phase 27 — Header-fallback actor attribution in the HTTP request recorder

*Realizes design Decision 17 (inbound instrumentation) — slice R-K59U-9DWS,
R-K6HQ-N5NH.*

The `appkit/server` request-record middleware resolves the record's `actor`
from two sources in order: the identity captured by an inner gate when one
ran, otherwise the raw `X-Owner-Email` / `X-Client-Id` request headers. The
observable end state is that a request answered before any appkit handler
executes — canonically the mux-synthesized `405` for a `GET /mcp` that
mismatches the `POST /mcp` method pattern — is recorded with the caller
identity its headers carried instead of as anonymous, while a route whose
gate establishes an identity differing from the headers (the
session-authenticated case) still records the gate-established identity.
No wire-visible behavior changes: routing, status codes, and response bodies
are untouched.

**Done when:**

- R-K59U-9DWS — through the real `server.New` chain with an MCP handler
  mounted, a `GET /mcp` carrying `X-Owner-Email` / `X-Client-Id` headers is
  answered `405` and produces a request record with `op` `GET /mcp`,
  `outcome.status` `"405"`, and `actor.owner_email` / `actor.client_id` equal
  to those headers — covered by a tagged test.
- R-K6HQ-N5NH — a route whose handler establishes identity through the
  identity seam with values different from the request's identity headers
  records the established identity, not the headers — covered by a tagged
  test.
- The suite is green per design's Conventions (`go test ./...` and
  `GOWORK=off go build ./...` both exit 0).
