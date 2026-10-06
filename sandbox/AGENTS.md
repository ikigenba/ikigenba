# sandbox

sandbox stands up the whole suite from the current worktree on the developer's machine. It is a Go binary run as the developer, with no root, that discovers the checkout's apps, builds each with `go build`, and runs every one as a pair of user-level systemd units (a `.socket` holding the app's Unix socket and a `Type=notify` `.service` that inherits it) behind one nginx per sandbox, itself a user unit on `127.0.0.1`. It renders its own units, nginx configuration, env files and services file, writes nothing to system directories, and keeps each sandbox's data under the developer's own directories, so many worktrees run sandboxes at once. It uses neither `devctl` nor `opsctl` and is never deployed to a host. The module path is `github.com/ikigenba/ikigenba/sandbox`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `cmd/sandbox` is the binary. `internal/` is everything else, one package per concern.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Linux.
- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The binary itself is cgo-free (gate 3).
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.
- The modules `go.mod` requires, in the module cache: `github.com/BurntSushi/toml`, for reading app manifests; the D05 tests need its `toml.ParseError` for their expected diagnostics. `go.sum` is committed and the gates run offline.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

The gates fake every external program, so they need no systemd user manager, nginx or network. Running the built `sandbox`, and `make live`, also needs `git` (finds the worktree with `git rev-parse --show-toplevel`), `go` (builds each app), a running systemd user manager (`systemctl --user`, `journalctl --user`), and `nginx` runnable by an ordinary user and installed where systemd's own executable search path finds a bare name (the standard `bin` and `sbin` directories): the nginx unit's `ExecStart=` names the bare word `nginx`, which the user manager resolves, not the developer's `PATH`.

The `scripts` app also needs `python3.12`: it checks for it at start and refuses to start without it (`scripts: python3.12 not found on PATH`). Its unit inherits the systemd user manager's environment, so `python3.12` must be on the user manager's `PATH` (`systemctl --user show-environment` shows it), not only the developer's shell's. Install it with `uv python install 3.12`, which puts it in `~/.local/bin`; that directory must be on the user manager's `PATH`. The gates do not need it.

## Test files

The test files are every `*_test.go` in the module: `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` holds no `*_test.go`.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, and sandbox echoes names, paths, manifest values, secret keys and tokens back unchanged in shape, so no test argument, expected output, fixture name or fixture content carries one.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No real machine in the gates.** The external programs sandbox drives (`git`, `go`, `systemctl`, `journalctl`, `nginx`) are reached only through the run seam design defines, and tests inject fakes that record what they were asked and answer as the case needs. No test starts a real systemd unit, an nginx, a `go build` of an app, or a `git` command against the developer's checkout. The working directory, the environment (`HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME` and the rest), the clock, the output streams and every process runner come in through that seam. A test that needs a checkout, a sandbox's data or the registry builds them under its own temporary directory and points the seam there; it never reads or writes the developer's real `~/.config`, `~/.local/state` or systemd user directory. A Unix socket a test binds lives in a short temporary directory (`os.MkdirTemp`), since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it.

**No test reads the checkout.** A test proves what design declares by using it (importing, calling, constructing, driving the command), never by opening this module's source, layout, `go.mod` or `go.sum`. The gates run offline as an ordinary user, with no systemd user manager and no nginx.

## Live tests

A live test is the only kind that touches the real machine: the real systemd user manager, a real nginx, real `git` and `go` against a real worktree. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs; a developer runs them on their own machine with `make live` (`go test -tags live -count=1 -run '^TestLive' ./...`). The build run never runs `make live`, and its result passes or fails nothing, so a live test proves no requirement and carries no id; every requirement is proved by a unit test the gates run. It proves lightly that the whole tool is glued together, about one per external program, leaving behavior, edge cases and error paths to the unit tests. It brings up its sandbox under a temporary directory and a name of its own, and takes it down and wipes it before it finishes, pass or fail.

## Gates

Run from `sandbox/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 go build -o /dev/null ./cmd/sandbox`, proving the binary builds without cgo
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)

Gate 5's linters include `govet`, so there is no separate `go vet` gate, and its formatters repeat gate 1 harmlessly.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Install

sandbox has no release machinery: it is built and installed from the checkout. `make install` runs `go install ./cmd/sandbox`; `make build` writes `bin/sandbox`. The version the binary reports is a source variable design names, bumped by hand outside the spec system.
