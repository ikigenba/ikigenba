# Phase 26 — Cursor pagination: the `page` codec + the list seams

*Realizes design Decision 15 (cursor pagination). Depends on Phase 2 (the stores) and Phase 24 (the `LLMCallStore` to page over).*

Make every unbounded listing return a bounded page plus an opaque cursor, with filters applied before paging, so a caller walks a large set instead of pulling it whole. Pagination is **keyset (seek)**, not offset — O(log n) per page and immune to skip/duplicate under concurrent inserts.

**What gets built (the observable end state):**

- `internal/page` — a pure package: `Params{Limit, Cursor}`, `DefaultLimit=50`/`MaxLimit=200` with clamping (`0 → DefaultLimit`, `>MaxLimit → MaxLimit`), and `EncodeCursor(parts...)` / `DecodeCursor(token) (parts, ok)` (opaque base64url over a delimited join). A malformed token decodes to `ok=false`.
- `internal/wiki` — the four list seams, each ordering by `(sort_col, id)` with the ULID `id` as unique tiebreak, appending the explicit keyset predicate, over-fetching by one to detect a further page, and returning `(items, nextCursor, error)` (empty `nextCursor` on the last page); an undecodable cursor returns a clean typed error:
  - `JobStore.ListJobs(ctx, JobFilter{Status, Since, Until}, page.Params)` — order `received_at, id`.
  - `SubjectStore.List(ctx, typ, nameContains, page.Params)` — **reshaped** to paginated; order `name, id`.
  - `ClaimStore.ListBySubject(ctx, subjectID, page.Params)` — **reshaped**; order `id`.
  - `LLMCallStore.List(ctx, LLMCallFilter{JobID, Stage, Since, Until}, page.Params)` — order `started_at, id`.
- A new migration (`bin/new-migration wiki`) adds the `subjects (name, id)` index backing the subjects ordering; the `jobs_pending_received`, `claims_subject`, and Phase-24 `llm_calls_*` indexes back the others.
- The `Service` wrappers and the existing composition-root MCP adapters are updated just enough to **keep the suite green** under the reshaped `SubjectStore.List` / `ClaimStore.ListBySubject` signatures; the real `{items, next_cursor}` MCP envelopes and the new list verbs land in Phase 27 (until then those verbs may serve the first page).

**Done when:**

- R-17C5-VP2I — paging N rows at limit `k` yields every row exactly once in key order, `nextCursor` non-empty until the final page and empty on it (no skips/dupes at page boundaries).
- R-18K2-9GT7 — a filter is applied before paging: status (jobs), `stage`/`job_id`/time-range (llm_calls), `type`/name-substring (subjects), required `subject` (claims).
- R-19RY-N8JW — `Params.Limit` clamps (`0 → DefaultLimit`, `>MaxLimit → MaxLimit`).
- R-1C7R-ES1A — `EncodeCursor`/`DecodeCursor` round-trip key parts losslessly; a malformed token yields `ok=false` (table test).
- R-1DFN-SJRZ — a list call given an undecodable cursor returns a clean error and does not silently restart from the first page.
- The suite is green (per design *Conventions*).
