# The spec system

A high-level overview of how spec-driven development works in this repository. For the authoritative details, see the `spec` skill and the operation skills that load it (`.agents/skills/`).

## The idea

Stories say **what the user wants**; designs define **what** the software must do; code and tests satisfy them. Every requirement carries a permanent id, and a **mechanical gap** between "requirements that exist" and "requirements that have tests" says exactly what is unbuilt. Nothing tracks "done" by hand — it is always derived from what is on disk.

Three properties make this work:

- **Requirements are testable.** Each one is a single assertion with a finite, deterministic, efficient test. If you cannot test it, it is not a requirement.
- **Requirements have permanent ids.** Minted once with `idgen` (form `R-XXXX-XXXX`), never edited or reused. The requirement's text is frozen with its id; changing the text means a new id.
- **Tests are tagged with the id they cover.** So a plain `grep` can answer "which requirements have tests?" — and that is the whole progress-tracking mechanism.

## Layout

Every sub-project carries its own `specs/` beside its own `AGENTS.md`:

```
<sub-project>/
  specs/
    stories/     user stories (S<int>-<slug>.md), one group per file, no ids
    design/      design documents (D<int>-<slug>.md), human-authored
    issues/      open blockers, one markdown file each — gitignored
  AGENTS.md      the project's ground: toolchain, test files, gates, commit conventions
```

The sub-project directory is the working directory for everything below: gate commands, paths, and the build run itself. The git root's `AGENTS.md` is repo-wide guidance, never a project's ground.

Sub-projects are independent. A project's `specs/` govern its own directory and nothing else: no requirement names a path inside a sibling, builds it, or writes into it. A sibling is consumed only as an installed external tool — with the same standing as `ssh` or `git`, through its published interface, never its internals. Dependencies point one way and are declared; a need only another project can satisfy is filed as an issue for a human to adjudicate, never designed around by reaching across the boundary.

## User stories

A story is the intent a project is built to serve, written before any design. It says who wants what, the preconditions, the exact interaction — literal command lines, literal output, literal exit codes — what each option does, and the postconditions once the interaction has run. A reader with only the story can sit at a terminal and check whether the project does what it says.

Stories are grouped one coherent seam per file (one command and its subcommands, one lifecycle, one store), numbered in the order they are meant to be designed. They speak from the outside only — never a package, a function, or how a behavior is implemented — and carry no requirement ids: a design realises a group of stories and mints the ids there. Stories are never deleted; when the intent changes, the stories are updated to match, so `specs/stories/` always describes the current intent.

## Design documents

A design document defines a feature or subsystem. It defines the **public contract between modules** — the domain language, module boundaries, names, types, operation signatures, interfaces, contract constants, dependencies, observable behavior, and any state machine — and says **nothing about how a module is implemented internally**.

The litmus test: if something could change without any other module noticing, it is an implementation detail and does not belong in the design. If changing it would break or surprise a consumer, it is part of the contract and does.

Each document is prose followed by a `## REQUIREMENTS` list, one id-tagged bullet per requirement. **The requirements are the design**: the list is the document's entire normative content, and the prose is a friendly overview that binds nothing. Structural requirements declare the public names and shapes; behavioral requirements state observable behavior, referring to those names without re-declaring them:

```
- R-NEDL-QRWM: The `uploads` module MUST export `MaxUploadBytes = 10_485_760` and `Put(name string, r io.Reader) (URL, error)`.
- R-QRWM-NEDL: `Put` MUST reject uploads larger than `MaxUploadBytes` without persisting any bytes.
```

Designs are never frozen. They change at any time; any change to a requirement's text is made by deleting the old requirement and minting a new id for the replacement. A design names *what* it depends on, never which version; a fact about anything outside the project must be proven by observing the real thing before the design is checked.

## The gap

The gap is a set difference on ids:

- an id in a design but not in any test → **add** it (implement it and write the tagged test),
- an id in a test but not in any design → **remove** it (delete that test and its dead code).

Presence alone defines the gap. Whether a test is *adequate* is judged separately (by the verifier inside the build run, and by audit).

## The operations

The `spec` skill is the shared foundation — layout, ids, the gap, project ground, issue filing, and the story and design formats. Five operation skills load it first:

- **`draft-stories`** — turn what the user wants into stories under `specs/stories/`, new or updated. It grills you (via `grill-me`), one question at a time, for whatever the intent leaves open — an output text, an exit code, a postcondition is never invented. Produces stories, not design.
- **`draft-spec`** — turn `specs/stories/` into a design and the `AGENTS.md` ground beside it, for one sub-project, by recursive delegation with independent verification of story coverage and contract consistency. It asks you only for decisions the inputs cannot settle, after the available work is exhausted. This is the only operation that mints ids.
- **`check-spec`** — report whether the design is buildable: the ground exists, every external fact is proven, no requirement reaches across the project boundary, and the gap is shown. It is feedback only: it gates nothing and commits nothing.
- **`build-spec`** — close the gap.
- **`audit-spec`** — judge whether existing tests genuinely verify their requirements. Inadequate tests are un-tagged, once a fresh verifier confirms, so the next build run rebuilds them; requirements that turn out to be untestable or wrongly designed become issues.

`check-spec`, `build-spec`, and `audit-spec` are human-gated: an agent never starts one on its own. `build-spec` and `audit-spec` commit, edit tests, or both; `check-spec` only reports.

## The build run

`build-spec` is not a loop and keeps no plan or state file. It is a single skill that describes the shape of the problem and what done looks like, and it closes the gap by **recursive delegation** so that no one context ever holds more than one scope's worth of work:

- A **coordinator** holds a gap and owns closing it but never edits a file. It partitions the gap into scopes, delegates each to a fresh agent, has every result verified, and reports upward. The root is always a coordinator.
- A **leaf** holds one scope — one design document, or a cluster of ids within one — and implements the code and tagged tests. A scope that turns out too large is split, never pushed through.
- A **verifier** holds one scope and tries to prove it is not closed: every tagged test must genuinely assert its requirement, every gate must exit 0, nothing skipped or suppressed. It never edits a file.

No work is accepted on the word of the agent that did it. Every scope is verified by a fresh agent, and the coordinator reruns the greps and gates itself. Work lands in green phase commits per the project's commit convention, each naming its ids. Because "done" is derived from the committed tests, an interrupted run resumes simply by rerunning `build-spec`.

`draft-spec` and `audit-spec` use the same tree for the same reason — authors or auditors in place of leaves, every result checked by a fresh verifier.

## Gates

The concrete, project-specific details live in the sub-project's `AGENTS.md`, not in the spec system. It declares four things: the required **toolchain**, where the **test files** live, an ordered list of **gate** commands (tests, end-to-end tests, linting, and so on) that must all pass, and the **commit conventions**. Every agent in the run reads it directly. A missing tool becomes an issue. `idgen` is never listed in the toolchain; it is an authoring tool, not a build tool.

## Issues

`specs/issues/` is the escalation channel for friction an agent **cannot** resolve in its role — a contradictory or unsatisfiable requirement, a wrong seam, a missing tool, a need only a sibling project can satisfy. The filing rules live in the `spec` skill ("Filing an issue"), and every operation files against them. Each issue is a markdown file named `<slug>.md` with no minted id, and it must carry proof. Any open issue halts the build run until a human resolves it (by deleting the file); a drafting run instead carries its issues to the end and reports them together. To keep it from becoming a lazy exit, an issue a child files is checked as hard as work is: "hard," "large," and "I would design it differently" are not blockers, and a parent that receives one deletes it and re-delegates.
