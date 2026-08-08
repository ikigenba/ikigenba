# Phase 34 — Statuses

*Realizes the status slice of design Decision 21 (R-KFAG-0JCQ, R-KHQ8-S2U4).
Depends on Phase 33.*

`internal/repos/` gains `Service.SetStatus` and `Service.ListStatuses` over the
Phase 28 `statuses` table: the closed `pending|success|failure` vocabulary, the
upsert on `(kind, name, sha, check)`, and the requirement that the sha resolve
to a commit in that repository (`git cat-file -e <sha>^{commit}`) before a
verdict is stored.

**Done when:** the suite is green and these ids are each covered by a
clearly-named test —

- R-KFAG-0JCQ — a second verdict for the same key replaces the first; an
  out-of-vocabulary state is `ErrValidation`; a sha that does not resolve is
  `ErrNotFound` and stores nothing.
- R-KHQ8-S2U4 — the listing returns every check for a sha, is empty for an
  unseen sha, and is scoped by `(kind, name)` rather than by sha alone.
