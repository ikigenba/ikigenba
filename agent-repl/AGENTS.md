# agent-repl

agent-repl is a REPL for exercising agentkit by hand. One conversation per session, the six `toolkit` file tools registered, every choice a `-c key=value` string, and a help screen generated from agentkit's catalog. The module path is `github.com/ikigenba/ikigenba/agent-repl`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- `cmd/agent-repl` is the binary. `internal/` is everything else, one package per concern.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `install.sh`, `.goreleaser.yaml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.
- The modules `go.mod` requires, from the Go module proxy: `agentkit` and `toolkit`, published from this repository under `agentkit/v*` and `toolkit/v*` tags, and `github.com/google/uuid`.

Prefer the standard library, then a widely used public module; adding one needs human approval.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped literal in a fixture.** agent-repl neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No live provider in the gates.** agentkit sends requests through Go's default HTTP client, so a test that needs a provider stands up an `httptest` server and points the session at it with `-c base_url=` (D1, D3). No gate needs a real API key, a real token file or the network beyond loopback; a test that reads `~/.agent-repl` or a real `*_API_KEY` variable is a bug.

## Live tests

A live test is the only kind that reaches an external service. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together, about one per external service, leaving behavior, edge cases and error paths to the unit tests; it carries the id it proves and the gap counts it; the code under test runs locally and only the external service is real; it reads credentials from the environment, fails rather than skips when one is missing, and no credential appears in the repository or in test output. It runs only as gate 5.

## Gates

Run from `agent-repl/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)
5. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` (the default) builds the binary; `make fmt` formats; `make test` and `make lint` run those gates; `make install` runs `go install ./cmd/agent-repl`. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the version bump, tags, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-agent-repl.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version in `internal/cli/version.go` (D4) to `vX.Y.Z`. The binary reports it verbatim, and the release refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `agent-repl/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux and darwin amd64 and arm64 archives and checksums; a prerelease tag (`agent-repl/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Once `gh release view agent-repl/vX.Y.Z` succeeds, install it from this directory with `AGENT_REPL_VERSION=vX.Y.Z sh install.sh`, which puts the binary in `~/.local/bin`. A release is not done until this step is. Plain `sh install.sh` installs the newest stable release.

`agent-repl --version` then prints `vX.Y.Z`.
