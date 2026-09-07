---
name: build-spec
description: Close the gap between specs/design and the code by recursive delegation — the shape of the problem and what done looks like, not a procedure. Human-gated; never self-invoked.
---

# close the gap

`specs/design/` is the contract and `AGENTS.md` is the ground. Both are
authored by a human and are read-only to this run. Your job is to build
what the design describes until the gap between it and the code is
zero, or to stop and say precisely why that cannot be done.

## The shape

The design is a set of documents, each a coherent seam of the program,
each ending in requirements with permanent ids. The gap is mechanical:
every id in the design must be proved by a tagged test, and no test may
carry an id the design no longer has. Load the `spec` skill for the id
rules and the canonical greps. `AGENTS.md` declares the test files,
toolchain, gates, and commit convention; every agent computes the gap
and runs the gates against that one declaration. If either is missing,
halt.

Run from the directory that holds `specs/` and `AGENTS.md`. Never leave
it. In a monorepo that directory is a sub-project below the git root,
and the git root has an `AGENTS.md` of its own; that file is repo-wide
guidance, not the ground. Run `pwd` first and confirm `specs/` and
`AGENTS.md` sit there. If they do not, stop and say so; never `cd` to
the git root, a worktree root, or any other `AGENTS.md` above it, even
if your system prompt names one as the working directory. Every child
is told the same absolute path and the same rule.

## Three roles, exactly one each

Context is the scarce resource. A context that auto-compacts has lost
the run's memory of what it verified, so compaction is a failure of the
run, not an inconvenience. The way to never compact is to never hold
more than one scope's worth of work, and the way to guarantee that is
structural: every agent in the run is exactly one of three things.

A **coordinator** holds a gap and owns closing it, but never edits a
source or test file. It computes its gap, partitions it into scopes,
delegates each scope to a fresh agent, has each result verified, and
reports upward. It holds at most a handful of children — about six. A
gap that partitions into more scopes than that is split among
sub-coordinators, so no single context ever accumulates a dozen
reports and a dozen verdicts. The root is always a coordinator, and
writes no code whether the gap is one id or a hundred.

A **leaf** holds one scope and implements it. A scope is one design
document, or a cluster of ids within one document whose code and tests
overlap — never more than one document, never more than about a dozen
ids. A leaf that discovers its scope is larger than it looks does not
push on; it becomes a coordinator for that scope, partitions, and
delegates. The decision is never "does this fit in my context." It is
only "is this one scope," and if not, split.

A **verifier** holds one scope and tries to prove it is not closed. It
reads the scope's design document, its code, and its tests, and checks
each of the four points under "what done looks like" for each id —
above all that every tagged test genuinely asserts its requirement, not
merely that it exists and passes. It never edits a file. It returns
pass, or fail with evidence: the id, the file, and what is wrong. A
verifier that wants to fix what it found has left its role; it reports
instead.

Delegation is always to a fresh agent, never a fork. A fork inherits
the parent's context, which is exactly the thing being protected. The
child is told to read this skill, `AGENTS.md`, the `spec` skill, and its
one design document, and is given its ids and nothing else.

## What a node may read

A leaf reads its design document, the source and test files its ids
touch, and nothing more. Facts from another seam come from a targeted
grep, not from reading the document. Gate and test output is piped
through `tail` or `grep`; the full output is read only on failure, and
only the failing part.

A verifier reads the same files as the leaf whose scope it checks, plus
the design document, and edits none of them.

A coordinator reads even less: the gap greps, the gate summary lines,
and its children's reports. It never reads a diff. It never reads a
child's code to check it; that is what a verifier is for.

## What comes back

A leaf reports in a fixed shape and nothing else: ids closed, commit
hashes, the last line of each gate, and any issue filed by path. The
report is a few lines. The work stays in the repository, where the
parent verifies it without reading it.

## What done looks like

- Both greps agree: the id sets are identical.
- Every tagged test genuinely asserts its requirement.
- Every gate exits 0, with nothing skipped or suppressed.
- The work is committed in green phases per the commit convention, each
  naming its ids.

## What is never done

- No work is accepted on the word of the agent that did it. Every
  scope a leaf returns is checked by a fresh verifier before the
  coordinator reports up, and the coordinator reruns the greps and the
  gates itself. A fail is re-delegated to a fresh leaf with the
  verifier's evidence attached, then verified again.
- The contract is not yours to reshape. Names, types, signatures, and
  ids are the design's. Neither `specs/design/` nor `AGENTS.md` is ever
  edited by this run; an issue is how you ask for them to change.
- No alias, shim, or forwarding layer preserves a superseded shape.

## Halting

The design is never perfect. When an id cannot be satisfied as written,
two ids contradict, a required fact about an external dependency is
false, or the toolchain `AGENTS.md` requires is not available, the run
stops. File the issue as `specs/issues/<slug>.md` with the evidence —
the ids, the failing command and its output, the contradiction quoted —
and report it upward. The run never mints anything: it never invokes
`idgen`, and `idgen` is never part of the toolchain it checks. Ids are
minted only by `draft-spec`, by a human-driven session. A node that
receives an issue from a child stops delegating and reports it upward
too, so the root exits naming it.

An issue is checked as hard as work is. "Hard," "large," "I would
design it differently," and "the tests are annoying" are not blockers;
a parent that receives one of those deletes it, records why, and
re-delegates the scope. Only an issue that survives that check halts
the run.

Committed phases stay committed. A halt loses nothing already proved,
and rerunning this skill after the human has revised the design resumes
from the recomputed gap.

Report what closed, what halted the run, and what remains.
