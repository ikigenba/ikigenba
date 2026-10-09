# Design document format

See `../SKILL.md` for the layout and id rules. This file defines what a design document is: its scope, its shape, and the rules its requirements follow. It is read by whoever authors one — the `draft-design` skill or a human — and by the build run and `audit-spec` when they judge a requirement against it. How a draft is produced is the `draft-design` skill.

A design document defines a feature or subsystem. It defines the public contract between modules and nothing about how modules are implemented internally.

**The requirements are the design.** The `## REQUIREMENTS` list is the design document's entire normative content: something is part of the contract if and only if a requirement states it. The prose above the list — code blocks included — is a friendly overview for the reader, never the contract; a name, field, signature, or constant that appears only in prose is not designed, because the machinery that makes design real (the id freeze, the gap, the build run, audit) sees only requirements. If an in-scope item matters enough to write down, it matters enough to state as a requirement.

## Scope

Litmus test: if something could change without any other module noticing or breaking, it is an implementation detail and is out of scope. If changing it would break or surprise a consumer, it is part of the contract and is in scope.

Everything in scope must be captured **as requirements**, not merely described in prose — see "The requirements are the design" above.

In scope (the public surface):

- Domain language: the concrete, shared vocabulary — entities, terms, and their definitions.
- Module boundaries: which package each exported name is imported from. A consumer proves this by importing it. A package is one concern, small enough to hold whole; keeping it so is guidance for the author and the run, not a requirement.
- Names of public things: modules, types, constants, operations.
- Public types and data shapes that cross a boundary, including their fields.
- Operation signatures: name, parameters, return type, and errors/failure modes surfaced. The shape only, never the body. (e.g. an exported Go function signature or interface, a module's exported JavaScript functions.)
- Interfaces/protocols the module implements or depends on.
- Constants that are part of the contract: limits, defaults, enumerated values, error codes. A copy constant is declared by name only; see "Copy" below.
- External tools and services the code relies on, reached through their published interfaces. Which internal package imports which is not observable and not a requirement.
- Observable behavior and invariants: what an operation does as seen from outside, and pre/postconditions at the boundary. Expressed as requirements (below), not as procedure steps.
- State machine, when the subsystem is stateful: the set of states, the events/operations that trigger transitions, which transitions are allowed, guards on them, and the observable effect of each.

Out of scope (implementation):

- Function/procedure bodies: algorithms, control flow, step-by-step logic.
- Private types, helpers, and local state.
- Internal data-structure choices a consumer cannot observe.
- Internal ordering of steps, micro-optimizations, and caching, unless a specific guarantee is itself part of the contract.
- How state is stored or how transition logic is coded.
- Anything a consumer can neither see nor depend on.
- Copy: the words, markup and hooks of a template, the value of a copy constant. See "Copy" below.
- Version numbers — a dependency's, a tool's, a sibling sub-project's release. A design names *what* it depends on; which release satisfies that is data and lives where the data belongs (`go.mod`, a lockfile, `AGENTS.md`'s toolchain). A requirement never states a version.

## Copy

`../SKILL.md`, "Copy is not contract", draws the line: text a program parses is contract; text only a person or an LLM reads is copy, and so is a page's markup with its classes, ids and attributes. A design treats copy as it treats an external tool's output: it says **that** it is shown and **where it comes from**, never what it says.

- A page is designed as a template name and the data it receives. A structural requirement declares the data type with its fields; a behavioral requirement says which template a route executes with which data, and under what status and headers. No requirement names a heading, a label, a sentence, a class, an id or an attribute, and no requirement defines how markup is read. The test executes the named template with the stated data and compares, or checks that a value it supplied appears in the body.
- A string the code emits without a template, an error line or a description, is a **copy constant**: the structural requirement declares its name and type (`const Unreachable string`) and never its value, and the source holds the words. A behavioral requirement says the answer is that constant; the test references the constant. A copy constant is the one contract constant declared without a value.
- A template that cannot show a state the design names is an issue, never a requirement about the template's text.

So a change to copy or markup touches one asset or one constant and no requirement, no id and no test.

## Depending on an external tool

A tool this sub-project shells out to — a compiler, `git`, any installed CLI — is reached only through its **published interface**, never its internals.

