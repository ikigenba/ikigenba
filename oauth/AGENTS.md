# oauth

A standalone Go CLI that runs the OAuth 2.0 authorization-code + PKCE login
flow against any protocol-compliant service and writes the token endpoint's
response verbatim to stdout. It holds no provider-specific knowledge — a
service is described entirely by flags. Module path
`github.com/ikigenba/ikigenba/oauth`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (`cmd/`, `internal/` are absent until it does). See
the `spec` and `build-spec` skills.
Everything below is what the build run computes the gap and runs the gates
against; it is human-authored and read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** Unlike idgen, oauth neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement
tag. idgen's rule about joining golden vectors at runtime has no analogue here.

**Build-tagged tests count toward the gap but are not all executed.** The grep
is textual, so an id tagged in `internal/browser/browser_darwin_test.go`
(`//go:build darwin`) is counted covered even though `go test -race ./...` on a
linux host never runs it. Gates 3 and 4 below guarantee those files at least
*compile* under their target platform. A requirement whose only proof is a
darwin-only test is proven to that weaker standard, and a design that needs
stronger proof must not put the id there.

## Gates

Run from this directory (`oauth/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `GOOS=darwin go vet ./...` — type-checks `internal/browser/browser_darwin.go`
   and its test
4. `GOOS=windows go vet ./...` — type-checks `internal/browser/browser_other.go`,
   the `!linux && !darwin` fallback
5. `go test -race ./...`
6. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form)

Gates 3 and 4 exist because `go build ./...` never compiles `_test.go` files
and `golangci-lint` analyzes only the default build configuration, so the
platform-tagged sources and their tests can rot silently on a linux host. They
use `go vet` rather than `go build` precisely because vet type-checks the test
files too. Deliberately **not** run per-platform: `golangci-lint`, whose extra
linters would fire on code paths nobody builds for diminishing returns.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Releasing

Release machinery — the version bump, tags, `.goreleaser.yaml`, and
`.github/workflows/release-oauth.yml` (repo root) — is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it.

1. Set the version in `internal/cli/version.go` (D10) to `vX.Y.Z`. It is
   source-carried, never ldflags-injected; the spec fixes only its shape, so no
   build run is needed to bump it.
2. Commit that on `main` and push `main`.
3. Tag that commit `oauth/vX.Y.Z` and push the tag. The workflow verifies the
   tag matches the in-source version (a mismatch fails the release), then runs
   GoReleaser from this directory: linux/darwin × amd64/arm64 tar.gz archives,
   checksums, and a GitHub release on the tag.

The latest release is `git tag --list 'oauth/v*' --sort=-v:refname | head -1`.
