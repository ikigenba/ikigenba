# sandbox

A local development tool: a Go binary run on a developer's own Linux machine,
as that developer, with no root, to stand up the whole Ikigenba suite exactly
as it exists in the current git worktree, uncommitted edits included. It
discovers the checkout's apps, builds each with `go build`, and runs every app
as a pair of user-level systemd units (a `.socket` holding the app's Unix
socket and a `Type=notify` `.service` that inherits it) behind one nginx per
sandbox, itself a user unit listening on `127.0.0.1`. It renders its own
units, nginx configuration, env files and services file, writes nothing to
system directories, and keeps each sandbox's data under the developer's own
directories, so many worktrees run sandboxes at once without interfering. It
does not use `devctl` or `opsctl`, and it is never deployed to a host. Module
path `github.com/ikigenba/ikigenba/sandbox`; the binary is `sandbox`, with
`main` at `cmd/sandbox/`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code under `cmd/` and `internal/` and the dependency
graph in `go.mod` and `go.sum`. See the `spec` and `build-spec` skills.
Everything below is what the build run computes the gap and runs the gates
against; it is human-authored and read-only to the run.

## Toolchain

- Linux
- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The binary itself
  is cgo-free, which gate 3 proves.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- GNU `make`: the developer targets in the `Makefile` (`make build`,
  `make install`, `make fmt`, `make live`); no gate runs through it

The only third-party module the code may require is
`github.com/BurntSushi/toml`, for reading app manifests. Adding it still
awaits the user's approval; until it is given, `go.mod` requires nothing.
Everything else is the standard library. Once required, the module sits in
the Go module cache (`go mod download` fetches it once, online), `go.sum` is
committed, and the gates themselves run offline.

The gates fake every external program, so they need nothing beyond the
tools above: no systemd user manager, no nginx, no network. Running the built
`sandbox`, and `make live`, needs these as well:

- `git` (finds the worktree: `git rev-parse --show-toplevel`)
- `go` (builds each app from the working tree)
- a running systemd user manager, reached with `systemctl --user` and
  `journalctl --user`
- `nginx` on `PATH`, runnable by an ordinary user

## Test files

The sub-project's tests are all `*_test.go` files in the module (`cmd/` and
`internal/`). This is the file set the canonical gap greps for requirement
ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` holds no `*_test.go`, so the design documents never enter the test
side of the grep.

**No id-shaped literal in a fixture.** sandbox echoes names, paths, manifest
values, secret keys and tokens back unchanged in shape, so a test that feeds
in a string matching the pattern lands a literal the grep counts as a covered
id. No test argument, expected output, fixture name, or fixture content
carries one.

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

**No real machine in the gates.** The external programs sandbox drives
(`git`, `go`, `systemctl`, `journalctl`, `nginx`) are reached only through the
injectable run seam the design defines, and tests inject fakes that record
what they were asked to do and answer as the case needs. No test starts a
real systemd unit, an nginx, a `go build` of an app, or a `git` command
against the developer's checkout. The working directory, the environment
(`HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, and the rest), the clock, the
output streams and every process runner come in through that seam. A test
that needs a checkout, a secrets file, a sandbox's data or the registry
builds them under its own temporary directory and points the seam there; it
never reads or writes the developer's real `~/.config`, `~/.local/state`, or
systemd user directory. A Unix socket a test binds lives in a short temporary
directory (`os.MkdirTemp("", ...)`), since a socket path is limited to 108
bytes and `t.TempDir()` can exceed it.

**No test reads the checkout.** A test proves what the design declares by
using it (importing, calling, constructing, driving the command), never by
opening this module's source, layout, `go.mod` or `go.sum`. The gates run
offline as an ordinary user, with no systemd user manager and no nginx.

## Live tests

Live tests are the only tests that touch the real machine: the real systemd
user manager, a real nginx, real `git` and `go` against a real worktree.
Every other test is a unit test and follows Test discipline.

- Opt-in, never a gate: live tests are `*_live_test.go` files guarded by
  `//go:build live`, with test functions named `TestLive*`. `go test ./...`
  never runs them; a developer runs them with `make live`
  (`go test -tags live -count=1 -run '^TestLive' ./...`) on their own
  machine. The build run never runs `make live`, and its result passes or
  fails nothing.
- Minimal: a live test proves lightly that the whole tool is glued together
  and works end to end, about one per external program, never an exhaustive
  suite. Behavior, edge cases, and error paths are the unit tests' job.
- No requirement ids: since no gate runs them, a live test proves no
  requirement. Every requirement is proved by a unit test the gates run, and
  a live test carries no requirement id.
- Self-cleaning: a live test brings up its sandbox under a temporary
  directory and a name of its own, and takes it down and wipes it before it
  finishes, pass or fail.

## Gates

Run from this directory (`sandbox/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 go build -o /dev/null ./cmd/sandbox` — proves the binary
   builds without cgo
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form; since the cache is private, `--allow-parallel-runners` skips
   golangci-lint's machine-wide lock so gates for several sub-projects can lint
   at once)

Gate 5's linters include `govet` (the v2 standard set), so there is no
separate `go vet` gate, and its `formatters` (`gofmt`, `goimports`) repeat
gate 1 harmlessly.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Install

sandbox has no release machinery: it is built and installed from the
checkout. `make install` runs `go install ./cmd/sandbox`; `make build` writes
`bin/sandbox`. The version the binary reports is a source variable the design
names, bumped by hand outside the spec system.
