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

The live box for this project is `ikigenba.dev` (ssh alias `dev`, root over
ssh). Any real-world verification — a live DNS round-trip, checking installed
prerequisites — runs there.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- `llm-lint` on PATH, with its provider API key present in the environment
- Linux amd64 gate environment, Bash 5.2+, and bubblewrap (`bwrap`) 0.11+
  with working unprivileged user, mount, PID, and network namespaces. These
  support installer subprocess tests inside the Go suite; they are not Go
  module dependencies or additional installed-host prerequisites. An absent
  tool or unavailable namespace is an environment failure, never a skipped
  test.

Production host tools such as nginx, certbot, systemctl, Litestream, and
archive utilities are observed on `dev`, not invoked against the gate host.
Tests use the injected D01 boundaries and controlled process fixtures.
Recorded observations and their limits live in
`specs/review/environment-observations.md`; mere tool availability is not
proof of its protocol or of a successful platform operation.

## Dependencies

Every direct dependency is approved by a human, and the approval is recorded
in the design: D1 names the exact set of direct requirements (the AWS SDK v2
core, `config`, and `service/route53` modules), and a gate test compares the
module paths in `go.mod` against it. Which release of each satisfies them is
data, and it lives in `go.mod`. Transitive modules are whatever
`go mod tidy` resolves for that set. The run never adds a direct module; a
phase that appears to need one files an issue for a human to adjudicate.

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

D15's standalone Bash installer does not use `cli.Deps`. Its behavior tests
also live in `cmd/` or `internal/` as `*_test.go` and execute the unmodified
installer in a bubblewrap sandbox. The harness supplies a fresh filesystem
view: writable deployment paths (`/usr/local`, `/etc`, `/opt`) and scratch
paths belong solely to temporary fixtures; host tool executables and their
runtime libraries may be mounted read-only. No live deployment tree,
credentials, home directory, or host communication socket is exposed. A
separate network namespace excludes external networking. Fixtures supply
release downloads and candidate responses, while namespace effective uid
is explicitly 0 or nonzero for the case under test, independent of the
parent's uid. Root ownership assertions refer to uid 0 inside that namespace.

A PATH wrapper alone is insufficient: shell redirects and absolute command
paths bypass it, and Bash's builtin `EUID` cannot be overridden through the
environment. Isolation must cover the shell and every descendant, including
candidate execution. The harness must establish that boundary before
executing installer code and fail if it cannot. There is no test-only
installer option or public root override. Test assets and harness support
files carry no requirement tags; the Go tests remain the sole test-id set.
Release artifact inspection and publication fixtures run locally in that
suite; gate tests never publish a release or push a tag. See
`specs/review/ground-usage.md` for the consumer exercise and capability probes.

## Gates

Run from this directory (`opsctl/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `golangci-lint run`
5. `llm-lint cmd internal`

llm-lint also loads this project's own rules from `lint-rules/` (wired via
`.llm-lint.json`, found by ancestor walk). Rules are promoted individually:
a promotion flips the rule file to `severity: error` and adds its id to the
`enable` allowlist in `.llm-lint.json`. Un-promoted rules stay disabled — they
make no LLM calls and print nothing — so every finding the gate reports fails
it. The un-promoted backlog is the `severity: warning` files in `lint-rules/`;
see `../docs/llm-lint-rule-candidates.md` for their provenance.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY

Co-Authored-By: Claude <noreply@anthropic.com>
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.
