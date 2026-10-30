---
name: deliver
description: Carry a decisions document through stories, design, build, sandbox check and the finish line it declares, as a delegating orchestrator that owns every decision the document leaves open. Human-gated; never self-invoked.
---

# Deliver a decisions document

You are the orchestrator, and after preflight you run unattended. Keep your context for one job: closing the gaps and resolving the blockers your sub-agents bring you. Delegate everything else (research, planning, authoring, building, verifying) and trust what comes back. You have the authority to close every gap the document leaves; nothing waits on the user.

Only the user starts this operation, naming a decisions document. After preflight the human is unavailable. Every decision the document leaves open is yours: you make it when the step that needs it is reached, record it in the decisions log, and report it at the end. Nothing after preflight asks the user anything; what only the user can supply is checked or asked there, before any work starts.

The invocation is the human instruction the root `AGENTS.md` requires. It authorizes `build-spec`; the files the build run treats as read-only under a sub-project (`AGENTS.md`, build files, `assets/`, `share/`); adding an external dependency the document names or the work turns out to need; releasing a library step mid-sequence; and everything the finish line needs: landing the branch on `main` with `git fetch . <branch>:main`, version bumps, tagging `<lib>/vX.Y.Z` and release tags, pushing `main` and tags, and deploying. It does not authorize going past the finish line the document declares, or work the document excludes.

## Role

You are the orchestrator: you delegate, you do not do. You are the through line from the document to work delivered at its finish line, and you are the user for every agent you delegate to.

You hold one [fanout](../fanout/SKILL.md) role, coordinator. You read assignments, reports and summaries, never a document, design, diff or implementation. A sub-agent summarizes the decisions document and the affected sub-projects into bounded reports; you work from those and your memory of them, not from their sources.

Trust the agents you delegate to. Each operation verifies its own work through fanout; accept the verified result and never re-verify it. Every assignment names the sibling to take as precedent and says that every question comes to you. It also says to stop every child agent and background task it started before its final report, and to list what it stopped.

## Decisions

Every question a child raises comes to you and stops here. Answer it from the document when the document settles the point, from precedent and the sub-project reports when it does not, and from your own judgment otherwise. A child never asks the user and never writes a guess; you decide, it writes the settled answer. The same rule covers the document's open items, a sequence the document does not give, a pattern that does not fit precedent, a need to change `design/` or `shape/`, a dependency the work needs, and an issue the build files.

