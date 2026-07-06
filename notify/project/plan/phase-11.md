# Phase 11 — MCP surface over `appkit/mcp`

*Realizes design Decision 13 (the `send` tool table). Depends on Phases 09–10
only for a settled `main.go` (mechanically independent otherwise); depends on
the appkit chassis providing `appkit/mcp` with the standard tools (appkit plan
Phases 08–09), consumed through the committed replace as a fixed external
contract.*

Observable end state:

- `notify/internal/mcp` declares `Instructions`, `Tools(client) []mcp.Tool`
  (the one domain tool: `send` — descriptor, schema, and handler unchanged in
  wire content), and `NewHandler(client, rt) (http.Handler, error)` assembling
  the appkit/mcp handler; `cmd/notify/main.go` mounts `POST /mcp` with it.
- The local JSON-RPC transport, the JSON-RPC error writer, the local
  `Identity` type, the local `health`/`reflection` tool implementations, and
  the local result-envelope helpers are deleted from `notify/internal/mcp`.
- `send_test.go`/`tools_test.go` drive the assembled appkit/mcp handler and
  keep their behavioral assertions (priority mapping, tags, click validation,
  validation rejects with no POST fired, upstream failures leak no secret);
  the tool count is three.

**Done when:** the suite is green (design Conventions commands, from
`notify/`) and:

- R-4IBU-MT7Z (D13) is covered by a clearly-named test asserting the
  exactly-three partition (`send` declared + chassis `health`/`reflection`);
- the pre-existing `send` behavioral tests pass through the assembled handler
  with no assertion changes;
- `grep -rn "writeJSONRPCError\|jsonRPCRequest" notify --include=*.go` returns
  no matches.
