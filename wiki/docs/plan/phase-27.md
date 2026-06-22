# Phase 27 — MCP surface expansion: control & footprint verbs + paginated lists

*Realizes design Decision 16 (MCP surface expansion). Depends on Phase 25 (D14 `Service.Abort`/`Rerun`), Phase 26 (D15 the `page` codec and the paginated list seams), and Phase 23 (the existing MCP surface and its result-shaping helpers).*

Expose the new control and footprint capabilities on the product surface and turn the two unbounded read verbs into paginated ones — taking the surface from eight verbs to twelve. Wired with the established `WithXxxService` Option / dispatch-switch / reflection-shaper patterns; nginx stays the trust boundary.

**What gets built (the observable end state):**

- `internal/mcp` — four new verbs plus two reshaped:
  - **`jobs`** `{status?, since?, until?, limit?, cursor?}` → `{items:[{job_id, status, title, received_at, started_at?, finished_at?, error}], next_cursor}`.
  - **`abort`** `{job_id}` → `{job_id, aborted, status}`.
  - **`rerun`** `{job_id}` → `{job_id, status}` or a clean tool error (in-progress / not-found).
  - **`llm_calls`** `{job_id?, stage?, since?, until?, limit?, cursor?}` → `{items:[{id, job_id, stage, attempt, provider, model, params, request, response, usage, error, started_at, ended_at}], next_cursor}`.
  - **`subjects`** / **`claims`** reshaped to `{items, next_cursor}`, accepting `limit`/`cursor`; the per-item shapes (paths-out, no internal id) are unchanged.
  - New Options `WithJobsService`/`WithAbortService`/`WithRerunService`/`WithLLMCallsService`; `tools()` registers each when wired.
- **Input validation as clean tool errors** — `status` validated against the closed five-state set; `since`/`until` parsed as RFC3339; an undecodable `cursor` (D15 typed error) rendered as a tool error; never a crash. No internal id crosses the boundary; `llm_calls` carries no subject id.
- `cmd/wiki/main.go` — wires the new `Service` methods (`ListJobs`/`Abort`/`Rerun`/`LLMCalls` and the paginated `Subjects`/`ClaimsBySubject`) into both the `serve` composition root and `wiki.Spec()`, so the two MCP wirings expose the identical twelve-verb surface.
- The four control/footprint verbs ship **ungated**; the product's future `--debug` gate is not built here.

**Done when:**

- R-37NS-BRXR — `jobs` returns a `{items, next_cursor}` page; a `status` filter restricts it and the returned cursor fetches the next page.
- R-38VO-PJOG — `abort` returns `{aborted:true, status:"aborted"}` for a pending/working job, `{aborted:false, status:<terminal>}` for a terminal one, and a clean not-found for an unknown id.
- R-3A3L-3BF5 — `rerun` returns `{status:"pending"}` for a terminal job, a clean error for a pending/working job, and not-found for an unknown id.
- R-3BBH-H35U — `llm_calls` returns a paged set of records with `request`/`response`/`params`/`provider`/`model`/`attempt`; `job_id` and `stage` filters narrow it.
- R-3CJD-UUWJ — `subjects` and `claims` return `{items, next_cursor}`, accept `limit`/`cursor`, and keep paths-out / no internal subject id.
- R-3EZ6-MEDX — a malformed `since`/`until`, an unknown `status`, or an undecodable `cursor` each return a clean tool error.
- R-3G73-064M — `tools/list` exposes `jobs`/`llm_calls`/`subjects` with `inputSchema` that omits `required` when empty (never `"required": null`).
- The D10 `tools/list` conformance test (R-MUQ4-K1JS) is updated to the **twelve**-verb set, both MCP wirings expose it identically, and the suite is green (per design *Conventions*).
