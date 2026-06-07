# Plan: rename the `agent` service to `prompts`

**Status:** ready to execute (code + docs only; box rollout deferred — see §5)
**Branch:** work on the existing `prompts` worktree
(`/mnt/projects/ikigai/prompts`, branch `prompts`); merge to `main` later.
**Audience:** an implementing subagent with no prior context. Read this whole
file first.

---

## 1. Background — what is being renamed

`agent` is one of the suite's seven deployable services (`/srv/agent/`, loopback
port 3004). It runs sandboxed Claude agent sessions on the owner's behalf:
owners create durable *sessions* (a prompt + model config + a persistent sandbox
folder) and trigger *runs*. It is an event-plane **producer** (emits
`run.succeeded` / `run.failed`) and **consumer** (cron triggers).

We are renaming the **service** to `prompts` because it reads better
conversationally — you create *prompts*, schedule them, and that creates
sessions — and it pairs with a planned future sibling service `scripts`
(deterministic scripts; triggerable, emits events). **`scripts` is NOT part of
this work**; the only task is `agent` → `prompts`.

This service was itself renamed from `ralph` in a prior pass; the companion plan
`docs/plan-rename-ralph-to-agent.md` is the template for this one.

### CRITICAL — three meanings of "agent", only one is renamed

A repo-wide grep for `agent` returns three distinct things. **Only the first is
renamed.**

1. **The service `agent`** — directory, module, binary, MCP tool prefix, route,
   manifest, env-var prefix, event-source identity. **→ rename to `prompts`.**
2. **`agentkit`** — the shared Go library (`agentkit/`) consumed by
   `agent`/`dropbox`/`wiki`, including its `agentkit/agent` package imported with
   the selector `agent.` (e.g. `agent.Run`, `agent.FramingPrompt` in
   `runner.go`). **→ LEAVE UNCHANGED.** It is shared infrastructure, not this
   service.
3. **Generic "agent" prose** — "Claude agent sessions", "the agent inside a
   run", "agent loop", "an agent actually calls it", the `AGENTS.md` filenames,
   and the `agentsevents` tables in `crm`/`dashboard`. **→ LEAVE UNCHANGED.**
   These describe the AI-agent concept, not our service.

When in doubt: if the text names *the deployable service in this repo*, rename
it. If it names *the shared library, a Go package selector, or the AI-agent
concept generally*, leave it.

### CRITICAL — the `agent.` selector trap

A naive `\bagent\b` replace **will corrupt code**: in `agent/internal/runner/runner.go`
the `agentkit/agent` package is referenced as `agent.Run` / `agent.FramingPrompt`
(the `.` is a word boundary, so `\bagent\b` matches). Before any prose/comment
replace, protect the `agent.` package selector and the literal token `agentkit`.
The *safe* mechanical subs in §2 deliberately never touch either.

### Decisions already made (do not relitigate)

- **FULL clean rename, including the event-source identity string `"agent"` →
  `"prompts"`** and the notify-side consumer files/strings. `int` is the only
  account and pre-production, so resetting notify's feed cursor on `agent`'s feed
  is acceptable.
