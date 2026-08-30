# Authoring a design document ($open-spec)

See `../SKILL.md` for the layout and id rules.

First establish what is already known by reading `specs/design/` and the codebase. If open questions remain that only the user can answer, run the `grill-me` procedure (one question at a time, each with your recommendation) until resolved. If the design is already fully determined, skip straight to writing.

A design document defines a feature or subsystem. It defines the public contract between modules and nothing about how modules are implemented internally.

## Scope

Litmus test: if something could change without any other module noticing or breaking, it is an implementation detail and is out of scope. If changing it would break or surprise a consumer, it is part of the contract and is in scope.

In scope (the public surface):

- Domain language: the concrete, shared vocabulary — entities, terms, and their definitions.
- Module boundaries and responsibilities: what modules/subsystems exist and what each owns.
- Names of public things: modules, types, constants, operations.
- Public types and data shapes that cross a boundary, including their fields.
- Operation signatures: name, parameters, return type, and errors/failure modes surfaced. The shape only, never the body. (e.g. an exported Go function signature or interface, a module's exported JavaScript functions.)
- Interfaces/protocols the module implements or depends on.
- Constants that are part of the contract: limits, defaults, enumerated values, error codes.
- Dependencies and their direction: who depends on whom.
- Observable behavior and invariants: what an operation does as seen from outside, and pre/postconditions at the boundary. Expressed as requirements (below), not as procedure steps.
- State machine, when the subsystem is stateful: the set of states, the events/operations that trigger transitions, which transitions are allowed, guards on them, and the observable effect of each.

Out of scope (implementation):

- Function/procedure bodies: algorithms, control flow, step-by-step logic.
- Private types, helpers, and local state.
- Internal data-structure choices a consumer cannot observe.
- Internal ordering of steps, micro-optimizations, and caching, unless a specific guarantee is itself part of the contract.
- How state is stored or how transition logic is coded.
- Anything a consumer can neither see nor depend on.

## Filename

Create the file in `specs/design/` as `D<int>-<slug>.md`.

- `<int>` is the next design number: the max existing number in `specs/design/` plus 1 (start at 1 if none).
- Zero-pad `<int>` so every file in the folder is the same width. If a new number needs more digits, re-pad the others to match. Padding is cosmetic — `D1`, `D01`, `D001` are the same design; the number is the identity.
- `<slug>` is a short kebab-case name. It is not part of the identity.

## Contents

1. Prose describing the design element.
2. A `## REQUIREMENTS` heading, followed by a list of requirements.

## Requirements

One bullet per requirement:

```
- <id>: <requirement text>
```

- Mint each `<id>` with `idgen` (`idgen -n N` mints N at once). See `../SKILL.md`.
- `<requirement text>` uses a modal verb (MUST/SHOULD/MAY) and states one testable assertion.
- A requirement must be testable: there must be a finite, deterministic procedure that decides whether it holds, and running that procedure must be efficient. Do not write requirements whose only test would be to exhaustively check an infinite or intractable set of cases.

## Changing a design

Design documents are never sealed; they may change at any time. Their prose may be rewritten freely. **Requirement text may not.**

- **A requirement's text is frozen the moment its id is minted.** To change it — by any amount, for any reason — delete the requirement and add a new one with a freshly minted id. There is no exception: not a typo, not a renamed symbol, not a clarification, not a rewording that means exactly the same thing. Never edit the text beside an existing id, and never reuse an id.
- Why the rule is absolute: the gap is computed from **id presence alone** (see `../SKILL.md`), so an edited requirement produces no gap entry. The loop never sees it, the tagged test is never revisited, and the design and the suite silently disagree — the id now points at text no test was ever checked against. A new id makes the change visible as `[add]`, and deleting the old one makes the stale test visible as `[remove]`.
- Do not reason about whether a change is "material" or "just wording." That judgement is what fails: a reworded requirement still means the same thing, which is precisely why it feels safe to edit and why the omission goes unnoticed. Text changed → new id. The only edits that keep an id are ones that leave its text byte-identical.
- After editing any design document, recompute the gap and confirm every change you made appears in it. A change you intended that produces no gap entry is a change the loop cannot apply.
- The `REQUIREMENTS` list holds only the current contract. Superseded requirements are deleted; git holds the history.

## Example

```
# D01-upload-limits

Uploads are size-capped to protect storage.

## REQUIREMENTS

- R-NEDL-QRWM: The system MUST reject uploads larger than 10 MB.
- R-QRWM-NEDL: The system SHOULD return a clear error message on rejection.
```
