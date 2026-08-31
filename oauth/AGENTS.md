# oauth

A standalone Go CLI that runs the OAuth 2.0 authorization-code + PKCE login
flow against any protocol-compliant service and writes the token endpoint's
response verbatim to stdout. It holds no provider-specific knowledge — a
service is described entirely by flags. Module path
`github.com/ikigenba/ikigenba/oauth`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build loop writes the code (`cmd/`, `internal/` are absent until it does). See
the `spec` skill and `docs/spec-system.md` at the repo root. Everything below
is declared for the loop's verify role, which reads this file directly.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- `llm-lint` on PATH, with its provider API key present in the environment
- `idgen` on PATH — for spec authoring only. Unlike idgen's own build, this
  project's loop never mints an id; ids are minted when a design document is
  written, not when a phase is built.

## Test files

The project's tests are all `*_test.go` files under `cmd/` and `internal/`.
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
6. `golangci-lint run`
7. `llm-lint cmd internal`

Gates 3 and 4 exist because `go build ./...` never compiles `_test.go` files
and `golangci-lint` analyzes only the default build configuration, so the
platform-tagged sources and their tests can rot silently on a linux host. They
use `go vet` rather than `go build` precisely because vet type-checks the test
files too. Deliberately **not** run per-platform: `golangci-lint`, whose extra
linters would fire on code paths nobody builds for diminishing returns.

llm-lint loads this project's own rules from `lint-rules/` (wired via
`.llm-lint.json`, found by ancestor walk — a sibling project's config is not on
that path, so this directory carries its own). Rules are promoted individually:
a promotion flips the rule file to `severity: error` and adds its id to the
`enable` allowlist in `.llm-lint.json`. Un-promoted rules stay disabled — they
make no LLM calls and print nothing — so every finding the gate reports fails
it. The rule set and its allowlist are currently a verbatim copy of idgen's,
all promoted to `severity: error`; consolidating the two copies into one shared
directory is deferred, not forgotten.

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

Releases are cut from this monorepo by tag. The release machinery is
hand-maintained infrastructure, not spec-governed code:

- Tag `oauth/vMAJOR.MINOR.PATCH` on `main`; the latest is
  `git tag --list 'oauth/v*' --sort=-v:refname | head -1`.
- Pushing the tag triggers `.github/workflows/release-oauth.yml` (repo root),
  which verifies the tag's version equals the in-source version string in
  `internal/cli/version.go` (a mismatched tag fails the release), then runs
  GoReleaser from this directory using `.goreleaser.yaml` — linux/darwin ×
  amd64/arm64, tar.gz archives, checksums, a GitHub release on the tag.
- The version string is source-carried (see
  `specs/design/D10-help-and-version.md`), never ldflags-injected. Its *value*
  is release data, not spec-governed: edit `internal/cli/version.go` directly to
  the new `vMAJOR.MINOR.PATCH` (the spec fixes only its shape), keep it valid
  against the gates, merge, then tag to match. No spec-loop cycle is needed to
  bump it.
