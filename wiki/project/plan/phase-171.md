# Phase 171 — Corrections data model: the `kind` column, the `suppressions` table, and the effective claim set

*Realizes design Decision 96 (statements: claims and corrections).*

Builds the durable substrate for corrections in `internal/wiki` and `internal/db`: one new timestamped migration (`bin/create-migration wiki <name>`) adding `claims.kind` (default `'claim'`, two-value CHECK) and the `suppressions` edge table; the `Claim.Kind` field with the two kind constants; the `Suppression` type, `SuppressionStore` (insert with the D96 edge invariants and `ErrInvalidSuppression`, list-by-subject, both-endpoint `DeleteForClaims` cascade); and the pure `Effective` function (liveness newest-first, chain revival, `suppressedBy` map). No pipeline, extract, or surface change in this phase — the new code is exercised by its own tests and changes no existing behavior.

**Done when:** the suite is green (`go test ./...` from `wiki/`) and each of these ids is covered by a genuine tagged test:

- R-74CQ-9083 — migration preserves existing claim rows as `kind='claim'`, enforces the CHECK, creates `suppressions`.
- R-76SJ-0JPH — `Effective` excludes a suppressed claim, never includes corrections, reports `suppressedBy`.
- R-780F-EBG6 — chain revival: suppress-the-suppressor revives targets; a third level re-suppresses.
- R-798B-S36V — `Insert` rejects claim-sourced, non-backward, and same-job correction→correction edges; accepts older-correction→newer-claim.
- R-7AG8-5UXK — `Kind` round-trips; `DeleteForClaims` cascades both endpoints inside the caller's tx.
