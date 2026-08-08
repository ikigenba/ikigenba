# gmail

gmail is a deployable, path-routed service in the ikigenba suite, mounted at
`/srv/gmail/` (Go module `gmail`, over SQLite on the shared appkit chassis). It
is a loopback-only Gmail connector: a bearer-gated MCP surface for agents plus a
Gmail History API poll daemon, and an event-plane producer that publishes
`mail.*` facts to its outbox `/feed`. nginx routes `/srv/gmail/` and stays the
sole trust boundary; the service accepts the trusted identity headers as input,
runs no token logic, and reads its Gmail OAuth secrets only from the environment.

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

- `cmd/gmail/`: binary entrypoint, builds the `appkit.Spec` and verb dispatch.
- `cmd/consent/`: one-time OAuth consent CLI.
- `internal/mcp/`: Gmail MCP tools, handler, and the published-event registry.
- `internal/gmail/`: Gmail client and the producer engine (poll loop, outbox).
- `internal/db/`: migration `FS` and load guards; SQLite/migrations are appkit's.
- `share/`: the human web landing assets served via `Spec.WWW`.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

- Package checks from this directory: `go build ./...`, `go vet ./...`,
  `gofmt -l .`, `go test ./...`.
- Default-gate test command: `cd gmail && go test ./...`.
- Layers present: **hermetic**, **composed**, and **live**.
- Manual layer: absent.
- Environmental preconditions beyond the Go toolchain: none.
- GOWORK mode: workspace (the repo-root `go.work`); the production build forces
  `GOWORK=off` via `bin/ship gmail`.
- Live invocation: `cd gmail && go test -tags live ./...`. It requires all three
  credentials: `GMAIL_CLIENT_ID`, `GMAIL_CLIENT_SECRET`, and
  `GMAIL_REFRESH_TOKEN`, supplied from the suite `.envrc`.
- Run the live invocation at deploy verification for gmail, and whenever a
  change touches the Gmail client (`internal/gmail/client.go`), the
  attachment/multipart path, or the OAuth token exchange.

## Versioning

The committed `gmail/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.9.1`). Advance it with `bin/bump gmail <major|minor|patch>`;
ship with `bin/ship gmail`. Git tags are not the version mechanism.
