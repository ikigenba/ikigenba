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

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

Production host tools such as nginx, certbot, systemctl, Litestream, and
archive utilities are observed on `dev`, not invoked against the gate host.
Tests use the injected D01 boundaries and controlled process fixtures.
Mere tool availability is not
proof of its protocol or of a successful platform operation.

## Dependencies

Every direct dependency is approved by a human, and the approval is recorded
in the design: D1 names the exact set of direct requirements (the AWS SDK v2
core, `config`, `service/route53`, `service/s3`, and `service/ssm` modules), and a gate test compares the
module paths in `go.mod` against it. Which release of each satisfies them is
data, and it lives in `go.mod`. Transitive modules are whatever
`go mod tidy` resolves for that set. The run never adds a direct module; a
phase that appears to need one files an issue for a human to adjudicate.

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** opsctl neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

**No root and no deployment-host paths in the gates.** Commands invoked
through `cli.Run` resolve host paths under `cli.Deps.Root` (design D01), and
the root check reads `cli.Deps.EUID`. Domain operations preserve the root
boundary, including paths sent through `host.Env.Execute`. Tests supply a
temporary root, explicit effective uid, process and cloud fixtures, DNS
fixtures, and deterministic time. They never depend on the gate process's
real effective uid or call real cloud, DNS, service, or certificate systems.
The gates run as an ordinary user.

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

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Deploy

Release machinery — the version bump, tags, `install.sh`, `.goreleaser.yaml`,
and `.github/workflows/release-opsctl.yml` (repo root) — is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it.

1. Set the version in `internal/cli/cli.go` (D02) to `vX.Y.Z`. The binary
   reports that string, and the release refuses a tag that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `opsctl/vX.Y.Z` and push the tag.
   `.github/workflows/release-opsctl.yml` builds with GoReleaser and publishes
   `opsctl-vX.Y.Z-linux-amd64`, `checksums.txt`, and `install.sh`.
4. On the host, as root, run that release's installer with the same version:

```
curl -fsSL -o /tmp/opsctl-install.sh https://github.com/ikigenba/ikigenba/releases/download/opsctl/vX.Y.Z/install.sh
sudo bash /tmp/opsctl-install.sh vX.Y.Z
```

`opsctl version` then prints `vX.Y.Z`.

## Host

The live box for this project is the host answering at `ikigenba.dev`. The
ssh alias `dev` reaches it by that name, so it follows the DNS record when the
instance is replaced; the login is `ec2-user`, and `sudo` needs no password.
Any real-world verification — a live DNS round-trip, checking installed
prerequisites — runs there.
