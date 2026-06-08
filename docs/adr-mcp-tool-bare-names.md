# ADR — Bare-verb MCP tool names

> **Status: ACCEPTED (2026-06-08).** This is the durable architecture decision
> record for how suite services name the MCP tools they register. It is the
> contract every phase of `docs/plan-mcp-tool-bare-names.md` reads and
> implements: each per-service rename, the docs sweep, the version bumps, and the
> redeploy all derive their behavior from this document.
>
> **Scope.** The MCP tool names registered by the ten services that expose an MCP
> surface — crm, ledger, notify, dropbox, cron, wiki, gmail, sites, prompts,
> scripts — set in each service's `<svc>/internal/mcp/tools.go`. It does **not**
> change the MCP server names, the `mcp__<server>__<tool>` client wrapping, the
> nginx auth contract, any service's domain surface, or the deploy model
> (`docs/adr-deployment-redesign.md`). The dashboard has **no** MCP surface and is
> untouched (except a manifest-rereading restart at the end of the rollout).
>
> It is written in the house *context → decision → consequences* shape of
> `docs/adr-deployment-redesign.md` and `docs/adr-migration-timestamps.md`.

---

## Context — the doubled prefix

Every MCP service names its tools with a hardcoded service prefix. In each
`<svc>/internal/mcp/tools.go`:

```go
const toolPrefix = "ikigenba_crm_"
func tool(verb string) string { return toolPrefix + verb }
```

So the verb `reflection` becomes the registered tool name
`ikigenba_crm_reflection`. The MCP client then wraps every tool as
`mcp__<server>__<tool>`. Because each service is its **own** MCP server named
`ikigenba_<svc>` (here `ikigenba_crm`), the fully-qualified name the user and the
agent actually see is the doubled:

```
mcp__ikigenba_crm__ikigenba_crm_reflection
```

The service name is said **twice** — once in the server segment and again baked
into the tool name.

### Why `mcp__<server>__<tool>` is fixed

The wrapped form is not ours to restructure. The server segment
(`ikigenba_crm`) already carries the org and the service, and the `__` separator
between server and tool is fixed by the client — it cannot be collapsed to a
single underscore, and the server name lives in the client integration, not in
this repo. The only redundancy we *can* remove — and the entire point of this
change — is the second `ikigenba_crm_` baked into the tool name the service
registers.

With bare verbs the name reads:

```
mcp__ikigenba_crm__reflection
```

— org (`ikigenba`) · service (`crm`) · tool (`reflection`), each said exactly
once.

---

## Decision

### Empty the prefix; keep the `tool()` helper

The per-service change is a single line:

```go
const toolPrefix = ""
```

The `tool(verb)` helper stays as a **harmless passthrough** so the diff is
minimal, uniform across all ten services, and trivially reversible. Inlining the
now-redundant helper is explicitly **out of scope** — do not do it as part of
this change.

### Server names and the MCP wrapping are unchanged

We change only the tool names a service *registers*. The `ikigenba_<svc>` server
name and the `mcp__<server>__<tool>` wrapping are untouched (they live in the
client integration, not this repo). Nothing in this repo references the wrapped
`mcp__…__…` form except documentation examples.

### Auth is unchanged

The nginx introspection contract, the trusted `X-Owner-Email` / `X-Client-Id`
headers, and every service's identity gate are untouched. Renaming a tool does
not touch the authorization path.

### The rename rule

```
ikigenba_<svc>_<verb>   →   <verb>
```

Applied to every tool each in-scope service registers — e.g.
`ikigenba_crm_reflection` → `reflection`, `ikigenba_sites_publish` →
`publish`, `ikigenba_prompts_run_get` → `run_get`. Bare verbs were already
unique within a service (modulo the prefix); uniqueness across services is
irrelevant because each service is a separate MCP server, and no bare verb
collides with a reserved MCP name.

---

## Scope — the ten MCP services

The ten services that expose MCP tools and share the identical prefix pattern:

| service | in-scope file | dashboard restart needed? |
|---|---|---|
| crm | `crm/internal/mcp/tools.go` (+ a `mcp.go` comment) | — |
| ledger | `ledger/internal/mcp/tools.go` | — |
| notify | `notify/internal/mcp/tools.go` | — |
| dropbox | `dropbox/internal/mcp/tools.go` | — |
| cron | `cron/internal/mcp/tools.go` | — |
| wiki | `wiki/internal/mcp/tools.go` | — |
| gmail | `gmail/internal/mcp/tools.go` | — |
| sites | `sites/internal/mcp/tools.go` | — |
| prompts | `prompts/internal/mcp/tools.go` | — |
| scripts | `scripts/internal/mcp/tools.go` | — |

Alongside the `toolPrefix` line, each service's `*_test.go` files and any
description-string or comment cross-references that name a tool by its old
prefixed form are updated in lockstep, so every module stays green at its phase
boundary (the per-service rename and its test updates land together).

**The dashboard has no MCP surface and is not touched.** Its only involvement is
a restart at the very end of the rollout so it re-reads the service manifests
(per the suite deploy rule for a changed MCP surface).

---

## Consequences

- **This is a user-visible API change.** The names the agent calls move from
  `ikigenba_<svc>_<verb>` to `<verb>` (wrapped: from
  `mcp__ikigenba_crm__ikigenba_crm_reflection` to
  `mcp__ikigenba_crm__reflection`). Any saved prompt, script, or external caller
  pinned to an old fully-qualified name must be updated.
- **A minor version bump per service.** All ten services are pre-1.0, so a minor
  bump carries the user-visible change per `docs/versioning.md`. Each affected
  service gets `bin/bump <svc> minor`, then the standard
  ship → stage → deploy flow against `int.ikigenba.com`.
- **A full redeploy of the ten services, then a dashboard restart.** The renames
  must be committed to `main` before any `bin/ship` (which builds `main` HEAD);
  after all ten are live, the dashboard is restarted so it re-reads the
  manifests and the bare-verb surface is exposed end to end.
- **Downtime is acceptable.** There are no live users on `int`, so the rollout
  takes the short, scheduled downtime the suite's operating bet already permits;
  no zero-downtime machinery is needed.
- **The change is uniform and reversible.** Ten one-line `toolPrefix` edits (plus
  their test/comment updates) with the `tool()` helper kept in place; reverting
  is the same one line per service.

See `docs/plan-mcp-tool-bare-names.md` for the phased implementation and
`docs/versioning.md` for the bump → ship → stage → deploy procedure.
