# Phase 54 — Chain-stamped peer calls via the `MCPServer` headers

*Realizes design Decision 45 (X-Correlation-Id in the MCPServer headers).
Depends on Phase 52.*

⛔ **External precondition (operator-sequenced, not built here).** appkit and
eventplane must be built first: this phase reads the run's chain id through
`eventplane/correlation` and stores/reads it per D44's schema (Phase 52). No
prompts phase edits appkit or eventplane.

## What gets built

The run's chain id rides every suite MCP call the in-run agent makes, through
the one seam that now exists for it — the `Headers` map of each
`agentkit.MCPServer` attachment (agentkit injects the map on every request it
makes to a server: `tools/list` and each `tools/call`):

- `internal/suite` — `Discover` gains the trailing `correlationID string`
  parameter; when non-empty, every returned entry's `Headers` carries
  `X-Correlation-Id: correlationID` beside the identity trio; when empty, the
  key is absent (not empty-valued).
- `internal/runner` — the runner passes the **run row's stored**
  `CorrelationID` (D44, Phase 52) into its `discover` seam and seeds the run's
  execution context with the same id via `eventplane/correlation`.
- **No outbound record is emitted for these calls** — agentkit v0.16.0 exposes
  no client-injection seam, per D45's recorded-boundary statement; the
  receiving peer's inbound record carries the chain. Nothing in this phase
  touches `rt.HTTPClient(...)`.

Observable end state: a peer served in-process observes every request an
attached run makes arriving with `X-Correlation-Id` equal to the run's stored
chain id, and the run's own context reports the same id.

## Done when

Every D45 Verification id is covered by a clearly-named, id-tagged test and the
suite is green (`cd prompts && go build ./...`, `go vet ./...`, `gofmt -l .`
empty, `go test ./...` all passing):

- R-HPLU-D8WR — `Discover` with correlation id `X` returns entries whose
  `Headers` all carry `X-Correlation-Id` exactly `X`; with an empty id, no
  entry contains the key.
- R-HS1N-4SE5 — end-to-end through a `Conversation` against a recording
  `httptest` MCP peer: both the peer's inbound `tools/list` and a dispatched
  `tools/call` arrive carrying `X-Correlation-Id` exactly `X`.
- R-HT9J-IK4U — for a run whose stored correlation id differs from its run id,
  the injected `discover` observes the stored id, and the run's context inside
  `execute` reports it through `correlation.FromContext`.
