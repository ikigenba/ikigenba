# agent-monitor

A local development tool: a Go binary run on a developer's own Linux machine,
as that developer, to observe the coding agents working on it (Claude Code,
Codex, and Grok). It reads the logs
those agents keep under the developer's home directory and the process facts
in `/proc`, and never writes to either. It is never deployed to a host.
Module path `github.com/ikigenba/ikigenba/agent-monitor`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run brings `cmd/`, `internal/`, and `go.mod` into agreement with that
target. See the `spec` and `build-spec` skills. Everything below is what the
build run computes the gap and runs the gates against; it is human-authored
and read-only to the run.

## Toolchain

- Linux (the program reads `/proc`)
- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): without one, gate 3 fails with
  `go: -race requires cgo`
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped literal in a fixture.** agent-monitor echoes arguments, ids,
paths, and transcript text back unchanged in shape, so a test that feeds in a
string matching the pattern lands a literal the grep counts as a covered id.
No test argument, expected output, fixture name, or fixture content carries
one.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs. Live
tests follow Live tests below.

- Offline: no network beyond loopback, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**No real machine in the gates.** The machine reaches the program only through
the run seam the design defines: tests drive it with buffers, injected
writers, a `testing/fstest.MapFS` root, and a fixture home, and fake faults
with wrapper filesystems over that `MapFS`. No test reads the real filesystem,
`/proc`, `HOME`, environment, or streams, and none sleeps. No test starts a
process or reads a file of the checkout: a test proves the design by using
what it declares (importing, calling, constructing, driving `Run`), never by
reading the module's source, layout, or `go.mod`. The gates run offline as an
ordinary user.

## Live tests

Live tests are the only tests that connect to external services. Every other
test is a unit test and follows Test discipline.

- Minimal: a live test proves lightly that the whole application or library
  is glued together and works end to end, about one per external service,
  never an exhaustive suite. Behavior, edge cases, and error paths are the
  unit tests' job.
- Separate: live tests are `*_live_test.go` files guarded by
  `//go:build live`, with test functions named `TestLive*`. `go test ./...`
  never runs them; `make live` runs
  `go test -tags live -count=1 -run '^TestLive' ./...`.
- Designed: a live test carries the requirement id it proves, and the gap
  counts it like any other test.
- Local: the code under test runs on the developer's machine; only the
  external service is real.
- Credentials: a live test reads its credentials from the environment. It
  never skips: a missing credential fails it. No credential appears in the
  repo or in test output.
- Run: live tests run only as the conditional `make live` gate below.

## Gates

Run from this directory (`agent-monitor/`), in order; every command must exit
0. No skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form; since the cache is private, `--allow-parallel-runners` skips
   golangci-lint's machine-wide lock so gates for several sub-projects can lint
   at once)
5. `make live` — **conditional**: run only when the phase's diff (the working
   tree against the last phase commit) adds or modifies a `*_live_test.go`
   file; otherwise it is not run and not counted. When it applies and a
   credential is absent, that is a missing tool: file an issue, do not pass or
   skip.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Deploy

Deploy machinery — the version bump, tags, the `Makefile`, `install.sh`,
`.goreleaser.yaml`, and `.github/workflows/release-agent-monitor.yml` (repo
root) — is hand-maintained infrastructure outside the spec system: the build
run never reads, edits, or tests it.

1. Set the version in `internal/cli/version.go` (D03) to `vX.Y.Z`. It is a
   source literal the binary reports verbatim, and the deploy refuses a tag
   that does not match what the built binary's `--version` prints.
2. Commit that on `main` and push `main`.
3. Tag that commit `agent-monitor/vX.Y.Z` and push the tag.
   `.github/workflows/release-agent-monitor.yml` builds with GoReleaser and
   publishes linux amd64/arm64 archives and checksums. A tag with a prerelease
   part (`agent-monitor/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Wait for the workflow to publish the release (`gh release view
   agent-monitor/vX.Y.Z` succeeds), then install it on the developer's machine
   from this directory. A release is not done until this step is:

   ```
   AGENT_MONITOR_VERSION=vX.Y.Z sh install.sh
   ```

   The installer puts the binary in `~/.local/bin`. Plain `sh install.sh`
   installs the newest stable release.

`agent-monitor --version` then prints `vX.Y.Z`.
