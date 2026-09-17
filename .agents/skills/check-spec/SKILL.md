---
name: check-spec
description: Check that a design is buildable — ground declared, external facts proven, boundary respected — show the gap, and commit the baseline a build run starts from. Human-gated; never self-invoked, since it commits.
---

# check a design

Load the sibling `spec` skill for the layout, id rules, and the canonical tag/grep.

A check confirms the design is buildable and pins the exact contract a build run starts from, so every phase commit the run makes diffs against a known point. It writes no plan and materializes no prompts; the run discovers the gap itself.

1. **Check the ground exists.** Confirm `AGENTS.md` (beside `specs/`) declares the toolchain, the test-file set, the ordered gate commands, and the commit convention. If it does not, stop and say so — the run halts without it.
2. **Check the proofs.** Every requirement that states a fact about an external dependency must be backed by the kind of proof the `spec` skill's `references/design-format.md` ("Never assume an external dependency") prescribes for that kind of fact. Flag: a fact about a vendor service or live system state with no recorded observation; a tool the design shells out to that `AGENTS.md`'s toolchain does not declare; a claim about a tool's behavior that its published documentation does not state and no observation backs. Do not flag the documented behavior of a well-known tool — a manual page is proof of what it documents. Verification gathered at any earlier point counts; do not re-run it. If a fact is genuinely unproven, stop and gather the proof (or ask the user to) before checking.
3. **Check the boundary.** Confirm no requirement reaches outside the project's own directory or encodes the internals of an external tool the project depends on: no path into a sibling project, no build of one, no write into one, no reliance on where a tool is installed, no claim about the content of relayed output, no dependence on anything but the published interface (see `references/design-format.md`, "Depending on an external tool", and the `spec` skill's "Project independence"). A dependency on a sibling or any other tool appears only as its published interface plus a pinned version. If one does not, stop and say so.
4. **Show the gap.** Run the canonical greps and report the ids to add and to remove, grouped by design document. This is information for the human, not a plan for the run.
5. **Commit the baseline.** Stage and commit everything the checked state depends on — the design documents and `AGENTS.md` (`specs/issues/` is gitignored working state), plus any other uncommitted project files the run will build against. Use the project's commit conventions with no `Requirements:` trailer — that belongs to phase commits.

The human then starts the run with the `build-spec` skill, from the directory that holds `specs/` and `AGENTS.md` — the sub-project directory, never the git root. An agent never starts it.
