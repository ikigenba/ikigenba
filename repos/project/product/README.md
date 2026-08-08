# repos — Product

**Authority: intent.** This doc owns *why* the repos service exists, *for
whom*, what is in and out of scope, and the user-facing promises — stated once,
in outcome terms. It does **not** state mechanism, exact routes, formats, exit
codes, schemas, or test assertions; those belong to `project/design/`. Where the
two could overlap (observable behavior), product states the *promise* and design
states the *exact, checkable proof* of that promise.

## Problem

Everything authored on the box is a file that changes over time — a hosted
site's pages, a script's body, a prompt's text, a small codebase — and none of
it has a history. An edit overwrites what was there. A user who wants to see
what a page looked like last week, undo an agent's change, work on a branch, or
open the same content on a laptop has nowhere to go: the only copy is the live
one, and the only version is now.

Meanwhile each service that authors content would otherwise have to grow its own
history mechanism — its own storage of past revisions, its own attribution, its
own notion of a branch. Four half-answers, none of them a place a person can
actually clone from.

## Purpose

repos is the suite's **git custodian**: it holds one git repository per
versioned artifact on the box, and it is the only place that holds one. Other
services author content and ask repos to record it; repos keeps the history,
serves the content back at any point in that history, and answers a real `git`
client so a human can clone the artifact onto a laptop, work on a branch, and
push it back. It does exactly this one job.

## Users

- **The owner** (an authenticated suite user) — clones an artifact from a
  laptop with ordinary `git`, pushes changes back, and drives repos through MCP
  to create a plain code repository, see what exists, rename it, archive it,
  merge a branch, or read and record a check's verdict.
- **Owning services** (sites, scripts, prompts) — record each edit their users
  make as history, and materialize the current content when they need to serve
  or run it. They are the everyday callers, and their users never learn that
  they are.
- **Automated actors** (scripts and prompts runs on the box) — clone a
  repository, work on a branch, push the branch, and post the verdict of a check
  back against the commit they tested.

## Scope

repos **does**:

- Hold one repository per versioned artifact, named by the kind of thing it is
  and the owning service's own name for it — so creating a site, a script, or a
  prompt is what creates its repository, with no second naming scheme and no
  setup step.
- Keep the current content of every repository readable: a single file at any
  point in history, a listing of what a repository contains, and a whole-tree
  export the owning service uses to refresh the copy it serves or runs from.
- Record a change as a commit — one edit or a batch of edits, attributed to
  whoever made it — so that history accumulates as a side effect of ordinary
  work.
- Answer a real `git` client over the network: clone, fetch, and push, for the
  owner from a laptop and for automated work on the box, with the on-box path
  confined to the one repository the work concerns.
- Protect the live line of every repository: the branch that is served or run
  can only move forward, never be rewritten or removed, no matter which door the
  push arrives at. Every other branch is unrestricted.
- Merge a branch into the live line on request, refusing when the change
  conflicts or when a recorded check against that branch is failing or still
  outstanding.
- Record and report **checks**: a verdict against a specific commit, posted by
  whoever ran the check. A commit nobody has recorded a check against is
  ungated.
- Announce every change of a branch, and every archival, so owning services
  refresh and automated work can be triggered by a push.
- Archive a repository rather than delete it: it disappears from listings and
  stops being served, and its history is still there.
- Serve the canonical suite landing page at its mount, session-gated like every
  other service.

repos does **nothing else**. Deliberately excluded:

- **No execution of any kind.** repos runs no agent, no script, no check, no
  workflow. Whoever wants a check run runs it elsewhere and tells repos the
  verdict.
- **No GitHub.** repos neither reads from nor writes to GitHub — no issues, no
  labels, no pull requests, no mirroring. It is the whole store, not a cache of
  someone else's.
- **No serving or running out of repos' own copy.** Owning services keep their
  own copy of what they serve and run; repos hands out content, never traffic.
