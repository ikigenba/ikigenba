---
name: check-spec
description: Report whether a design is buildable — AGENTS.md declared, toolchain declared, boundary respected — and show the gap. Discretionary feedback the human runs when they choose: it gates nothing, commits nothing, changes nothing, and is never a step before build-spec or in any other sequence.
---

# check a design

Load the sibling `spec` skill for the layout, id rules, and the canonical tag/grep.

A check reports whether the design is buildable and what the gap is. It is feedback for the human, run whenever they want to know the state of things: it gates nothing, commits nothing, and edits nothing. It writes no plan and materializes no prompts; the run discovers the gap itself.

1. **Check the sub-project's `AGENTS.md`.** Confirm `AGENTS.md` (beside `specs/`) declares the toolchain, the test-file set, the ordered gate commands, and the commit convention. If it does not, stop and say so — the run halts without it.
2. **Check the toolchain.** Flag a tool the design shells out to that `AGENTS.md`'s toolchain does not declare.
3. **Check the boundary.** Confirm no requirement reaches outside the sub-project's own directory or encodes the internals of an external tool the sub-project depends on: no path into a sibling sub-project, no build of one, no write into one, no reliance on where a tool is installed, no claim about the content of relayed output, no dependence on anything but the published interface (see `references/design-format.md`, "Depending on an external tool", and the `spec` skill's "Sub-project independence"). A dependency on a sibling or any other tool appears only as its published interface plus a pinned version. If one does not, stop and say so.
4. **Show the gap.** Run the canonical greps and report the ids to add and to remove, grouped by design document. This is information for the human, not a plan for the run.
5. **Report.** State what passed, what did not and why, and the gap. Commit nothing.

This skill is a discretionary tool the human runs when they choose to. It is not a step in any sequence: no other skill or process depends on it, precedes it, or follows it, and an agent never starts it or suggests running it.
