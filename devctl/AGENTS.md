# devctl

devctl is the developer's CLI; it creates and destroys spaces on the substrate. It is a Go binary built and run on the developer's own machine, as an ordinary user under the developer's own AWS identity; it never runs as root and holds no host-side secrets. It has no configuration of its own: the root domain and region come from `infra/terraform.tfvars.json` at the top of the checkout it runs inside, the same file Terraform reads. It manages spaces, each complete on one Linux host, through AWS APIs and, over ssh, through the `opsctl` installed on the host, reached only by its published interface. The module path is `github.com/ikigenba/ikigenba/devctl`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `cmd/devctl` is the binary. `internal/` is everything else, one package per concern.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `.goreleaser.yaml`, `install.sh` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- The modules `go.mod` requires, in the module cache: the AWS SDK v2 modules and `github.com/BurntSushi/toml`. `go.sum` is committed and the gates run offline.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

The gates fake every external process. Running the built `devctl` also needs on `PATH`: `git` (finds the checkout), `ssh` and `scp` (reach a space's host as `ec2-user`; `deploy` copies a release with `scp`), `tar` with `xz` support (`build` writes and `deploy` reads `.tar.xz` archives with `tar -J`), and `secret-tool` (libsecret, the developer's keyring).

## Operator setup

What the operator, human or agent, supplies when running the built `devctl` against the real platform. What devctl does with it is design's business.

- **Run from inside the checkout.** Every command that touches AWS or a host runs from a directory inside this repository's checkout; its `infra/terraform.tfvars.json` is the one statement of the root domain and region.
- **One AWS profile, named after the root.** `~/.aws/config` holds a profile named exactly as the root file spells the root domain (`ikigenba.dev`), with a live SSO session before any cloud command (`aws sso login --profile ikigenba.dev`). No account id is configured anywhere; the account is whatever that profile reaches.
- **ACME email.** The address `space create --acme-email`, and `space init --acme-email` when changing it, take is a question for the user. Ask every time; never take one from the specs or tests, or reuse one from an earlier space.
- **ssh.** The developer's ssh configuration reaches a space's instance as `ec2-user` with the platform's key pair, which is named after the root.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped literal in a fixture.** devctl neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No network and no real identity in the gates.** Tests never make a network call, never load the AWS SDK's default credential chain, and never read `~/.aws`, the developer's home, environment, keyring, ssh configuration or checkout, the real `infra/terraform.tfvars.json` and git checkout included. The working directory, the effective uid, the environment, the clock, every cloud client and every process runner come in through the run seam D01 defines (`seam.Deps`), and tests inject fakes. A test that needs a checkout builds a temporary one, with its own root file, and passes a directory inside it as the working directory. A test that reaches a real AWS endpoint, or whose result depends on the developer's credentials, checkout or machine, is a bug. The gates run offline as an ordinary user.

## Live tests

A live test is the only kind that reaches an external service. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together, about one per external service, leaving behavior, edge cases and error paths to the unit tests; it carries the id it proves and the gap counts it; the code under test runs locally and only the external service is real; it reads credentials from the environment, fails rather than skips when one is missing, and no credential appears in the repository or in test output. It runs only as gate 5.

## Gates

Run from `devctl/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

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

`make build` (the default) builds `bin/devctl`; `make install` runs `go install ./cmd/devctl`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the version bump, tags, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-devctl.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version in `internal/cli/run.go` to `vX.Y.Z`. The binary reports it verbatim, and the release refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `devctl/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux and darwin amd64 and arm64 archives and checksums; a prerelease tag (`devctl/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Once `gh release view devctl/vX.Y.Z` succeeds, install it from this directory with `DEVCTL_VERSION=vX.Y.Z sh install.sh`, which puts the binary in `~/.local/bin`. A release is not done until this step is. Plain `sh install.sh` installs the newest stable release.

`devctl --version` then prints `vX.Y.Z`.
