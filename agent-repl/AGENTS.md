# agent-repl

An interactive REPL over `github.com/ikigenba/ikigenba/agentkit`: one
conversation per session, the six `toolkit` file tools registered, every
choice a `-c key=value` string, and a help screen generated from agentkit's
catalog. Module path `github.com/ikigenba/ikigenba/agent-repl`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (`cmd/`, `internal/`, `go.mod` are absent until it
does). See the `spec` and `build-spec` skills. Everything below is what the
build run computes the gap and runs the gates against; it is human-authored and
read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- Network access to the Go module proxy for `agentkit` (`v0.11.0`+) and
  `toolkit`, both published from this monorepo under `agentkit/v*` and
  `toolkit/v*` tags, and for `github.com/google/uuid`.

## Dependencies

Every direct dependency is approved by a human, and the approval is recorded
in the design: D1 names the exact set of direct requirements, and a gate test
compares `go.mod` against it. The run never adds a module; a phase that
appears to need one files an issue for a human to adjudicate. Adopting a new
release of an approved dependency is a dependency edit, not a design change,
and the run makes it whenever a phase's requirements only compile against
the newer release.

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** agent-repl neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

**No live provider in the gates.** agentkit sends requests through Go's
default HTTP client, so every test that needs a provider stands up an
`httptest` server and points the session at it with `-c base_url=` (design
D1, D3). No gate needs a real API key, a real token file, or the network
beyond loopback; a test that reads `~/.agent-repl` or a real `*_API_KEY`
variable is a bug.

## Gates

Run from this directory (`agent-repl/`), in order; every command must exit 0.
No skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form)

A per-finding `//nolint` comment counts as a disabled linter. Never add one
to make a gate pass.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Releasing

Release machinery — the version bump, tags, `.goreleaser.yaml`, and
`.github/workflows/release-agent-repl.yml` (repo root) — is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it.

1. Set the version in `internal/cli/version.go` (D4) to `vX.Y.Z`. It is
   source-carried, never ldflags-injected; the spec fixes only its shape, so no
   build run is needed to bump it.
2. Commit that on `main` and push `main`.
3. Tag that commit `agent-repl/vX.Y.Z` and push the tag. The workflow verifies
   the tag matches the in-source version (a mismatch fails the release), then
   runs GoReleaser from this directory: linux/darwin × amd64/arm64 tar.gz
   archives, checksums, and a GitHub release on the tag.

The latest release is `git tag --list 'agent-repl/v*' --sort=-v:refname | head
-1`.
