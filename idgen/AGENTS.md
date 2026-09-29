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

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs. Live
tests follow Live tests below.

- Offline: no network beyond loopback, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

## Live tests

Live tests are the only tests that connect to external services. Every other
test is a unit test and follows Test discipline.

- Minimal: a live test proves lightly that the whole application or library
  is glued together and works end to end, about one per external service,
  never an exhaustive suite. Behavior, edge cases, and error paths are the
  unit tests' job.
- Separate: live tests are `*_live_test.go` files guarded by
  `//go:build live`, with test functions named `TestLive*`. `go test ./...`
  never runs them; `make live` runs
  `go test -tags live -count=1 -run '^TestLive' ./...`.
- Designed: a live test carries the requirement id it proves, and the gap
  counts it like any other test.
- Local: the code under test runs on the developer's machine; only the
  external service is real.
- Credentials: a live test reads its credentials from the environment. It
  never skips: a missing credential fails it. No credential appears in the
  repo or in test output.
- Run: live tests run only as the conditional `make live` gate below.

## Gates

Run from this directory (`idgen/`), in order; every command must exit 0. No
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
5. `make live` — **conditional**: run only when the phase's diff (the working
   tree against the last phase commit) adds or modifies a `*_live_test.go`
   file; otherwise it is not run and not counted. When it applies and a
   credential is absent, that is a missing tool: file an issue, do not pass or
   skip.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Deploy

Deploy machinery — the version bump, tags, `.goreleaser.yaml`, and
`.github/workflows/release-idgen.yml` (repo root) — is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it.

1. Set the version in `internal/cli/version.go` (D6) to `vX.Y.Z`. It is a
   source literal the binary reports verbatim, and the deploy refuses a tag
   that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `idgen/vX.Y.Z` and push the tag. The workflow runs
   GoReleaser from this directory: linux/darwin × amd64/arm64 tar.gz archives,
   checksums, and a GitHub release on the tag.
4. Install it locally: download that release's archive for your platform and
   put the `idgen` binary on your `PATH`.

`idgen --version` then prints `vX.Y.Z`.
