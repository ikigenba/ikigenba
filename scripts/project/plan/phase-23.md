# Phase 23 — `describe` teaches the runtime contract

*Realizes design Decision 26 (`describe` teaches the runtime contract).
Depends on Phases 21 and 22.*

Revise `internal/mcp/describe.go`'s `describeText` in place: the runtime
contract grows the `suite` module — `suite.event()`, `suite.mcp`,
`suite.fetch`, `suite.files.*` (framed as "the file share", never the backing
service), the `ToolError` exception model with its closed-vocabulary codes,
and the products-travel-by-reference guidance (`content_url` on `run_fs_list`
entries; write results as files, keep durable/shared results in the file
share). The tool stays prose (no `outputSchema`); existing sections stay
truthful.

**Done when:**

- R-IOUA-M8K1 — a named test proves the `describe` result names the `suite`
  module and each surface (`suite.event`, `suite.mcp`, `suite.fetch`,
  `suite.files`), names `ToolError` with at least `not_found` and
  `source_unavailable`, states that non-directory `run_fs_list` entries carry
  a `content_url`, and names no backing service in the file-share guidance.
- The scripts suite is green per design Conventions.
