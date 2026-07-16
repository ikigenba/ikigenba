# cron

The `cron` service for the ikigenba suite: a loopback-only scheduled-event emitter
under `/srv/cron/`. It keeps a programmable crontab of named UTC schedules and,
once per matching wall-clock minute, publishes a `tick` event on the event plane.
An appkit binary and event-plane producer, it serves a bearer-gated MCP surface for
agents and a session-gated landing page for humans, with no token logic (nginx is
the sole trust boundary). Module path: `cron`.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/cron`: the composition root (the inline `appkit.Spec`, wiring store, MCP,
  landing, and the tick worker).
- `internal/`: `crontab` (store), `cron` (expression parser/matcher), `tick`
  (the minute-aligned firing worker), `event` (the `tick` contract), `mcp`
  (the domain tools), `db` (embedded migrations).
- `etc/`: `manifest.env` and the nginx location fragment.
- `share/www`: the landing page.
- `project/`: the spec the build loop works from.

## Tests

- `go test ./...` from `cron/`.
- Green also means clean `go build ./...`, `go vet ./...`, and `gofmt -l .`.

## Versioning

The committed `cron/VERSION` file is the single source of truth (v-prefixed SemVer,
currently `v0.10.1`). Advance it with `bin/bump cron <major|minor|patch>`; ship with
`bin/ship cron`. Git tags are not the version mechanism.
