# Plan — bare-verb MCP tool names

Implementation plan for removing the redundant `ikigenba_<svc>_` prefix from
every service's MCP tool names, leaving bare verbs (`reflection`, `log`,
`create`, `file_read`, …), then bumping and redeploying every affected service.

Designed for **sequentially-run subagents**: phases are strictly linear, each is
sized for one subagent, and each ends green and verifiable. Subagents do not
share context, so **every phase begins by reading `docs/adr-mcp-tool-bare-names.md`**
(produced in Phase 1) and this plan.

## The problem

Every MCP service names its tools with a hardcoded service prefix. In each
`<svc>/internal/mcp/tools.go`:

```go
const toolPrefix = "ikigenba_crm_"
func tool(verb string) string { return toolPrefix + verb }
```

So the verb `reflection` becomes the tool name `ikigenba_crm_reflection`. The
MCP client then wraps every tool as `mcp__<server>__<tool>`, and because each
service is its **own** MCP server named `ikigenba_crm`, the fully-qualified name
becomes the doubled `mcp__ikigenba_crm__ikigenba_crm_reflection`.

The service name is already carried by the **server** segment (`ikigenba_crm`),
and the `__` separator between server and tool is fixed by the client and cannot
be collapsed to a single underscore. So the only redundancy we can remove — and
the entire point of this change — is the second `ikigenba_crm_` baked into the
tool name. With bare verbs the name reads `mcp__ikigenba_crm__reflection`: org
(`ikigenba`) · service (`crm`) · tool (`reflection`), each said exactly once.

## Load-bearing design decisions

1. **Empty the prefix; keep the `tool()` helper.** The per-service change is a
   single line: `const toolPrefix = ""`. The `tool(verb)` helper stays as a
   harmless passthrough so the diff is minimal, uniform across all ten services,
   and trivially reversible. Inlining the now-redundant helper is explicitly
   **out of scope** — do not do it.
2. **Server names and the MCP wrapping are unchanged.** The `ikigenba_<svc>`
   server name lives in the client integration, not this repo. We change only
   the tool names the service *registers*. Nothing in this repo references the
   `mcp__…__…` wrapped form except documentation examples.
3. **Bare verbs stay unique within a service** (they already were, modulo the
   prefix), and uniqueness across services is irrelevant — different servers.
   No verb collides with a reserved MCP name.
4. **Tests change in lockstep with each service.** Each service's `*_test.go`
   files reference the old names as string literals; the rename and its test
   updates land in the same phase so every module stays green at the phase
   boundary.
5. **This is a user-visible API change → minor version bump per service**
   (all services are pre-1.0; minor carries the change per `docs/versioning.md`),
   followed by a full redeploy. Downtime is acceptable (no live users).

## Scope (from a full-repo survey)

Ten services expose MCP tools and use the identical prefix pattern. **dashboard
has no MCP surface and is not touched** (except a restart at the end so it
re-reads manifests).

| service | tools.go prefix line | # tools | test file(s) (≈ refs) | in-desc cross-refs |
|---|---|---|---|---|
| crm | `tools.go:20` | 7 | `tools_test.go` (29) | `tools.go:62,90`; comment `mcp.go:53` |
| ledger | `tools.go:26` | 9 | `tools_test.go` (24) | `tools.go:45,76,86` |
| notify | `tools.go:18` | 2 | `tools_test.go` (5) | none |
| dropbox | `tools.go:18` | 2 | `tools_test.go` (11) | none |
| cron | `tools.go:22` | 7 | `tools_test.go` (5) | none |
| wiki | `tools.go:17` | 6 | `tools_test.go`, `ask_test.go`, `ingest_test.go`, `search_test.go` (28) | `tools.go:24-29,35,47,68,75` (comments+desc) |
| gmail | `tools.go:23` | 12 | `tools_test.go` (30) | none |
| sites | `tools.go:19` | 14 | `tools_test.go`, `files_test.go` (63) | none |
| prompts | `tools.go:17` | 16 | `mcp_test.go` (42) | none |
| scripts | `tools.go:17` | 16 | `tools_test.go` (21) | none |

Line numbers are survey-time hints — each phase re-greps to confirm before
editing. Services are independent: phase order among 2–11 does not matter
functionally; the listed order goes simplest → most-referenced.

VERSION files (current): crm 0.2.2, ledger 0.2.2, notify 0.5.0, dropbox 0.2.3,
cron 0.1.0, wiki 0.2.2, gmail 0.1.0, sites 0.2.0, prompts 0.6.0, scripts 0.1.0.

## Phases

### Phase 1 — Decision record (the contract for all later phases)
- **Scope:** write `docs/adr-mcp-tool-bare-names.md`. Pure docs, no code.
- **Content:** the problem (doubled prefix); why `mcp__<server>__<tool>` is fixed
  and the server segment already carries the service; the decision (bare verbs
  via `toolPrefix = ""`, helper kept); the rename rule (`ikigenba_<svc>_<verb>` →
  `<verb>`); scope (the ten services above; dashboard untouched; server names and
  auth unchanged); that it is a user-visible change requiring a minor bump and
  redeploy per service. Match the style of existing `docs/adr-*.md`.
- **Done when:** the ADR fully specifies the change. Every later phase reads it.

### Phases 2–11 — per-service rename (one service per phase)

Each of these phases is identical in shape, for exactly one service `<svc>` from
the scope table. Do them one at a time, in this order:

> **2 crm · 3 ledger · 4 notify · 5 dropbox · 6 cron · 7 wiki · 8 gmail · 9 sites · 10 prompts · 11 scripts**

