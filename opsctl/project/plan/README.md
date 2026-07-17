# opsctl — Plan

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order** of **pending** work only. Completion
is deletion: when a phase is done, the build loop removes its line from
`project/plan/STATUS.md` and deletes its `project/plan/phase-NN.md` body file in
the completion commit — the plan never holds finished work, so it can never
contradict a design that has since moved on. The record of what was built is
git's (the completion commits, and the deleted files recoverable there). To
extend the plan, update `project/product/` and `project/design/` **in place**
first, then **append** a new phase — a new `project/plan/phase-NN.md` body plus
a new line in `project/plan/STATUS.md`, numbered from the `Next phase` counter.
Phase numbers are never reused: never renumber an existing phase, never reuse a
number after its files are deleted.

The **coverage invariant**: every *current* design Verification id is either
already realized — its id appearing verbatim as a tag in a test file that runs
under the suite — or assigned to exactly one pending phase. No current id is
unassigned, split, or duplicated across pending phases.

## One phase = one package = one build-turn context

Each phase is a single coherent unit — almost always one package — scoped to that
package's design Decisions and the *interfaces* (not internals) of the packages it
depends on, and sized so the gather→build→verify loop can carry it in **one fresh
`build`-turn context** and ideally finish it in a turn or two (with `verify`
confirming on the next turn). The loop does not accumulate one long context across
the phase: `build` runs in a fresh context each turn and `verify` in another — so
size to a single `build` turn, not an imagined single sitting. Sizing a phase as
large as cleanly fits one `build` turn is good — one turn can then cover several
requirements, meaning fewer cycles and less context churn. If a single package
must split across phases to fit one context, the affected phase files say so
(partial-Decision split).

## Done bar

A phase is **done** when every Verification item — the `R-XXXX-XXXX` ids — in the
design Decisions it realizes, or the slice of those ids assigned to it, is covered
by a clearly-named test and the suite is green. "Green" is defined in design's
*Conventions* (`GOWORK=off go build ./...` succeeds and `GOWORK=off go test ./...`
passes, from the service root); "covered" is defined by each Decision's
Verification list (`project/design/INDEX.md` resolves an id to its Decision).
Every phase's acceptance bar must be expressed as **deterministic exit
conditions** — mechanically-checkable predicates, reproducible on identical repo
state, whose passing state is actually reachable — never a subjective prose
judgment and never a self-referential or unsatisfiable check.

Some design ids are **real-substrate** (live-box) checks that are not reproducible
on identical repo state and therefore cannot be loop-driven phases; those are
verified by the operator out-of-loop (tracked in a `*-verification.md` doc), and
the phase that realizes the rest of that Decision records the partial-Decision
split.

## Layout

The plan is **split for addressability**:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus one line
  per **pending** phase in build order, and the **only** home of a phase's
  pending marker (`⬜`).
- `project/plan/phase-NN.md` — one body file per **pending** phase (zero-padded;
  sub-phases keep their suffix, e.g. `phase-07a.md`). Carries no status marker of
  its own.
- `project/plan/README.md` — these static, invariant rules; it never grows with
  the project.

Completion-is-deletion for this layout: the build loop's only mutations are
removing a finished phase's `STATUS.md` line together with its `phase-NN.md`.
The `Next phase` counter is never decremented and never touched by that
deletion — it only ever advances when a new phase is appended.
