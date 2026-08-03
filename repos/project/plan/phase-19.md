# Phase 19 — Canonical-subset input schemas

*Realizes design Decision 7 (The MCP tool surface) — the canonical-subset
slice: R-1WZF-FVH9.*

The tool-schema helper in `internal/mcp/tools.go` (`obj`) stops emitting
`additionalProperties`, so every declared tool `inputSchema` stays within
agentkit's canonical tool-schema subset and the prompts service can attach
repos as an MCP peer without agentkit's `Send` gate failing the run. Nothing
else about the emitted schemas changes: properties, `required`, types, and
descriptions are byte-identical, output schemas keep their current shape, and
handlers keep ignoring unknown argument keys as they do today.

**Done when:**

- R-1WZF-FVH9 — a test walks every registered tool's emitted `inputSchema`
  recursively (maps and arrays) and asserts the key `additionalProperties`
  appears nowhere.
- `grep -rn "additionalProperties" --include='*.go' --exclude='*_test.go' --exclude-dir=project .`
  from the repos root prints nothing (exit 1).
- The suite is green: `go test ./...` from `repos/` passes.
