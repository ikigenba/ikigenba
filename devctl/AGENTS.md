# devctl

The developer's CLI for the Ikigenba platform: a Go binary built and run on
the developer's own machine, as an ordinary user, under the developer's own
AWS identity. It creates and manages the platform's deployments, each one
complete on one Linux host, and the hosts they run on. It talks to AWS APIs
and, over ssh, to those hosts. It never runs as root and holds no host-side
secrets. `opsctl`, the sibling sub-project, is the host-side counterpart that
runs as root on a host. Module path `github.com/ikigenba/ikigenba/devctl`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (`cmd/` and `internal/` are absent until it does;
`go.mod` exists but carries no requirements until D1 names them). See the
`spec` and `build-spec` skills and `docs/spec-system.md` at the repo root.
Everything below is the ground the run computes the gap and runs the gates
against; it is human-authored and read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

## Dependencies

Every direct dependency is approved by a human, and the approval is recorded
in the design: D1 names the exact set of direct requirements with pinned
versions, and a gate test compares `go.mod` against it. Transitive modules
are whatever `go mod tidy` resolves for that set. The run never adds a direct
module; a phase that appears to need one files an issue for a human to
adjudicate. Until D1 exists, `go.mod` has no requirements.

## Test files

The project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** devctl neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

**No network and no real identity in the gates.** Tests never make a network
call, never load the AWS SDK's default credential chain, and never read
`~/.aws`, the developer's home directory, or the developer's environment.
Every cloud client, the process environment, and the home directory come in
through the run seam that design D1 defines, and tests inject fakes. A test
that reaches a real AWS endpoint, or whose result depends on the developer's
credentials or machine, is a bug. The gates run offline as an ordinary user.

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

Conventions for running the built `devctl` against the real accounts, by a
human or an agent. They are not devctl behaviour: devctl requires the values
on the command line, and these say what to pass.

- **Account from domain.** A space is asked for by its domain, and the
  domain names the account: a space at or under `sbx.ikigenba.dev` is in the
  sandbox account, `--account ikigenba-sandbox` (602773793009); any other
  space at or under `ikigenba.dev` is in the prod account,
  `--account ikigenba-prod` (295229566359). The longer suffix wins, so
  `mg.sbx.ikigenba.dev` is sandbox and `staging.ikigenba.dev` is prod. Those
  are the profile names in `~/.aws/config`; devctl passes the name through
  to the AWS shared-config loader and derives nothing from it.

- **ACME email.** The address given to `space create --acme-email` and
  `space init --acme-email` is always `mgreenly+<account id>@gmail.com`,
  where `<account id>` is the id of the account the space lives in. For any
  `*.sbx.ikigenba.dev` space that is `mgreenly+602773793009@gmail.com`; for
  any space under `ikigenba.dev` itself it is `mgreenly+295229566359@gmail.com`.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.
