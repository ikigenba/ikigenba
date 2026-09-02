# toolkit

A Go library that gives consumers of `github.com/ikigenba/ikigenba/agentkit` a
standard set of local tools — `Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep` —
as ready-made `agentkit.Tool` values, each built against an explicit root
directory. Module path `github.com/ikigenba/ikigenba/toolkit`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build loop writes the code (the `toolkit` package and `go.mod`'s dependency
graph fill in as it does). See the `spec` skill and `docs/spec-system.md` at the
repo root. Everything below is declared for the loop's verify role, which reads
this file directly.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `bash` on PATH (the `Bash` tool shells out to it; its tests run real commands)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- `llm-lint` on PATH, with its provider API key present in the environment
- `idgen` on PATH (minting requirement ids for spec work)

## Test files

The project's tests are all `*_test.go` files under this module. This is the
file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

Test files MUST NOT contain any id-shaped literal that is not a genuine
requirement-id tag. Tests exercise the tools against temporary directories
created per test; no test reads or writes outside its own temporary root, and
no test depends on network access.

## Gates

Run from this directory (`toolkit/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `golangci-lint run`
5. `llm-lint --concurrency 16 --verbose .` (doubles the default in-flight calls of 8; `--verbose` prints per-pair progress)

llm-lint also loads this project's own rules from `lint-rules/` (wired via
`.llm-lint.json`, found by ancestor walk) and recurses the module from the root.
Rules are promoted individually: a promotion flips the rule file to
`severity: error` and adds its id to the `enable` allowlist in `.llm-lint.json`.
Un-promoted rules stay disabled — they make no LLM calls and print nothing — so
every finding the gate reports fails it.

A per-finding `llm-lint:ignore` directive (and likewise a `//nolint` comment for
golangci-lint) counts as a disabled linter. Never add one to make a gate pass.
A finding is fixed by applying the rule's recommendation below the contract
seam; if that cannot be done without changing an exported name, signature, or
observable behavior, or if the finding is wrong, file an issue under
`specs/issues/` so a human can adjudicate — restructure the code, sharpen the
rule in `lint-rules/`, or amend the design.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY

Co-Authored-By: Claude <noreply@anthropic.com>
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Releasing (infrastructure — outside the spec loop)

toolkit is a library, consumed by module path; there is no binary to ship. It
is versioned by tag from the monorepo:

- Tag `toolkit/vMAJOR.MINOR.PATCH` on `main`; the latest is
  `git tag --list 'toolkit/v*' --sort=-v:refname | head -1`.
- A consumer pins a version with an ordinary `require
  github.com/ikigenba/ikigenba/toolkit vMAJOR.MINOR.PATCH` in its own `go.mod`.
- toolkit pins agentkit the same way (`agentkit/v*` tags); bumping that pin is
  a design change, since the required version is stated in `specs/design/D1`.
- The version is release data, not spec-governed: the spec fixes the library's
  shape, never its version number. Cut a release by tagging a green `main`.
