# Phase 4 — The MCP tool surface

*Realizes design Decision 4 (the MCP tool surface). Depends on Phase 3 (the
`Client` the tools dispatch to) and Phase 2 (`health` drives the auth source).*

> This phase **wires** `health` to drive the real authenticated GitHub call, but
> D2's live-substrate id `R-DMUT-QF4A` is **not** a loop-gating id for this phase:
> it cannot be proven by a reachable offline test (`go test` runs with no real
> GitHub), so it is verified out-of-loop by an operator per
> `project/github-verification.md`. This phase's autonomous done-bar covers the
> seven offline D4 ids only.

## What gets built

`internal/mcp/` — the JSON-RPC 2.0 `Handler` mounted at `POST /mcp` behind
`rt.RequireIdentity`, structurally like `gmail/internal/mcp`: `initialize`,
`tools/list`, `tools/call`. It registers the fifteen **bare** verbs (13 domain +
`health` + `reflection`), each with a JSON input schema, dispatching to the Phase-3
`Client` (held behind a `GitHubClient` interface so tests use a fake). The
composition root (`internal/githubapp/spec.go` `Handlers`) is completed here:
build the `tokenSource` + `Client` from env config and mount the handler.

- `health` returns the chassis envelope and drives a real authenticated GitHub
  call (D2), reporting failure loudly.
- `reflection` reports an empty published-event set and empty subscriptions
  (github is neither producer nor consumer).
- Every **write** verb emits one structured `slog` line carrying `X-Owner-Email`,
  `X-Client-Id`, the verb, and the target — owner provenance to logs only, never
  into the GitHub request.
- `Client` errors become `isError` tool results, not transport crashes.

Observable end state: an MCP client can `initialize`, list all fifteen bare verbs
with schemas, and call them; identity comes from the nginx headers; writes are
logged with owner provenance; `health` proves real auth.

## Done when

All hold on identical repo state, from `github/`:

- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0; `gofmt -l .`
  empty; `go vet ./...` clean.
- Clearly-named offline tests cover and pass for `R-EEWI-J569` (all 15 bare verbs +
  schemas advertised), `R-EHCB-AONN` (missing/malformed arg → error, no client
  call), `R-EIK7-OGEC` (identity from headers), `R-EJS4-2851` (each write verb logs
  owner+verb+target; the produced GitHub request carries no owner field),
  `R-EL00-FZVQ` (health envelope reflects the auth call, faked offline),
  `R-EM7W-TRMF` (reflection reports empty events + subscriptions), and
  `R-ENFT-7JD4` (each `Client` error → `isError` result, transport stays up) — each
  id named in a test.
`R-DMUT-QF4A` is **out of this phase's loop bar** — its live smoke is verified by
an operator per `project/github-verification.md`, not by the autonomous loop.
