# repos

The suite's **development plane**: a path-routed service (`/srv/repos/`, loopback
port `3007`) that keeps local clones of the org's GitHub repos and runs agent
sessions against them. A human labels a GitHub issue `execute`; repos clones the
repo on first contact, runs a confined agent session in an isolated worktree on a
bot-namespace branch, and (when the repo's own check passes) opens a pull request
attributed to `@ikibot`, reporting progress on the issue itself. Owners drive it
through MCP (onboard, start, list, inspect, cancel sessions, read transcripts).
It is a standalone Go module (`repos`) on the shared appkit chassis over SQLite,
an event-plane **consumer** (of `webhooks`) and **producer** (session outcomes on
`/feed`).

## How changes are made

Changes go through the spec under `project/`, not direct edits, settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/repos/`: the binary (composition root and appkit verb dispatch).
- `internal/repos/`: core domain (intake, git custody, sessions, reaper, events).
- `internal/runner/`: the confined agent session runner.
- `internal/mcp/`, `internal/tools/`: the owner-facing MCP tool surface.
- `internal/db/`: SQLite handle, embedded `migrations/`, feed consumer.
- `etc/`, `share/`: nginx fragment, manifest/deploy env, the landing page.
- `project/`: the spec (product/research/design/plan) the build loop works from.

## Tests

- Unit: `go test ./...`

## Versioning

The committed `repos/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.1.2`). Advance it with `bin/bump repos <major|minor|patch>`;
ship with `bin/ship repos`. Git tags are not the version mechanism.
