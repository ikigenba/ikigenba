# Checking a design (check-spec)

See `../SKILL.md` for the layout, id rules, and the canonical tag/grep.

A check confirms the design is buildable and pins the exact contract a build run starts from, so every phase commit the run makes diffs against a known point. It writes no plan and materializes no prompts; the run discovers the gap itself.

1. **Check the ground exists.** Confirm `AGENTS.md` (beside `specs/`) declares the toolchain, the test-file set, the ordered gate commands, and the commit convention. If it does not, stop and say so — the run halts without it.
2. **Check the proofs.** Every requirement that states a fact about an external dependency (see `draft.md`, "Never assume an external dependency") must be backed by an observation of the real thing before the check. Verification gathered at any earlier point counts; do not re-run it. If any such fact is still unproven, stop and gather the proof (or ask the user to) before checking.
3. **Show the gap.** Run the canonical greps and report the ids to add and to remove, grouped by design document. This is information for the human, not a plan for the run.
4. **Commit the baseline.** Stage and commit everything the checked state depends on — the design documents and `AGENTS.md` (`specs/issues/` is gitignored working state), plus any other uncommitted project files the run will build against. Use the project's commit conventions with no `Requirements:` trailer — that belongs to phase commits.

The human then starts the run with the `build-spec` skill, from the directory that holds `specs/` and `AGENTS.md` — the sub-project directory, never the git root. An agent never starts it.
