# registry — Plan

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order of pending work only**. Completion is
deletion: the build loop removes the finished phase's `STATUS.md` line and its
`phase-NN.md` in the completion commit — construction history lives in git,
never in the plan. To extend it, update `project/product/` and
`project/design/` **in place** first, then **append** a new phase — a new
`project/plan/phase-NN.md` body plus a new line in `project/plan/STATUS.md`,
numbered from the `Next phase` counter in `STATUS.md`. Phase numbers are
**never renumbered and never reused** — a number names one phase forever, even
after its files are gone.

## Coverage invariant

Every *current* design Verification id is either already **realized** — its id
appearing verbatim as a tag in a test file that runs under the suite — or
assigned to **exactly one** pending phase: no current id unassigned, none
split, none duplicated across pending phases.

## One phase = one package = one build-turn context

Each phase is a single coherent unit — here, one flat package `registry` built up
in a few steps — scoped to that step's design Decision(s) and the *interfaces* (not
internals) of what it depends on, and sized so the gather→build→verify loop can
carry it in **one fresh `build`-turn context** and finish it in a turn or two (with
`verify` confirming on the next turn). The loop does not accumulate one long
context across a phase: `build` runs in a fresh context each turn and `verify` in
another — so size to a single `build` turn, not an imagined single sitting. This
project is tiny; each phase is a small standalone increment of one package.

## Done bar

A phase is **done** when every Verification item — the `R-XXXX-XXXX` ids — in the
design Decisions it realizes, or the slice assigned to it, is covered by a
clearly-named test and the suite is green. "Green" is defined in design's
*Conventions* (`GOWORK=off go build ./...` succeeds and `GOWORK=off go test ./...`
passes with no failures and no `SKIP`, from `registry/`); "covered" is defined by
each Decision's Verification list (`project/design/INDEX.md` resolves an id to its
Decision). Every phase's acceptance bar must be expressed as **deterministic exit
conditions** — mechanically-checkable predicates, reproducible on identical repo
state, whose passing state is actually reachable — never a subjective prose
judgment and never a self-referential or unsatisfiable check.

A **structural phase** (one that realizes a structural Decision and owns no ids) is
held to the same bar with a deterministic non-id check: the green build plus a
named smoke (e.g. a `go list -deps` command producing no output), never a prose
claim.

## Layout

The plan is **split for addressability**:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus one line
  per **pending** phase in build order, and the **only** home of a phase's `⬜`
  marker.
- `project/plan/phase-NN.md` — one body file per **pending** phase (zero-padded;
  sub-phases keep their suffix, e.g. `phase-02a.md`). Carries no status marker of
  its own.
- `project/plan/README.md` — these static, invariant rules; it never grows with
  the project.

Completion is deletion for this layout: the build loop's only mutation on a pass is
removing the finished phase's `STATUS.md` line together with its `phase-NN.md` in
the completion commit. The `Next phase` counter is **never decremented** and never
otherwise touched by the loop — only `$seal-spec` bumps it, when it appends a new
phase.
