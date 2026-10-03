# agent-monitor

agent-monitor watches the coding agents running on the developer's machine. Nothing depends on it. It is a Go binary run on a developer's own Linux machine, as that developer, to observe Claude Code, Codex and Grok at work: it reads the logs those agents keep under the developer's home directory and the process facts in `/proc`, and never writes to either. It is never deployed to a host. The module path is `github.com/ikigenba/ikigenba/agent-monitor`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `cmd/agent-monitor` is the binary. `internal/` is everything else, one package per concern.
- The build run writes `cmd/`, `internal/` and `go.mod`. `Makefile`, `.golangci.yml`, `install.sh`, `.goreleaser.yaml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Linux: the program reads `/proc`.
- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 3).
- `golangci-lint` v2, configured by `.golangci.yml` here.
- Prefer the standard library, then a widely used public module; adding one needs human approval. `go.mod` requires nothing today.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, and agent-monitor echoes arguments, ids, paths and transcript text back unchanged in shape, so no test argument, expected output, fixture name or fixture content carries one.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No real machine in the gates.** The machine reaches the program only through the run seam design defines: tests drive it with buffers, injected writers, a `testing/fstest.MapFS` root and a fixture home, and fake faults with wrapper filesystems over that `MapFS`. No test reads the real filesystem, `/proc`, `HOME`, environment or streams. No test starts a process or reads a file of the checkout: it proves what design declares by using it (importing, calling, constructing, driving `Run`), never by reading the module's source, layout or `go.mod`. The gates run offline as an ordinary user.

## Live tests

Live tests are the only tests that reach an external service; every other test follows Test discipline. A live test is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs; `make live` runs `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together end to end, about one per external service, and leaves behavior, edge cases and error paths to the unit tests. It carries the id it proves and the gap counts it like any other test. The code under test runs on the developer's machine; only the external service is real. It reads its credentials from the environment and fails rather than skips when one is missing; no credential appears in the repo or in test output. It runs only as gate 5.

## Gates

Run from `agent-monitor/`, in order; every command must exit 0. No skipped tests and no disabled linters.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`; `go fmt` itself always exits 0, so the check form is the gate)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and stops applying `//nolint` and `.golangci.yml` suppressions), and parallel runners skip golangci-lint's machine-wide lock so several sub-projects can lint at once (`make lint` runs this form)
5. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` (the default) builds the binary; `make fmt` formats; `make test` and `make lint` run those gates; `make install` runs `go install ./cmd/agent-monitor`.

## Releasing

Release machinery, the version bump, tags, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-agent-monitor.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version in `internal/cli/version.go` (D03) to `vX.Y.Z`. It is a source literal the binary reports verbatim, and the release refuses a tag that does not match what the built binary's `--version` prints.
2. Commit on `main` and push `main`.
3. Tag the commit `agent-monitor/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux amd64 and arm64 archives and checksums; a tag with a prerelease part (`agent-monitor/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Wait for the release (`gh release view agent-monitor/vX.Y.Z` succeeds), then install it on the developer's machine from this directory with `AGENT_MONITOR_VERSION=vX.Y.Z sh install.sh`, which puts the binary in `~/.local/bin`. A release is not done until this step is. Plain `sh install.sh` installs the newest stable release.

`agent-monitor --version` then prints `vX.Y.Z`.
