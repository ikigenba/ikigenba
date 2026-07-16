# webhooks

The `webhooks` service for the ikigenba suite: the inbound ingress for the event
plane. An owner mints a named, secret-protected URL an outside system can call, and
a valid call becomes one durable fact on the event plane. It has two surfaces: an
owner-facing MCP table (create/list/delete/rotate) reached through the front-door
auth chain, and a public `POST /srv/webhooks/in/<name>` ingress that third parties
call directly, guarded only by a per-webhook secret (not behind the dashboard
auth_request). An appkit binary and event-plane producer (emits `received`). Module
path: `webhooks`.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/webhooks`: the composition root (the `appkit.Spec`, both surfaces + the
  producer hook).
- `internal/`: `webhooks` (domain: secret lifecycle, the public ingress with its
  bearer/github-hmac verification and byte-identical 404, the `received` event),
  `mcp` (the four owner tools), `db` (store + embedded migrations), `ids`, `e2e`.
- `etc/`, `share/www`: manifest, nginx fragment, landing page.
- `project/`: the spec the build loop works from.

## Tests

- `go test ./...` from `webhooks/` (real temp-file SQLite, injected clock). Green
  also means clean `go build ./...` and `go vet ./...`.
- The prod build (via `bin/ship webhooks`) forces `GOWORK=off`; a `Makefile` drives
  local dev.

## Versioning

The committed `webhooks/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.6.2`). Advance it with `bin/bump webhooks <major|minor|patch>`;
ship with `bin/ship webhooks`. Git tags are not the version mechanism.
