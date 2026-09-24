# agent-monitor

A local development tool: one Go binary a developer builds from the checkout
and runs on their own machine to observe the coding agents there through their
logs and hooks. It is never deployed to a space. The module path is
`github.com/ikigenba/ikigenba/agent-monitor`. Its package layout, import
direction, declarations, and run seam are design D01
(`specs/design/D01-layout-and-run-seam.md`); the command line is D02, the help
and version output D03, sessions and the `list` table D04, process facts read
from `/proc` D05, and the Claude, Codex, and Grok harnesses D06, D07, and D08.
This file restates none of them.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (`cmd/`, `internal/`, and `go.mod`); a hand edit
there desynchronizes the tree from the design. See the `spec` and `build-spec`
skills. Everything below is what the build run computes the gap and runs the
gates against; it is human-authored and read-only to the run.

## Infrastructure

Several files this document relies on are hand-authored infrastructure outside
the spec system, not code the build run derives: in this directory the
`Makefile`, `.golangci.yml`, `install.sh`, and `.goreleaser.yaml`, and at the
repo root `.github/workflows/release-agent-monitor.yml`. They are written by
hand under direct user instruction and must exist before the first build run.
The build run never creates, edits, or deletes any of them; one that is
missing, or that a gate needs in a form it lacks, is an environment blocker
the run files as an issue. The sections below describe what each one does.

## Toolchain

- Linux. The program reads `/proc` (D05), and one gate test writes the built
  binary's standard output to `/dev/full` (D01), which exists only there; on
  any other system gate 3 cannot run, which is an environment blocker, never a
  skip. No test reads `/proc` itself.
- Go 1.26 (`go version` must report 1.26+; verified with go1.26.5). The seam
  needs `fs.ReadLink`/`fs.ReadLinkFS` and `fstest.MapFS` link support, which
  arrived in Go 1.25.
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 3 fails with `go: -race requires cgo`
- `golangci-lint` v2 (verified with 2.12.2; config: `.golangci.yml` in this
  directory)

## Dependencies

D01 allows the standard library only, so `go.mod` carries no `require`
directive. The run never adds one; a phase that appears to need a module files
an issue for a human to adjudicate.

## Build

`make` builds `bin/agent-monitor` from the checkout (`make build`, the
`Makefile` in this directory), and that is what a story's
"`bin/agent-monitor` exists" precondition means. The gates below do not go
through `make`: they call the Go tool directly, and the tests that need a
binary build their own into a temporary directory.

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

Should `cmd/` or `internal/` not exist, grep reports the missing directory on
stderr and exits non-zero; that error means no test ids there, not a failure
of the gap.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: a literal matching the pattern anywhere in
a `*_test.go` file is counted as a covered id. agent-monitor names an offending
argument back in its escaped form, and prints session ids, paths, and titles
in theirs; both leave an id-shaped string unchanged, so a test that feeds one
in lands a matching literal in the test file. No test argument, expected
diagnostic, fixture file name or content, or other literal carries one.

**Tests run in process, through the seam.** The machine reaches the program
only through `cli.System` (D01): tests drive `cli.Run` with buffers, injected
writers (a writer that fails is how the write-error paths are exercised), and
a `System` whose `Root` is a `testing/fstest.MapFS` and whose `Home` is a
fixture path. `MapFS` implements `fs.ReadLinkFS`, so `proc/<pid>/cwd` is a
symlink entry, and a `MapFile.Sys` may carry a `*syscall.Stat_t` for
`proc.FileIDOf`. The package tests of `internal/session`, `internal/proc`, and
`internal/harness/*` work the same way, with wrapper filesystems over a
`MapFS` that inject errors (permission denied, say) or record the names
opened (how a test proves a `*.key` file is never read and nothing is
written). No test reads the real filesystem, `/proc`, the real `HOME` or
environment, or the real streams, and none starts a process.

The exceptions are the checks D01 states against the built program and its
package graph. The tests in `cmd/agent-monitor` may build the binary into a
temporary directory and exec it to verify `main`'s wiring: the bare run, the
bare run with standard output opened on `/dev/full`, argument pass-through
(for example, an unknown command reaching standard error), which stream each
output lands on, the exit code, and `list` wiring with `HOME` set to an empty
temporary directory holding no harness directories, with `HOME` set to the
empty string, and with `HOME` unset. Such a test sets the child's environment
explicitly and never mutates its own. The package-graph checks may run `go
list`: on `./...` for the package set and per package for the import
allow-lists, both as D01 states them. No other test builds, execs, or waits on
a process. The gates run offline as an ordinary user; a test never sleeps.

**Versions are data.** No test names a version value. A test that needs the
version reads `cli.Version`, and the shape test applies D03's pattern to it.

## Gates

Run from this directory (`agent-monitor/`), in order; every command must exit
0. No skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...` — includes the built-binary tests of D01, the
   `/dev/full` one among them
4. `golangci-lint run`

A per-finding `//nolint` comment for golangci-lint counts as a disabled
linter. The run never adds one to make a gate pass; a finding it cannot fix
below the contract seam, or believes is wrong, is filed as an issue for a
human to adjudicate.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id. Attribution follows the repository's rule.

## Releasing (infrastructure — outside the spec system)

Releases are cut from this monorepo by tag. The release machinery
(`Makefile`, `.goreleaser.yaml`, `install.sh`, and the workflow below) is
hand-maintained infrastructure, not spec-governed code.

- The version is `Version` in `internal/cli/version.go`, a source literal the
  binary reports verbatim — never linker-injected. D03 fixes only its shape
  (`v` plus a semantic version, prerelease and build metadata allowed); its
  *value* is release data. Edit that file directly, keep the gates green,
  merge, then tag to match. No build run is needed to bump it.
- Tag `agent-monitor/v<semver>` on `main`, where the tag's version equals
  `Version` exactly. A release tag never carries build metadata (`+...`):
  `Version` may have that shape, but it is not released that way. A tag with a
  prerelease part (`agent-monitor/v1.2.0-rc.1`) is a prerelease.
- Pushing the tag triggers `.github/workflows/release-agent-monitor.yml`
  (repo root), which builds `bin/agent-monitor` and verifies that
  `agent-monitor --version` prints exactly the tag's version (a mismatched tag
  fails the release), then runs GoReleaser from this directory using
  `.goreleaser.yaml` — linux/darwin × amd64/arm64, tar.gz archives, checksums —
  and publishes a GitHub release on the tag, marked as a GitHub prerelease
  when the tag has a prerelease part.
- Find the newest stable tag by filtering out prereleases first:

  ```
  git tag --list 'agent-monitor/v*' --sort=-v:refname \
    | grep -E '^agent-monitor/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' \
    | head -1
  ```

  Do not take the head of the unfiltered list: git's version sort places
  `v1.0.0-rc.1` above `v1.0.0`. Order prereleases by semver.org precedence,
  not by git's sort.
- `install.sh` (this directory) installs a release. With no version it
  installs the newest stable release: the highest semver precedence among
  releases without a prerelease part. `AGENT_MONITOR_VERSION=<version>`
  installs exactly that version, a prerelease included. It installs into
  `BINDIR`, defaulting to `${PREFIX:-$HOME/.local}/bin` (so `~/.local/bin`),
  and warns on stderr, prefixed `agent-monitor: `, when that directory is not
  on `PATH`.