A sibling sub-project in the same repository is exactly such a tool (see `../SKILL.md`, "Sub-project independence"). It is not a module of this one: it is reached only as an installed external tool, with the standing of `ssh`, `git`, or a compiler.

Allowed — the tool's published interface:

- Invoking it by bare name on PATH, and relying on its documented command grammar, flags, arguments, and exit codes.
- Naming it in prose, in help text, and in usage output.
- Obtaining it from its published release, and running the installer that release ships.

Not allowed — anything that is not the published interface:

- Naming a path inside another sub-project: its source directory, its build output, its templates, its configuration files.
- Building it, or invoking a compiler on it. The other sub-project builds and releases itself.
- Writing anything into another sub-project's directory.
- Encoding its internals: where it is installed, its source layout, build arrangement, release archive naming, or the shape of its output beyond the documented grammar.
- Asserting what its output *says* when the bytes are relayed or carried verbatim: a requirement may assert **that** the relaying happens and that nothing alters the bytes, never what the bytes are. A test fixture standing in for the tool emits arbitrary bytes — a fixture shaped like the real output encodes exactly the knowledge the requirement was forbidden to state.
- Stating which release of it to use; that is data.
- Parsing its output to learn something this design already decided.

An external tool is an external dependency like any other, so "Never assume an external dependency" above applies to it: a well-known tool's published documentation — its manual page, reference, or release notes — is proof of the behavior it documents, and only behavior the documentation does not state must be proven by observing the real tool. A sibling is proven by its published interface and its own documentation, never by reading its design documents.

Direction is one-way and declared. If two sub-projects would each have to know about the other, one of them is wrong. A behavior only the other sub-project can supply is filed in `specs/issues/`; it is never designed around by reaching across the boundary.

## Filename

Create the file in `specs/design/` as `D<int>-<slug>.md`.

- `<int>` is the next design number: the max existing number in `specs/design/` plus 1 (start at 1 if none).
- Zero-pad `<int>` so every file in the folder is the same width. If a new number needs more digits, re-pad the others to match. Padding is cosmetic — `D1`, `D01`, `D001` are the same design; the number is the identity.
- `<slug>` is a short kebab-case name. It is not part of the identity.

## Contents

1. Prose giving a friendly overview of the design element. It orients the reader and motivates the requirements, but binds nothing — only the `## REQUIREMENTS` list is contract. No code blocks: a signature, type, or constant belongs in the requirement that declares it, where it is contract and cannot drift from one. See the Example below.
2. A `## REQUIREMENTS` heading, followed by a list of requirements. This list is the design (see above).

## Requirements

One bullet per requirement:

```
- <id>: <requirement text>
```

- Mint each `<id>` with `idgen` (`idgen -n N` mints N at once). See `../SKILL.md`.
- `<requirement text>` uses a modal verb (MUST/SHOULD/MAY) and states one testable assertion.
- A requirement must be testable: there must be a finite, deterministic procedure that decides whether it holds, and running that procedure must be efficient. Do not write requirements whose only test would be to exhaustively check an infinite or intractable set of cases.
- Never assume an external dependency. What counts as proof depends on what the fact is about:
  - A fact about a vendor service or about state only a live system holds — a vendor host, an API's response shape, a protocol's required fields, a credential a host honors, a quota or limit, what a specific account or host is configured with — must be proven by observing the real thing (a live request, a real response) before the requirement that states it is written. Prior art and memory are not proof.
  - A fact about a well-known tool with published documentation — `systemctl`, `git`, `ssh`, `curl`, a POSIX shell, a coreutil, a compiler, any versioned tool with a manual — is proven by that documentation. Cite where the behavior is documented; observation is required only for behavior the documentation does not state, or when the documented behavior is contradicted by a real run.
  - A tool the design depends on that is not declared in `AGENTS.md`'s toolchain, or whose documentation cannot be found for the behavior relied on, is an unproven dependency.

  The proof comes before the writing: a fact is proven while the requirement that states it is drafted — by a probe run while designing, an existing live test, or a real response already in hand. Only new and changed requirements are proven; a requirement already in the design is not proven again.

Requirements come in two forms, and a design needs both:

