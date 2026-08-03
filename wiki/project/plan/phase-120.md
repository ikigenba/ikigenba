# Phase 120 — Canonical-subset input schemas

*Realizes design Decision 10 (The MCP tool surface) — the canonical-subset
slice: R-1Y7B-TN7Y.*

The shared object-schema helper in `internal/mcp/mcp.go` stops emitting
`additionalProperties`, so every declared tool `inputSchema` stays within
agentkit's canonical tool-schema subset and the prompts service can attach
wiki as an MCP peer without agentkit's `Send` gate failing the run. Nothing
else about the emitted schemas changes: properties, `required` (including the
D10 omit-when-empty rule), types, enums, and descriptions are byte-identical,
output schemas keep their current shape, and handlers keep ignoring unknown
argument keys as they do today.

**Done when:**

- R-1Y7B-TN7Y — a test walks every declared tool's emitted `inputSchema`
  recursively (maps and arrays) and asserts the key `additionalProperties`
  appears nowhere.
- `grep -rn "additionalProperties" --include='*.go' --exclude='*_test.go' --exclude-dir=project .`
  from the wiki root prints nothing (exit 1).
- The suite is green: `go test ./...` from `wiki/` passes.
