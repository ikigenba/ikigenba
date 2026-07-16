# github

A path-routed service in the ikigenba suite: the single loopback connector to
the `@ikigenba` GitHub organization. It holds the one GitHub App installation,
mints and refreshes the installation token itself, and exposes the org's
repositories, pull requests, issues, and file contents as MCP tools that other
services (a `prompts` agent, a `scripts` job) drive on the owner's behalf, so no
other service handles GitHub credentials or GitHub's API. nginx routes
`/srv/github/` to the loopback server on port 3203; the service owns no domain
database and is not an event-plane producer or consumer. Module path: `github`.

## How changes are made

Changes go through the spec under `project/`, not direct edits, settle the
spec, then let the build loop realize it. Edit code directly only on explicit
operator instruction. See the `$ikispec` skill for the `project/` spec contracts
and `$ralph` for the unattended build workflow.

## Layout

- `cmd/github/`: `main.go`, the binary entrypoint and composition root.
- `internal/gh/`: GitHub REST client, installation-token mint/refresh.
- `internal/githubapp/`: appkit service spec (mount, port, wiring).
- `internal/mcp/`: the domain tool surface (repos, PRs, issues, files).
- `internal/db/`: bootstrap migration tracking (no domain state).
- `internal/web/`: landing page and nginx fragment.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

- Unit: `GOWORK=off go test ./...`
- Vet and format: `GOWORK=off go vet ./...`, `gofmt -l .`

## Versioning

The committed `github/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.4.2`). Advance it with `bin/bump github <major|minor|patch>`;
ship with `bin/ship github`. Git tags are not the version mechanism.
