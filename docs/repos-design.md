# repos: git-backed development plane

**Status: v1 sealed into service specs (2026-07-15).** The
"v1: the development loop" section below is the settled scope, resolved
question by question with the operator, now sealed as `repos/project/`
(greenfield, D1–D10, phases 01–09), `webhooks/project/` (D17, phase 17), and
`github/project/` (D9–D10, phases 13–14); the rest of this doc is the settled
*direction* (architecture, planes, release model) whose build is deferred.
Prior-art
survey: `repos-research.md` (industry convergence validates the gate,
branch-namespace/PR conventions, worktree-off-canonical-state, and ephemeral
scoped tokens; the VM-per-task isolation machinery is multi-tenant safety we
deliberately skip).

> Naming note: this service was designed as `projects`; it was renamed to
> `repos` to avoid colliding with the suite's `project/` spec workspaces.
> The domain noun is now **repo** (the entity is literally a git repository).

## The problem

Some suite artifacts (sites, scripts, prompts) outgrow their current storage: a
real website, a multi-file automation, an agent that does ongoing software
development. Those want git: history, branches, review, a remote on GitHub. But
most artifacts stay small. A three-line script should remain a SQLite row; a
one-page site should remain a folder. Any design that forces git onto every
artifact, or that bolts a sync sideways onto each service, was rejected on the
way here (see "Rejected shapes" below).

## The decision

Split the concern into two planes:

- **Development plane: a new `repos` service.** It owns git trees on disk
  and the agent sessions that work on them. A session checks out work, modifies
  it, tests it, checks it in. Repos is about the SDLC.
- **Execution plane: prompts, scripts, sites, unchanged in character.** They
  run and serve content from their own storage, exactly as today. None of them
  grows git. None of them touches another service's disk.

Work crosses from the development plane to the execution plane only by an
**explicit release**: repos emits a release event, and the execution-plane
services import a copy through their existing import/sync machinery, recording
the released SHA for provenance.

This preserves three invariants that earlier shapes broke:

1. **Run/state separation.** Canonical repo state and in-flight session work
   are distinct on disk (git worktrees per session); execution-plane services
   never depend on repos being up to serve or run.
2. **The suite's grain.** One domain per service; no shared state dirs. The
   agent that needs native filesystem access to a tree lives inside the service
   that owns the tree.
3. **Opt-in.** Hand-made prompts/scripts/sites never touch repos. Released
   ones carry `repo + SHA` provenance.

## The repos service

A core-block service on the appkit chassis (registry row TBD), SQLite for
metadata, event-plane producer, content-plane holder.

### Domain model

- **repo**: a named git repository on local disk. Created by `init` (local
  only, no remote) or `clone` (from the configured GitHub org). A remote can be
  attached later (`attach_remote`); GitHub is optional per repo.
- **session**: one agent run against one repo. Ephemeral worktree in,
  commits out. Sessions are the only writers to repo trees.
- **release**: a named, immutable pointer (`repo + SHA`, likely also a git
  tag) that the execution plane consumes.

### State layout

```
state/
  repos.db                       # metadata registry, sessions, outbox
  repos/<name>/                  # canonical repo + working tree (the state)
  sessions/<session_id>/
    worktree/                    # git worktree for this session (the run)
    output.jsonl                 # transcript, same shape as prompts runs
```

Each session gets its own `git worktree` off the canonical repo, on its own
branch (or detached HEAD). Concurrent sessions on one repo are possible on
separate branches; the canonical tree is never the cwd of an agent. Worktrees
are pruned after the session ends; commits are the durable product.

### Agent sessions

The session engine reuses the prompts run pattern: agentkit conversation, the
`runtools` toolset (bash + confined file tools + fetch/share), cwd set to the
session worktree. The existing prompts `bash` tool already provides
desktop-parity execution (real git, real test runners, anything installed on
the box, running as the service user); this is the same trust posture as
scripts executing arbitrary python on a single-owner box.

Whether the engine is extracted into a shared library consumed by both prompts
and repos, or grown independently in repos, is open (Q1).

### Git and GitHub

- Sessions run real `git` via bash. The repos service itself performs only
  lifecycle git: `init`, `clone`, `pull` (webhook-driven), tag on release.
- **Credentials:** the agent must never see the installation token. A git
  credential helper is baked into each cloned repo's config; it fetches a
  short-lived installation token from a new loopback twin on the `github`
  service (pattern precedent: `github` loopback `GET /pr`, D05). Pushes are
  bot-attributed, consistent with the github service's no-owner-PII promise.
- The `github` service otherwise stays exactly as designed: stateless outbound
  API actor (PRs, issues, reviews, merges). Repos does git; github does
  GitHub. Review flow for repo work is the existing github verbs.
