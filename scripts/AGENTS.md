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

Changes go through the spec under `project/`, not direct edits — settle the
spec, then let the build loop realize it. The spec itself is direction-gated:
`project/**` is written only inside an operator-invoked move (the `$open-spec`
→ `$grill-me` → `$seal-spec` arc, or the build loop's completion mutations).
In any other session `project/` is read-only reference — a stale or wrong spec
is a finding to report, not a license to edit, and a settled discussion is not
direction: say what should change and wait. Edit code directly only on
explicit operator instruction. See the `$ikispec` skill for the `project/`
spec contracts and `$ralph` for the unattended build workflow.

## Layout

- `cmd/scripts`: the composition root (`scriptsSpec()`, the domain wiring).
- `internal/`: `script` (domain), `runner` (async lifecycle; execs `python3` in a
  process group) plus the embedded `runner/suite.py` client every run imports,
  `consume` (the consumer fan-out), `mcp`, `db` (embedded migrations), `ids`.
- `etc/`, `share/www`, `bin/` (start/stop/teardown).
- `project/`: the spec the build loop works from.

## Tests

- The default gate test command is `cd scripts && go test ./...`.
- The tree has hermetic and composed test layers. It has no live layer and
  no manual layer. The `suite.py` client is tested hermetically through a real
  `python3` probe harness (no pytest); local subprocesses and loopback HTTP are
  part of the default gate.
- Beyond the Go toolchain, `python3` on `PATH` is an environmental precondition
  and its absence is a hard test failure.
- GOWORK mode is workspace, using the repo-root `go.work`; the production build
  forces `GOWORK=off` through `bin/ship scripts`.
- Green also means clean `go build ./...`, `go vet ./...`, and `gofmt -l .`. The
  tree declares no live invocation and requires no live credentials.

## Versioning

The committed `scripts/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.11.2`). Advance it with `bin/bump scripts <major|minor|patch>`;
ship with `bin/ship scripts`. Git tags are not the version mechanism.
