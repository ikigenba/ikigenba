---
name: deliver
description: Carry a decisions document through stories, design, build and a sandbox check as a delegating orchestrator, stopping short of merge and deploy. Human-gated; never self-invoked.
---

# Deliver a decisions document

Only the user starts this operation, naming a decisions document. The invocation is the human instruction the root `AGENTS.md` requires: it authorizes `build-spec` and the files a human otherwise writes under a sub-project (`AGENTS.md`, build files, assets). It does not authorize merging, pushing, deploying, adding an external dependency, or changing `design/`; those go to the user.

## Role

You are the orchestrator: you delegate, you do not do. You are the through line from the document to verified, committed work on the current branch, and the single contact with the user.

You hold one [fanout](../fanout/SKILL.md) role, coordinator. You read assignments, reports and summaries, never a document, design, diff or implementation. A sub-agent summarizes the decisions document and the affected sub-projects into bounded reports; you work from those and your memory of them, not from their sources.

Trust the agents you delegate to. Each operation verifies its own work through fanout; accept the verified result and never re-verify it. Every assignment names the sibling to take as precedent.

## Precedent

Every sub-project has the same shape, so an agent that knows one knows the next. Nothing here invents a pattern a sibling already has. Before authoring, a sub-agent reports how the nearest sibling does the thing at hand, and the new work matches it: stories take an existing group's shape and grammar; design reuses appkit's seams, names and error forms and lays out packages as the siblings do; scaffolding copies a sibling's `AGENTS.md`, `Makefile` and `.golangci.yml`, changing only what the document requires. `dummy` is the reference app, `appkit` the reference library; the most recently built sub-project is the precedent for anything newer.

A sibling's `specs/`, `AGENTS.md` and layout are read as precedent, never cited as a dependency; the `spec` boundary holds. A pattern that does not fit is a user question showing the sibling's version beside the proposed divergence, never a quiet local variant. Where the document departs from precedent, the document wins and the design records the departure.

## Inputs

- The decisions document, by absolute path, outside the repository and read-only. Its **Sequence** section orders the work; without one, derive an order from the document's dependencies and confirm it with the user first.
- The sub-projects the document names. One it introduces does not exist yet; creating it is part of the work.
- A running sandbox with a stored token. Check with `sandbox status` and `sandbox token`; if either fails, ask the user.

Defaults the user may override at invocation: `draft-stories` and `draft-design` run as Claude sub-agents, Opus where decisions are weighed, Sonnet for discovery and summaries; `build-spec` runs through [dispatch](../dispatch/SKILL.md) on a `sol` Codex agent; scaffolding and sandbox exercise run as Claude sub-agents.

## Method

Walk the sequence one seam at a time:

1. **Stories.** Delegate `draft-stories` with the document summary, the seam and the precedent. A library has no stories; go to design with the interface the document settles.
2. **Design.** Delegate `draft-design` the same way. Answer children's questions from the document summary when it settles them; otherwise put them to the user one at a time with a recommendation, as [grill-me](../grill-me/SKILL.md) does.
3. **Human inputs.** Before any build, a sub-agent creates or updates what the build run treats as read-only: the sub-project's `AGENTS.md`, `Makefile`, `.golangci.yml` and `assets/`, copied from the precedent. Assets follow `design/`; a need to change `design/` is a user question.
4. **Build.** Dispatch `build-spec`. Wait with `dispatch --wait`; never poll. Read `last.md`, not the diff. A filed issue blocks that seam: report it with the issue path and continue independent seams.
5. **Sandbox.** A sub-agent runs `sandbox up` and drives the seam's observable behavior with the stored token, reporting what it drove and saw. A failure is a defect: delegate the repair through `build-spec` for a gap, `draft-design` for a wrong contract.

Open decisions in the document are the user's. Raise each when the seam depending on it is reached, not all up front.

## Context

Your context is the through line; running low is a failure that makes every later result untrusted. Keep assignments and reports bounded, spawn fresh sub-agents without inherited conversation, and ask for conclusions plus the path to evidence rather than the evidence.

If you are running low anyway: start no new seam, let in-flight work finish and verify, write a [handoff](../handoff/SKILL.md) focused on resuming this operation, and report the partial result with its path.

## Completion

Complete when every seam in the sequence has verified, committed stories, design and implementation on the current branch, each seam's sandbox exercise passed, and nothing remains but what the user excluded or left as a recorded open decision. Nothing is merged, pushed or deployed; those are the user's.

Report per seam: sub-project, commits, sandbox evidence, issues filed, and any divergence from precedent with the decision that allowed it. Then the open decisions with the seams they block, and the one step left to the human: land the branch on `main`.
