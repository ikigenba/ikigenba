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

- `cmd/github/`: `main.go`, the binary entrypoint and composition root.
- `internal/gh/`: GitHub REST client, installation-token mint/refresh.
- `internal/githubapp/`: appkit service spec (mount, port, wiring).
- `internal/mcp/`: the domain tool surface (repos, PRs, issues, files).
- `internal/db/`: bootstrap migration tracking (no domain state).
- `internal/web/`: landing page and nginx fragment.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

The default gate runs from `github/` with `GOWORK=off go test ./...`. A green
suite also requires clean `GOWORK=off go build ./...`, `GOWORK=off go vet ./...`,
and `gofmt -l .` runs (the formatter check prints nothing). `GOWORK=off` is the
tree's required mode: it mirrors the production build and proves the module
resolves standalone through its committed `replace` directives.

The gate has two test layers. The hermetic layer is fully offline and covers
package behavior, migrations, rendering, and committed-file checks. The
composed layer is the install-layout boot smoke in `cmd/github/main_test.go`,
which builds and runs the real binary but remains offline. The manual layer is
the committed operator runbook at `project/github-verification.md`. This tree
has no live layer.

The only environmental precondition beyond the Go toolchain is the `go` binary
on `PATH` in the test process's environment, with the module cache already
resolving github's `replace` siblings. Its absence is a hard failure, never a
skip.

## Versioning

The committed `github/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.4.2`). Advance it with `bin/bump github <major|minor|patch>`;
ship with `bin/ship github`. Git tags are not the version mechanism.