- **notify is the ONLY listener** (verified: `notify/etc/manifest.env` has
  `CONSUMES=crm,agent`; no other service consumes agent's feed). notify must
  change in lockstep.
- **Keep loopback `PORT=3004`.** No reason to move it.
- **CODE + DOCS ONLY this round.** Box/deploy artifacts (systemd unit, the
  `/opt/agent` data dir, release dirs on `int`, the live `opsctl teardown`) are
  **out of scope** — handled later at deploy time. See §5.
- **Leave `agent/internal/runner` as a directory name.** It's the run engine;
  the name is accurate and generic. (Only `agent/cmd/agent` is the
  service-named command dir that moves.)

---

## 2. Verified facts that de-risk the rename

- **The event-source identity flows from `App`.** The appkit chassis stamps the
  outbox `Source` from the service's `App` name; there is no separate hardcoded
  producer-source string in non-test code (`grep` for `outbox.New`/`Source:` in
  `agent/**` non-test shows only the `cron` *consumer* source). So flipping
  `App: "agent"` → `"prompts"` in `cmd/.../main.go` is what changes the producer
  identity, and notify's `agentSource = "agent"` literal must change to match.
  The two test files that assert `Source: "agent"`
  (`agent/internal/session/outcome_test.go:32,255`) must change too.
- **Migrations are tracked by version NUMBER, not filename.** `appkit/db`
  applies/records by the leading number, so renaming `002_agent.sql` →
  `002_prompts.sql` is **safe** — version 2 is unchanged. Schema DDL uses generic
  table names (`sessions`, `runs`), no "agent".
- **The MCP tool prefix is a single constant:** `toolPrefix = "ikigenba_agent_"`
  at `agent/internal/mcp/tools.go:17`. One edit flips all tools.
- **Env-var prefix is `AGENT_`** (`AGENT_DB_PATH`, `AGENT_RUN_TTL`,
  `AGENT_CRON_FEED_URL`, `AGENT_CRON_FROM`, `AGENT_PORT`, and notify's upstream
  `AGENT_FEED_URL`). All become `PROMPTS_`.
- **The service tree is 54 tracked files** under `agent/`.

### Safe mechanical substitutions (never collide with `agentkit` / `agent.`)

Run these as global subs scoped to the moved tree and notify; none of them match
`agentkit`, the `agent.` selector, or generic prose:

- `ikigenba_agent_` → `ikigenba_prompts_`
- `AGENT_` → `PROMPTS_` (env vars; all upper-case prefix)
- `/srv/agent/` → `/srv/prompts/`
- `"agent.db"` / `agent.db` → `prompts.db`
- `/opt/agent` → `/opt/prompts`
- `./tmp/agent.db` → `./tmp/prompts.db`

Everything else (bare service-name `agent` in comments/prose, serverInfo names,
nginx vars) is a **case-by-case** edit, done only after protecting `agent.` and
`agentkit`.

---

## 3. Execution — four phases (one commit each, run serially)

Each phase is sized for **one fresh subagent** with no prior context: it reads
this whole file, does its phase, runs the phase's verification, and commits.
Run the phases **in order** — each starts from the previous phase's commit on a
clean tree. Do **not** squash; each commit is independently meaningful.

Phases 1 and 2 both touch only `prompts/`, but are split deliberately: Phase 1
is mechanical and compile-critical (and ends at a green build); Phase 2 is the
delicate prose/judgment work that should be done against an already-compiling
tree, so a mistake there is obviously a wording issue and never a build break.

### Phase 1 — `prompts`: mechanical service identity (compile-critical)

All edits inside the service plus the workspace wiring. Nothing here requires
prose judgment — it is rename-and-rewire, and it MUST end green.

- `git mv agent prompts`
- `git mv prompts/cmd/agent prompts/cmd/prompts`
- `prompts/go.mod` — module path `agent` → `prompts`
- **Rewrite all 31 intra-module import paths** `"agent/internal/..."` →
  `"prompts/internal/..."` across `prompts/**/*.go` (every `cmd` and `internal`
  file that imports a sibling package). **This is the single most build-critical
  step** — the module rename breaks every internal import until this is done.
  A scoped sub is safe: `"agent/internal` → `"prompts/internal` over
  `prompts/**.go` (does not match `agentkit` or the `agent.` selector).
- root `go.work` — entry `./agent` → `./prompts` (keep the list alphabetised;
  it moves from just after `./agentkit` to between `./opsctl` and `./wiki` —
  re-sort and verify it parses)
- `prompts/internal/mcp/tools.go:17` — `toolPrefix = "ikigenba_prompts_"`
- `prompts/cmd/prompts/main.go`:
  - `App: "agent"` → `App: "prompts"`
  - `consumerID = "agent"` → `consumerID = "prompts"`
  - env vars `AGENT_DB_PATH`, `AGENT_RUN_TTL`, `AGENT_CRON_FEED_URL`,
    `AGENT_CRON_FROM` → `PROMPTS_*`
  - default db path `./tmp/agent.db` → `./tmp/prompts.db`; on-box comment
    `/opt/agent/data/agent.db` → `/opt/prompts/data/prompts.db`
- `prompts/etc/manifest.env` — `APP=prompts`, `MOUNT=/srv/prompts/` (keep
  `PORT=3004`, `CONSUMES=cron`, the `OUTBOX_*` lines)
- `prompts/etc/nginx.conf` — route `/srv/agent/` → `/srv/prompts/` (all
  occurrences); nginx vars `$agent_owner`→`$prompts_owner`,
  `$agent_client`→`$prompts_client`, named location
  `@agent_authn_500`→`@prompts_authn_500`
- `prompts/etc/deploy.env` — comment header only
- `prompts/Makefile` — `APP := prompts`, `BIN := build/prompts.bin`,
  `./cmd/prompts`, the header comment
- `AGENT_*` → `PROMPTS_*` and bare service refs in: `prompts/bin/{start,stop,
  backup,restore,secrets,teardown}`, `prompts/.envrc`, `prompts/.gitignore`
  (bare `/agent` binary, `agent.db`)
- migrations: `git mv prompts/internal/db/migrations/002_agent.sql 002_prompts.sql`
- tests: `git mv prompts/internal/db/agent_schema_test.go prompts_schema_test.go`;
  rename func `TestMigrate_CreatesAgentSchema` → `TestMigrate_CreatesPromptsSchema`;
  update `Source: "agent"` → `"prompts"` in
  `prompts/internal/session/outcome_test.go` (lines 32, 255)

**Verify (must be green before Phase 2):**
```
cd prompts && GOWORK=off go build ./... && GOWORK=off go test ./...
```

> **⚠ Port 3004 stays — `prompts` cannot start while `agent` still holds it.**
> This is a *rename in place*, not a coexistence: the new service binds the same
> loopback port (3004) as the old one. If a stale `agent` process is still
> running it will own 3004 and `prompts` will fail to start with
> `address already in use` (this exact failure bit the ralph→agent pass).
> The renamed `bin/stop` no longer knows the name `agent`, so it will **not**
> stop a pre-rename `agent` process. Before running the suite locally after the
> rename, make sure any old `agent` process is dead, e.g.:
> ```
> pkill -f '/bin/agent' 2>/dev/null; pkill -f 'cmd/agent' 2>/dev/null || true
> ```
> then `bin/start`. On the box the same rule applies — see §5: stop/teardown
> `agent` **before** deploying `prompts`.

### Phase 2 — `prompts`: prose & MCP-surface judgment edits

Tree already compiles. Every edit here is wording; none should change the build
result. Apply the *three meanings* rule (§1) line-by-line — **leave the
`agent.` selector, `agentkit`, and generic AI-agent prose.**

- MCP-surface text:
  - `prompts/internal/mcp/mcp.go` — `serverInfo` name "Agent" → "Prompts";
    instructions "Agent runs sandboxed…" / "haven't used agent before" →
    prompts wording. **Keep** "an agent actually calls it", "Claude agent
    sessions".
  - `prompts/internal/mcp/describe.go` — only the brand line "Agent runs
    sandboxed…" → "Prompts". **Keep** "Claude agent", "the agent inside a run".
  - `prompts/internal/mcp/tools.go` / `mcp_test.go` — "overview of agent" /
    "the agent service" → prompts.
- service docs/comments self-referencing the service:
  `prompts/{README,ARCHITECTURE,PLAN,TODO}.md`, `prompts/.plan/manifest.md`,
  and `// agent …` comments throughout `prompts/internal/**` and
  `prompts/cmd/**` (including the `main.go` header comment block describing the
  service identity — keep generic "Claude agent" wording).

**Judgment-call hot spots (service-name vs. generic "agent"):**
- `prompts/internal/sandbox/sandbox.go`, `prompts/internal/consume/consume.go`
  — mixed service vs. AI-agent uses; edit line-by-line, do not bulk-replace.
- `prompts/internal/mcp/tools.go` — "idle agent session" is arguably generic
  (it *runs* a Claude agent); lean **keep**, but flag for review.
- Possessive awkwardness: the service named "prompts" reads as plural — prefer
  rewording ("the prompts domain", "prompts' feed") over "prompts's".

**Verify (build result unchanged from Phase 1 — still green):**
```
cd prompts && GOWORK=off go build ./... && GOWORK=off go test ./...
```

### Phase 3 — notify: lockstep consumer + event source

- `git mv notify/internal/push/agent.go notify/internal/push/prompts.go`
- `git mv notify/internal/push/agent_test.go notify/internal/push/prompts_test.go`
- in those files: the source-filter literal `agentSource = "agent"` → value
  `"prompts"` (rename the const to `promptsSource` and all uses); comments
- `notify/cmd/notify/main.go`:
  - `agentSource = "agent"` → `promptsSource = "prompts"` (const + all uses,
    lines ~48, 64, 156)
  - `Consumes: []string{crmSource, agentSource}` → `…, promptsSource}`
  - field `agentFeedURL` → `promptsFeedURL`; env var `AGENT_FEED_URL` →
    `PROMPTS_FEED_URL` (default URL stays `http://127.0.0.1:3004/feed`);
    comments (lines ~44, 175, 192, 194)
- `notify/etc/manifest.env` — `CONSUMES=crm,agent` → `CONSUMES=crm,prompts`
- `notify/CLAUDE.md` — any line naming `agent` as the consumed service

**Verify:**
```
cd notify && GOWORK=off go build ./... && GOWORK=off go test ./...
```

### Phase 4 — cosmetic cross-references (docs + root scripts)

Update mentions that point *at the service* (NOT the library / AI-agent
concept).

- root operator scripts:
  - `bin/start` — `launch_agent()` → `launch_prompts()`, `AGENT_DB_PATH` →
    `PROMPTS_DB_PATH`, `agent.db`, `bin/agent`, the `for svc in … agent …`
    loops, the `PORTS=([…]=3004)` map key, the `/srv/agent/` summary, the
    loopback summary line — all `agent` → `prompts` (lines ~84-87, 116, 127,
    132, 134, 152-153)
  - `bin/stop` — the `for name in … agent …` loop (line ~24); **leave** the
    generic "agent's …" tmp-tree comment at line ~48
  - `nginx/run` — `for svc in agent crm … ` → `prompts` (keep alphabetical)
- `cron/etc/nginx.conf:27` — example "(e.g. agent)" → "(e.g. prompts)"
- top-level `CLAUDE.md` and `AGENTS.md` — the service-table row and the
  app-list line (the seven deployable apps); update `/srv/agent/` and the
  service description. **Leave** the `AGENTS.md` filename references and generic
  agent prose.
- suite docs under `docs/` — service references only (deploy examples, service
  tables, event-plane producer lists). Likely files (verify each, edit only
  live-surface mentions): `adr-deployment-redesign.md`,
  `appkit-extraction-map.md`, `versioning.md`, `event-triggering-decisions.md`,
  `event-triggering-plan.md`, `gmail-connector-decisions.md`,
  `plan-mcp-rebrand-health.md`, `plan-mcp-reflection.md`,
  `plan-rebrand-ikigenba.md`, `event-protocol.md`. **Leave historical plan
  docs** (including `plan-rename-ralph-to-agent.md` and this file) that describe
  past/this work as written — they name `agent` as a historical fact.

**Verify (whole tree):**
```
GOWORK=off go build ./...   # per renamed module
GOWORK=off go test ./...     # prompts + notify
# remaining service-name hits — should be ONLY agentkit, the agent. selector,
# generic AI-agent prose, AGENTS.md, agentsevents, and historical plan docs:
grep -rIn '\bagent\b' . --exclude-dir=.git | grep -v agentkit
```

---

## 4. Done criteria (this round)

- `prompts/` exists; no `agent/` directory; `go.work` lists `./prompts`.
- `prompts` and `notify` build and test green with `GOWORK=off`; the whole
  workspace builds.
- MCP tools are prefixed `ikigenba_prompts_`; manifest is `APP=prompts`,
  `MOUNT=/srv/prompts/`, `PORT=3004`; notify has `CONSUMES=crm,prompts` and
  filters events on source `"prompts"`.
- A `grep` for service-name `agent` shows only the intentional survivors:
  `agentkit`, the `agent.` package selector, generic AI-agent prose,
  `AGENTS.md`, `agentsevents`, and historical plan docs.

---

## 5. Out of scope — box rollout (follow-up, NOT this round)

These are deliberately deferred to deploy time and tracked separately.

**Ordering is forced by the shared port 3004.** Because `prompts` reuses the
exact loopback port the live `agent` service holds, the two **cannot run at the
same time** — `prompts` would fail to bind. So unlike a normal cutover, you must
take `agent` **down first**, accept the brief gap (the operating bet allows
scheduled downtime; `int` is pre-prod), then bring `prompts` up. notify pointing
at a momentarily-dead feed is harmless — its cursor on the old feed resets
anyway, which is already an accepted consequence of this rename.

1. **Decommission `agent` first** — `opsctl teardown agent --force` (the
   `teardown` verb already exists from the ralph→agent pass). This stops/disables
   the unit, frees port 3004, and removes the `/opt/agent` data dir, the systemd
   unit, and old release dirs. Old `agent` sessions/DB are discarded by design.
2. **Stand up `prompts`** on the now-free port: `opsctl setup prompts` →
   `bin/ship prompts` → `stage` → `deploy`.
3. **Repoint notify** — `bin/ship notify` → `stage` → `deploy` (now
   `CONSUMES=crm,prompts`, consuming `prompts`'s feed on 3004).
4. **Restart the dashboard** so it re-reads manifests and shows `/srv/prompts/`
   in the service inventory.