- Inbound GitHub facts (push to a tracked repo) arrive via the `webhooks`
  service as suite events; repos consumes them and pulls. Poll or
  reconcile-on-start is the fallback. The github service stays off the event
  plane (its D01 decision is unchanged).

### MCP surface (sketch)

Lifecycle: `create` (init or clone), `attach_remote`, `list`, `get`, `delete`.
Sessions: `session_start` (repo, instructions, branch?), `session_list`,
`session_get`, `session_output`, `session_cancel`.
Release: `release` (repo, ref?) mints the release, emits the event.
Inspection: `log`, `status`, `diff` style read verbs over a repo, plus
chassis `health`/`reflection`.

Sessions are also event-triggerable, like prompt runs (see "The development
process"); trigger verbs mirror prompts' `set_trigger`/`clear_trigger`.

### Events and content plane

- Producer of `repos:release/<name>` (payload: repo, SHA, tag, manifest
  or content references) and likely `repos:session_completed/<name>`.
  Events carry references, never bytes (event-plane rule).
- Content-plane holder: loopback `GET /content?repo=...&path=...&ref=...`
  serving tree bytes at a pinned ref, so execution-plane importers fetch
  server-side over loopback (content-plane convention, `docs/content-plane-design.md`).

## The handoff: release-gated materialization

Backing is **opt-in per artifact**, declared in the artifact's own config:

```
source: { repo: "acme", path: "site/", release: "v4" }        # pinned
source: { repo: "tools", path: "backup/main.py", release: "latest" }
```

Absent `source`, the artifact is inline: today's row or folder, unchanged.

**The release is the only crossing.** The execution plane never tracks
branches or HEAD. When `release` is called on a repo it mints an immutable
release (tag + SHA) and emits `repos:release/<name>`; execution services
then **materialize** a copy of the content at that SHA, fetched from repos'
loopback content endpoint, into their own storage, via their existing external
import machinery (Dropbox import precedent: `import`/`sync` tools,
`source_path` columns, `docs/adr-dropbox-import-sync.md`):

- **sites**: the released tree (or subdir) lands in
  `SITES_ROOT/<vis>/<slug>/`. Serving is unchanged.
- **scripts**: the released file lands in `scripts.body`. Execution is
  unchanged (runner materializes from the DB row).
- **prompts**: a serialized prompt dir maps into the prompt row (convention
  needed, Q4). Prompt runs keep their empty ephemeral sandbox;
  development-style agent work belongs in a repos session, not a prompt
  run.

`release: "latest"` bindings re-materialize automatically on the release
event; pinned bindings re-materialize only when their config is bumped. Each
materialization records `repo + release + SHA` on the artifact for
provenance.

**Between releases the repo is a free workspace.** Sessions commit,
branches churn, main moves; none of it reaches the execution plane until the
next release. Runtime is fully independent: runs and serving consume the
service's own storage exactly as today, so repos being down affects
nothing but the next release.

Backed artifacts are read-only on the execution side: edits happen in the
repo (enforcement and a possible `eject`-to-inline verb are Q3).

Rollback is re-releasing a prior SHA, or pinning back to an earlier release.
A broken commit is never live anywhere until released.

## The development process: guide, then release to execute

There is **one process**, not an autonomous mode and an interactive mode.
Every unit of development work moves through the same lifecycle; what varies
is how much guidance it needs and who is authorized to apply the gate.

1. **Goal artifact.** Work is anchored to a durable goal record. For
   remote-attached repos this is a GitHub issue; its thread carries the
   deliberation and its labels carry the state
   (guidance → `execute` → PR linked → closed). Local-only repos fall back
   to direct dispatch from the owner's front-door agent (the instructions
   themselves are the record, kept in the session).
2. **Guidance (read-only, cheap, stateless).** Humans and agents refine the
   goal. Guidance agents may read the repo — repos read verbs and the
   loopback content endpoint; no worktree, no session — and converse on two
   surfaces:
   - the **front-door MCP agent** (synchronous chat; exists today, costs
     nothing to build), which can read, grill toward a goal, and write the
     issue when the goal settles;
   - the **issue thread** (asynchronous): owner comments → GitHub App webhook
     → `webhooks` → event → a discussion run that reads repo + thread and
     replies via the github connector's `issue_comment`, bot-attributed. Each
     turn is a fresh stateless run; the thread is the memory.
3. **The gate.** Execution begins only by a deliberate act: a human applies
   the `execute` label (or calls `session_start` directly, which is itself
   consent), or a **delegated policy** applies it — e.g. a classifier run,
   triggered by a PR-opened event, authorized in advance to gate simple bug
   fixes on its own. An unconfident classifier doesn't fail into a different
   system; it leaves the goal in guidance and asks a human.
