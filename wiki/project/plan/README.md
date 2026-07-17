# wiki — Plan

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order** of **pending** work only.
**Completion is deletion:** when a phase lands, the build loop removes its
`project/plan/STATUS.md` line and its `project/plan/phase-NN.md` body file in
the completion commit — the plan never holds finished work, so it can never
contradict a design that has since moved on. The record of what was built lives
in **git** (the completion commits, and the deleted files recoverable there),
never here. To extend the work, update the product (`project/product/README.md`)
and design (`project/design/README.md` + `project/design/`) **in place** to
stay authoritative for the current state, then **append** a new phase — a new
`project/plan/phase-NN.md` body file plus a new line in
`project/plan/STATUS.md` — numbered from the `Next phase` counter. A phase
number is **never renumbered and never reused**, even after its files are
deleted.

**Coverage invariant.** Every *current* design Verification id is either
already **realized** — its id appearing verbatim as a tag in a test file that
runs under the suite — or assigned to **exactly one** pending phase: no current
id unassigned, none split, none duplicated across pending phases.

**One phase = one package = one build-turn context.** Each phase is a single
coherent unit — almost always one Go package (`internal/<pkg>` or `cmd/wiki`) —
scoped to that unit's design Decisions and the *interfaces* (not the internals)
of the packages it depends on, and **sized so the build loop can carry it in
one fresh build-turn context** and ideally finish it in a turn or two. Sizing a
phase as large as cleanly fits one turn is good: fewer cycles, less context
churn. The composition root (`cmd/wiki/main.go`) is the one shared file
legitimately touched by more than one phase — it is assembled incrementally as
packages come online. If a single package ever must split across phases to fit
one context, the affected phase files say so explicitly and carry the
partial-Decision split.

**Done bar.** A phase is **done** when every Verification item — the
`R-XXXX-XXXX` ids — in the design Decisions it realizes (or the slice of those
ids assigned to it) is covered by a clearly-named test and the suite is green.
"Green" is defined concretely in design's *Conventions*: `go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed
with zero failures. "Covered" means each listed id has a genuine test
exercising the behavior that Decision's Verification list describes — see each
`project/design/DNN.md` Verification section for what the id requires. The
acceptance bar is always deterministic exit conditions — never a subjective
judgment, never a self-referential/unsatisfiable check.

## Layout

The plan is physically split so the build loop reads only what it needs:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus one
  line per **pending** phase in build order, and the **only** home of the `⬜`
  marker.
- `project/plan/phase-NN.md` — one body file per **pending** phase
  (zero-padded: `phase-01.md`, `phase-02.md`, …; a sub-phase keeps its suffix,
  e.g. `phase-07a.md`). A phase body carries **no** status token — status lives
  only in `STATUS.md`.
- `project/plan/README.md` — this file: the static, invariant rules above. It
  lists no phases and carries no status, so it never grows with the project.

**Completion-is-deletion, restated for this layout:** the build loop's only
mutations to this layout are removing a finished phase's `STATUS.md` line
together with its `phase-NN.md`. The `Next phase` counter is never decremented
and never touched on completion — only bumped when a new phase is appended.
