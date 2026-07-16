# crm

A deployable path-routed service (`/srv/crm/`) in the ikigenba suite: a
loopback-only domain service for a sales CRM (organizations, contacts, deals,
tasks, interactions), exposed as an MCP surface and an event-plane producer.
Module path: `crm`, built on the shared `appkit` chassis over SQLite. nginx
(owned by the dashboard) is the sole trust boundary: it introspects each request
against the dashboard, strips the `/srv/crm/` prefix, and injects trusted
`X-Owner-Email` / `X-Client-Id` headers, so this service runs no token logic and
binds `127.0.0.1` only.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/crm/`: the `appkit.Main` entrypoint (serve plus fixed verbs).
- `internal/crm/`: the domain package, one file per entity, plus `service.go`
  (dispatcher seam) and `events.go`.
- `internal/db/`: SQLite open, migration runner, and `migrations/`.
- `internal/mcp/`: JSON-RPC transport, tool registry, `guide.md`.
- `etc/`: `manifest.env` and deploy config.
- `bin/`: on-box `start`/`stop` systemd control.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

- Unit: `go test ./...` (or `make test`).

## Versioning

The committed `crm/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.12.1`). Advance it with `bin/bump crm <major|minor|patch>`;
ship with `bin/ship crm`. Git tags are not the version mechanism.
