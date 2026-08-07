# Suite contracts — Plan

**Authority: construction order.** This document and the `project/plan/`
directory own the build order of **pending** work only — nothing else (the
*why* is product's, the *shape and its proof* is design's). **Completion is
deletion**: when a phase finishes, the build loop removes its line from
`STATUS.md` **and** its `phase-NN.md` in the completion commit. The plan never
holds finished work, so it can never contradict a design that has moved on;
construction history lives in git (the completion commits, and the deleted
files recoverable there). To extend the project: update
`project/product/README.md` and `project/design/` **in place** first, then
**append** a new `phase-NN.md` plus a `STATUS.md` line, taking the number from
the `Next phase` counter and bumping it — never renumber a pending phase,
never reuse a number.

**This project builds no code**, so a phase here never produces an
implementation. A phase amends a contract and, where the amendment changes what
some tree must do, records the downstream assignment that follows from it — the
work itself is then built by that tree's own plan, under its own gate.

**Coverage invariant.** Every *current* design Verification id
(`R-XXXX-XXXX`) is either already **realized** — appearing verbatim as a tag
in a test file of the tree its proof-location marker names (or, for a
`[proof: per-service]` id, in each adopting service's tree) — or assigned to
**exactly one** pending phase: no current id unassigned, none split, none
duplicated. Realized-ness is read from the code (the tagged tests), never from
a ledger. Because this tree holds no tests, the check follows the markers, not
the tree-local grep; design's Conventions state the per-marker commands.

**One phase = one coherent amendment = one build-turn context.** Each phase is
a single coherent unit of work — here, one contract or one tight group of
related contracts — scoped to the Decisions it touches and sized so the loop
can carry it in one fresh build-turn context.

**Done bar.** Every phase's acceptance bar is expressed as **deterministic exit
conditions**: mechanically-checkable predicates (an exit code, an exact match
count, a `project/`-excluded grep) that are reproducible and reachable — never
a subjective prose judgment, never a self-referential or unsatisfiable check.
Since phases here are docs-only, the bar is structural rather than a green
suite; where a phase changes an id's marked proof location, the bar includes
confirming the tag in the newly named tree.

## Layout

The plan is physically split so the build loop reads only the one unit of
work it needs, never the whole queue:

- **`project/plan/STATUS.md`** — the manifest: the `Next phase` counter plus
  one line per **pending** phase in build order, the **only** home of the `⬜`
  markers. The loop finds its next work with
  `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1` and reads only
  that phase's body file.
- **`project/plan/phase-NN.md`** — one body file per pending phase
  (zero-padded; a sub-phase keeps its suffix, e.g. `phase-08a.md`). Carries
  **no** status marker of its own.
- **`project/plan/README.md`** — this file: the static rules. It lists no
  phases and never grows.

**Completion is deletion, for this layout:** the build loop's only mutations
are removing a finished phase's `STATUS.md` line together with its
`phase-NN.md`. There is no done marker on disk — done is gone. The `Next
phase` counter is never decremented and never touched by the loop.
