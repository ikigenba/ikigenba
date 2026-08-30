# idgen

A small Go CLI that mints short, traceable `PREFIX-XXXX-XXXX` ids (default
prefix `R`) from a 2026 UTC epoch and decodes them back to timestamps. Module path
`github.com/ikigenba/ikigenba/idgen`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build loop writes the code (`cmd/`, `internal/`, `go.mod` are absent until it
does). See the `spec` skill and `docs/spec-system.md` at the repo root.
Everything below is declared for the loop's verify role, which reads this file
directly.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- `llm-lint` on PATH, with its provider API key present in the environment
- `idgen` on PATH (minting requirement ids for spec work)

## Test files

The project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

Because idgen's own output shares that exact shape, test files MUST NOT
contain any id-shaped literal that is not a genuine requirement-id tag:
golden-vector ids are built by joining prefix and body at runtime (see the
spec-system note in `specs/design/D2-id-format.md`).

## Gates

Run from this directory (`idgen/`), in order; every command must exit 0. No
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

## Releasing (infrastructure — outside the spec loop)

Releases are cut from this monorepo by tag. The release machinery is
hand-maintained infrastructure, not spec-governed code:

- Tag `idgen/vMAJOR.MINOR.PATCH` on `main`; the latest is
  `git tag --list 'idgen/v*' --sort=-v:refname | head -1`.
- Pushing the tag triggers `.github/workflows/release-idgen.yml` (repo root),
  which verifies the tag's version equals the in-source version string in
  `internal/cli/version.go` (a mismatched tag fails the release), then runs
  GoReleaser from this directory using `.goreleaser.yaml` — linux/darwin ×
  amd64/arm64, tar.gz archives, checksums, a GitHub release on the tag.
- The version string is source-carried (see `specs/design/D6-help-and-version.md`),
  never ldflags-injected: to release, bump `internal/cli/version.go` through
  the spec loop (it is a design fact), merge, then tag to match.
