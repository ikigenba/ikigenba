# Phase 35 — The merge verb

*Realizes the merge slice of design Decision 21 (R-KIY5-5UKT, R-KK61-JMBI,
R-KLDX-XE27, R-KMLU-B5SW, R-KNTQ-OXJL, R-KP1N-2PAA, R-KQ9J-GH0Z, R-KRHF-U8RO).
Depends on Phase 34.*

`internal/repos/` gains `Service.Merge` and the `gate` helper: resolve, detect
already-merged, apply the status gate, test with `git merge-tree --write-tree`,
then fast-forward or `commit-tree` a two-parent merge commit and move `main`
through `ApplyRefUpdate` so the `push` event fires. Every refusal leaves `main`
byte-identical and publishes nothing.

**Done when:** the suite is green and these ids are each covered by a
clearly-named test —

- R-KIY5-5UKT — an ahead branch fast-forwards, creates no commit object, and
  publishes one `push`.
- R-KK61-JMBI — a diverged non-conflicting branch produces a two-parent merge
  commit containing both sides.
- R-KLDX-XE27 — a conflicting merge is `ErrConflict` with `main` unchanged and
  nothing published.
- R-KMLU-B5SW — a `failure` status blocks and names the check; recording
  `success` unblocks.
- R-KNTQ-OXJL — a `pending` status blocks.
- R-KP1N-2PAA — zero statuses merge freely, all-`success` merges, and a status
  on a different sha does not block.
- R-KQ9J-GH0Z — an already-merged branch is an `up-to-date` no-op.
- R-KRHF-U8RO — unknown branch, unknown repository, and `branch == "main"` all
  refuse without touching `main`.
