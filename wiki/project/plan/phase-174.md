# Phase 174 — The pipeline's match phase: handoff, apply, and the effective-set integrate

*Realizes design Decision 97 (pipeline slice: R-7GJQ-2PN1, R-7HRM-GHDQ, R-7IZI-U94F, R-7LFB-LSLT, R-7MN7-ZKCI, R-7NV4-DC37, R-7QAX-4VKL) extending Decisions 4/14 in place. Depends on Phases 172 and 173.*

Wires the match phase into the worker (`internal/worker`): the `'match'` phase value; extract apply staging split statements, deciding per subject whether matching is needed, ensuring match items (`<jobID>/match/<norm-name>/a1`) for needing subjects and compile items for the rest, or preserving today's straight-to-compile flow when none need it; match apply parsing/validating/filtering, staging edge sets (existing rows by id, new statements by staged-plan ordinal), ensuring the subject's compile item rendered from the post-replacement **effective** set, and honoring the staged-row replay guards; the integrate commit inserting claims and corrections with explicit kinds, resolving ordinals to minted ids, inserting edges, building pages from effective sets, and deleting the page of a fully-suppressed subject while retaining its row; D82 composition and D62/D64 attribution on the match calls.

**Done when:** the suite is green and each id is covered by a genuine tagged test:

- R-7GJQ-2PN1 — captured match payload: base system, identity + numbered lists only, no ids.
- R-7HRM-GHDQ — scope instructions compose onto the match system; empty scope stays byte-exact base.
- R-7IZI-U94F — semantic failure → corrective item; exhausted → job `failed`, nothing written.
- R-7LFB-LSLT — handoff: match items only for needing subjects; no-correction jobs unchanged.
- R-7MN7-ZKCI — match apply stages edges, ensures that subject's compile item, replays idempotently.
- R-7NV4-DC37 — compile requests carry the effective claims only.
- R-7QAX-4VKL — one-transaction integrate; fully-suppressed subject loses its page, keeps its row.
