# opsctl

The operator CLI for the Ikigenba platform: a Go binary installed to
`/usr/local/bin` on the single Linux host that runs the core platform
services, and run there (typically over ssh) by humans and agents to
bootstrap and manage the platform itself. Module path
`github.com/ikigenba/ikigenba/opsctl`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (`cmd/`, `internal/`, `go.mod` are absent until it
does). See the `spec` and `build-spec` skills and `docs/spec-system.md` at the
repo root. Everything below is the ground the run computes the gap and runs
the gates against; it is human-authored and read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- `llm-lint` on PATH, with its provider API key present in the environment

## Dependencies

Every direct dependency is approved by a human, and the approval is recorded
in the design: D1 names the exact set of direct requirements (currently none —
standard library only), and a gate test compares `go.mod` against it. The run
never adds a module; a phase that appears to need one files an issue for a
human to adjudicate.

## Test files

The project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** opsctl neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

**No root and no host paths in the gates.** Every host path is resolved under
`cli.Deps.Root` (design D1), and the root check reads `cli.Deps.EUID`. Tests
pass a temporary directory as `Root` and `0` as `EUID`; a test that touches
`/etc`, `/opt`, or `/etc/systemd`, or that depends on the real effective uid,
is a bug. The gates run as an ordinary user.

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
