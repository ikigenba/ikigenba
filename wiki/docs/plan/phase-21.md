# Phase 21 — `ask` citations carry the `type/slug` path

*Realizes the in-place D9 edit (ask citations as paths). Depends on Phase 19 (`Path`) and Phase 17 (the subject-extraction `ask` pipeline this amends).*

D9 was edited so `ask` never surfaces an internal subject id: each citation is identified by its public `type/slug` path. The synthesis call still cites internally by subject id; `ask` maps each validated citation to `wiki.Path` before returning. This phase amends `internal/ask`.

**What gets built (the observable end state):**

- `internal/ask`:
  - `Citation` becomes `{ Path, Title string }` (the public, path-valued citation).
  - The synthesis stage still parses internal `{subject, title}` citations and validates each against the gathered set (unchanged grounding), then maps each surviving citation's subject to its `wiki.Path` for the returned `Answer.Citations`.
  - No internal subject id appears in any returned citation, on any path (found, honest-empty, parse-failure).

**Done when:**

- R-05CG-3H6Y — a test asserts each returned citation identifies its page by the `type/slug` Path (mapped from the validated subject), and no internal subject id appears in any citation.
- R-6A8D-0RK9 — the existing D9 contract test is updated to the new shape: `Ask` returns `Answer{Found, Text, Citations{Path, Title}}` synthesized by the extract → resolve → synthesize pipeline (re-covering this changed id, not adding a new one).
- The other D9 ids (resolution, gather, honest-empty gate, grounding) remain green unchanged.
- The suite is green per the design Conventions.
