# Design — Git-backed artifacts (scripts, prompts, sites)

> **Status: settled direction, not yet built.** This doc records the decisions
> from the design discussion; a `git-artifacts-plan.md` will sequence the
> build. Nothing below exists in code yet except where explicitly noted as
> existing machinery being reused.

## The core principle

> **Git is the canonical store. The repo *is* the artifact; the services are
> porcelain over it.**

A script, a prompt, and a site are all just files. Today their content lives in
mutable SQLite columns (`scripts.body`, `prompts.user_prompt`) or a bare
directory tree (sites), with zero history: `update` overwrites, `delete` is
gone forever. This design moves the content into one git repository per
artifact, so that every artifact gets full version history, diffs, restore, and
(later) an external mirror — from one mechanism, not three.

This is deliberately **not** a shadow ledger. A design where git records copies
of mutations while the real content stays in SQLite was considered and
rejected: the history you consult must be the thing that runs.

## Motivation

- Artifacts will grow more complicated. A script that is one blob today becomes
  a package with modules; a prompt becomes a prompt plus fragments plus
  fixtures. Files-in-a-repo absorbs that growth with no schema change; a `body`
  column never will.
- Iterative development needs history: what changed, when, by which actor
  (owner vs. an agent run), and the ability to restore a prior version.
- Eventually, durable off-box history in an external store (GitHub). Not in v1,
  but nothing in v1 may make it harder.

## The model

### One repo per artifact

Each script, each prompt, and each site is its own repository:

- a **bare repo** under the owning service's state dir (`state/git/<artifact>.git`)
- a **checkout** per artifact — the working tree the service actually serves
  (sites) or executes from (scripts, prompts)

Per-artifact granularity was chosen over per-service or per-owner monorepos
because artifacts version, restore, and (later) mirror independently, and
because the repos service's unit-of-work machinery (worktrees, sessions,
eventual PRs) assumes repo-per-unit. Hundreds of tiny bare repos cost nothing.

A tidy directory of symlinks (e.g. `/srv/git/script-<name>.git`) makes remote
paths guessable without exposing service state-dir layout.

### What is versioned vs. what is a row

**Behavior is versioned; identity and access are not.**

In the repo (versioned):

- the source: `main.py` and whatever it grows into; the site's full tree; the
  prompt text and system prompt
- a manifest (`ikigenba.json`): interpreter, timeout, event triggers, and
  future behavior knobs (e.g. service mode). The manifest schema tolerates
  unknown fields so future features are additive.

In SQLite (not versioned):

- identity and access: name/slug, owner, a site's visibility, (later) mirror
  settings. Restoring an old commit must never flip a site public or rename it
  out from under its URL; access changes happen only by explicit tool call.
- runtime records: runs, feed offsets, outbox — unchanged.

Consequence: the existing trigger tables become a **derived index of the
manifest**. On every head change the service reconciles trigger rows from
`ikigenba.json`. Restoring Tuesday's commit restores Tuesday's trigger wiring —
anything less and "restore" is a lie.

### Activation: head is live immediately

When an artifact's main moves — by MCP edit, merge, or restore — the new head
is live at once: sites serve the new tree on the next request; scripts and
prompts execute head on their next run. (When script service-mode arrives, a
running service restarts per its own manifest config; that clause is inert
until the feature exists.)

## Writing

### MCP mutations are commits