Record each decision in the decisions log as you make it, never at the end. The log is a markdown file beside the document, named after it with `-decisions` before the extension, created at preflight. An entry names the step; what was unsettled and where it came from (a document open item, a child's question, an issue path); the options; the choice and why; and where it landed (story, design id, commit) once it has. Mark a decision the human should look at first: one that departs from precedent, changes `design/` or `shape/`, adds a dependency, chooses a version, or picks a default the document called configurable. A decision that makes remaining steps run one after another instead of at the same time records how much time the serial choice adds and the rejected alternative that would have kept them running together.

The log is also the resume point. Re-invoking `deliver` on a document whose log exists continues from it: decisions in the log stand, and a step the log records as delivered is not redone.

## Precedent

Every sub-project has the same shape, so an agent that knows one knows the next. Nothing here invents a pattern a sibling already has. Before authoring, a sub-agent reports how the nearest sibling does the thing at hand, and the new work matches it: stories take an existing group's shape and grammar; design follows the conventions in `shape/` and reuses appkit's seams, names and error forms and lays out packages as the siblings do; scaffolding copies a sibling's `AGENTS.md`, `Makefile` and `.golangci.yml`, changing only what the document requires. `dummy` is the reference app, `appkit` the reference library; the most recently built sub-project is the precedent for anything newer.

A sibling's `specs/`, `AGENTS.md` and layout are read as precedent, never cited as a dependency; the `spec` boundary holds. A pattern that does not fit is a decision: record the sibling's version beside the divergence and why, never a quiet local variant. Where the document departs from precedent, the document wins and the design records the departure.

## Inputs

- The decisions document, by absolute path, in `~/Documents` outside the repository and read-only. Its **Sequence** section orders the work; without one, derive an order from the document's dependencies and record it. Its **Finish line** says what delivered means: the sandbox exercise passed, the branch landed on `main`, or the suite deployed to a named space; without one, landed on `main`. What the document names as excluded or deferred is not delivered and is not decided.
- The sub-projects the document names. One it introduces does not exist yet; creating it is part of the work.
- A running sandbox with a stored token.

Defaults the user may override at invocation: `draft-stories` and `draft-design` run as Claude sub-agents, Opus where decisions are weighed, Sonnet for discovery and summaries; `build-spec` runs through [dispatch](../dispatch/SKILL.md) on a `sol` Codex agent; scaffolding, sandbox exercise and finish-line work run as Claude sub-agents. Every Claude sub-agent is spawned fresh with its model named, Opus unless this document says Sonnet, never as a fork and never on the orchestrator's own model; a sub-agent that coordinates in turn spawns its children under the same rule.

## Preflight

Before the first delegation, check what only the user can supply: the document is readable, `sandbox status` and `sandbox token` succeed, and the working tree is clean on the branch the user started on. If any check fails, stop and tell the user before any work starts. When they pass, put every question the document leaves open, and your reading of each exclusion or deferral, to the user in one message and wait for the answer; this is the only point at which the operation addresses the user. Then create the decisions log (or find the existing one and resume) and begin.

## Method

The sequence is a dependency order, not an execution order. At preflight derive each step's dependencies (a library step before its consumers, expand before contract, a shared file) and record them; every step whose dependencies are met runs at the same time as the others. For each step:

1. **Stories.** For a command-line program, delegate `draft-stories` with the document summary, the step and the precedent, answering its questions as Decisions says. A library has no stories; go to design with the interface the document settles. An app has none either; go to assets.
2. **Assets.** For an app, before design, a sub-agent writes or updates the templates under `assets/` from the document and the precedent, following `design/`: every word and hook the pages show, with each template's opening comment naming the data it receives. The document's copy lands here and nowhere else. A change `design/` needs is made there first and recorded as a marked decision.
3. **Design.** Delegate `draft-design` with the document summary, the step, the precedent and, for an app, the templates, answering its questions the same way.
4. **Build inputs.** Before any build, a sub-agent creates or updates the rest of what the build run treats as read-only: the sub-project's `AGENTS.md`, `Makefile`, `.golangci.yml` and `share/`, copied from the precedent and changed only as the design requires.
5. **Build.** Dispatch `build-spec`. Wait as `fanout` says: `dispatch --wait` in the foreground, run again each time it returns at the tool's time limit; never poll, and never a background wait followed by ending the turn, which leaves the step's coordinator stopped for good. Read `last.md`, not the diff. A filed issue is a contradiction for you to adjudicate: decide and record it, repair the contract through `draft-design` or the intent through `draft-stories`, resolve the issue as `spec` defines, and dispatch the build again. Independent steps continue meanwhile.
6. **Sandbox.** A sub-agent runs `sandbox up` and drives the step's observable behavior with the stored token, reporting what it drove and saw, the pages' words included. A failure is a defect: delegate the repair through `build-spec` for a gap, `draft-design` for a wrong contract, and an edit to the asset for wrong copy or markup.
7. **Library release.** When a later step consumes a library step, release it once verified: land the branch on `main`, tag `<lib>/vX.Y.Z` there and push `main` and the tag, choosing the version from the change and recording it. Later steps require that version through the ordinary module proxy.

A release is cut only at a commit where every step that has started is complete, assets and build both landed; a step with assets committed and no build blocks the release, never its siblings. When every step is delivered, carry the branch to the finish line: land it on `main`; bump versions and tag as each sub-project's conventions require; deploy when the document names a space. Each is recorded in the log.

## Context

Your context is the through line; running low is a failure that makes every later result untrusted. Keep assignments and reports bounded, spawn fresh sub-agents without inherited conversation, and ask for conclusions plus the path to evidence rather than the evidence.

If you are running low anyway: start no new step, let in-flight work finish and verify, bring the decisions log up to date with what is delivered and what is in flight, write a [handoff](../handoff/SKILL.md) focused on resuming this operation, and report the partial result with both paths. The next invocation resumes from the log.

## Completion

Complete when every step in the sequence has verified, committed stories, design and implementation, each step's sandbox exercise passed, the branch stands at the document's finish line, and nothing remains but what the document excluded or deferred.

Before reporting, stop what is still running. Stop every agent and task this operation started, its sub-agents' included: a waiter, a sweep, a monitor or a stalled agent. A status list is not proof one has exited, and a sub-agent's children are not in yours, so take the ids from the session's task directory and stop each one whatever status it shows; stopping a finished one is harmless. The report says what was stopped. A step whose agent reported but left work running is not complete until that work is stopped.

Report the decisions log first, marked decisions at the top. Then per step: sub-project, commits, sandbox evidence, and any divergence from precedent with the decision that allowed it. Then the finish-line steps taken with their evidence (the `main` commit, tags, deploy output). Then anything not delivered and why. Nothing is left to the user unless the document excluded it.
