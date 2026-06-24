# Phase 22 — MCP surface: `type/slug` paths in and out + the page link footer

*Realizes the in-place D10 edit (MCP path I/O + footer). Depends on Phase 19 (`GetByPath`/`Path`), Phase 20 (`PageWithLinks`/`RenderFooter`), and Phase 21 (ask citations as paths).*

D10 was edited so the internal ULID never crosses the MCP boundary: subjects are named everywhere by their `type/slug` path, path inputs are resolved via `GetByPath`, and the `page` body carries the D12 link footer. This phase amends `internal/mcp` and the composition root wiring; it adds **no new verb** (still eight).

**What gets built (the observable end state):**

- `internal/mcp` (+ `cmd/wiki/main.go` wiring so the handlers can reach `SubjectStore.GetByPath`, `Service.PageWithLinks`, and `wiki.Path`):
  - `page` and `claims` accept the `subject` input as a `type/slug` path, resolving it via `GetByPath`; an unmatched path returns a clean not-found result, and `ErrAmbiguousPath` returns a clean tool error explaining the collision (never a wrong subject, never a 500).
  - `subjects` returns each row's `path` (and `type`, `name`, `has_page`) with no internal id.
  - `status` reports a finished job's produced subjects as `type/slug` paths.
  - The `page` result's own `subject` field is the path, and its `body` is `RenderFooter(...)` over `Service.PageWithLinks` (D12 footer with `type/slug` hrefs).
  - `ask` citations serialize as `[{path, title}]` (from Phase 21).

**Done when:**

- R-01OQ-Y5YV — `subjects` returns each entry's `type/slug` path and no internal id.
- R-02WN-BXPK — `page` and `claims` accept a `type/slug` path as `subject`, resolve it via `GetByPath` to the right subject, and return a clean not-found for a path matching no subject.
- R-044J-PPG9 — `status` reports a finished job's produced subjects as `type/slug` paths, not internal ids.
- R-MYDT-PCRV — the existing not-found test is updated: `status` (unknown job) and `page`/`claims` (unmatched subject path) return a clean not-found result, not an error or crash (re-covering this changed id).
- The unchanged D10 ids (`tools/list` set, reflection, health, identity gating, JSON-Schema `required`) remain green; the `page` body now includes the D12 footer and `ask` citations are `{path,title}`.
- The suite is green per the design Conventions.
