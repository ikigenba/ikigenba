# agent-repl

agent-repl is a REPL for exercising agentkit by hand. One conversation per session, the six `toolkit` file tools registered, every choice a `-c key=value` string, and a help screen generated from agentkit's catalog. The module path is `github.com/ikigenba/ikigenba/agent-repl`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- `cmd/agent-repl` is the binary. `internal/` is everything else, one package per concern.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `install.sh`, `.goreleaser.yaml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- The modules `go.mod` requires, from the Go module proxy: `agentkit` and `toolkit`, both published from this monorepo under `agentkit/v*` and `toolkit/v*` tags, and `github.com/google/uuid`.

Prefer the standard library, then a widely used public module; adding one needs human approval.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** agent-repl neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No live provider in the gates.** agentkit sends requests through Go's default HTTP client, so every test that needs a provider stands up an `httptest` server and points the session at it with `-c base_url=` (design D1, D3). No gate needs a real API key, a real token file, or the network beyond loopback; a test that reads `~/.agent-repl` or a real `*_API_KEY` variable is a bug.

## Live tests

Live tests are the only tests that connect to external services; every other test follows Test discipline.

- Minimal: a live test proves lightly that the whole is glued together end to end, about one per external service. Behavior, edge cases and error paths are the unit tests' job.
- Separate: `*_live_test.go` files behind `//go:build live`, with `TestLive*` functions. `go test ./...` never runs them; `make live` runs `go test -tags live -count=1 -run '^TestLive' ./...`.
- Designed: a live test carries the requirement id it proves, and the gap counts it like any other test.
- Local: the code under test runs on the developer's machine; only the external service is real.
- Credentials: read from the environment, never in the repo or in test output. A missing credential fails the test, never skips it.
- Run: only as the conditional `make live` gate below.

## Gates

Run from `agent-repl/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding `//nolint` comment is a disabled linter; never add one to make a gate pass.

1. `test -z "$(gofmt -l .)"` (`go fmt` always exits 0, so this check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and stops applying `//nolint` and `.golangci.yml` suppressions), and since the cache is private, `--allow-parallel-runners` skips golangci-lint's machine-wide lock so several sub-projects can lint at once (`make lint` runs this form)
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

1. Set the version in `internal/cli/version.go` (D4) to `vX.Y.Z`; it is a source literal the binary reports verbatim, and the deploy refuses a tag that does not match what the built binary's `--version` prints.
2. Commit on `main` and push `main`.
3. Tag the commit `agent-repl/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux/darwin amd64/arm64 archives and checksums; a tag with a prerelease part (`agent-repl/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Wait for the release (`gh release view agent-repl/vX.Y.Z` succeeds), then install it on the developer's machine from this directory. A release is not done until this step is:

   ```
   AGENT_REPL_VERSION=vX.Y.Z sh install.sh
   ```

   The installer puts the binary in `~/.local/bin`. Plain `sh install.sh` installs the newest stable release.

`agent-repl --version` then prints `vX.Y.Z`.
