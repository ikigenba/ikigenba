# opsctl

The operator CLI for the Ikigenba platform: a Go binary installed to
`/usr/local/bin` on a Linux host that runs one complete deployment of the
platform, and run there (typically over ssh) by humans and agents to
bootstrap and manage that deployment. A project runs many such hosts over
time, each created and torn down independently; `opsctl` reasons only about
the one it runs on. Module path `github.com/ikigenba/ikigenba/opsctl`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run brings the existing `cmd/`, `internal/`, and `go.mod` into agreement
with that target. See the `spec` and `build-spec` skills. Everything below is
what the build run computes the gap and runs the gates against; it is
human-authored and read-only to the run.

## Host

opsctl runs on a space's host, as root. It is not designed to run on the
developer's machine, and there is no permanent test host.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

Production host tools such as nginx, certbot, systemctl, Litestream, and
archive utilities are observed on `dev`, not invoked against the gate host.
Tests use the injected D01 boundaries and controlled process fixtures.
Mere tool availability is not
proof of its protocol or of a successful platform operation.

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** opsctl neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

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

**No root and no deployment-host paths in the gates.** Commands invoked
through `cli.Run` resolve host paths under `cli.Deps.Root` (design D01), and
the root check reads `cli.Deps.EUID`. Domain operations preserve the root
boundary, including paths sent through `host.Env.Execute`. Tests supply a
temporary root, explicit effective uid, process and cloud fixtures, DNS
fixtures, and deterministic time. They never depend on the gate process's
real effective uid or call real cloud, DNS, service, or certificate systems.
The gates run as an ordinary user.

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

opsctl has none yet: its external system is the host itself, and there is no
throwaway space to run against.

## Gates

Run from this directory (`opsctl/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form)
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

Deploy machinery — the version bump, tags, `install.sh`, `.goreleaser.yaml`,
and `.github/workflows/release-opsctl.yml` (repo root) — is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it.

1. Set the version in `internal/cli/cli.go` (D02) to `vX.Y.Z`. It is a
   source literal the binary reports verbatim, and the deploy refuses a tag
   that does not match what the built binary's `--version` prints.
2. Commit that on `main` and push `main`.
3. Tag that commit `opsctl/vX.Y.Z` and push the tag.
   `.github/workflows/release-opsctl.yml` builds with GoReleaser and publishes
   `opsctl-vX.Y.Z-linux-amd64`, `checksums.txt`, and `install.sh`. A tag with
   a prerelease part (`opsctl/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. On the host, as root, run that release's installer with the same version:

```
curl -fsSL -o /tmp/opsctl-install.sh https://github.com/ikigenba/ikigenba/releases/download/opsctl/vX.Y.Z/install.sh
sudo bash /tmp/opsctl-install.sh vX.Y.Z
```

`opsctl version` then prints `vX.Y.Z`.
