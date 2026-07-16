# suite

The **suite** is ikigenba's deployable application suite: one **dashboard** plus N
small **services** on a single box, one box per customer, answering on the apex
`<account>.ikigenba.com`. It is a single **mono-repo** (one `.git`). The dashboard
owns identity (OAuth, IAM, grants, install landing, service inventory); each
service owns one domain, its own SQLite database, and a loopback HTTP server.
**nginx is the sole trust boundary**: it introspects each `/srv/<svc>/` request
against the dashboard, strips the prefix, and forwards it with trusted
`X-Owner-Email` / `X-Client-Id` headers, so services run no UI and no token logic.
The product surface is **MCP**. Every app is one static `linux/amd64` Go binary on
the shared **appkit** chassis over SQLite (fixed verbs
`serve`/`version`/`manifest`/`migrate`/`schema`), and services exchange facts over
the **event plane** (append-only outbox, `/feed` SSE) rather than private API
calls. The bet: tolerating short scheduled downtime buys a cheaper,
easier-to-recover system, with no cluster and no broker. Infrastructure lives
separately in `~/projects/metaspot`.

**Each subproject has its own `AGENTS.md`** (what it is, layout, tests,
versioning); this file is only the suite-level map and whole-suite workflows. You
almost always work in exactly one subfolder: read its `AGENTS.md` first and keep
everything for the unit of work (code, schema, `.envrc`, its `project/` spec) under
it. If unsure which subfolder a task belongs to, ask; do not default to the root.

## Top-level layout

| dir | what's in it |
|---|---|
| **dashboard** | Apex/`DEFAULT` app: OAuth server, IAM, grants, install landing, service inventory. Owns nginx + TLS on the box. |
| **crm** | `/srv/crm/` sales CRM. |
| **ledger** | `/srv/ledger/` double-entry bookkeeping. |
| **notify** | `/srv/notify/` push notifications; the worked-example consumer. |
| **dropbox** | `/srv/dropbox/` loopback sync daemon mirroring a Dropbox app folder. |
| **prompts** | `/srv/prompts/` sandboxed Claude agent sessions (uses `agentkit`). |
| **wiki** | `/srv/wiki/` knowledge base (ingest / search / RAG ask). |
| **cron** | `/srv/cron/` scheduled-event emitter. |
| **gmail** | `/srv/gmail/` Gmail connector. |
| **scripts** | `/srv/scripts/` runs owner Python scripts wired to events. |
| **sites** | `/srv/sites/` static-website host. |
| **webhooks** | `/srv/webhooks/` inbound-webhook receiver (public `POST /in/<name>` ingress). |
| **github** | `/srv/github/` GitHub connector. |
| **repos** | `/srv/repos/` development plane: dispatches confined agent sessions in worktrees and opens PRs. |
| **appkit** | Shared **chassis** library: verb dispatcher, config, migrations, loopback server, `/feed`, manifest. |
| **eventplane** | Shared **library**: event-plane producer/consumer plumbing (outbox, feed, routing). |
| **registry** | Shared **library**: the authoritative service-name to loopback-port table. |
| **opsctl** | **On-box CLI**: stage/deploy/rollback/prune/status/provision/backup. Installed to `/usr/local/bin/opsctl`. |
| **bin** | Repo-root operator scripts: off-box build/version tooling (`ship`, `bump`, `start`, `stop`, `create-migration`). |
| **nginx** | Local-dev front door on **:8080** mirroring prod `/srv/<svc>/` routing (`./run`). |
| **docs** | Suite-level docs: deployment ADR, versioning, runbooks, event-plane protocol. |
| **sops** | Standard operating procedures for agents (e.g. seeding secrets). Check here first. |
| **design** | The shared Carbon design-system reference (tokens, example). |
| **project** | The suite-level spec workspace (product/design/plan). |

The **fourteen deployable apps** each carry a committed `<app>/VERSION` and ship
independently: **dashboard, crm, ledger, notify, dropbox, prompts, wiki, cron,
gmail, scripts, sites, webhooks, github, repos**. `appkit`/`eventplane`/`registry`
(libraries) and `opsctl` (tooling) are **not** versioned. `agentkit` is a separate
repo (`github.com/ikigenba/agentkit`), consumed as a tagged module. The root
`go.work` wires modules for local dev; the production build forces `GOWORK=off`.
Loopback port assignments live in **`registry/`**.

## Working locally

You almost always work in one subfolder; read its `AGENTS.md` first. Testing
usually needs the whole suite up, driven from the root:

- **`bin/start`** builds every service, launches each on its loopback port, and
  brings up the nginx front door on **:8080** for the full path-routed auth chain.
  Logs land in `tmp/<svc>.log`.
- **`bin/stop`** tears the stack down; **`bin/stop --clean`** also wipes `tmp/`
  dev state.

With the suite up you should have the `ikigenba_<svc>` MCP tools reachable. If
they are missing or a `health` check fails, complain prominently rather than
proceeding as if testing passed (usually the suite just is not up).

> ⚠️ **Only stop the stack this worktree started.** The suite binds shared host
> ports, so another worktree or clone may own a running stack. `bin/stop`,
> `kill`/`pkill`, or freeing a port is permitted only for the stack your own
> `bin/start` launched. Anything holding a port you did not start (for example a
> stale nginx on :8080) is a question for the operator: identify the owner
> (`ss -ltnp`), stop, and surface it. A port conflict is never permission to kill.

## Deploying

> ⚠️ **`int.ikigenba.com` is the live account.** Do not `ssh int` or invoke
> `opsctl` against the box, even read-only, unless explicitly told to deploy. The
> default workflow is local-only.

> ⚠️ **TEMPORARY (migration window, REMOVE when done): only `ledger` holds live
> customer data.** Protect ledger's `state/` as if data loss is unacceptable.
> Every other service's `state/` is disposable until migration completes and may
> be wiped and rebuilt. This flips service by service; when migration finishes,
> all services hold real data and this note must be deleted.

The full `bump → ship → stage → deploy` runbook, rollback, and inspection commands
live in **`deploy.md`**.

## Migrations, timestamped and immutable

Each service owns its schema under `<svc>/internal/db/migrations/`, applied
forward-only by the appkit runner. Two hard rules:

- **Never hand-number a migration.** Run `bin/create-migration <service> <name>`;
  it stamps a UTC timestamp so two branches do not collide. (Legacy `NNN_*.sql`
  files are frozen and sort first.)
- **Never modify or delete a committed migration.** The runner keys on the version
  and silently skips an edited body, so the change reaches new databases but not
  existing ones. Change schema by adding a new migration.

## Source changes go through the spec, not the editor

> ⚠️ A subproject with a `project/` tree is **spec-governed**: its source is
> produced only by its build loop from `project/design` + `project/plan`. Do not
> hand-edit governed code, templates, migrations, or config directly, not even a
> one-line fix. Amend the spec (`$open-spec` → `$seal-spec`) and let the loop
> build it. The only exception is an explicit operator instruction to edit a
> **named file**; a broad ask is a spec change. If unsure, ask before writing.

## Designing and planning work

Suite-level work moves through paired documents in `docs/` sharing one slug:
**`<slug>-design.md`** (the how and the decisions) then **`<slug>-plan.md`**
(ordered phases, each sized for one subagent), after which a coordinator reads the
plan and `/finish`es it. Optional companions: **`<slug>-research.md`** (research
feeding the design) and **`<slug>-verification.md`** (a post-build validation
step).
