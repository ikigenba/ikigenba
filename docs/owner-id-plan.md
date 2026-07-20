# Plan — Owner-Id Keying (owner-id)

Companion to `owner-id-design.md`. Each phase is sized for one subagent and
follows the same shape: amend that module's `project/` spec to the design
(open-spec, seal-spec), run the module's build loop until its plan queue is
empty, and leave the module's tests green (`go test ./...` in the module).
No hand edits to governed source.

Phase 1 must land first (the hard flip). Phases 2 through 14 are independent
of each other and may run in any order or in parallel; each depends only on
phase 1. Phase 15 runs last. Between phase 1 and a module's own phase, that
module's tests are expected red; a phase is done only when its module is green.

## Phase 1 — appkit: Identity and the gate flip

Spec-amend appkit's `project/` per Decision 1:

- `server.Identity` gains `OwnerID`, `OwnerName`, `OwnerPicture` (keeping
  `OwnerEmail`, `ClientID`).
- `requireIdentityHeaders` gates on `X-Owner-Id` (401 with the same challenge
  when absent); `X-Owner-Email` is no longer gated on.
- `mcp.identityFromRequest` fallback reads all four headers.
- Health envelope adds `owner_id` beside `owner_email` and `client_id`.
- appkit's own tests inject `X-Owner-Id`.

Done when appkit's plan queue is empty and `go test ./...` plus
`GOWORK=off go build ./...` pass in `appkit/`.

## Phases 2–7 — storing services: schema rebuild + rekey

One phase per service, each amending that service's `project/` spec per
Decisions 2–5. Common content:

- New migration via `bin/create-migration <svc> <name>` rebuilding the
  owner-scoped tables with `owner_id` + `owner_email` columns (rows dropped,
  per Decision 4). Never edit committed migrations.
- All queries, scoping, and uniqueness key on `owner_id`; `owner_email` is
  written once at create from `Identity.OwnerEmail` and never read for logic.
- Tests inject `X-Owner-Id` (plus display headers where asserted).
- MCP surfaces exposing an owner keep `owner_email` and add `owner_id`.

| phase | service | tables |
|---|---|---|
| 2 | prompts | `prompts`, `runs` |
| 3 | repos | `repos`, `sessions` (intake consumer follows webhooks' payload rename: `owner` → `owner_id`/`owner_email`, keys on `owner_id`) |
| 4 | scripts | `scripts` |
| 5 | webhooks | `webhooks` |
| 6 | wiki | `wiki_ingest`, `wiki_jobs`, `jobs` (rename `owner`; consumer seam rekeys) |
| 7 | sites | `sites` (rename `created_by` to `owner_email`, add `owner_id`) |

## Phases 8–9 — non-storing services with owner code

Code-only conversions (no migration):

| phase | service | scope |
|---|---|---|
| 8 | dropbox | health/display reads move to the new Identity shape; tests inject `X-Owner-Id` |
| 9 | github | log fields gain `owner_id`; tests inject `X-Owner-Id` |

## Phases 10–14 — remaining services: test/identity alignment

crm (10), ledger (11), notify (12), cron (13), gmail (14). No owner columns,
no migrations. Tests inject `X-Owner-Id`; any `Identity.OwnerEmail` display
reads keep working under the new shape. **Ledger holds live production data:
its phase must ship no migration and touch no schema.**

## Phase 15 — suite verification

- `bin/start`; all services healthy through the nginx auth chain (MCP `health`
  for every service returns the caller's `owner_id` and `owner_email`).
- Exercise one storing service end to end (create via MCP, confirm the row
  carries both columns and lists render email).
- Audit greps: no logic path keys on `owner_email` (only write-at-create and
  display); no remaining `X-Owner-Email` gating anywhere outside display
  assertions.
- `bin/stop` when done.

Deploy is not part of this plan; it is a separate operator-triggered step per
`deploy.md`.
