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

## Dependencies

The design allows the standard library only, so `go.mod` carries no `require`
directive. The run never adds one; a phase that appears to need a module files
an issue for a human to adjudicate.

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

**No real machine in the gates.** The machine reaches the program only through
the run seam the design defines: tests drive it with buffers, injected
writers, a `testing/fstest.MapFS` root, and a fixture home, and fake faults
with wrapper filesystems over that `MapFS`. No test reads the real filesystem,
`/proc`, `HOME`, environment, or streams, and none sleeps. The only process a
test starts is the `go list` the design's structure checks name. The gates
run offline as an ordinary user.

## Gates

Run from this directory (`agent-monitor/`), in order; every command must exit
0. No skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form)

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Deploy

Release machinery — the version bump, tags, the `Makefile`, `install.sh`,
`.goreleaser.yaml`, and `.github/workflows/release-agent-monitor.yml` (repo
root) — is hand-maintained infrastructure outside the spec system: the build
run never reads, edits, or tests it. `make` builds `bin/agent-monitor` from
the checkout.

1. Set `Version` in `internal/cli/version.go` to `vX.Y.Z`. The binary reports
   that string, and the release refuses a tag that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `agent-monitor/vX.Y.Z` and push the tag.
   `.github/workflows/release-agent-monitor.yml` builds with GoReleaser and
   publishes linux amd64/arm64 archives and checksums.
4. Wait for the workflow to publish the release (`gh release view
   agent-monitor/vX.Y.Z` succeeds), then install it on the developer's
   machine from this directory. A release is not done until this step is:

```
AGENT_MONITOR_VERSION=vX.Y.Z sh install.sh
agent-monitor --version
```

   The installer puts the binary in `~/.local/bin`, and `--version` must
   print `vX.Y.Z`. Plain `sh install.sh` installs the newest stable release.

## Live data

The live data is the developer's own agent logs: `~/.claude`, `~/.codex`, and
`~/.grok` on this machine. Any real-world verification — probing a transcript
format, running a built binary against real sessions — reads them only, and
runs from a scratch directory outside the repository.
