# devctl

devctl is the developer's CLI; it creates and destroys spaces on the substrate. It is a Go binary built and run on the developer's own machine, as an ordinary user under the developer's own AWS identity; it never runs as root and holds no host-side secrets. The platform is one root domain in one AWS account, and devctl has no configuration of its own: it takes the root and its region from `infra/terraform.tfvars.json` at the top of the checkout it runs inside, the same file Terraform reads. It manages the platform's spaces, each complete on one Linux host, through AWS APIs and, over ssh, through `opsctl` installed on the host, reached only by its published interface. The module path is `github.com/ikigenba/ikigenba/devctl`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `cmd/devctl` is the binary. `internal/` is everything else, one package per concern.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `.goreleaser.yaml`, `install.sh` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- The modules `go.mod` requires (the AWS SDK v2 modules and `github.com/BurntSushi/toml`), in the module cache; `go.sum` is committed and the gates run offline. Prefer the standard library, then a widely used public module; adding one needs human approval.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

The gates fake every external process, so they need nothing beyond Go and `golangci-lint`. Running the built `devctl` also needs on `PATH`: `git` (finds the checkout with `git rev-parse`), `ssh` (reaches a space's host as `ec2-user`), `tar` with `xz` support (`build` writes and `deploy` reads `.tar.xz` archives with `tar -J`), `secret-tool` (libsecret, the developer's keyring) and `curl` (fetches opsctl's published releases).

## Operator setup

What the operator, human or agent, supplies when running the built `devctl` against the real platform. What devctl does with the root file, the profile and the operands is design's business.

- **Run from inside the checkout.** Every command that touches AWS or a host runs from a directory inside this repository's checkout; its `infra/terraform.tfvars.json` is the one statement of the root domain and region.
- **One AWS profile, named after the root.** `~/.aws/config` holds a profile named exactly as the root file spells the root domain (`ikigenba.dev`), with a live SSO session before any cloud command (`aws sso login --profile ikigenba.dev`). No account id is configured anywhere; the account is whatever that profile reaches.
- **ACME email.** `space create --acme-email`, and `space init --acme-email` when changing it, take `ops@ikigenba.dev`, the address the stories use. It must be one the CA will accept.
- **ssh.** The developer's ssh configuration reaches a space's instance as `ec2-user` with the platform's key pair, which is named after the root.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

devctl neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**No network and no real identity in the gates.** Tests never make a network call, never load the AWS SDK's default credential chain, and never read `~/.aws`, the developer's home, environment, keyring, ssh configuration or checkout: in particular never the real `infra/terraform.tfvars.json`, and never the real git checkout. The working directory, the effective uid, the environment, the clock, every cloud client and every process runner come in through the run seam design D01 defines (`seam.Deps`), and tests inject fakes. A test that needs a checkout builds a temporary one, with its own root file, under a temporary directory and passes a directory inside it as the working directory. A test that reaches a real AWS endpoint, or whose result depends on the developer's credentials, checkout or machine, is a bug. The gates run offline as an ordinary user.

## Live tests

Live tests are the only tests that connect to external services; every other test follows Test discipline.

- Minimal: a live test proves lightly that the whole is glued together end to end, about one per external service. Behavior, edge cases and error paths are the unit tests' job.
- Separate: `*_live_test.go` files behind `//go:build live`, with `TestLive*` functions. `go test ./...` never runs them; `make live` runs `go test -tags live -count=1 -run '^TestLive' ./...`.
- Designed: a live test carries the requirement id it proves, and the gap counts it like any other test.
- Local: the code under test runs on the developer's machine; only the external service is real.
- Credentials: read from the environment. A missing credential fails the test, never skips it. No credential appears in the repo or in test output.
- Run: only as the conditional `make live` gate below.

## Gates

Run from `devctl/`, in order; every command must exit 0. No skipped tests and no disabled linters.

1. `test -z "$(gofmt -l .)"` (`go fmt` always exits 0, so the check form is the gate; fix with `make fmt`)
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

`make build` (the default) builds `bin/devctl`; `make install` runs `go install ./cmd/devctl`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the version bump, tags, `Makefile`, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-devctl.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version in `internal/cli/run.go` to `vX.Y.Z`. It is a source literal the binary reports verbatim, and the deploy refuses a tag that does not match what the built binary's `--version` prints.
2. Commit on `main` and push `main`.
3. Tag the commit `devctl/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux/darwin amd64/arm64 archives and checksums; a prerelease tag (`devctl/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Wait for the release (`gh release view devctl/vX.Y.Z` succeeds), then install it on the developer's machine from this directory. A release is not done until this step is:

   ```
   DEVCTL_VERSION=vX.Y.Z sh install.sh
   ```

   The installer puts the binary in `~/.local/bin`. Plain `sh install.sh` installs the newest stable release.

`devctl --version` then prints `vX.Y.Z`.
