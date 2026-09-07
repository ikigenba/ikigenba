---
name: spec
description: The specs/ system — layout, requirement ids, the gap, and the draft-spec / check-spec / build-spec / audit-spec operations.
---

# specs/

Spec-driven development: designs define the contract, a mechanical gap says what is unbuilt, and every requirement is tracked by a permanent id.

The specs describe the **current target**, not a commitment to earlier designs. A design iteration may replace names, boundaries, and behavior that earlier iterations established, and the code then realizes that replacement completely. Existing code, tests, and package layout are not compatibility obligations unless a current requirement states one. Superseded requirements are deleted from the design; git holds the history.

## Layout

- `specs/design/` — design documents (`D<int>-<slug>.md`). Human-authored.
- `specs/issues/` — escalation channel; one markdown file per open issue, named `<slug>.md` (issues carry no minted id).
- `AGENTS.md` — beside `specs/`; the project's ground (below). Human-authored.

## Requirement ids

- Mint every id with `idgen`; ids have the form `R-XXXX-XXXX`. `idgen` guarantees global uniqueness. Only `draft-spec` mints ids; the build run and `audit-spec` never invoke `idgen`, and it is not part of the toolchain `AGENTS.md` declares.
- Never hand-author, edit, or reuse an id. An id is permanent once minted.
- An id's **requirement text is equally permanent**. Changing it at all — including a pure rewording — means deleting that requirement and minting a new id. The gap is computed from id presence alone, so an edited requirement is invisible and never gets applied. See `references/draft.md`.

## Tagging and the gap (canonical)

Every test marks the requirement id it covers with the id in a comment, or in a string where a comment cannot sit. The id appears verbatim so it is greppable. This is the single source of truth for how ids are tagged and found; every consumer — the build run, `audit-spec`, a human — relies on it identically.

Enumerate ids over two file sets — the design documents, and the project's test files as declared in `AGENTS.md` (the same declared set for every consumer, so all compute the identical gap):

```
design ids: grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' specs/design            | sort -u
test ids:   grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' <AGENTS.md test files>  | sort -u
```

The gap is the diff: an id in design but not tests must be **added**; an id in tests but not design must be **removed**. Presence alone defines the gap; adequacy is judged separately (audit, and the adversarial check inside the run). A revision that replaces a requirement therefore appears as a paired add and remove, and the pair is landed together so the old shape is retired, never bridged.

## Project ground (AGENTS.md)

Each project has an `AGENTS.md` beside `specs/` that declares the concrete ground the gap is computed against. That directory — the parent of `specs/` — is the project's working directory: every gate command and path is relative to it, and a monorepo may hold several such projects below one git root. The git root's own `AGENTS.md` is repo-wide guidance, never a project's ground; the ground is always the `AGENTS.md` that sits beside `specs/`. It is authored with the design, by a human, and is read-only to the run. It must declare:

- **Toolchain**: the tools and versions the gates need. `idgen` is never listed here; it is an authoring tool, not a build tool.
- **Test files**: where the project's tests live (the file set the canonical gap greps for ids).
- **Gates**: an ordered list of exact commands, each of which must exit 0 to pass — there may be several (for example tests, end-to-end tests, and linting). All must pass, with no skips laundering a failure. A per-finding suppression comment (`nolint`, `llm-lint:ignore`, `eslint-disable`, and the like) is a skip: the run never adds one; a finding it cannot fix below the contract seam, or believes is wrong, is filed as an issue for a human to adjudicate.
- **Commit conventions**: the phase-commit message format, plus any co-author/session trailer. Default:

  ```
  <imperative summary of the phase, <=50 chars>

  <optional: one or two lines on what changed and why>

  Requirements: R-XXXX-XXXX, R-YYYY-YYYY
  ```

  The `Requirements:` trailer lists the phase's ids, so history stays greppable by id like the tests.

If a required tool or version is absent, a gate cannot run; that is an issue (an environment blocker), never a pass or a skip.

## Operations

- `draft-spec` — author a design, and the `AGENTS.md` ground beside it. Read `references/draft.md`.
- `check-spec` — check the design is buildable and commit the baseline the run starts from. Read `references/check.md`.
- `audit-spec` — audit test adequacy, by recursive delegation like `build-spec`. Read `references/audit.md`.

Closing the gap is the `build-spec` skill. It is human-gated: an agent never starts it on its own.