The existing mutation tools keep their names and become commit-producing:
`update`, `file_write`, `file_edit`, `sync`, `import` each commit to main with
a structured author and message carrying the owner, the `X-Client-Id` (so an
agent run's edits are attributed to that run), and the tool name. Commits
happen in-process, in the same code path as the mutation — no RPC to a peer.

### Single writer to main

> **Main has exactly one writer: the box.** Divergence and rebase are made
> unreachable by construction, not avoided by discipline.

- Service processes are the only thing that commits to main (via MCP tools,
  merges, and restores).
- Every bare repo gets a `pre-receive` hook, installed at init, that rejects
  any push to `refs/heads/main`. Hooks run server-side regardless of transport,
  so this holds for ssh pushes.
- External work rides branches: clone, branch, push the branch to the box.
  "Merge" is a box action (the `merge` verb), which is race-free because the
  box is the only main-writer. The only conflict that can exist is the
  ordinary stale-branch conflict, surfaced to the branch author by git itself —
  never to the box, never to an agent without bash access.

### Ingress: ssh, as-is

Branch pushes go **directly to the box over ssh** using existing ssh access.
No new users, no key management, no dashboard involvement: whoever can ssh to
the box can push branches (and cannot push main, per the hook). GitHub is not
a relay for any workflow.

**Filesystem sharing** is the only real work here, and it reuses what exists:

- Each app already has a matching system group (`useradd --system <app>`
  creates user and group `<app>` — `opsctl/internal/opsctl/seam.go`).
- The operator's ssh user is added to the `scripts`, `prompts`, and `sites`
  groups (supplementary membership; one opsctl provision step).
- gitkit inits every bare repo with `core.sharedRepository=group` and
  group-write, group-owned by the app's own group.

This deliberately does **not** introduce a new shared group: deploy runs an
unconditional `chown -R <app>:<app>` over the state tree
(`opsctl/internal/opsctl/deploy.go`), which would silently revert any foreign
group. With the app's own group, the deploy chown *maintains* push access
instead of fighting it. No deploy changes required.

## gitkit — a shared library, not a service

A new suite-root library alongside `appkit`/`eventplane`/`registry` (wired via
`go.work`, not versioned). Each of scripts, prompts, and sites embeds it; repos
eventually refactors onto it. A central git-host service was rejected: it would
put a loopback RPC on every content mutation and break per-service state
ownership.

gitkit's job, extracted and generalized from `repos/internal/repos/git.go`:

- shell out to the real `git` binary (the go-git rejection stands)
- init/own bare repos with the shared-group config and the pre-receive hook
- maintain the per-artifact checkout
- commit-on-mutation with structured author/message
- log/diff/merge/reset plumbing behind the uniform verb set

## The uniform verb set

gitkit contributes the same MCP verbs to all three services:

| verb | does |
|---|---|
| `history` | commit list for an artifact: hash, actor, message, time |
| `diff` | one commit, or between two |
| `restore` | make an old version the new head — **as a new commit on main**, never a reset; history stays append-only |
| `branches` | list branches pushed to the artifact's repo |
| `merge` | merge a pushed branch into main |

The surface is intentionally small; git's model makes new verbs additive
porcelain over the same repos.

## Deferred — deliberately cheap to add later

**GitHub.** No involvement in v1: no mirror, no tokens, no webhook consumer,
no inbound sync. When it arrives: a `publish` verb creates the external repo
and pushes the **entire history** outward (starting local-only loses nothing);
mirroring is opt-in per artifact and **outbound-only** — the box pushes main
after every commit; ssh remains the sole ingress. Credentials follow the
proven repos pattern: gitkit takes a `TokenSource`, wired to the github
service's short-lived installation tokens. Branch-protect the mirror's main
(App-only pusher) so the single-writer invariant is enforced there too.
GitHub-side repo naming (e.g. `script-`/`prompt-`/`site-` prefixes) is parked
until `publish` exists.

**Scripts as services.** `mode: service` in the manifest, `start`/`stop`/
`restart` verbs, supervision with crash-restart, running-version pinned to a
commit hash (restart-on-new-head is deploy; restart-at-old-hash is rollback).
Its own feature, fully separable: the manifest is extensible, the verbs are
additive, and the activation rule already carries the clause.

**HTTP git transport.** Additive if ever wanted: stock `git http-backend` on
each service's loopback, one nginx fragment per service, dashboard-minted
long-lived tokens in the basic-auth password slot introspected like MCP bearer
tokens today. The pre-receive hook is transport-agnostic. The only substantive
work (PAT issuance) is self-contained in the dashboard. What it would buy:
per-owner push attribution and access without box ssh — neither matters on a
one-operator box, which is why ssh ships first.

## Migration

- **scripts, prompts**: no data to preserve. Rows are purged; every artifact
  starts life as a fresh repo at `create`. The content columns
  (`scripts.body`, `prompts.user_prompt`, `prompts.system_prompt`) go away
  with the cutover.
- **sites**: production data, preserved. On first boot of the new version the
  service seeds one repo per existing site — the live tree becomes the initial
  commit (authored "migration") and the tree becomes the checkout in place.
  History starts at migration day; none is invented.
