# devctl

The developer's CLI for the Ikigenba platform: a Go binary built and run on
the developer's own machine, as an ordinary user, under the developer's own
AWS identity. The platform is one root domain in one AWS account; devctl has
no configuration of its own and takes the root and its region from
`infra/terraform.tfvars.json` at the top of the checkout it is run inside,
the same file Terraform reads. It creates and manages the platform's spaces,
each one complete on one Linux host, and the hosts they run on. It talks to
AWS APIs and, over ssh, to those hosts. It never runs as root and holds no
host-side secrets. `opsctl`, the sibling sub-project, is the host-side
counterpart that runs as root on a host; devctl reaches it only as an
installed tool through its published interface. Module path
`github.com/ikigenba/ikigenba/devctl`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code under `cmd/` and `internal/`. See the `spec` and
`build-spec` skills. Everything
below is what the build run computes the gap and runs the gates against; it
is human-authored and read-only to the run.

## Operator setup

Conventions for running the built `devctl` against the real platform, by a
human or an agent. They are not devctl behaviour: they say what the operator
supplies. What devctl itself does with the root file, the profile, and the
operands is the stories' and design's business and is not restated here.

- **Run from inside the checkout.** Every command that touches AWS or a host
  is run from a directory inside this repository's checkout; the checkout's
  `infra/terraform.tfvars.json` is the only statement of the root domain and
  region.

- **One AWS profile, named after the root.** `~/.aws/config` holds a profile
  whose name is the root domain exactly as the root file spells it
  (`ikigenba.dev`), and that profile has a live SSO session before any cloud
  command is run (`aws sso login --profile ikigenba.dev`). No account id is
  configured anywhere; the account is whatever that profile reaches.

- **ACME email.** The address given to `space create --acme-email` and, when
  changing it, `space init --acme-email` is `ops@ikigenba.dev`, the address the
  stories use. It must be one the CA will accept.

- **ssh.** The developer's ssh configuration reaches a space's instance as
  `ec2-user` with the platform's key pair, which is named after the root.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory, `version: "2"`)
- GNU `make`: the developer targets in the `Makefile` (`make install`,
  `make fmt`); no gate runs through it

The gates fake every external process, so they need nothing beyond Go and
`golangci-lint`. Running the built `devctl` needs these on `PATH` as well:

- `git` (finds the checkout: `git rev-parse`)
- `ssh` (reaches a space's host as `ec2-user`)
- `tar` with `xz` support (`build` writes and `deploy` reads `.tar.xz` archives, `tar -J`)
- `secret-tool` (libsecret; reads the developer's keyring)
- `curl` (fetches opsctl's published releases)

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** devctl neither mints nor emits
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

**No network and no real identity in the gates.** Tests never make a network
call, never load the AWS SDK's default credential chain, and never read
`~/.aws`, the developer's home directory, the developer's environment, the
developer's keyring or ssh configuration, or the developer's own checkout:
in particular they never read the real `infra/terraform.tfvars.json` or
discover the real git checkout. The working directory, the effective uid,
the environment, the clock, every cloud client, and every process runner come
in through the run seam that design D01 defines (`seam.Deps`), and tests
inject fakes. A test that needs a checkout builds a temporary one under a
temporary directory, with its own root file, and passes a directory inside it
as the working directory. A test that reaches a real AWS
endpoint, or whose result depends on the developer's credentials, checkout,
or machine, is a bug. The gates run offline as an ordinary user.

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

## Gates

Run from this directory (`devctl/`), in order; every command must exit 0. No
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

Deploy machinery — the version bump, tags, the `Makefile`, `install.sh`,
`.goreleaser.yaml`, and `.github/workflows/release-devctl.yml` (repo root) —
is hand-maintained infrastructure outside the spec system: the build run never
reads, edits, or tests it.

1. Set the version in `internal/cli/run.go` (D02) to `vX.Y.Z`. It is a source
   literal the binary reports verbatim, and the deploy refuses a tag that does
   not match what the built binary's `--version` prints.
2. Commit that on `main` and push `main`.
3. Tag that commit `devctl/vX.Y.Z` and push the tag.
   `.github/workflows/release-devctl.yml` builds with GoReleaser and publishes
   linux/darwin amd64/arm64 archives and checksums. A tag with a prerelease
   part (`devctl/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Wait for the workflow to publish the release (`gh release view
   devctl/vX.Y.Z` succeeds), then install it on the developer's machine from
   this directory. A release is not done until this step is:

   ```
   DEVCTL_VERSION=vX.Y.Z sh install.sh
   ```

   The installer puts the binary in `~/.local/bin`. Plain `sh install.sh`
   installs the newest stable release.

`devctl --version` then prints `vX.Y.Z`.
