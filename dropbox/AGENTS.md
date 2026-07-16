# dropbox

`dropbox` is the ikigenba suite's single-tenant, Dropbox-backed filesystem
service, deployed path-routed at `/srv/dropbox/`. It is a loopback daemon that
keeps a private local mirror in sync with one Dropbox app folder
(`ikigai-onebox`): downloads flow down through a longpoll engine, while suite
writes commit locally and flow up asynchronously via a durable upload queue, so
Dropbox is a replica of local state. It runs behind the dashboard's nginx
session gate, trusting the injected identity headers with no token logic of its
own; it exposes MCP to agents plus a loopback filesystem API for sibling
services, and is an event-plane producer (`create`/`modify`/`delete`).
Module path: `dropbox`.

## How changes are made

Changes go through the spec under `project/`, not direct edits, settle the
spec, then let the build loop realize it. Edit code directly only on explicit
operator instruction. See the `$ikispec` skill for the `project/` spec contracts
and `$ralph` for the unattended build workflow.

## Layout

- `cmd/dropbox/`: `main.go` composition root wiring domain, sync engine, and uploader into appkit.
- `internal/dropbox/`: the mirror, index, sync engine, uploader, events, and filesystem handlers.
- `internal/mcp/`: the six service MCP tools.
- `internal/db/`: embedded SQLite migrations.
- `share/www/`: landing page and static assets.
- `docs/`, `etc/`, `bin/`: service docs, config, and local scripts.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

- Unit: `go test ./...`

## Versioning

The committed `dropbox/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.14.2`). Advance it with `bin/bump dropbox <major|minor|patch>`;
ship with `bin/ship dropbox`. Git tags are not the version mechanism.
