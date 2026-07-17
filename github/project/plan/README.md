# github — Plan

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order of pending work only**. Completion is
deletion: when a phase is done, the build loop removes its line from
`project/plan/STATUS.md` and deletes its `project/plan/phase-NN.md` body file in
the completion commit — the plan never holds finished work, so it can never
contradict a design that has since moved on. Construction history lives in git
(the completion commits, and the deleted files recoverable there), never in the
spec. To extend the plan: update `project/product/` and `project/design/` **in
place** first, then **append** a new phase — a new `project/plan/phase-NN.md`
body plus a new line in `project/plan/STATUS.md`, numbered from the `Next phase`
counter — never renumber, never reuse a number.

## One phase = one package = one build-turn context

Each phase is a single coherent unit — almost always one package under
`github/internal/` (or the composition root) — scoped to that phase's design
Decision(s) and the *interfaces* (not internals) of the packages it depends on,
and sized so the gather→build→verify loop can carry it in **one fresh `build`-turn
context** and finish it in a turn or two (with `verify` confirming on the next
turn). The loop does not accumulate one long context across a phase: `build` runs
in a fresh context each turn and `verify` in another — so size to a single `build`
turn, not an imagined single sitting. Sizing a phase as large as cleanly fits one
`build` turn is good: one turn can then cover several Verification ids.

## Done bar

A phase is **done** when every Verification item — the `R-XXXX-XXXX` ids — in the
design Decisions it realizes, or the slice assigned to it, is covered by a
clearly-named test and the suite is green. "Green" is defined in design's
*Conventions* (`GOWORK=off go build ./...` succeeds, `GOWORK=off go test ./...`
passes with no failures and no `SKIP`, `gofmt -l .` is empty, `go vet ./...` is
clean, all from `github/`); "covered" is defined by each Decision's Verification
list (`project/design/INDEX.md` resolves an id to its Decision). Every phase's
acceptance bar must be expressed as **deterministic exit conditions** —
mechanically-checkable predicates, reproducible on identical repo state, whose
passing state is actually reachable — never a subjective prose judgment and never
a self-referential or unsatisfiable check.

One id, `R-DMUT-QF4A`, is proven against the **live** GitHub App and is **not**
covered by the offline unit suite — and therefore is **not a loop-gating id for
any phase**. The autonomous loop cannot reach it (`go test` runs with no real
GitHub, and a network test is not reproducible on identical repo state), so it is
verified **out of loop** by an operator per `project/github-verification.md`
(`bin/start` + the `github` `health` tool, plus a corrupted-key negative check).
Design still owns it under D2; the plan simply does not schedule it as loop work.

A **structural phase** (one that realizes a structural Decision and owns no ids)
is held to the same bar with a deterministic non-id check: the green build plus a
named smoke (an exact grep/command result over the built artifact), never a prose
claim.

## Coverage invariant

Every *current* design Verification id is either already **realized** — its id
appearing verbatim as a `// R-XXXX-XXXX` tag in a `*_test.go` file that runs
under the suite — or assigned to **exactly one** pending phase: no current id
unassigned, none split, none duplicated across pending phases.

## Layout

The plan is **split for addressability**:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus the
  **only** home of each pending phase's `⬜` marker.
- `project/plan/phase-NN.md` — one body file per **pending** phase (zero-padded;
  sub-phases keep their suffix, e.g. `phase-04a.md`). Carries no status marker of
  its own.
- `project/plan/README.md` — these static, invariant rules; it never grows with
  the project.

Completion-is-deletion for this layout: the build loop's only mutations are
removing a finished phase's `STATUS.md` line together with its `phase-NN.md` in
the completion commit. The `Next phase` counter is never decremented and never
touched by the loop — only `$seal-spec` bumps it, on every append.
