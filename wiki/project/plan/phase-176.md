# Phase 176 — The corrections read surface: `claims` labels and the guide

*Realizes design Decision 98. Depends on Phase 175.*

Extends the MCP surface (`internal/mcp`): the `claims` items gain `kind`, `suppressed`, and (when suppressed) `suppressed_by` — computed through D96 `Effective` — with the D61 output schema declaring the fields and the no-internal-subject-id guarantee intact; the embedded guide document and `initialize` instructions text gain the corrections model per D98's content list, preserving D57's guide-pointer rule.

**Done when:** the suite is green and each id is covered by a genuine tagged test:

- R-7WEF-1QA2 — the extended `claims` items through the assembled handler, schema included.
- R-7XMB-FI0R — the guide and instructions text describe the corrections model.