- **Structural** — declares a public name and its shape: a package and what it exports, a type and its exact fields, an operation's signature, an enumeration's members, a contract constant's name and value; for a program, a command, flag, path, environment variable, or route. Structural requirements are proved by use: a test imports the package and references the declared shape exactly, so it compiles only if the declaration holds, or invokes the command as declared. Write one requirement per declaration — the type with its field list in one requirement, not one per field — so a rename or reshape re-mints exactly one id. A structural requirement declares only what a consumer can use. How many packages exist, what imports what, what `main` holds, what `go.mod` requires, and which files sit in a directory are not usable and are never requirements.
- **Behavioral** — a claim about what a declared symbol does. It names the symbol and nothing more: the signature, fields, or value live in the structural requirement that declares it. So a reshaped declaration re-mints one structural id, and the behavioral requirements that mention it keep theirs unless their own text changes. A behavioral requirement states an observable outcome at a public seam: given this input or state, this output or state change. It never prescribes how the code gets there: no internal calls, call counts, call order, or values passed between internal parts.

If every structural requirement were deleted, the design should no longer name anything; if that is not true, some of the contract is squatting in prose.

## Changing a design

A design document defines the current target, not a commitment to what earlier iterations decided. When adding to or revising a design, actively reconsider the decisions already in place — names, type shapes, boundaries, and especially which package owns each exported name, which is usually settled when the scope was one design and quietly goes stale as designs accumulate — wherever changing them would produce clearer, simpler code. The existing implementation is not a reason to keep an inferior shape: the run restructures to fit the current design, and superseded structure and tests carry no compatibility claim unless a current requirement states one. When a revision replaces something, put the whole replacement in the requirements — the new declaration, the deletion of the old one, and any compatibility that genuinely must be kept — so the gap shows the addition and the removal together and the run retires the old shape instead of bridging to it.

Design documents are never frozen; they may change at any time, and an existing requirement is never a reason a design cannot change — it is a decision under review like every other. Their prose may be rewritten freely — it is non-normative, so rewriting it changes nothing; a rewrite that *would* change the contract is really a requirement change and must be made in the `REQUIREMENTS` list. The only rule is how a requirement changes: **new text, new id**.

- **An id names exactly one text.** To change a requirement — by any amount, for any reason — delete it and add a new one with a freshly minted id. There is no exception: not a typo, not a renamed symbol, not a clarification, not a rewording that means exactly the same thing. Never edit the text beside an existing id, and never reuse an id.
- Why the rule is absolute: the gap is computed from **id presence alone** (see `../SKILL.md`), so an edited requirement produces no gap entry. The run never sees it, the tagged test is never revisited, and the design and the suite silently disagree — the id now points at text no test was ever checked against. A new id makes the change visible as `[add]`, and deleting the old one makes the stale test visible as `[remove]`.
- Do not reason about whether a change is "material" or "just wording." That judgement is what fails: a reworded requirement still means the same thing, which is precisely why it feels safe to edit and why the omission goes unnoticed. Text changed → new id. The only edits that keep an id are ones that leave its text byte-identical.
- After editing any design document, recompute the gap and confirm every change you made appears in it. A change you intended that produces no gap entry is a change the run cannot apply.
- The `REQUIREMENTS` list holds only the current contract. Superseded requirements are deleted; git holds the history.

## Review: canonical consumer usage

Before the design is checked, write the intended consumer experience as a review exercise — alongside the design, never in it. For a package, write canonical usage: complete, representative tasks a consumer accomplishes with the proposed API, using exactly the names and signatures the structural requirements declare. For an application, show the equivalent user interaction. Present this first and with minimal prose — the complete task, not the individual declaration, is the unit of review. For a revision, show the current usage and the proposed usage side by side. Put each unresolved decision immediately beside the usage it affects.

Judge from the examples whether the names, shapes, and sequence of interactions make sense together: could someone who sees only these names say what each thing is and why there is exactly one of it? Then check that every name the usage introduces resolves to a structural requirement (grep the design for each identifier); a name that appears only in the example is contract squatting in prose. This adds no approval step; the build run stays human-gated.

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

Canonical usage for review — every name in it resolves to the structural
requirement above:

```go
url, err := uploads.Put("report.pdf", file)   // rejected past uploads.MaxUploadBytes
if err != nil {
    return err
}
serve(url)
```
