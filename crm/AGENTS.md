# crm

A deployable path-routed service (`/srv/crm/`) in the ikigenba suite: a
loopback-only domain service for a sales CRM (organizations, contacts, deals,
tasks, interactions). It serves two doors under `/srv/crm/`: an MCP surface for
agents (bearer-gated) and a human web landing page (session-cookie-gated), and it
is also an event-plane producer. Module path: `crm`, built on the shared `appkit`
chassis over SQLite. nginx (owned by the dashboard) is the sole trust boundary: it
introspects each request against the dashboard, strips the `/srv/crm/` prefix, and
injects trusted `X-Owner-Email` / `X-Client-Id` headers, so this service runs no
token logic and binds `127.0.0.1` only.

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

- `cmd/crm/`: the `appkit.Main` entrypoint (serve plus fixed verbs).
- `internal/crm/`: the domain package, one file per entity, plus `service.go`
  (dispatcher seam) and `events.go`.
- `internal/db/`: SQLite open, migration runner, and `migrations/`.
- `internal/mcp/`: the MCP tool table over `appkit/mcp`, plus `guide.md`.
- `etc/`: `manifest.env` and deploy config.
- `bin/`: on-box `start`/`stop` systemd control.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

crm adopts the suite testing-language contract in `root project/design/D23.md`.

- Default gate: from the repository root, run `cd crm && go build ./...`,
  `cd crm && go vet ./...`, confirm `cd crm && gofmt -l .` prints nothing, and
  run `cd crm && go test ./...`.
- Layers present: **hermetic** and **composed**. There is **no live layer** and
  **no manual layer**.
  - Hermetic contains the in-process domain, database, MCP, web, and event-plane
    tests, plus the shipped-file guards for `etc/nginx.conf`,
    `etc/manifest.env`, and the loopback source scan. These tests are offline
    and use only loopback where HTTP is involved.
  - Composed is the boot smoke in `cmd/crm/main_test.go`; it builds crm's binary,
    assembles an `/opt/crm/`-shaped tree in a temporary directory, runs the
    binary on a free loopback port, and checks `/health`.
- Environmental preconditions beyond the Go toolchain: **none**. Tests require
  no external service, browser, credential, running suite, `git`, or `python3`.
- GOWORK mode: **workspace**. The gate resolves `appkit`, `eventplane`, and
  `registry` through the repository-root `go.work` and committed sibling
  replacements; the standalone production build belongs to `bin/ship crm`.

## Versioning

The committed `crm/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.12.1`). Advance it with `bin/bump crm <major|minor|patch>`;
ship with `bin/ship crm`. Git tags are not the version mechanism.
