# eventplane — Plan

**Authority: construction order.** This document and the `project/plan/`
directory own the build order of **pending** work only. Completion is
deletion: the build loop removes the finished phase's `STATUS.md` line and
its `phase-NN.md` in the completion commit; history lives in git, never here.
To extend the project: update product and design in place first, then append
a new `phase-NN.md` and its `STATUS.md` line, numbered from the `Next phase`
counter — never renumber, never reuse a number. **Coverage invariant:** every
*current* design Verification id is either already realized by a tagged test
in the codebase, or assigned to exactly one pending phase — no current id
unassigned, none split, none duplicated across pending phases.

**One phase = one package = one build-turn context.** Each phase is a single
coherent unit of work — almost always one package — scoped to its design
Decisions and the *interfaces* of what it depends on, and sized so the build
loop can carry it in one fresh build-turn context. If a Decision is too large
for one context it is split across phases, each naming its slice of the
Decision's Verification ids.

**Done bar.** A phase is done when every Verification id it realizes (or its
explicit slice) is covered by a clearly-named test and the suite is green —
"green" as defined in design's Conventions (`go test ./...` and
`go vet ./...` from `eventplane/`, both exit 0). Every phase's acceptance bar
is deterministic exit conditions — never a prose judgment, never a
self-referential or unsatisfiable check.

## Layout

`STATUS.md` is the manifest: the `Next phase` counter plus the **only** home
of the pending `⬜` markers; `phase-NN.md` is one body file per **pending**
phase (zero-padded; sub-phases keep their suffix, e.g. `phase-07a.md`); this
README is the static rules. Completion is deletion: the build loop's only
mutations are removing a finished phase's `STATUS.md` line together with its
`phase-NN.md`; the counter is never decremented and never touched by the
loop.
