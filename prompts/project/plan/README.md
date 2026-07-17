# prompts — Plan

**Authority: construction order.** This document and the `project/plan/` directory it heads own the build order of **pending** work only. Completion is deletion: the build loop removes the finished phase's `STATUS.md` line and its `phase-NN.md` in the completion commit; history lives in git, never in the spec. To extend the plan, update product and design in place, then **append** a new phase — a new `project/plan/phase-NN.md` plus a new line at the end of `project/plan/STATUS.md`, numbered from the `Next phase` counter — never renumber, never reuse a number.

**Coverage invariant.** Every *current* design Verification id is either already realized — its id appearing verbatim as a tag in a test file that runs under the suite — or assigned to exactly one pending phase: no current id unassigned, none split, none duplicated across pending phases.

**One phase = one package = one build-turn context.** Each phase is a single coherent unit — almost always one package — sized so the build loop can carry it in one fresh build-turn context and ideally finish it in a turn or two, reading only that package's design Decisions and the *interfaces* (not internals) of the packages it depends on. This is what keeps every phase the size of a small standalone tool no matter how large the project grows. If a single package must split across phases to fit one context, the affected phase files state the partial-Decision split explicitly.

**Done bar.** A phase is **done** when every Verification item (the `R-XXXX-XXXX` ids) in the design Decisions it realizes — or the slice of those ids assigned to it — is covered by a clearly-named test and the suite is green. See the design's *Conventions* section for what "green" concretely means and each Decision's Verification list for what "covered" means. Every phase's acceptance bar is deterministic exit conditions, never a subjective judgment, never a self-referential/unsatisfiable check.

## Layout

`project/plan/STATUS.md` is the manifest: the `Next phase` counter plus the **only** home of the `⬜` pending markers; `project/plan/phase-NN.md` is one body file per **pending** phase (zero-padded; sub-phases keep their suffix, e.g. `phase-07a.md`); `project/plan/README.md` is these static rules. Completion is deletion: the build loop's only mutations are removing a finished phase's `STATUS.md` line together with its `phase-NN.md`; the counter is never decremented and never touched by the loop.
