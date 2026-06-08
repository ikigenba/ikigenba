# Plan: rename the `ralph` service to `agent`

**Status:** ready to execute
**Branch:** create a worktree `rename-ralph-to-agent` off `main` (sibling under `/mnt/projects/ikigai/`)
**Audience:** an implementing subagent with no prior context. Read this whole file first.

> *Historical: the `ikigenba_<svc>_` MCP tool-name prefix this plan carried was
> later dropped for bare verbs — see `docs/adr-mcp-tool-bare-names.md`.*

---

## 1. Background — what is being renamed

`ralph` is one of the suite's seven deployable services (`/srv/ralph/`, loopback
port 3004). It is an **agent harness**: owners create durable *sessions* (a
prompt + model config + a persistent sandbox folder) and trigger *runs* that
execute a sandboxed Claude agent loop. It is an event-plane **producer**
(emits `run.succeeded` / `run.failed`) and **consumer** (cron triggers).

We are renaming the **service** to `agent` because the current name is a person's
name, not a domain noun like every other service (`crm`, `ledger`, `notify`,
`dropbox`, `wiki`, `dashboard`). `agent` describes what it is.

### CRITICAL — three meanings of "ralph", only one is renamed

A repo-wide grep for `ralph` returns three distinct things. **Only the first is renamed.**

1. **The service `ralph`** — directory, module, binary, MCP tool prefix, route,
   manifest, env-var prefix, event-source identity. **→ rename to `agent`.**
2. **"The Ralph pattern" / "Ralph technique"** — the industry concept
   (folder-as-memory, run-in-a-loop). Appears in `agentkit/agent/prompt.go`,
   `agent/README.md`, etc. **→ LEAVE UNCHANGED.** It names a technique, not our
   service.
3. **`ralph-loops` and `~/projects/ralph-wikis`** — `ralph-loops` is the wire
   protocol partner referenced in `agentkit/wire`, `agentkit/schema`,
   `agentkit/trace`; `ralph-wikis` is an unrelated external prototype mentioned
   in `wiki/` notes. **→ LEAVE UNCHANGED.**

When in doubt: if the text refers to *the deployable service in this repo*,
rename it. If it refers to *a technique or an external project*, leave it.

### Decisions already made (do not relitigate)

- **Event source string `ralph` → `agent`.** This is the producer identity that
  listeners subscribe to. The event *type* strings (`run.succeeded`,
  `run.failed`) are generic and **stay**.
