# opsctl

The operator CLI for the Ikigenba platform: a Go binary installed to
`/usr/local/bin` on a Linux host that runs one complete deployment of the
platform, and run there (typically over ssh) by humans and agents to
bootstrap and manage that deployment. A project runs many such hosts over
time, each created and torn down independently; `opsctl` reasons only about
the one it runs on. Module path `github.com/ikigenba/ikigenba/opsctl`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run brings the existing `cmd/`, `internal/`, and `go.mod` into
agreement with that target. See the `spec` and `build-spec` skills and `docs/spec-system.md` at the
repo root. Everything below is the ground the run computes the gap and runs
the gates against; it is human-authored and read-only to the run.

## Host

The live box for this project is the host answering at `ikigenba.dev`. The
ssh alias `dev` reaches it by that name, so it follows the DNS record when the
instance is replaced; the login is `ec2-user`, and `sudo` needs no password.
Any real-world verification — a live DNS round-trip, checking installed
prerequisites — runs there.

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

## Out of scope

The spec owns local development. The design covers the Go code under `cmd/`
and `internal/` and its `go.mod`; the `Makefile`, the lint configuration, and
the gates are the ground that builds and tests it here. Release publication
and installation of the `opsctl` binary onto a host are maintained by hand
and are not part of the design or the gap. `install.sh` and `.goreleaser.yaml`
in this directory, and the release workflow at
`.github/workflows/release-opsctl.yml` under the repository root, are
hand-maintained files: the build run never reads, edits, tests, or deletes
them, and no requirement describes them. Gate tests never publish a release
or push a tag.

## Test files

The project's tests are all `*_test.go` files under `cmd/` and `internal/`.
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
4. `golangci-lint run`

`llm-lint` is available as an optional manual check through `make llm-lint`,
but it is not a quality gate. Its configuration and project rules remain in
`.llm-lint.json` and `lint-rules/`. The rule-candidate backlog and provenance
are documented in `../docs/llm-lint-rule-candidates.md`.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.
