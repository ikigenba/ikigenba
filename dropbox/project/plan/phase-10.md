# Phase 10 — MCP surface over `appkit/mcp`

*Realizes design Decision 12 (the `list`+`get` tool table). Depends on phases 7–9
only for a settled `main.go` (mechanically independent otherwise); depends on the
appkit chassis providing `appkit/mcp` with the standard `health`/`reflection`
tools (appkit design D8–D9), consumed through the committed replace as a fixed
external contract.*

Observable end state:

- `dropbox/internal/mcp` declares `Instructions` (the current MCP instructions
  string, verbatim), `Tools(svc *dropbox.Service) []mcp.Tool` (the two domain
  tools — `list` and `get`, descriptors, schemas, and handlers unchanged in wire
  content), and `NewHandler(svc *dropbox.Service, rt *appkit.Router) (http.Handler, error)`
  assembling the `appkit/mcp` handler over `Instructions` + `Tools(svc)` + the
  Router-threaded `Service`/`Version`/`Health`/`Events`/`Subscriptions`;
  `cmd/dropbox/main.go` mounts `POST /mcp` with it
  (`handler, err := mcp.NewHandler(svc, rt); rt.Handle("POST /mcp", rt.RequireIdentity(handler))`).
- The local JSON-RPC transport (`ServeHTTP`, `writeJSONRPCResult`/`writeJSONRPCError`,
  `idOrNull`, `jsonRPCRequest`, the local `Identity` type, `handleToolCall`/
  `dispatchTool`), the local `toolHealth`/`toolReflection`/`renderSubscriptions`/
  `reflectionUnknownTypeError` implementations, and the local result-envelope
  helpers (`toolResultText`/`toolResultErr`/`toolResultJSON`, replaced by
  `mcp.TextResult`/`mcp.ErrorResult`/`mcp.JSONResult`) are deleted from
  `dropbox/internal/mcp`. The `list`/`get` bodies, the `toolErr` sentinel mapping,
  `toolErrorJSON`, and `firstN` are kept.
- `tools_test.go` drives the assembled `appkit/mcp` handler (built via a
  `server.New`/`Register` seam over a migrated domain `dropbox.Service`, the
  crm/notify pattern) and keeps its `list`/`get` behavioral assertions (path
  scoping, cursor pagination, the 25 MiB `too_large` cap, rev-pin conflict,
  base64 body, sentinel→code envelope); the tool count is four. Any assertion that
  pinned the retired **local** reflection error wording is realigned to the
  chassis behavior (an `isError` result naming the known types).

**Done when:** the suite is green (design Conventions commands + `bin/check-migrations dropbox`,
from `dropbox/`) and:

- R-QQJT-LKCV (D12) is covered by a clearly-named test asserting the exactly-four
  partition (`list`/`get` declared + chassis `health`/`reflection`, and no
  retired `whoami`);
- the pre-existing `list`/`get` behavioral tests pass through the assembled
  handler with no assertion changes;
- `grep -rn "writeJSONRPCError\|jsonRPCRequest\|func (h \*Handler) ServeHTTP" dropbox --include=*.go`
  returns no matches.
