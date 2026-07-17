# repos — Plan

**Authority: construction order.** This document and the `project/plan/`
directory own the build order of **pending** work only. Completion is
deletion: the build loop removes the finished phase's `STATUS.md` line and
its `phase-NN.md` in the completion commit; construction history lives in
git, never here. To extend the project: update product and design in place
first, then **append** a new `phase-NN.md` and its `STATUS.md` line, numbered
from the `STATUS.md` counter — never renumber, never reuse a number. The
**coverage invariant**: every *current* design Verification id is either
already **realized** — its id appearing verbatim in a tagged test file that
runs under the suite — or assigned to **exactly one** pending phase; no
current id unassigned, none split, none duplicated across pending phases.

**One phase = one package = one build-turn context.** Each phase is a single
coherent unit of work — almost always one package — scoped to that unit's
design Decisions and the *interfaces* (not internals) of the packages it
depends on, and sized so the build loop can carry it in one fresh build-turn
context. If a Decision is too large for one context it is split across
phases, each naming the slice of Verification ids it carries.

**Done bar.** A phase is **done** when every Verification id it realizes (or
its explicit slice) is covered by a clearly-named test and the suite is green
— see design's Conventions for what "green" concretely means (`go build
./...`, `go vet ./...`, `go test ./...` clean and `gofmt -l .` empty, from
`repos/`). Every phase's acceptance bar is deterministic exit conditions,
never a subjective judgment, never a self-referential or unsatisfiable check.

## Layout

`STATUS.md` is the manifest: the `Next phase` counter plus the **only** home
of the pending `⬜` markers; `phase-NN.md` is one body file per **pending**
phase (zero-padded; sub-phases keep their suffix, e.g. `phase-07a.md`); this
README is the static rules. Completion-is-deletion in the layout too: the
build loop's only mutations are removing a finished phase's `STATUS.md` line
together with its `phase-NN.md`; the counter is never decremented and never
touched by the loop.
