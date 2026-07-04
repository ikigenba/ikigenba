# github — Plan

**Authority: construction order and history.** This document and the
`project/plan/` directory it heads own the **build order** and the **record of
what has been built**. The plan is **append-only**: completed phases are never
rewritten or deleted, so the plan doubles as the construction history. To extend
it, update `project/product/` and `project/design/` **in place** first, then
**append** a new phase here — a new `project/plan/phase-NN.md` body plus a new line
in `project/plan/STATUS.md`. Never edit a finished phase except to flip its status
marker in `STATUS.md`.

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

## Layout

The plan is **split for addressability**:

- `project/plan/STATUS.md` — the manifest: one line per phase in build order, and
  the **only** home of each phase's status marker (`⬜` not started / `✅` done).
- `project/plan/phase-NN.md` — one body file per phase (zero-padded; sub-phases
  keep their suffix, e.g. `phase-04a.md`). Carries no status marker of its own.
- `project/plan/README.md` — these static, invariant rules; it never grows with
  the project.

Append-only for this layout: never rewrite or delete a `phase-NN.md`, never delete
a `STATUS.md` line; the only build-time mutation is flipping one phase's `⬜ → ✅`
in `STATUS.md`.
