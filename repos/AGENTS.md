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

- `cmd/repos/`: the binary (composition root and appkit verb dispatch).
- `internal/repos/`: core domain (intake, git custody, sessions, reaper, events).
- `internal/runner/`: the confined agent session runner.
- `internal/mcp/`, `internal/tools/`: the owner-facing MCP tool surface.
- `internal/db/`: SQLite handle, embedded `migrations/`, feed consumer.
- `etc/`, `share/`: nginx fragment, manifest/deploy env, the landing page.
- `project/`: the spec (product/research/design/plan) the build loop works from.

## Tests

Run the default gate from `repos/` with `go test ./...`. A green change also
requires `go build ./...` and `go vet ./...` to succeed and `gofmt -l .` to
print nothing.

This tree has two test layers, both in the default gate:

- **hermetic** tests use temp-directory filesystems, real temp-file SQLite,
  local subprocesses and loopback peers. In particular, git custody is tested
  with the real git implementation over local fixture remotes, never a mock.
- **composed** testing is the install-layout boot smoke in
  `cmd/repos/main_test.go`, which builds and runs the real service in a
  temporary `/opt/repos/`-shaped tree.

There is no live layer and no tree-local manual runbook. Tests do not contact
non-loopback services or read real credentials.

Beyond the Go toolchain, the test environment has two hard preconditions; an
absent tool is a failure, never a skip:

- The real `git` binary must be on `PATH` for the never-mocked git custody and
  worktree tests.
- The `go` binary must be on `PATH` in the test process's environment for the
  composed smoke, with the module cache resolving repos' `replace` siblings and
  the pinned `agentkit` module.

Tests, builds, and vet run in workspace mode through the repo-root `go.work`.
The production build forces `GOWORK=off`; that mode is not part of the default
gate.

## Versioning

The committed `repos/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.1.2`). Advance it with `bin/bump repos <major|minor|patch>`;
ship with `bin/ship repos`. Git tags are not the version mechanism.
