# dashboard

The apex/`DEFAULT` app of the ikigenba suite: the suite's OAuth authorization
server, IAM, grants, install landing, and service inventory. It owns identity
(an external IdP authenticates the human; the dashboard mints its own opaque
tokens, which services trust via nginx-injected headers) and, on the box, the
nginx trust boundary plus apex TLS. Small business scale (≤100 users per box):
one box, SQLite, in-process everything, deliberately. Deployed at
`<account>.ikigenba.com/` (first account: `int`). Module path: `dashboard`.

## How changes are made

Changes go through the spec under `project/`, not direct edits, settle the
spec, then let the build loop realize it. Edit code directly only on explicit
operator instruction. See the `$ikispec` skill for the `project/` spec contracts
and `$ralph` for the unattended build workflow.

## Layout

- `cmd/dashboard/`: main entry point (appkit one-binary contract).
- `internal/`: the app packages (`googleidp`, `oauth`, `oauthstate`,
  `session`, `identity`, `pat`, `ratelimit`, `audit`, `grantevents`, `server`,
  `db`, `telemetry`, `ids`).
- `ui/`: embedded HTML templates and static assets (login, grants).
- `etc/`: `manifest.env`, `deploy.env`, `nginx.conf`.
- `bin/`: box-side scripts (`start`, `stop`, `secrets`, `teardown`).
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

- Unit: `go test ./...`

## Versioning

The committed `dashboard/VERSION` file is the single source of truth
(v-prefixed SemVer, currently `v0.17.2`). Advance it with
`bin/bump dashboard <major|minor|patch>`; ship with `bin/ship dashboard`. Git
tags are not the version mechanism.
