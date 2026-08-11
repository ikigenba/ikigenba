# Phase 158 — The ingest pipeline split: handoff, inbox applier, staging, `waiting`

*Realizes design Decision 4 (handoff/apply pipeline) and Decision 14 (the `waiting`-state control additions). Depends on Phase 157.*

Rebuilds the ingest pipeline into the D4 split: the claim loop hands extract work to the queue (derived keys, context envelopes) and parks the job `waiting`; the new inbox tick drains terminal items each interval and applies them — extract apply (stage + fan out compile items + ack-last), compile apply (stage; last unit runs D14's atomic integrate guarded on `waiting`), corrective re-ensures for semantic failures, queue-failure and stray-item handling. Adds the `job_staging` table via `bin/create-migration wiki create-job-staging`, the `waiting` status, the boot-sweep discrimination (`working` swept, `waiting` untouched), and the D14 abort/re-run extensions for `waiting`. Existing carried ids (R-M8RN-87WV, R-MB7F-ZRE9, R-MCFC-DJ4Y, R-MG31-IUD1; the D82 composed-system ids; the D62/D64 attribution/correlation captures) keep their tests, adapted to the split orchestration.

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim (worker/pipeline integration over real temp SQLite + the httptest queue): R-K73J-J3W3, R-K8BF-WVMS, R-K9JC-ANDH, R-KAR8-OF46, R-KBZ5-26UV, R-KEEX-TQC9, R-KFMU-7I2Y, R-KGUQ-L9TN, R-KI2M-Z1KC.
- `grep -rn 'R-M9ZJ-LZNK\|R-MDN8-RAVN' --include='*_test.go' .` from `wiki/` returns nothing (the two replaced D4 ids' tests are deleted with them).
- R-M8RN-87WV, R-MB7F-ZRE9, R-MCFC-DJ4Y, and R-MG31-IUD1 remain tagged by green tests against the split pipeline.
- `go test ./...` from `wiki/` is green.
