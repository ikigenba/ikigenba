# appkit — Plan

**Authority: construction order.** This document and the `project/plan/`
directory it heads own the **build order** of appkit's ralph-governed
**pending** work only. Completion is deletion: the build loop removes the
finished phase's `STATUS.md` line and its `phase-NN.md` in the completion
commit — construction history lives in git, not here. To extend the work,
update the design (`project/design/README.md` + `project/design/`) **in
place**, then **append** a new phase — a new `project/plan/phase-NN.md` plus a
new line in `project/plan/STATUS.md`, numbered from the `Next phase` counter.
Never renumber a phase and never reuse a number.

**Coverage invariant.** Every **current** design Verification id is either
already **realized** — its id appearing verbatim as a tag in a test file that
runs under the suite — or assigned to **exactly one pending** phase: no current
id unassigned, none split, none duplicated across pending phases. Design
(rewritten in place) is the denominator; realized-ness is read from the code
itself, never from a ledger or history kept here.

**One phase = one coherent increment.** Each phase is a single coherent unit sized
for one subagent, built against the design Decision(s) it realizes (resolved
through `STATUS.md` → `phase-NN.md` → the brief). The phases are **sequential and
dependency-ordered**: each phase depends only on earlier ones (e.g. in the
manifest thread the reader moved onto `current` (D1) before the repo-root
launcher was re-shaped to feed it (work since re-homed to the suite-level
workspace); in the chassis thread the `appkit/web` package
(D6) exists before the server integration (D7) mounts it).

**Boundary note.** In the layout-parity thread only D1 is `appkit/`-package
work; the repo-root readers `bin/registry` and `bin/start` are governed by
**`bin/project/`** and tested by the **`bin/bintest`** module under `bin`'s own
gate (the repo-root aggregate gate is `suite_test.go` in the repo-root module),
never by an appkit Decision or phase. D4's live-box action
crosses into one production step and is a live-box check recorded in
`project/appkit-verification.md`, not an unattended loop build. In the chassis thread, D7's dev wiring (a one-line
`<APP>_WWW_PATH` export in a converted service's `bin/start` launch function)
crosses the same way and is verified by the live `bin/start` smoke.

**Done bar.** A phase is **done** when every Verification id in the design
Decision(s) it realizes is covered by a clearly-named, genuinely-asserting test (or
the named live check) and the relevant suite is green. For appkit "green" is
defined in design's *Conventions*: from `appkit/`, `go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed. For
the integration/box ids, the named live check passes.

## Layout

The plan is physically split so the build loop reads only what it needs:

- `project/plan/STATUS.md` — the manifest: the `Next phase` counter plus the
  **only** home of the `⬜` pending markers.
- `project/plan/phase-NN.md` — one body file per **pending** phase
  (zero-padded). A phase body carries **no** status token — status lives only
  in `STATUS.md`.
- `project/plan/README.md` — this file: the static rules (plus operator notes
  below). It never accumulates phase content.

Completion is deletion: the build loop's only mutations to this directory are
removing a finished phase's `STATUS.md` line together with its `phase-NN.md`.
The `Next phase` counter is never decremented and never touched by the loop.

## Operator steps (outside the unattended loop)

Some Verification lives on the **live `int` box** and cannot be a `STATUS.md`
phase: the unattended loop is forbidden to `ssh int` / `opsctl` / mutate the box,
so an id whose only pass-predicate is a live-box command could never clear its
pending `⬜` line and would make the loop non-convergent. Those live-box ids —
their procedures, pass-predicates, and running record of results — live in
**`project/appkit-verification.md`** (mirroring opsctl's
`project/opsctl-verification.md`), run **only on explicit instruction to
deploy/verify**, and are deliberately **absent from `STATUS.md`** so
`gather`/`verify` never treat them as loop work. The coverage ratchet in
`project/loops/verify.md` reads the live-tracked id set from that doc's check
headers.

One operator note that is a mechanical sibling-module sweep, not a live-box
verification id, stays here:

**Registry replace mirror (D10, operator-applied).** Phase 10 makes appkit
require the in-repo `registry` module. A dependency's `replace` directives are
not transitive, so every module that requires appkit must carry its own
`replace registry => ../registry` (plus the `require registry v0.0.0` Go adds
on tidy) — exactly as the `eventplane` require already forced on all of them.
notify, prompts, scripts, and sites already carry it; the remaining consumers
of appkit (crm, cron, dashboard, dropbox, github, gmail, ledger, webhooks,
wiki) need the one-line addition before their next `GOWORK=off` build
(`bin/ship`) will succeed. This is a mechanical sweep across sibling modules
appkit phases must not edit, applied by the operator (or each service's own
workspace) alongside landing Phase 10. Check:
`grep -L "replace registry" */go.mod` from the repo root lists only modules
that do not require appkit (`eventplane`, `registry`, and `opsctl` if it stays
appkit-free).