- **notify is the ONLY listener** (verified: `notify/etc/manifest.env` has
  `CONSUMES=crm,ralph`; no other service consumes ralph's feed). notify must
  change in lockstep.
- **Leave `agent/internal/runner` as-is.** It's the run engine; the name is
  accurate. (The future deterministic-script service will NOT be named `runner`.)
- **Keep loopback `PORT=3004`.** No reason to move it.
- **Box migration: fresh DB.** Old sessions are abandoned by design.
- **Add an `opsctl teardown <app>` verb** (opsctl currently has no way to remove
  a service) and use it to decommission `ralph` on the box.

---

## 2. Verified facts that de-risk the rename

- **Migrations are tracked by version NUMBER, not filename.** `appkit/db` enforces
  `NNN_name.sql` naming but applies/records by the leading number
  (`applied=N embedded=N`). So renaming `002_ralph.sql` → `002_agent.sql` is
  **safe** — version 2 is unchanged. Schema DDL uses generic table names
  (`sessions`, `runs`), no "ralph".
- **The MCP tool prefix is a single constant:** `toolPrefix = "ikigenba_ralph_"`
  at `ralph/internal/mcp/tools.go:17`. One edit flips all 13 tools.
- **Env-var prefix is `RALPH_`** (`RALPH_DB_PATH`, `RALPH_RUN_TTL`,
  `RALPH_CRON_FEED_URL`, `RALPH_CRON_FROM`, `RALPH_PORT`). All become `AGENT_`.
- **opsctl has NO removal verb.** Its `setup` provisions four things with no
  inverse — hence the new `teardown` verb (Commit 3).

---

## 3. Execution — four commits on the worktree branch

Do the commits in order. After each code commit, run the verification for that
module. Do **not** squash; each commit is independently meaningful.

### Commit 1 — `agent`: service identity (repo-only, atomic)

All edits inside the service plus the two wiring files (`go.work`).

- `git mv ralph agent`
- `git mv agent/cmd/ralph agent/cmd/agent`
- `agent/go.mod` — module path `ralph` → `agent`
- root `go.work` — entry `./ralph` → `./agent`
- `agent/internal/mcp/tools.go:17` — `toolPrefix = "ikigenba_agent_"`
- `agent/cmd/agent/main.go`:
  - `App: "ralph"` → `App: "agent"`
  - `consumerID = "ralph"` → `consumerID = "agent"`
  - env vars `RALPH_DB_PATH`, `RALPH_RUN_TTL`, `RALPH_CRON_FEED_URL`,
    `RALPH_CRON_FROM` → `AGENT_*`
  - default db path `./tmp/ralph.db` → `./tmp/agent.db`; on-box comment
    `/opt/ralph/data/ralph.db` → `/opt/agent/data/agent.db`
- `agent/etc/manifest.env` — `APP=agent`, `MOUNT=/srv/agent/` (keep `PORT=3004`,
  `CONSUMES=cron`, the OUTBOX_* lines)
- `agent/etc/nginx.conf` — route `/srv/ralph/` → `/srv/agent/` (all occurrences:
  the `=` well-known/feed locations and the prefix location); nginx vars
  `$ralph_owner`→`$agent_owner`, `$ralph_client`→`$agent_client`, named location
  `@ralph_authn_500`→`@agent_authn_500`; comments
- `agent/etc/deploy.env` — comment header only (`ralph is a path-routed…`)
- `RALPH_*` → `AGENT_*` in: `agent/bin/start`, `agent/.envrc`, `agent/.gitignore`
- migrations: `git mv agent/internal/db/migrations/002_ralph.sql 002_agent.sql`
- tests: `git mv agent/internal/db/ralph_schema_test.go agent_schema_test.go`;
  update any `ralph`-as-service references inside test files (NOT pattern refs)
- service docs/comments self-referencing the service:
  `agent/{README,ARCHITECTURE,PLAN,TODO}.md`, `agent/Makefile`, `agent/bin/*`,
  `agent/.plan/manifest.md`, and `// ralph …` comments throughout
  `agent/internal/**` and `agent/cmd/**`. **Leave "Ralph pattern" mentions.**

**Verify:**
```
cd agent && GOWORK=off go build ./... && GOWORK=off go test ./...
```

### Commit 2 — notify: lockstep consumer + event source

- `git mv notify/internal/push/ralph.go notify/internal/push/agent.go`
- `git mv notify/internal/push/ralph_test.go notify/internal/push/agent_test.go`
- in those files: `RalphSubscriptions` → `AgentSubscriptions`; the source filter
  string `"ralph"` → `"agent"`; comments
- `notify/cmd/notify/main.go`:
  - `ralphSource = "ralph"` → `agentSource = "agent"` (rename the const + all uses)
  - `Consumes: []string{crmSource, ralphSource}` → `…, agentSource}`
  - `runRalphConsumer` → `runAgentConsumer`
  - `push.RalphSubscriptions()` → `push.AgentSubscriptions()`
  - any `RALPH_*` upstream env-var names → `AGENT_*`; comments
- `notify/etc/manifest.env` — `CONSUMES=crm,ralph` → `CONSUMES=crm,agent`

**Verify:**
```
cd notify && GOWORK=off go build ./... && GOWORK=off go test ./...
```

### Commit 3 — new `opsctl teardown <app>` verb

opsctl's `setup` (`opsctl/internal/opsctl/setup.go`) provisions four things and
has no inverse. Add `teardown` as the inverse, through the same `System` seam,
in reverse order.

- New file `opsctl/internal/opsctl/teardown.go` with `runTeardown`:
  1. **Guards:** refuse if `app` is the `DEFAULT`/apex app; require `--force`;
     error clearly if `/opt/<app>` is not provisioned.
  2. `stop` then `disable` the unit (System seam).
  3. remove the systemd unit file (`l.UnitPath()`) + `daemon-reload`.
  4. remove the nginx fragment (`l.FragmentPath()`,
     `conf.d/locations/<app>.conf`) + `nginx -t` + reload.
  5. `rm -rf /opt/<app>` (`l.AppDir()`) — DB intentionally discarded.
  6. drop the app system user by default; `--keep-user` to retain it.
- Register `teardown` in BOTH maps in `opsctl/cmd/opsctl/main.go`: the `groups`
  doc registry (Provisioning group) and the `runners` dispatch table. The
  help-coverage test asserts these key sets match.
- Add `opsctl/internal/opsctl/teardown_test.go`, mirroring the patterns in
  `setup_test.go` / `provision_test.go` (fake System seam; assert the reverse
  sequence and the DEFAULT-app / missing-`--force` guards).

**Verify:**
```
cd opsctl && GOWORK=off go build ./... && GOWORK=off go test ./...
```

### Commit 4 — cosmetic cross-references

Update mentions that point *at the service* (NOT the pattern/external projects):

- `wiki/GOALS.md`, `wiki/PLAN.md`, `wiki/notes/agentkit-extraction.md`,
  `wiki/internal/store/confine.go`, `wiki/internal/ingest/ingest.go` — only the
  lines naming the `ralph/` service as a sibling/example; **keep `ralph-wikis`
  and pattern refs**
- `dropbox/PLAN.md`, `dropbox/internal/dropbox/sync.go` — comments modeling on
  `ralph/internal/runner` → `agent/internal/runner`, and the
  `for svc in crm ledger notify ralph` loop → `… agent`
- `agentkit/job/job.go` — comments referencing `ralph`'s tables/types as the
  example service (`ralph: runs`, `ralph's session.Run*`, etc.). **Leave the
  `agentkit/agent/prompt.go`, `agentkit/wire/*`, `agentkit/schema/*`,
  `agentkit/trace/*` "ralph-loops" / "Ralph pattern" mentions untouched.**
- suite docs: `docs/*.md`, `AGENTS.md`, `DECISIONS.md` — service references
  (e.g. deploy examples, service tables). Leave historical plan docs that
  describe past work as written unless they name the live service surface.
- root operator scripts: `bin/start`, `bin/stop`, `nginx/run`,
  `cron/etc/nginx.conf` — any `ralph` route/service entries → `agent`

**Verify (whole tree):**
```
GOWORK=off go build ./... ; go test ./...   # per module, or via go.work for local dev
grep -rIil ralph . --exclude-dir=.git       # remaining hits should ONLY be:
                                            #   "Ralph pattern", ralph-loops, ralph-wikis
```

---

## 4. Box rollout (after merge to `main`)

The service is live on `int.ikigenba.com`. Order matters: stand up `agent`,
repoint notify, THEN remove `ralph` (so notify never points at a dead feed).

1. `bin/bump agent <major|minor|patch>` (if a version change is wanted) →
   `bin/ship agent` → follow printed box commands:
   `ssh int sudo opsctl setup agent` →
   `ssh int sudo opsctl stage agent v<ver> --artifact /tmp/agent-v<ver>` →
   `ssh int sudo opsctl deploy agent v<ver>`
2. `bin/ship notify` → `stage` → `deploy` (now consumes `agent`'s feed,
   `CONSUMES=crm,agent`)
3. `ssh int sudo opsctl teardown ralph --force`
4. Restart the dashboard so it re-reads manifests and shows `/srv/agent/` in the
   service inventory.

Old `ralph` sessions and DB are discarded by design.

---

## 5. Done criteria

- All four modules (`agent`, `notify`, `opsctl`, and the workspace) build and
  test green with `GOWORK=off`.
- `grep -rIil ralph` shows only the three intentional concept mentions
  (Ralph pattern / ralph-loops / ralph-wikis).
- `opsctl teardown` exists, is documented in `--help`, and has tests.
- On the box: `opsctl status` shows `agent` healthy, no `ralph`; notify pushes on
  `agent` run outcomes; dashboard inventory lists `/srv/agent/`.
