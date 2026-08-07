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
| **telemetry** | `/srv/telemetry/` forensic record store: the suite-wide audit trail (MCP calls, HTTP requests, events, lifecycle). |
| **appkit** | Shared **chassis** library: verb dispatcher, config, migrations, loopback server, `/feed`, manifest. |
| **eventplane** | Shared **library**: event-plane producer/consumer plumbing (outbox, feed, routing). |
| **registry** | Shared **library**: the authoritative service-name to loopback-port table. |
| **opsctl** | **On-box CLI**: stage/deploy/rollback/prune/status/provision/backup. Installed to `/usr/local/bin/opsctl`. |
| **bin** | Repo-root operator scripts: off-box build/version tooling (`ship`, `bump`, `start`, `stop`, `create-migration`), plus the `bintest/` proof tier. Spec-governed (`bin/project/`). |
| **nginx** | Local-dev front door on **:8080** mirroring prod `/srv/<svc>/` routing (`./run`), plus the committed `parked/` default_server files for live non-apex domains. Spec-governed (`nginx/project/`). |
| **docs** | Non-normative prose only (positioning). Suite contracts live in the root `project/design/`, never here. |
| **sops** | Standard operating procedures for agents (e.g. seeding secrets). Check here first. |
| **design** | The shared Carbon design-system reference (tokens, example). |
| **project** | The **umbrella** spec workspace: the suite's shared contracts (install tree, release bundle, semver, env contract, identity, event plane, content plane, MCP surface, telemetry/correlation). It builds no code. |

The **fifteen deployable apps** each carry a committed `<app>/VERSION` and ship
independently: **dashboard, crm, ledger, notify, dropbox, prompts, wiki, cron,
gmail, scripts, sites, webhooks, github, repos, telemetry**. `appkit`/`eventplane`/`registry`
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

> ⚠️ **`int.ikigenba.com` is the live account.** `ssh int` and read-only
> `opsctl` inspection are fine when the work calls for them. What lives there is
> production data: never mutate it casually, never wipe or rebuild a `state/`,
> and confirm before anything that writes, deploys, restarts, or rolls back.

> ⚠️ **Every service holds production data.** Migration is complete: treat each
> service's `state/` as live customer data and assume data loss is unacceptable.
> No service's `state/` may be wiped or rebuilt, and a schema change that cannot
> preserve existing rows is a design problem, not a cleanup step.

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
> one-line fix. Amend the spec and let the loop build it. The only exception is
> an explicit operator instruction to edit a **named file**; a broad ask is a
> spec change. If unsure, ask before writing.

The spec itself is **direction-gated**: `project/**` is written only inside an
operator-invoked move (the `$open-spec` → `$grill-me` → `$seal-spec` arc, or the
build loop's completion mutations). In any other session `project/` is read-only
reference — a stale or wrong spec is a finding to report, not a license to edit,
and a settled discussion is not direction: say what should change and wait. This
holds for the suite-level `project/` at the root and for every subproject's.
See the `$ikispec` skill for the `project/` spec contracts and `$ralph` for the
unattended build workflow.

## Suite contracts and the umbrella project

Suite-wide conventions are **contracts**: Decisions in the root `project/design/`
(the umbrella project). A subproject **cites** a contract by its `DNN.md` path
and conforms by default; it never restates one, and it deviates only via a local
Decision naming the contract and the project-specific reason. See the `$ikispec`
skill ("The umbrella project and suite contracts") for the mechanics, including
proof-location markers and citation-is-adoption.
