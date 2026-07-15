# Phase 20 — `suite.mcp`: the generic MCP verb client

*Realizes design Decision 22 (the generic MCP verb client). Depends on
Phase 19.*

Implement `suite.mcp(service, tool, args=None)` in `suite.py`: `service`
resolved in the `SUITE_SERVICES` map (unknown → local `ValueError`, zero HTTP);
one stateless JSON-RPC `tools/call` POST to `<base>/mcp` carrying
`X-Owner-Email: $SUITE_OWNER_EMAIL`, `X-Client-Id: scripts:$SUITE_SCRIPT_ID`,
and no `X-Forwarded-Proto`; the D22 result ladder — transport failure →
`ToolError("source_unavailable")`, non-200 → `internal`, JSON-RPC error →
`validation` (-32602) / `internal`, `isError` → `ToolError` from
`structuredContent {code, message}`, success → `structuredContent` dict
verbatim, else concatenated text str. Tests ride the Phase 19 probe harness
against a recording `httptest` `/mcp` stand-in.

**Done when:**

- R-I0GA-YTQ5 — a named test proves the happy path: one recorded `POST /mcp`
  with the exact JSON-RPC body and identity headers (no `X-Forwarded-Proto`),
  the response's `structuredContent` returned deep-equal, and omitted `args`
  sent as `{}`.
- R-I1O7-CLGU — a named test proves a schema-less success (two text blocks)
  returns their concatenated text as a `str`.
- R-I2W3-QD7J — a named test proves an `isError` result raises `ToolError`
  with `.code`/`.message` from its `structuredContent`.
- R-I5BW-HWOX — a named test proves an unknown service raises `ValueError`
  with zero recorded requests.
- R-I6JS-VOFM — a named test proves a connection-refused service entry raises
  `ToolError` with `.code == "source_unavailable"`.
- The scripts suite is green per design Conventions.
