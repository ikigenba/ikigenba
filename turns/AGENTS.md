# turns

turns is the session pusher: it reads one person's Claude Code, Codex CLI and Grok CLI session files and pushes content-free turns to the `sessions` service through the space's gateway. It is a Go binary run on a laptop or a host, as an ordinary user under that person's own account, by hand and from that account's user timer; it never runs as root. It is a command-line program with stories. The module path is `github.com/ikigenba/ikigenba/turns`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `cmd/turns` is the binary. `internal/` is everything else, one package per concern.
- `share/systemd/` holds `turns.service` and `turns.timer`, the host form of the user units; the binary embeds them.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `.goreleaser.yaml`, `install.sh`, `share/` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- The modules `go.mod` requires, in the module cache. `go.sum` is committed and the gates run offline.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

The gates fake every external process. Running the built `turns` also needs on `PATH`: `systemctl` (`install` reloads the user manager and enables the timer).

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped literal in a fixture.** turns neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the person's home, config or real state.

**No network, no home directory and no real session files in the gates.** Tests never make a network call and never read the person's home, environment, configuration, token, state or real session files under `~/.claude`, `~/.codex`, `~/.grok` or `~/.local/state/dispatch`, nor `/etc/xdg/turns/` or `/etc/systemd/user/`. Everything external comes in through the run seam the design defines: the effective uid, the environment, the home and the system directories, the clock, the host name, the HTTP client and every process runner, and tests inject fakes. A test that needs session files builds them in a temporary home and points the injected environment at it. At most one test execs the built binary; every other test drives the run seam in process. A test that reaches a real space, or whose result depends on the person's home, sessions or machine, is a bug. The gates run offline as an ordinary user.

## Live tests

A live test is the only kind that reaches an external service. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together, about one per external service, leaving behavior, edge cases and error paths to the unit tests; it carries the id it proves and the gap counts it; the code under test runs locally and only the external service is real; it reads credentials from the environment, fails rather than skips when one is missing, and no credential appears in the repository or in test output. It runs only as gate 5. turns has none yet.

## Gates

Run from `turns/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

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

`make build` (the default) builds `bin/turns`; `make install` runs `go install ./cmd/turns`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the version bump, tags, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-turns.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version in `internal/cli/run.go` to `vX.Y.Z`; the first release is `v0.1.0`. The binary reports it verbatim, and the release refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `turns/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux and darwin amd64 and arm64 archives and checksums; a prerelease tag (`turns/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Once `gh release view turns/vX.Y.Z` succeeds, install it from this directory with `TURNS_VERSION=vX.Y.Z sh install.sh`, which puts the binary where `make install` does (`$GOBIN`, else `$GOPATH/bin`). A release is not done until this step is. Plain `sh install.sh` installs the newest stable release.

`turns --version` then prints `vX.Y.Z`.

The suite release also carries turns, at `turns/bin/turns` beside `turns/share/`: `devctl build` builds it from the same commit and opsctl installs it on a host, linking `/usr/local/bin/turns` and copying the units into `/etc/systemd/user/`. `share/systemd/` holds the host form of the user units, read-only to the build run and embedded by the binary, so a change to either unit is an edit there alone.
