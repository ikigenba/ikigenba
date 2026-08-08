# dashboard

The apex/`DEFAULT` app of the ikigenba suite: the suite's OAuth authorization
server, IAM, grants, install landing, and service inventory. It owns identity
(an external IdP authenticates the human; the dashboard mints its own opaque
tokens, which services trust via nginx-injected headers) and, on the box, the
nginx trust boundary plus apex TLS. Small business scale (≤100 users per box):
one box, SQLite, in-process everything, deliberately. Deployed at
`<account>.ikigenba.com/` (first account: `int`). Module path: `dashboard`.

The human web surface is four pages: a logged-out **login** page, a logged-in
**landing/home** page (connect-your-agent install instructions plus the clickable
service list), a session-gated **profile** page reached from the owner's email,
and a session-gated **metrics** page graphing the box's resource health.
Personal-access-token management and OAuth-grant management live on the profile
page, not the landing.

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

- `cmd/dashboard/`: main entry point (appkit one-binary contract).
- `internal/`: the app packages (`googleidp`, `oauth`, `oauthstate`,
  `session`, `identity`, `pat`, `ratelimit`, `audit`, `grantevents`, `server`,
  `db`, `metrics`, `ids`).
- `ui/`: embedded HTML templates and static assets (login, grants).
- `etc/`: `manifest.env`, `deploy.env`, `nginx.conf`.
- `bin/`: box-side scripts (`start`, `stop`, `secrets`, `teardown`).
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

The dashboard adopts the suite testing-language contract in root
`project/design/D23.md`. Its default gate is `cd dashboard && go test ./...`,
inside the full green bar: `go build ./...`, `go vet ./...`, a silent
`gofmt -l .`, and `go test ./...`.

- **Hermetic:** the package suites, including `internal/server` HTTP tests over
  the real route table with `httptest` and a migrated temporary SQLite database;
  identity, OAuth, PAT, session, rate-limit, audit, grant-event, ID, database,
  IdP-fake, metrics-fixture, and in-process edge-ingest coverage; and shipped-file guards
  for `etc/nginx.conf` and `etc/manifest.env`.
- **Composed:** `cmd/dashboard/main_test.go` builds the real dashboard binary,
  assembles an `/opt/dashboard/`-shaped temporary tree, starts `bin/run serve` on
  loopback, and checks health, database creation, and the composed cache path
  without contacting an identity provider.
- **Manual:** interactive Google and GitHub sign-in, live apex routing and
  identity-header emission through nginx, and the real GitHub organization gate
  are deploy-time operator checks because they require a browser handshake.
- **No live layer:** the tree has no `//go:build live` test files; automatable
  claims are hermetic or composed, while interactive claims are manual.

There are no environmental preconditions beyond the Go toolchain: tests need no
`git`, `python3`, browser, credentials, or already-running service. The gate uses
**GOWORK mode: workspace**, resolving `appkit`, `eventplane`, and `registry`
through the repo-root `go.work` and committed replace-siblings; `bin/ship`'s
production `GOWORK=off` is not the test-gate mode.

## Versioning

The committed `dashboard/VERSION` file is the single source of truth
(v-prefixed SemVer, currently `v0.17.2`). Advance it with
`bin/bump dashboard <major|minor|patch>`; ship with `bin/ship dashboard`. Git
tags are not the version mechanism.
