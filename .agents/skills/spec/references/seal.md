# Sealing designs into build instructions ($seal-spec)

See `../SKILL.md` for the layout, id rules, and the canonical tag/grep.

1. **Check the gates exist.** Confirm `AGENTS.md` (beside `specs/`) declares a gate section (toolchain + ordered gate commands). If it does not, stop and say so — verify has nothing to run without it.
2. **Ensure the loop exists.** For each of `specs/loops/{gather,build,verify}.md` that is missing, copy it from this skill's `assets/loops/`. Create `specs/build/` and `specs/issues/` if absent.
   **Reset stale build state.** `specs/build/` is gitignored working state that may hold a previous run's residue. Remove any leftover `brief.md` and overwrite `PLAN.md` from scratch, so the loop starts from the freshly computed gap. (The gap already excludes completed requirements — they have tests — so a fresh plan contains only outstanding work.)
3. **Compute the gap** with the canonical greps in `../SKILL.md`: ids in design but not tests → add; ids in tests but not design → remove.
4. **Write `specs/build/PLAN.md`** — the gap ordered into phases. A phase is one or more requirement ids that form one minimal, testable change; its ids share a single action. Order phases logically (dependencies inferred from the design: build a type before the operation that returns it, an earlier state before a later one). Keep each phase ultra-brief — the action and its ids only; later agents resolve the ids against `specs/`. Overwrite the file per gap.

```
1. [add] R-NEDL-QRWM, R-QRWM-NEDL
2. [remove] R-ABCD-EFGH
```

The plan only orders the current gap. Executing the steps and verifying adequacy belong to the loop.

5. **Commit the seal.** Stage and commit everything the sealed state depends on — the design documents, `AGENTS.md`, and `specs/loops/` (`specs/build/` and `specs/issues/` are gitignored working state), plus any other uncommitted project files the loop will build against. Use the project's commit conventions (no `Requirements:` trailer — that belongs to phase commits). This pins the exact contract the loop starts from, so every later phase commit diffs against a known seal point.

Then run the loop:

```
ralph specs/loops/gather.md specs/loops/build.md specs/loops/verify.md
```
