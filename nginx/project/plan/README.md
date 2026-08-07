# nginx — Plan

**Authority: construction order.** This document — and the `project/plan/`
directory it heads — owns the build order of this tree's **pending** work only.
It does **not** restate *why* (that is `project/product/README.md`) or
*how/proof* (that is `project/design/`); it orders design's Decisions into
dependency-respecting phases. **Completion is deletion**: when a phase finishes,
its `STATUS.md` line and its `phase-NN.md` are removed in the completion commit —
the plan never holds finished work, so it can never contradict a design that has
since moved on. Construction history lives in git, not here. To extend the plan,
first update product and design *in place*, then **append** a new phase — a new
`project/plan/phase-NN.md` body plus a new line in `project/plan/STATUS.md`,
numbered from the `Next phase` counter — never renumber, never reuse a number.

## Coverage invariant

Every *current* design Verification id is either already **realized** — its id
appearing verbatim as a tag in a test file that runs under the suite — or
assigned to **exactly one** pending phase: no current id unassigned, none split,
none duplicated across pending phases. This tree currently mints **no ids**
(design's *Requirement ids* says why), so the invariant is satisfied vacuously
and every phase here is a structural phase. That is a fact about today's
Decisions, not an exemption: the moment a Decision mints an id, the invariant
binds normally.

## One phase = one unit = one build-turn context

Each phase is a single coherent unit of work — here, one Decision's artifacts —
scoped to that unit's design Decisions and sized so a build turn can carry it in
one fresh context. Sizing a phase as large as cleanly fits one turn is good:
fewer cycles, less context churn. No phase edits, tests, or builds anything
outside `nginx/`; work that would require touching a service's `etc/nginx.conf`,
the dashboard's apex block, `bin/`, `opsctl/`, or the repo-root `deploy.md`
belongs to the tree that owns it and is a blocked phase to report, never a
license to cross.

## Done bar

A phase is **done** when the tree is green and its own deterministic exit
conditions hold. "Green" is defined in design's *Conventions*
(`project/design/README.md`): from the repo root, `bash -n nginx/run` exits 0 and
`mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t` exits 0. Because no
Decision here mints ids, each phase's bar is a structural check instead of a
tagged-test check — exact named files at exact paths, an exact `diff` or grep
result, a real `nginx -t` — never a prose judgment, never a self-referential or
unsatisfiable check (classically a grep for a phrase this tree's own `project/`
docs also contain, which can never come back empty; any such grep must exclude
`project/`). Behavior that only a real nginx, a real certificate authority, or
the running suite can falsify is **not** a phase exit condition: design names it
as a manual check outside the gate, and a phase never claims it.

## Layout

The plan is **split for addressability** so a build turn reads only the one phase
it is working on, never the whole queue:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus the
  **only** home of the pending marker (`⬜`) for each pending phase.
- `project/plan/phase-NN.md` — one body file per **pending** phase, zero-padded
  (`phase-01.md`, `phase-02.md`, …; a sub-phase keeps its suffix, e.g.
  `phase-03a.md`). The body carries **no** status token.
- `project/plan/README.md` — this file: the static, invariant rules. It lists no
  phases and carries no status, so it never grows with the project.

Completion-is-deletion, restated for this layout: the only mutations on
completion are removing a finished phase's `STATUS.md` line together with its
`phase-NN.md` in the completion commit. The counter is never decremented and
never touched except to bump it when a new phase is appended.
