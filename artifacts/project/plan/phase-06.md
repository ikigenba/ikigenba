# Phase 6 — The MCP tool surface

*Realizes design Decision 6 (MCP). Depends on Phase 5.*

The `internal/mcp` tool table over the chassis MCP layer: `upload`,
`import`, `list`, `get`, `update`, `set_visibility`, `delete`, `guide` —
box-shared scoping, the one record shape (tier-correct `url` +
content-plane reference), patch semantics on `update`, the closed error
vocabulary, output schemas + structured results per
`root project/design/D20.md`, and the Tier-0 instructions. End state: an
agent can drive the full store lifecycle over MCP without a byte of file
content ever entering a tool call or result.

**Done when:** the suite is green and each of R-4CKW-4X7Z, R-4DSS-IOYO,
R-4F0O-WGPD, R-4G8L-A8G2, R-4HGH-O06R, R-4IOE-1RXG, R-4JWA-FJO5,
R-4L46-TBEU, R-4MC3-735J is covered by a test tagged with its id.
