# opsctl

opsctl is the operator's CLI on a host; it bootstraps and manages that one deployment. It is a Go binary that ships inside the suite release unpacked on a Linux host, run from `/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl`, with `/usr/local/bin/opsctl` a link to `/opt/ikigenba/current/opsctl/bin/opsctl` that every activate re-creates. The host runs one complete deployment of the platform, and opsctl runs there as root, typically over ssh, by humans and agents. A project runs many such hosts over time, each created and torn down independently, and opsctl reasons only about the one it runs on. It is a command-line program with stories. The module path is `github.com/ikigenba/ikigenba/opsctl`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `cmd/opsctl` is the binary. `internal/` is everything else, one package per concern.
- `bootstrap.md` tells an agent how to bring a fresh host to the point where a release can be unpacked on it; `setup.md` picks up from there: with the release unpacked, it sets the config keys, runs `init`, and activates the release, all by the absolute path of the release's own opsctl.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, the two documents above and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- The modules `go.mod` requires, in the module cache: `github.com/aws/aws-sdk-go-v2` with its `config`, `service/route53`, `service/s3` and `service/ssm` modules. `go.sum` is committed and the gates run offline.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

Host tools such as nginx, certbot, systemctl, Litestream and archive utilities are observed on a host, never invoked on the gate machine. Tests go through the injected D01 boundaries and process fixtures; a tool's presence proves neither its protocol nor a successful operation.

## Host

opsctl runs on a space's host, as root, from the release it belongs to: `/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl`. `/usr/local/bin/opsctl` is a link to `/opt/ikigenba/current/opsctl/bin/opsctl`, re-created on every activate, so plain `opsctl` is always the running release's. It is not designed to run on the developer's machine, and there is no permanent test host.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped literal in a fixture.** opsctl neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No root and no host paths in the gates.** Commands invoked through `cli.Run` resolve host paths under `cli.Deps.Root` (D01), and the root check reads `cli.Deps.EUID`. Domain operations keep that boundary, paths sent through `host.Env.Execute` included. Tests supply a temporary root, an explicit effective uid, process, cloud and DNS fixtures, and deterministic time; they never depend on the gate process's real effective uid or call real cloud, DNS, service or certificate systems. The gates run as an ordinary user.

## Live tests

A live test is the only kind that reaches an external service. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together, about one per external service, leaving behavior, edge cases and error paths to the unit tests; it carries the id it proves and the gap counts it; the code under test runs locally and only the external service is real; it reads credentials from the environment, fails rather than skips when one is missing, and no credential appears in the repository or in test output. It runs only as gate 5. opsctl has none yet: its external system is the host itself, and there is no throwaway space to run against.

## Gates

Run from `opsctl/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

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

`make build` builds the binary from the checkout; `make install` runs `go install ./cmd/opsctl`; `make fmt` formats; `make test` and `make lint` run those gates. `make deploy` builds a linux/amd64 binary and installs it on `DEPLOY_HOST` as `DEPLOY_USER` over ssh, for trying a change on a host without a release; the file it puts at `/usr/local/bin/opsctl` lasts only until the next activate re-creates the link. The gates themselves call the Go tool directly.

## Releasing

opsctl has no version of its own and no release of its own: it ships inside the suite release, built with `devctl build <sha|tag>` and put on a host with `devctl deploy <space> <sha|tag>`, and `opsctl version` prints its release's display string (label and short sha, or the short sha). There is no version literal to set, and nothing is injected at build time. Building and unpacking a release are devctl's, and the build run neither reads nor tests that machinery.
