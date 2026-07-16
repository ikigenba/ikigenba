# scripts

The `scripts` service for the ikigenba suite: a loopback-only service under
`/srv/scripts/` that runs deterministic Python scripts wired to suite events,
exposed as MCP. A script is the owner's own code, authored and supervised over MCP,
execed as `python3 main.py` in a per-run dir. An appkit binary, event-plane
producer (emits `succeeded`/`failed` completion events) and multi-upstream consumer
(fires matching scripts on cron/crm/ledger/dropbox/prompts events), so it
self-chains. No token logic (nginx is the sole trust boundary). Module path:
`scripts`.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/scripts`: the composition root (`scriptsSpec()`, the domain wiring).
- `internal/`: `script` (domain), `runner` (async lifecycle; execs `python3` in a
  process group) plus the embedded `runner/suite.py` client every run imports,
  `consume` (the consumer fan-out), `mcp`, `db` (embedded migrations), `ids`.
- `etc/`, `share/www`, `bin/` (start/stop/teardown).
- `project/`: the spec the build loop works from.

## Tests

- `go test ./...` from `scripts/`. The `suite.py` client is tested through a real
  `python3` probe harness (no pytest).
- Green also means clean `go build ./...`, `go vet ./...`, and `gofmt -l .`. The
  prod build forces `GOWORK=off`.

## Versioning

The committed `scripts/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.11.2`). Advance it with `bin/bump scripts <major|minor|patch>`;
ship with `bin/ship scripts`. Git tags are not the version mechanism.
