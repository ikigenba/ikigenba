# idgen

A small Go CLI that mints short, traceable `PREFIX-XXXX-XXXX` ids (default
prefix `R`) from a 2026 UTC epoch and decodes them back to timestamps. Module path
`github.com/ikigenba/ikigenba/idgen`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (`cmd/`, `internal/`, `go.mod` are absent until it
does). See the `spec` and `build-spec` skills. Everything below is what the
build run computes the gap and runs the gates against; it is human-authored and
read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
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
`.github/workflows/release-idgen.yml` (repo root) — is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it.

1. Set the version in `internal/cli/version.go` (D6) to `vX.Y.Z`. It is
   source-carried, never ldflags-injected; the spec fixes only its shape, so no
   build run is needed to bump it.
2. Commit that on `main` and push `main`.
3. Tag that commit `idgen/vX.Y.Z` and push the tag. The workflow verifies the
   tag matches the in-source version (a mismatch fails the release), then runs
   GoReleaser from this directory: linux/darwin × amd64/arm64 tar.gz archives,
   checksums, and a GitHub release on the tag.

The latest release is `git tag --list 'idgen/v*' --sort=-v:refname | head -1`.
