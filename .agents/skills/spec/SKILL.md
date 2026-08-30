---
name: spec
description: The specs/ system — layout, requirement ids, and the $open-spec / $seal-spec / $audit-spec operations.
---

# specs/

Spec-driven development: designs define the contract, a mechanical gap drives a build loop, and every requirement is tracked by a permanent id.

## Layout

- `specs/design/` — design documents (`D<int>-<slug>.md`).
- `specs/build/PLAN.md` — the current gap, ordered into phases.
- `specs/build/brief.md` — the current build phase (loop state).
- `specs/loops/{gather,build,verify}.md` — the vendored loop prompts ralph runs.
- `specs/issues/` — escalation channel; one markdown file per open issue.

## Requirement ids

- Mint every id with `idgen`; ids have the form `R-XXXX-XXXX`. `idgen` guarantees global uniqueness.
- Never hand-author, edit, or reuse an id. An id is permanent once minted.
- An id's **requirement text is equally permanent**. Changing it at all — including a pure rewording — means deleting that requirement and minting a new id. The gap is computed from id presence alone, so an edited requirement is invisible to the loop and never gets applied. See `references/design.md`.

## Tagging and the gap (canonical)

Every test marks the requirement id it covers with the id in a comment, or in a string where a comment cannot sit. The id appears verbatim so it is greppable. This is the single source of truth for how ids are tagged and found; `$seal-spec`, `$audit-spec`, and the loop prompts all rely on it identically.

Enumerate ids over two file sets — the design documents, and the project's test files as declared in `AGENTS.md` (the same declared set for every consumer, so all compute the identical gap):

```
design ids: grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' specs/design            | sort -u
test ids:   grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' <AGENTS.md test files>  | sort -u
```

The gap is the diff: an id in design but not tests must be **added**; an id in tests but not design must be **removed**. Presence alone defines the gap; adequacy is judged later (verify, audit).

`$seal-spec` orders the gap into **phases**. A phase is one or more requirement ids that form one minimal, testable change; the loop closes one phase per cycle.

## Project gates (AGENTS.md)

Each project has an `AGENTS.md` beside `specs/` that declares the concrete verification gates. The specifics are kept here, not in this skill, because projects differ. It must declare:

- **Toolchain**: the tools and versions that must be present.
- **Test files**: where the project's tests live (the file set the canonical gap greps for ids).
- **Gates**: an ordered list of exact commands, each of which must exit 0 to pass — there may be several (for example tests, end-to-end tests, and linting). All must pass, with no skips laundering a failure.
- **Commit conventions**: the phase-commit message format verify follows, plus any co-author/session trailer. Default:

  ```
  <imperative summary of the phase, <=50 chars>

  <optional: one or two lines on what changed and why>

  Requirements: R-XXXX-XXXX, R-YYYY-YYYY
  ```

  The `Requirements:` trailer lists the phase's ids, so history stays greppable by id like the tests.

verify reads `AGENTS.md` directly (the authoritative, independent source of gates). gather folds the toolchain and gate commands into the brief so build — which reads only the brief — can run them. If a required tool or version is absent, verify cannot run the gate; it files an issue (an environment blocker, out-of-role) rather than passing or skipping.

## Operations

- `$open-spec` — author a design. Read `references/design.md`.
- `$seal-spec` — turn designs into build instructions. Read `references/seal.md`.
- `$audit-spec` — audit test adequacy. Read `references/audit.md`.