- **No destruction of history.** There is no verb that erases a repository's
  commits or reclaims its disk. Archiving is as far as any caller can go;
  reclaiming disk is a deliberate operator act outside this service.
- **No second namespace, and no cross-service naming rules.** Each owning
  service owns uniqueness within its own kind; repos does not arbitrate names
  between services.

## Contractual constants

- **The kinds**: `sites`, `scripts`, `prompts`, and `code`. The first three are
  named by their owning service; `code` names plain repositories created
  directly through repos.
- **The live branch is `main`** in every repository, of every kind.
- **The automation branch namespace is `ikigenba/`** — the convention automated
  actors name their branches under. It is a convention, not a restriction.
- **Loopback port `3007`, mount `/srv/repos/`** — frozen in the suite registry.
- **Starting version `v0.1.0`.**

## What we promise (user-facing behavior)

- **Versioning is ambient.** Nobody performs a git action to get history.
  Creating a site, script, or prompt creates its repository; every edit made
  through the owning service's ordinary surface becomes a commit attributed to
  whoever made it. Git stays invisible until the day someone wants it.
- **Clone it from a laptop.** With a token from the dashboard, ordinary
  `git clone` against the artifact's address retrieves its full history; work on
  a branch and `git push` puts the branch on the box, where it can be reviewed
  and merged.
- **`main` is the live thing, and it only moves forward.** A push that would
  rewrite or delete `main` is refused — from the laptop, from an on-box run,
  from any door — and the repository is left exactly as it was. Any other branch
  can be rewritten freely.
- **Automation proposes; a merge accepts.** Work done on the box pushes a
  branch, never `main`. A branch reaches `main` only through an explicit merge,
  which refuses on a conflict and refuses while any recorded check against that
  branch's tip is failing or still outstanding. A tip nobody checked merges
  freely.
- **Delete means archive.** Archiving a repository removes it from listings and
  from service, and keeps every commit — the history is still readable, and the
  same name can be used again afterward.
- **Renaming keeps the history.** When an owning service changes its name for
  something, its repository follows and its history comes along whole.
- **Every change is announced.** Any movement of any branch — an edit through a
  service, a push from a laptop or a run, a merge — is published as an event
  naming the repository, the branch, the new commit, and who did it, so the
  owning service refreshes and automated work can trigger on it.
- **A logged-in human who opens the mount sees a real page** — the canonical
  suite landing (service name, running version) gated by the dashboard session.

## Success criteria (outcomes)

- Editing a site, script, or prompt through its own service leaves a commit in
  that artifact's repository, attributed to the actor that made the edit, with
  no separate git step by anybody.
- The owner can `git clone` an artifact from a laptop using a dashboard-issued
  token, see its full history, commit on a branch, and push that branch back;
  the branch is then visible on the box.
- `git push --force` to `main` fails against a repository, and afterward the
  repository's `main` points at exactly the commit it pointed at before.
- An on-box run can clone the one repository it was given and push a branch to
  it, cannot push to `main` even fast-forward, and cannot touch any other
  repository.
- Merging a branch whose tip has a failing or outstanding recorded check is
  refused and names the check; recording a passing verdict for that tip and
  merging again succeeds and moves `main` to include the branch's work.
- Merging a branch that conflicts with `main` is refused and leaves `main`
  unchanged.
- An owning service can fetch the whole current tree of its artifact as an
  ordinary set of files with no version-control leftovers, and can fetch any
  single file at a named point in history.
- Archiving a repository makes it vanish from listings while its history remains
  intact and recoverable, and the same name can be created again.
- Renaming a repository leaves it reachable under the new name with the same
  commits, and unreachable under the old one.
- Every branch movement, from any door, is observable as one suite event naming
  the repository, branch, commit, and actor.
- Nothing repos does causes a request to github.com.
- A logged-in dashboard user opening `/srv/repos/` sees the service name and
  running version; a browser with no session is refused.
