# opsctl

opsctl is the operator's CLI on a host; it bootstraps and manages that one deployment. It is a Go binary installed to `/usr/local/bin` on a Linux host that runs one complete deployment of the platform, run there as root, typically over ssh, by humans and agents. A project runs many such hosts over time, each created and torn down independently, and opsctl reasons only about the one it runs on. The module path is `github.com/ikigenba/ikigenba/opsctl`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `cmd/opsctl` is the binary. `internal/` is everything else, one package per concern.
- `bootstrap.md` tells an agent how to bring a fresh host to the point where opsctl can be installed; `setup.md` picks up from there and installs and configures it.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `install.sh`, `.goreleaser.yaml`, the two documents above and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- The modules `go.mod` requires, in the module cache: `github.com/aws/aws-sdk-go-v2` with its `config`, `service/route53`, `service/s3` and `service/ssm` modules. `go.sum` is committed and the gates run offline.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs human approval.

Production host tools such as nginx, certbot, systemctl, Litestream and archive utilities are observed on a host, never invoked against the gate machine. Tests go through the injected D01 boundaries and controlled process fixtures; a tool's presence proves neither its protocol nor a successful operation.

## Host

opsctl runs on a space's host, as root. It is not designed to run on the developer's machine, and there is no permanent test host.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** opsctl neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No root and no deployment-host paths in the gates.** Commands invoked through `cli.Run` resolve host paths under `cli.Deps.Root` (design D01), and the root check reads `cli.Deps.EUID`. Domain operations keep that boundary, paths sent through `host.Env.Execute` included. Tests supply a temporary root, an explicit effective uid, process, cloud and DNS fixtures, and deterministic time; they never depend on the gate process's real effective uid or call real cloud, DNS, service or certificate systems. The gates run as an ordinary user.

## Live tests

Live tests are the only tests that reach an external service; every other test follows Test discipline. A live test is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together end to end, about one per external service; behavior, edge cases and error paths are the unit tests' job. It carries the id it proves and the gap counts it like any other test. The code under test runs on the developer's machine; only the service is real. It reads credentials from the environment and fails rather than skips when one is missing; no credential appears in the repo or in test output. It runs only as the conditional gate below.

opsctl has none yet: its external system is the host itself, and there is no throwaway space to run against.

## Gates

Run from `opsctl/`, in order; every command must exit 0. No skipped tests and no disabled linters.

1. `test -z "$(gofmt -l .)"` (`go fmt` always exits 0, so this check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and stops applying `//nolint` and `.golangci.yml` suppressions), and with a private cache `--allow-parallel-runners` skips the machine-wide lock so several sub-projects can lint at once (`make lint` runs this form)
5. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` builds the binary from the checkout; `make install` runs `go install ./cmd/opsctl`; `make fmt` formats; `make test` and `make lint` run those gates. `make deploy` builds a linux/amd64 binary and installs it on `DEPLOY_HOST` as `DEPLOY_USER` over ssh, for trying a change on a host without a release. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the version bump, tags, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-opsctl.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version literal in `internal/cli/cli.go` to `vX.Y.Z`; the binary reports it verbatim and the deploy refuses a tag that does not match what `--version` prints.
2. Commit on `main` and push `main`.
3. Tag the commit `opsctl/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes `opsctl-vX.Y.Z-linux-amd64`, `checksums.txt` and `install.sh`; a tag with a prerelease part (`opsctl/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. On the host, as root, run that release's installer with the same version:

```
curl -fsSL -o /tmp/opsctl-install.sh https://github.com/ikigenba/ikigenba/releases/download/opsctl/vX.Y.Z/install.sh
sudo bash /tmp/opsctl-install.sh vX.Y.Z
```

`opsctl version` then prints `vX.Y.Z`.