- **Scope:** `<svc>/internal/mcp/` only (its `tools.go` and `*_test.go`; for crm
  also the `mcp.go` comment).
- **Changes:**
  1. In `<svc>/internal/mcp/tools.go`, change `const toolPrefix = "ikigenba_<svc>_"`
     to `const toolPrefix = ""`. Leave the `tool()` helper intact.
  2. Update every **cross-reference inside description strings and comments** in
     the service's mcp package that names a tool by its old prefixed form
     (`ikigenba_<svc>_<verb>`) → the bare `<verb>`. (Only crm, ledger, wiki have
     these — see the table — but grep the package to be sure.)
  3. Update every **test string literal** in the service's `*_test.go` files from
     `"ikigenba_<svc>_<verb>"` → `"<verb>"`. Use a scoped, mechanical
     find/replace within the test files; verify no stray prefix remains
     (`grep -rn 'ikigenba_<svc>_' <svc>/`).
- **Verify:** `(cd <svc> && go test ./...)` green. Do **not** set `GOWORK=off`.
- **Done when:** the module builds and all its tests pass referencing bare verbs,
  and `grep -rn 'ikigenba_<svc>_' <svc>/internal/mcp/` returns nothing.

### Phase 12 — docs sweep
- **Scope:** repo docs and per-service `CLAUDE.md` that document the tool surface.
- **Changes:** update operational/reference docs to the bare names —
  `docs/runbook-dashboard-box-cutover.md` (health-check examples),
  `crm/docs/user-guide.md`, and the service `CLAUDE.md` files (crm, ledger,
  dropbox, notify and any others) that show `ikigenba_<svc>_*` in tables or prose.
  For **historical** decision/plan records (`DECISIONS.md`,
  `docs/plan-mcp-reflection.md`) do **not** rewrite history — add a short
  superseding note pointing to `docs/adr-mcp-tool-bare-names.md`. Use judgement;
  keep edits minimal and accurate. Grep all `*.md` for `ikigenba_<svc>_` to find
  every reference.
- **Verify:** `grep -rn 'ikigenba_[a-z]*_' --include=*.md .` surfaces only
  intentional historical references (each now annotated as superseded).
- **Done when:** live docs match the bare-verb reality; history is annotated, not
  falsified.

### Phase 13 — full verification + commit to main
- **Scope:** the whole working tree; git.
- **Changes:** none beyond what prior phases produced.
- **Verify (proof pass):** `go build ./...` and `go test ./...` green across all
  go.work modules (enumerate them); `grep -rn 'ikigenba_[a-z]*_<verb-ish>'`
  confirms no service still self-prefixes its tools. Then **commit all rename +
  docs changes to `main` and push** (one commit). The renames must be on `main`
  before any `bin/ship`, which builds `main` HEAD.
- **Done when:** suite is green and the rename is committed and pushed to `main`.

### Phase 14 — version bumps
- **Scope:** the ten services' VERSION files, via the bump tool.
- **Changes:** run `bin/bump <svc> minor` for each of the ten services (crm,
  ledger, notify, dropbox, cron, wiki, gmail, sites, prompts, scripts). Each call
  advances `<svc>/VERSION` on `main` and pushes it.
- **Verify:** each VERSION file shows the expected new minor; `git log`/`git
  status` clean; the pushes succeeded.
- **Done when:** all ten services carry a bumped minor version on `main`.

### Phases 15–17 — deploy (batched)

Deploy each service with the standard flow against **int.ikigenba.com**:
`bin/ship <svc>` (builds `main` HEAD, scps artifact to box `/tmp`, prints the two
box commands), then `ssh int sudo opsctl stage <svc> v<ver> --artifact
/tmp/<svc>-v<ver>`, then `ssh int sudo opsctl deploy <svc> v<ver>`. Capture the
version `bin/ship` reports and use it verbatim in the stage/deploy commands.
Downtime is acceptable.

- **Phase 15 — batch A:** crm, ledger, notify, dropbox.
- **Phase 16 — batch B:** cron, wiki, gmail.
- **Phase 17 — batch C:** sites, prompts, scripts.
- **Verify (each batch):** `ssh int sudo opsctl status` shows each deployed
  service's unit active and at the new version; `ssh int sudo opsctl releases
  <svc>` shows the new release as current. Roll back with `ssh int sudo opsctl
  rollback <svc>` only if a deploy fails — and stop and report.
- **Done when:** every service in the batch is live at its bumped version.

### Phase 18 — dashboard restart + end-to-end verification
- **Scope:** the box; no code.
- **Changes:** restart the dashboard so it re-reads the service manifests (per
  the suite deploy rule for changed MCP surfaces). Use the documented restart
  path (`opsctl` restart of the dashboard unit).
- **Verify:** `ssh int sudo opsctl status` shows all units (including dashboard)
  active at expected versions. Confirm the new tool surface is exposed — e.g. a
  service's manifest/reflection now advertises bare verbs (`reflection`,
  `health`, …) with no `ikigenba_<svc>_` prefix. Report the observed names.
- **Done when:** all units are live, the dashboard has re-read manifests, and the
  bare-verb tool names are confirmed exposed end to end.

## Dependency chain

1 → (2 … 11, each independent but run sequentially) → 12 → 13 → 14 → 15 → 16 →
17 → 18. Phases 2–11 may be reordered among themselves; everything else is
strictly linear. 13 (commit to main) must precede 14 (bump) which must precede
15–17 (ship builds `main` HEAD); 18 is last.
