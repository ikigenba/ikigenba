# toolkit

A Go library that gives consumers of `github.com/ikigenba/ikigenba/agentkit` a
standard set of local tools — `Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep` —
as ready-made `agentkit.Tool` values, each built against an explicit root
directory. Module path `github.com/ikigenba/ikigenba/toolkit`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (the `toolkit` package and `go.mod`'s dependency
graph fill in as it does). See the `spec` and `build-spec` skills. Everything
below is what the build run computes the gap and runs the gates against; it is
human-authored and read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `bash` on PATH (the `Bash` tool shells out to it; its tests run real commands)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

## Test files

The sub-project's tests are all `*_test.go` files under this module. This is the
file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

Test files MUST NOT contain any id-shaped literal that is not a genuine
requirement-id tag.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs. Live
tests are separate, carry the `live` build tag, and are not part of it.

- Offline: no network beyond loopback, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

## Gates

Run from this directory (`toolkit/`), in order; every command must exit 0. No
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

A per-finding `//nolint` comment counts as a disabled linter. Never add one
to make a gate pass. A finding that cannot be fixed below the contract
seam without changing an exported name, signature, or observable behavior, or
that is wrong, is filed as an issue under `specs/issues/` so a human can
adjudicate — restructure the code or amend the design.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Releasing

Release machinery — the tags — is hand-maintained infrastructure outside the
spec system: the build run never reads, edits, or tests it.

toolkit is a library consumed by module path; there is no binary to ship. The
spec fixes its shape, never its version number.

1. Tag a green `main` `toolkit/vX.Y.Z` and push the tag.
2. A consumer pins it with an ordinary `require
   github.com/ikigenba/ikigenba/toolkit vX.Y.Z` in its own `go.mod`.
3. toolkit pins agentkit the same way (`agentkit/v*` tags). The pin lives
   only in `go.mod`, never in a design document: D1 names the module, and the
   build run moves the pin to whatever release carries the surface the current
   designs use.

The latest release is
`git tag --list 'toolkit/v*' --sort=-v:refname | head -1`.
