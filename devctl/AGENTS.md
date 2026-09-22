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
`build-spec` skills and `docs/spec-system.md` at the repo root. Everything
below is what the build run computes the gap and runs the gates against; it
is human-authored and read-only to the run.

## Deploy

devctl installs on the developer's own machine, as an ordinary user; there is
no host and no root step.

1. Set `version` in `internal/cli/run.go` to `vX.Y.Z`. The binary reports that
   string, and the release refuses a tag that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `devctl/vX.Y.Z` and push the tag.
   `.github/workflows/release-devctl.yml` builds with GoReleaser and publishes
   `devctl_<os>_<arch>.tar.gz` (linux and darwin, amd64 and arm64) and
   `checksums.txt`.
4. Install it locally, either way:
   - from a checkout at that tag, `make install` (`go install ./cmd/devctl`);
   - or download that release's archive for your platform and put the `devctl`
     binary on your `PATH`.

`devctl version` then prints `vX.Y.Z`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory, `version: "2"`)

## Dependencies

Every direct dependency is approved by a human, and the approval is recorded
in the design: D01 names the exact set of direct requirements with pinned
versions, `go.mod` carries exactly that set, and a gate test compares the two.
Transitive modules are whatever `go mod tidy` resolves for that set. The run
never adds a direct module; a phase that appears to need one files an issue
for a human to adjudicate.

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** devctl neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

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

## Gates

Run from this directory (`devctl/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `golangci-lint run`

llm-lint is **disabled for devctl for now**: it is not a gate and not part of
the toolchain, so the run neither needs it on PATH nor a provider API key.
The `make llm-lint` target, the rule files under `lint-rules/`, and
`.llm-lint.json` are kept so it can be re-enabled later by adding
`llm-lint cmd internal` back to this gate list and `llm-lint` back to the
toolchain. See `../docs/llm-lint-rule-candidates.md` for the rules' provenance.

## Operating defaults

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

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.
