# The spec system

A high-level overview of how spec-driven development works in this repository. For the authoritative details, see the `spec` and `build-spec` skills (`.agents/skills/`).

## The idea

Designs define **what** the software must do; code and tests satisfy them. Every requirement carries a permanent id, and a **mechanical gap** between "requirements that exist" and "requirements that have tests" says exactly what is unbuilt. Nothing tracks "done" by hand — it is always derived from what is on disk.

Three properties make this work:

- **Requirements are testable.** Each one is a single assertion with a finite, deterministic, efficient test. If you cannot test it, it is not a requirement.
- **Requirements have permanent ids.** Minted once with `idgen` (form `R-XXXX-XXXX`), never edited or reused. The requirement's text is frozen with its id; changing the text means a new id.
- **Tests are tagged with the id they cover.** So a plain `grep` can answer "which requirements have tests?" — and that is the whole progress-tracking mechanism.

## Layout

Every sub-project carries its own `specs/` beside its own `AGENTS.md`:

```
<sub-project>/
  specs/
    design/      design documents (D<int>-<slug>.md), human-authored
    issues/      open blockers, one markdown file each — gitignored
  AGENTS.md      the project's ground: toolchain, test files, gates, commit conventions
```

The sub-project directory is the working directory for everything below: gate commands, paths, and the build run itself. The git root's `AGENTS.md` is repo-wide guidance, never a project's ground.

## Design documents

A design document defines a feature or subsystem. It defines the **public contract between modules** — the domain language, module boundaries, names, types, operation signatures, interfaces, contract constants, dependencies, observable behavior, and any state machine — and says **nothing about how a module is implemented internally**.

The litmus test: if something could change without any other module noticing, it is an implementation detail and does not belong in the design. If changing it would break or surprise a consumer, it is part of the contract and does.

Each document is prose followed by a `## REQUIREMENTS` list, one id-tagged bullet per requirement. **The requirements are the design**: the list is the document's entire normative content, and the prose is a friendly overview that binds nothing. Structural requirements declare the public names and shapes; behavioral requirements state observable behavior, referring to those names without re-declaring them:

```
- R-NEDL-QRWM: The `uploads` module MUST export `MaxUploadBytes = 10_485_760` and `Put(name string, r io.Reader) (URL, error)`.
- R-QRWM-NEDL: `Put` MUST reject uploads larger than `MaxUploadBytes` without persisting any bytes.
```

Designs are never frozen. They change at any time; any change to a requirement's text is made by deleting the old requirement and minting a new id for the replacement.

## The gap

The gap is a set difference on ids:

- an id in a design but not in any test → **add** it (implement it and write the tagged test),
- an id in a test but not in any design → **remove** it (delete that test and its dead code).

Presence alone defines the gap. Whether a test is *adequate* is judged separately (by the verifier inside the build run, and by audit).

## The four operations

- **`draft-spec`** — author a design and the `AGENTS.md` ground beside it. It interrogates you (via `grill-me`) only when it needs information it cannot discover on its own. This is the only operation that mints ids.
- **`check-spec`** — confirm the design is buildable: the ground exists, every external fact is proven, the gap is shown, and the baseline is committed so the run starts from a known point.
- **`build-spec`** — close the gap. Human-gated; an agent never starts it on its own.
- **`audit-spec`** — judge whether existing tests genuinely verify their requirements. Inadequate tests are un-tagged so the next build run rebuilds them; requirements that turn out to be untestable or wrongly designed become issues.

## The build run

`build-spec` is not a loop and keeps no plan or state file. It is a single skill that describes the shape of the problem and what done looks like, and it closes the gap by **recursive delegation** so that no one context ever holds more than one scope's worth of work:

- A **coordinator** holds a gap and owns closing it but never edits a file. It partitions the gap into scopes, delegates each to a fresh agent, has every result verified, and reports upward. The root is always a coordinator.
- A **leaf** holds one scope — one design document, or a cluster of ids within one — and implements the code and tagged tests. A scope that turns out too large is split, never pushed through.
- A **verifier** holds one scope and tries to prove it is not closed: every tagged test must genuinely assert its requirement, every gate must exit 0, nothing skipped or suppressed. It never edits a file.

No work is accepted on the word of the agent that did it. Every scope is verified by a fresh agent, and the coordinator reruns the greps and gates itself. Work lands in green phase commits per the project's commit convention, each naming its ids. Because "done" is derived from the committed tests, an interrupted run resumes simply by rerunning `build-spec` after `check-spec`.

## Gates

The concrete, project-specific details live in the sub-project's `AGENTS.md`, not in the spec system. It declares four things: the required **toolchain**, where the **test files** live, an ordered list of **gate** commands (tests, end-to-end tests, linting, and so on) that must all pass, and the **commit conventions**. Every agent in the run reads it directly. A missing tool becomes an issue. `idgen` is never listed in the toolchain; it is an authoring tool, not a build tool.

## Issues

`specs/issues/` is the escalation channel for friction an agent **cannot** resolve in its role — a contradictory or unsatisfiable requirement, a wrong seam, a missing tool. Each is a markdown file named `<slug>.md` that must carry proof. Any open issue halts the run until a human resolves it (by deleting the file). To keep it from becoming a lazy exit, an issue a child files is checked as hard as work is: "hard," "large," and "I would design it differently" are not blockers, and a parent that receives one deletes it and re-delegates.
