# Phase 79 — `context` is a JSON value on the wire

*Realizes design Decision 29 (the completion queue) — the context-type slice.*

Retypes the queue's `context` field from a Go `string` to raw JSON across `internal/completion`: the Ensure decode accepts any well-formed JSON value there (object, array, string, number), the store keeps its serialized bytes in the existing `TEXT` column unchanged (no migration — `''` still means absent), and every read shape — Get at any stage, the inbox rows, and Ensure's `200` existing-item body — emits the stored bytes verbatim, `null` when absent. A malformed `context` fails the body decode like any other malformed field: `400`, nothing stored. Existing handler tests that assert string-shaped contexts are updated to the JSON-value contract.

This closes the seam defect found in the 2026-08-11 local verification: wiki sends its apply envelope as a JSON object (per its D5 and research §12), and the `string`-typed decoder rejected every ingest handoff with `400 cannot unmarshal object`.

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim:
  - R-U7PZ-0AIV — object- and array-shaped `context` accepted at Ensure and echoed byte-identical at every read; omitted `context` reads back `null`
  - R-U8XV-E29K — malformed-JSON `context` → `400` naming the problem, no `completions` row, no `calls` row
- `go test ./...` from `prompts/` is green.
