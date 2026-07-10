# Phase 83 — Adopt the MCP self-discovery convention: instructions, lean tool descriptions, a `guide` tool

*Realizes design Decision 57 (self-discovery convention). Depends on Phase 80 (the `appkit/mcp` tool table + `NewHandler`) and Phase 82 (the D56 `ask` result change, which this phase's neutrality claim excludes).*

wiki's MCP surface adopts `../docs/mcp-discovery-convention.md`, surface-and-description only — no domain-tool behavior changes. In `internal/mcp`:

- **`const Instructions`** is rewritten to the D57 pinned text: names the domain in user vocabulary with everyday synonyms (notes / second brain; entities / events / concepts), states the verb flow, and points once at `guide`.
- **Tool descriptions** (the `*Tool()` descriptor funcs) are slimmed/sharpened to when/args/returns plus cross-cutting semantics (fire-and-return job handles, `type/slug` paths in for `page`/`claims`/`merge`, irreversible/terminal-state constraints, cursor pagination); reference catalogs move to the guide; no tool description except `guide`'s references the guide.
- **A `guide` tool** is added: `guideTool()` (input-free schema, `required` omitted) + `handleGuideCall` returning an embedded, non-empty guide document (`//go:embed guide.md`) — flat, read-only, input-free, never errors, mutates nothing. `Tools(...)` appends it **unconditionally** (no service gate). The guide document holds wiki's own catalogs (subject types, the `type/norm_name` path format, job statuses, claim shape, cursor-pagination contract) and basic + advanced worked examples.
- The existing tool-membership test is updated from fifteen to **sixteen** (the 13 domain tools + `guide` + chassis `health`/`reflection`).

**Done when** the following are covered by clearly-named tests and the suite is green (`go test ./...` from `wiki/`, per design Conventions):

- R-YDS9-MBQZ — the `guide` tool is flat/read-only/input-free: `inputSchema` is `{"type":"object"}` with no `required`; `tools/call guide` (empty/absent args) returns the embedded, non-empty guide document as a non-`isError` result, mutates no DB state, and never errors.
- R-YF06-03HO — `tools/list` returns exactly sixteen tools: the 13 domain tools plus the wiki-declared `guide` plus chassis `health` and `reflection` — no more, no fewer.
- R-YG82-DV8D — the `initialize` `instructions` names the domain in user vocabulary with synonyms (contains "notes"/"second brain" and "entities"/"events"/"concepts") and points to the guide (contains "guide").
- R-YHFY-RMZ2 — the guide is referenced in exactly two places: the `instructions` string and the `guide` tool's own `Description` both mention the guide, and no other tool's `Description` does.
- R-YINV-5EPR — behavior neutrality (all tools except `ask`): every existing domain tool call other than `ask` returns byte-for-byte the same result and `isError` envelope as before adoption — the D10/D16/D27 domain-tool behavioral tests pass unchanged for `ingest`/`status`/`abort`/`rerun`/`jobs`/`jobs_count`/`merge`/`merges`/`subjects`/`claims`/`page`/`llm_calls`.
