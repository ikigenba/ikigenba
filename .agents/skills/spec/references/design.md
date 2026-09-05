# Authoring a design document ($open-spec)

See `../SKILL.md` for the layout and id rules.

First establish what is already known by reading `specs/design/` and the codebase. If open questions remain that only the user can answer, run the `grill-me` procedure (one question at a time, each with your recommendation) until resolved. If the design is already fully determined, skip straight to writing.

A design document defines a feature or subsystem. It defines the public contract between modules and nothing about how modules are implemented internally.

**The requirements are the design.** The `## REQUIREMENTS` list is the design document's entire normative content: something is part of the contract if and only if a requirement states it. The prose above the list — code blocks included — is a friendly overview for the reader, never the contract; a name, field, signature, or constant that appears only in prose is not designed, because the machinery that makes design real (the id freeze, the gap, the loop, audit) sees only requirements. If an in-scope item matters enough to write down, it matters enough to state as a requirement.

## Scope

Litmus test: if something could change without any other module noticing or breaking, it is an implementation detail and is out of scope. If changing it would break or surprise a consumer, it is part of the contract and is in scope.

Everything in scope must be captured **as requirements**, not merely described in prose — see "The requirements are the design" above.

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

1. Prose giving a friendly overview of the design element. Non-normative: it orients the reader and motivates the requirements, but it binds nothing. Illustrative code blocks are welcome here, on the same terms — illustration, not contract.
2. A `## REQUIREMENTS` heading, followed by a list of requirements. This list is the design (see above).

## Requirements

One bullet per requirement:

```
- <id>: <requirement text>
```

- Mint each `<id>` with `idgen` (`idgen -n N` mints N at once). See `../SKILL.md`.
- `<requirement text>` uses a modal verb (MUST/SHOULD/MAY) and states one testable assertion.
- A requirement must be testable: there must be a finite, deterministic procedure that decides whether it holds, and running that procedure must be efficient. Do not write requirements whose only test would be to exhaustively check an infinite or intractable set of cases.
- Never assume an external dependency. A fact about something outside the project — a vendor host, a protocol's required fields, a credential a host honors, a limit — is proven by observing the real thing (a live request, a real response) before it is written into a requirement. Prior art and memory are not proof.

Requirements come in two forms, and a design needs both:

- **Structural** — declares a public name and its shape: a module and what it exports, a type and its exact fields, an operation's signature, an enumeration's members, a contract constant's name and value. Structural requirements are testable by construction: code referencing the declared shape compiles (or a reflection/introspection check passes). Write one requirement per declaration — the type with its field list in one requirement, not one per field — so a rename or reshape re-mints exactly one id.
- **Behavioral** — declares an observable behavior or invariant at the boundary, referring to public things by the names the structural requirements declare. A behavioral requirement mentions names; it never re-declares shapes. This keeps the blast radius of a structural change small: the reshaped declaration's id is deleted and re-minted, while behavioral requirements that merely mention the name are re-minted only if their own text must change.

If every structural requirement were deleted, the design should no longer name anything; if that is not true, some of the contract is squatting in prose.

## Changing a design

Design documents are never sealed; they may change at any time. Their prose may be rewritten freely — it is non-normative, so rewriting it changes nothing; a rewrite that *would* change the contract is really a requirement change and must be made in the `REQUIREMENTS` list. **Requirement text may not be rewritten.**

- **A requirement's text is frozen the moment its id is minted.** To change it — by any amount, for any reason — delete the requirement and add a new one with a freshly minted id. There is no exception: not a typo, not a renamed symbol, not a clarification, not a rewording that means exactly the same thing. Never edit the text beside an existing id, and never reuse an id.
- Why the rule is absolute: the gap is computed from **id presence alone** (see `../SKILL.md`), so an edited requirement produces no gap entry. The loop never sees it, the tagged test is never revisited, and the design and the suite silently disagree — the id now points at text no test was ever checked against. A new id makes the change visible as `[add]`, and deleting the old one makes the stale test visible as `[remove]`.
- Do not reason about whether a change is "material" or "just wording." That judgement is what fails: a reworded requirement still means the same thing, which is precisely why it feels safe to edit and why the omission goes unnoticed. Text changed → new id. The only edits that keep an id are ones that leave its text byte-identical.
- After editing any design document, recompute the gap and confirm every change you made appears in it. A change you intended that produces no gap entry is a change the loop cannot apply.
- The `REQUIREMENTS` list holds only the current contract. Superseded requirements are deleted; git holds the history.

## Example

```
# D01-upload-limits

Uploads are size-capped to protect storage. The `uploads` module owns the cap
and rejects oversized files at the door, before any bytes hit disk.

## REQUIREMENTS

- R-NEDL-QRWM: The `uploads` module MUST export `MaxUploadBytes = 10_485_760` and `Put(name string, r io.Reader) (URL, error)`.
- R-QRWM-NEDL: `Put` MUST reject uploads larger than `MaxUploadBytes` without persisting any bytes.
- R-XKCD-PLTE: `Put` SHOULD return a clear error message on rejection.
```

The first requirement is structural — it declares the names and shapes; the
others are behavioral and refer to those names without re-declaring them.
