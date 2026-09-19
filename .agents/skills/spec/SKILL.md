---
name: spec
description: The specs/ system — layout, requirement ids, the canonical gap, project ground, issue filing, and the story and design formats. Shared foundation for the draft-stories / draft-spec / check-spec / build-spec / audit-spec skills.
---

# specs/

Spec-driven development: designs define the contract, a mechanical gap says what is unbuilt, and every requirement is tracked by an id that names exactly one text.

The specs describe the **current target**, not a commitment to earlier designs. A design iteration may replace names, boundaries, and behavior that earlier iterations established, and the code then realizes that replacement completely. Existing code, tests, and package layout are not compatibility obligations unless a current requirement states one. Superseded requirements are deleted from the design; git holds the history.

## Layout

- `specs/stories/` — user stories, one group per file (`S<int>-<slug>.md`). The intent the designs realise; written first, carry no ids. Format: `references/story-format.md`.
- `specs/design/` — design documents (`D<int>-<slug>.md`). Human-authored.
- `specs/issues/` — escalation channel; one markdown file per open issue, named `<slug>.md` (issues carry no minted id).
- `AGENTS.md` — beside `specs/`; the project's ground (below). Human-authored.

Those four entries are the whole of `specs/`. Nothing else is created under it: no review, evidence, ledger, or progress files. Working material a skill needs while it runs — a coverage ledger, verifier reports, gathered observations, alternatives under consideration — goes to an ephemeral scratch file outside the repository (the `handoff` skill's scratch-file convention), is reported to the user, and is never committed.

## Requirement ids

Designs are never frozen. Any requirement can be replaced at any time, and a requirement is never a reason a design cannot change; only the build run and `audit-spec` treat the design as read-only. The one rule is about ids, not about the design: **an id names exactly one text**. New text means a new id.

- Mint every id with `idgen`; ids have the form `R-XXXX-XXXX`. `idgen` guarantees global uniqueness. Only `draft-spec` mints ids; the build run and `audit-spec` never invoke `idgen`, and it is not part of the toolchain `AGENTS.md` declares.
- Never hand-author or reuse an id. Once minted, an id is bound to the text it was minted for.
- To change a requirement's text at all — including a pure rewording — delete that requirement and mint a new id for the new text. The gap is computed from id presence alone, so text edited beside an existing id is invisible and never gets applied. See `references/design-format.md`.

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
- **Commit conventions**: the phase-commit message format; attribution is the repository's rule, not the project's. Default:

  ```
  <imperative summary of the phase, <=50 chars>

  <optional: one or two lines on what changed and why>

  Requirements: R-XXXX-XXXX, R-YYYY-YYYY
  ```

  The `Requirements:` trailer lists the phase's ids, so history stays greppable by id like the tests.

If a required tool or version is absent, a gate cannot run; that is an issue (an environment blocker), never a pass or a skip.

## Project independence

A monorepo holds several projects below one git root; each is independent and stays that way. A project's `specs/` govern that project's own directory and nothing else.

- **Never reach into a sibling's tree.** No requirement names a path inside another project, builds or reads another project's source, or writes into another project's directory.
- **A sibling is consumed only as an installed external tool**, with the same standing as `ssh`, `git`, or a compiler: its published interface, never its internals or which release of it to use. See `references/design-format.md`, "Depending on an external tool".
- **Dependencies point one way and are declared.** If two projects would each have to know about the other, one of them is wrong. A need only the other project can satisfy is filed in `specs/issues/` for a human to adjudicate, never designed around by reaching across the boundary.

## Filing an issue

`specs/issues/` is the escalation channel for friction that cannot be resolved in-role — a wrong seam, contradictory requirements, a missing dependency, broken tooling. It is distinct from a gap a builder can close within the current contract. Only the unattended runs file issues: `build-spec` and `audit-spec`. An interactive skill such as `draft-spec` has the user present and asks instead.

- One markdown file per issue, named `specs/issues/<slug>.md`. Issues carry no minted id; nothing outside `draft-spec` invokes `idgen`.
- Contents: filing context, the requirement id(s) involved, the friction, why it is unresolvable in-role, evidence (conflicting ids, failing command output), and a suggested resolution.
- An issue must carry proof; a vague "cannot proceed" issue is invalid.
- Resolve by deleting the file (git holds history). The gate is simply whether `specs/issues/` is empty.
- Any open issue halts the build run.

## Operations

Each operation is a sibling skill. All five load this one for the shared rules above.

- `draft-stories` — turn the user's intent into stories under `specs/stories/`, new or updated, grilling the user for what the intent leaves open. The format is `references/story-format.md` in this skill.
- `draft-spec` — author a design, and the `AGENTS.md` ground beside it, by recursive delegation from user stories, for one sub-project at a time. The design format and id rules it authors against are `references/design-format.md` in this skill.
- `check-spec` — report whether the design is buildable and show the gap. Feedback only; it gates nothing and commits nothing.
- `build-spec` — close the mechanical gap by recursive delegation.
- `audit-spec` — audit adequacy of tests for ids already proved on both sides.

`draft-stories`, `draft-spec`, `build-spec`, and `audit-spec` each load
`fanout` for delegation and independent verification. Invoke the desired
operation directly; naming `fanout` separately is unnecessary. Each operation
supplies its own goal, completion criteria, authority, and reporting channel.

`check-spec`, `build-spec`, and `audit-spec` are human-gated: an agent never starts one on its own. `build-spec` and `audit-spec` commit, edit tests, or both; `check-spec` only reports.
