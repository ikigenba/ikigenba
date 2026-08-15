# Phase 175 — Corrections end to end: reassertion, recency, re-run, and the merge cross-match

*Realizes design Decision 97 (scenario and merge slice: R-7RIT-INBA, R-7SQP-WF1Z, R-7TYM-A6SO, R-7V6I-NYJD) extending Decision 26 in place. Depends on Phase 174.*

Completes the correction semantics over the wired pipeline: the standing-correction suppression of later reassertions; correction-vs-correction recency with revival on the recompiled page; re-run replacing rows, cascading edges, and re-matching fresh claims against standing corrections; and the merge cross-pair match — loading both sides' corrections and edges in Phase B, running the live match call only when a cross product has both sides non-empty, keeping only cross-subject pairs, compiling the winner from the merged effective set, and inserting the surviving cross edges in the merge transaction with existing edges intact across the claim repoint.

**Done when:** the suite is green and each id is covered by a genuine tagged test:

- R-7RIT-INBA — a reasserted retired fact is suppressed and stays off the page.
- R-7SQP-WF1Z — a newer contradicting correction overrules; revived claims return to the page.
- R-7TYM-A6SO — re-run re-matches; resurrected claims end suppressed again.
- R-7V6I-NYJD — merge mints the cross edges, preserves existing edges, compiles from the merged effective set, and skips the call when no cross pairs exist.
