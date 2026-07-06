# Phase 09 — MCP surface over `appkit/mcp`

*Realizes design Decision 13 (the tool table), re-proving D9/D10/D11's ids
through the new seam. Depends on Phase 08 only for a settled `main.go`
(mechanically independent otherwise); depends on the appkit chassis providing
`appkit/mcp` with the standard tools (appkit plan Phases 08–09), consumed
through the committed replace as a fixed external contract.*

Observable end state:

- `crm/internal/mcp` declares `Instructions`, `Tools(svc) []mcp.Tool` (the six
  domain tools: `search`, `get`, `save`, `delete`, `log`, `guide` — descriptors,
  schemas, and handlers unchanged in wire content), and
  `NewHandler(svc, rt) (http.Handler, error)` assembling the appkit/mcp
  handler; `cmd/crm/main.go` mounts `POST /mcp` with it.
- The local JSON-RPC transport, `writeJSONRPCError`, the local `Identity`
  type, the local `health`/`reflection` tool implementations, and the local
  result-envelope helpers are deleted from `crm/internal/mcp`.
- `tools_test.go` drives the assembled appkit/mcp handler and keeps its
  behavioral assertions; the tool count stays eight.

**Done when:** the suite is green (design Conventions commands, from `crm/`)
and:

- R-MW1X-S9EV (D13) is covered by a clearly-named test asserting the
  exactly-eight partition (six declared + chassis `health`/`reflection`);
- R-PDZ7-HTAN, R-PF73-VL1C (D9), R-PGF0-9CS1, R-PIUT-0W9F (D10), and
  R-PK2P-EO04, R-PLAL-SFQT, R-PMII-67HI (D11) remain covered through the
  assembled handler;
- `grep -rn "writeJSONRPCError\|jsonRPCRequest" crm --include=*.go` returns no
  matches.