4. **Execution.** `session_start(repo, instructions, branch)` → worktree,
   commits, push, PR via the github verbs.
5. **Review.** The PR is reviewed and merged by a human, or per policy.

Consequences:

- **Sessions are event-triggerable** (resolves Q2): pipelines are events →
  classifier → gate → session, composed from webhooks + event plane +
  triggers. No workflow engine is built; the state machine is a convention
  over labels and events, and `scripts` is the home for deterministic glue if
  pipelines need it.
- **Conversation ≠ session is structural**, not conventional: guidance agents
  have no worktree and cannot write, so the gate is enforced by architecture.
- This is the suite's own spec discipline (open-spec → grill → seal → build)
  mapped onto GitHub-native objects: guidance is the grilling, the label is
  the seal, the session is the build loop.

## v1: the development loop (settled)

v1 builds exactly one thing: **detect work in a repo, fetch it, do it, push,
open a PR.** No release machinery, no `repos:release` events, no content
endpoint, no `source` bindings — sites, scripts, and prompts are untouched.
The bot identity is **@ikibot** (GitHub App attribution now; the mention name
when conversation arrives later).

### Detection

- The GitHub App's single App-level webhook (org-wide by construction — one
  URL + HMAC secret covers every repo in the installation) delivers to the
  **webhooks** service, which gains a per-hook verification scheme:
  `bearer` (today's) or `github-hmac` (verify `X-Hub-Signature-256`).
  Deliveries become suite events; repos subscribes.
- v1 signal: **an issue is labeled `execute`**. Nothing else. Events authored
  by @ikibot are ignored (identity-based loop suppression, day one).
- Repos dispatches GitHub facts through one event-type → handler table so
  later signals (comments, PR events, cron) are added rows, not redesigns.

### Label lifecycle

`execute` (the human gate) → @ikibot swaps to `executing` + comments the
session id (ack + double-trigger guard; one active session per issue) → on
success: push, PR with `Fixes #N`, PR link comment, remove `executing` → on
failure: branch still pushed, `failed` + reason comment. Retry = re-apply
`execute` (clears `failed`, fresh session). `discuss` is **reserved** for the
v2 conversational mode (round-trip comments until the human flips to
`execute`); v1 accommodates it by having sessions read the full issue thread,
not just the body. v1 is reporting-only: guidance happens before the label,
wherever the owner talks to an agent; the contract is the issue; the gate is
the label; the result is the PR.

### Repos service (new; core port 3007, mount `/srv/repos/`)

- **Lazy provisioning**: any org repo is eligible; the first `execute` label
  auto-creates the repo record and clones, under complete org-wide
  defaults. Explicit `create`/`clone` remain for pre-onboarding and policy.
  **v1 repos are clone-only** — every repo has a GitHub remote;
  local-only `init` arrives with the release/backing work.
- **Sessions**: worktree-per-session off the canonical clone, branched from
  the default branch tip after a fresh pull. Branch **`ikibot/issue-<N>`**;
  retries get fresh `ikibot/issue-<N>.2`, `.3` (never force-push over an
  inspected failure). Never touches the default branch.
- **Concurrency**: one active session per repo (merge semantics, not git,
  is the constraint); global cap 2 concurrent sessions (env override);
  30-minute wall-clock timeout; excess sessions queue FIFO with an @ikibot
  "queued" comment. Revisit all three post-v1.
- **Engine**: copied from prompts' runner/sandbox/tools pattern, not
  extracted into a shared library. Extraction is reconsidered once both
  runners are stable (mandatory at a third copy). Known cost: shared-shape
  bug fixes land twice.
- **Model**: env-configured default (`REPOS_PROVIDER`/`REPOS_MODEL`,
  validated against agentkit's pricing table at startup), v1 default
  **`anthropic` / `claude-opus-4-8`**. Per-repo/per-issue overrides
  deferred. Separate non-blocking task: agentkit release adding
  `gpt-5.6-sol` (constant + rate tiers), after which the default may switch.

### Division of labor: credential-less agent

The runner does everything deterministic and everything GitHub-facing; the
agent only does the work:

- **Runner**: pull, worktree + branch, fetch + pin the issue thread into
  `instructions.md`, run **`.ikibot/check`** as the mechanical exit gate,
  push, `pr_create`, label swaps, comments — GitHub I/O via the github
  service.
- **Agent**: bash + confined file tools, cwd = worktree, **no credentials,
  no MCP, no GitHub access**. Issue text can talk at it, but it holds
  nothing exfiltratable.

**Definition of done**: `.ikibot/check` — an in-repo executable, exit 0 =
pass — run by the runner (not trusted from the transcript). Pass → real
(non-draft) PR carrying `Fixes #N`, session id, check summary. Fail → branch
pushed, no PR, `failed` + check output comment. No check declared → agent
self-assessment, PR body notes "no check declared." `AGENTS.md` remains prose
guidance the agent reads during the session (all surveyed vendors honor it).

### State layout and retention

```
state/
  repos.db
  repos/<name>/                  # canonical clone
  sessions/<session_id>/
    instructions.md              # pinned issue body + thread
    output.jsonl                 # transcript — durable, survives everything
    check.log                    # check output — durable
    worktree/                    # pruned on success / superseded / 14-day sweep
```

Success prunes the worktree immediately; failure keeps it (the crime scene)
until a later session on the issue succeeds or the age sweep (14 days, env
override) reclaims it. Transcripts survive pruning and repo deletion.
Everything is under `state/`, so opsctl's S3 snapshots cover it for free.

### Changes to other services (v1 dependencies)

- **webhooks**: per-hook `github-hmac` verification scheme.
- **github**: `pr_create` verb; atomic `label_add`/`label_remove` verbs
  (full-set `issue_update` replace is race-prone for the label state
  machine); loopback **`GET /token`** twin (D05 pattern) minting short-lived
  installation tokens — the github service stays the **single custodian** of
  the App private key; repos' runner uses the twin for clone/pull/push.
  Branch-namespace enforcement (`ikibot/*` only) is v1 runner code;
  token-side enforcement returns if agents ever get push access.
- **sites / scripts / prompts / agentkit**: no changes in v1.

## Rejected shapes

- **Per-service GitHub import (B):** three copies of git plumbing, no shared
  history, no dev-process foundation; the github connector is Contents-API
  only (no tree primitive).
- **Repo as the storage for existing services (D):** forces git onto every
  artifact, rewrites scripts/prompts storage, couples serving/running to the
  repo holder.
- **Prompt sandbox = repo tree (shared disk):** put the developing agent's
  cwd inside another service's state dir; broke run/state separation and the
  no-shared-disk grain. Solved instead by moving the developing agent into
  repos itself.
- **Fattening the github connector:** reverses its settled D01 decisions
  (stateless, off the event plane) and conflates the API actor with the
  content holder.
- **Live branch/HEAD tracking at the execution plane:** a site or script
  following `main` would go live on every commit, leaking in-progress dev
  state past any deliberate act. The release gate exists precisely so the
  repo can be worked in freely between releases. (Run-start read-through
  resolution was rejected with it: it coupled run availability to repos
  for no gain.)

## Open questions

Resolved by the v1 grill (see "v1: the development loop"): engine sharing
(copy, don't extract), session triggering (event-triggered via webhooks →
event plane), branch/merge policy (`ikibot/issue-<N>`, PR-only, never default
branch), registry port (3007 core), loop suppression (ignore @ikibot-authored
events), backup for v1 (clone-only, everything recoverable from GitHub or in
state/ snapshots).

Still open — deferred with the work they belong to:

- **Discuss mode (v2).** Comment-driven read-only guidance runs: webhook
  event subscription for `issue_comment`, per-turn run mechanics, and how the
  settled plan is represented before the `execute` flip.
- **Gate policy for delegated classification.** What classes of work a
  classifier may auto-gate, where the policy lives, defaults. Belongs to the
  autonomous-pipeline phase.
- **Read-only enforcement and `eject`** on backed artifacts — belongs to the
  release/backing phase, with **prompt serialization convention** and
  local-only (`init`) repos and their backup story.
- **Toolchain provisioning.** Which dev tools the box guarantees (git,
  python, node, go, ...); an opsctl/provisioning concern. v1 stance: missing
  tools fail loudly in-session (`failed` + comment naming the gap).
- **Post-v1 reviews explicitly promised:** concurrency limits (per-repo
  serialization, global cap, timeout), engine extraction, model overrides.

## Grounding (key existing code)

- github connector: `github/internal/gh/{client.go,token.go,pr_route.go}`,
  design `github/project/design/D01,D02,D05`; GitHub App, one org, public and
  private repos, no clone, off the event plane.
- prompts run engine: `prompts/internal/runner/runner.go`,
  `prompts/internal/sandbox/sandbox.go`, `prompts/internal/tools/tools.go`
  (`runBash` at tools.go:439 is the desktop-parity precedent).
- Import precedent: `scripts`/`prompts` `import`, `sites` `sync`,
  `source_path` columns, `docs/adr-dropbox-import-sync.md`.
- Mirror + events + content precedent: `dropbox/internal/dropbox/{sync.go,
  mirror.go,service.go,events.go,content.go}`.
- Conventions: `docs/content-plane-design.md`, `docs/event-routing-design.md`,
  `docs/service-registry-design.md`, `registry/registry.go`.
