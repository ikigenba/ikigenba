# Phase 134 — True the autotune compile folder to the clean-prose contract

*Realizes design Decisions 71 (tune folders) and 72 (scorers) as updated for D7's clean-prose contract. Depends on Phase 133.*

The committed `autotune/compile/` folder stops rewarding the retired citation discipline. End state:

- `autotune/compile/prompt.txt` is refreshed as a plain copy of the rewritten production prompt (still no mechanical tie).
- Compile case `input.txt` files are the production-rendered user message under the new renderer: subject identity + plainly rendered claim texts, no job-id tags.
- The `score` deterministic gates are: parses to `{title, body}` non-empty; body ≤ 12,000 chars; body contains **no bracketed ULID marker and no ULID at all** (a leaked id floors the gate). The citation-resolution gate is gone.
- `judge-prompt.txt` scores lead discipline as natural-category framing (never `entity`/`concept`/`event` as the subject's category, no database/ingest vocabulary) alongside coverage/groundedness/organization.
- `fixtures/` are updated to the new gates (including a leaked-id fixture with its expected floored score), and `improve.md` no longer steers toward citations.

**Done when:**

- R-AD4E-PZJF — the compile `score` with the judge skipped floors an over-cap body, a body containing a bracketed ULID marker, and a malformed `{title, body}`, passes a clean fixture, and scores identically across two runs — its test and fixtures updated to the new gate set.
- `grep -rn 'job-id' autotune/compile/ --include='*.txt' --include='*.md' --include='score'` over the folder's prompt, improver, judge, and scorer files returns no matches (case inputs excepted only if a fixture deliberately embeds a leaked id to prove the floor).
- The suite is green per design Conventions (`go test ./...` from the wiki module root).
