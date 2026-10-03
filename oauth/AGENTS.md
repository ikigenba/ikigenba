# oauth

oauth is a CLI that runs the OAuth login flow agent-repl's providers need. One Go binary runs the OAuth 2.0 authorization-code plus PKCE flow against any protocol-compliant service and writes the token endpoint's response verbatim to stdout. It holds no provider-specific knowledge: a service is described entirely by flags. The module path is `github.com/ikigenba/ikigenba/oauth`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- `cmd/oauth` is the binary. `internal/` is everything else, one package per concern.
- The build run writes the Go source and the tests. `Makefile`, `.golangci.yml`, `install.sh`, `.goreleaser.yaml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

`go.mod` requires no module. Prefer the standard library, then a widely used public module; adding one needs human approval.

## Test files

The test files are every `*_test.go` under `cmd/` and `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** oauth neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

**Build-tagged tests count toward the gap but are not all executed.** The grep is textual, so an id tagged in `internal/browser/browser_darwin_test.go` (`//go:build darwin`) counts as covered even though `go test -race ./...` on a linux host never runs it. Gates 3 and 4 guarantee those files at least compile for their platform. A requirement whose only proof is a darwin-only test is proven to that weaker standard; a design that needs stronger proof must not put the id there.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

## Live tests

Live tests are the only tests that reach an external service; every other test follows Test discipline.

- Minimal: a live test proves lightly that the whole is glued together end to end, about one per external service. Behavior, edge cases and error paths are the unit tests' job.
- Separate: a `*_live_test.go` file behind `//go:build live`, with `TestLive*` functions. `go test ./...` never runs them; `make live` runs `go test -tags live -count=1 -run '^TestLive' ./...`.
- Designed: it carries the requirement id it proves, and the gap counts it like any other test.
- Local: the code under test runs on the developer's machine; only the external service is real.
- Credentials: read from the environment. A missing one fails the test, never skips it. No credential appears in the repo or in test output.
- Run: only as the conditional `make live` gate.

## Gates

Run from `oauth/`, in order; every command must exit 0. No skipped tests and no disabled linters.

1. `test -z "$(gofmt -l .)"` (`go fmt` always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `GOOS=darwin go vet ./...`, which type-checks `internal/browser/browser_darwin.go` and its test
4. `GOOS=windows go vet ./...`, which type-checks `internal/browser/browser_other.go`, the `!linux && !darwin` fallback
5. `go test -race ./...`
6. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and stops applying `//nolint` and `.golangci.yml` suppressions), and since the cache is private, `--allow-parallel-runners` skips the machine-wide lock so several sub-projects can lint at once (`make lint` runs this form)
7. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

Gates 3 and 4 exist because `go build ./...` never compiles `_test.go` files and `golangci-lint` analyzes only the default build configuration, so platform-tagged sources and their tests can rot silently on a linux host. They use `go vet` rather than `go build` because vet type-checks the test files too. `golangci-lint` is deliberately not run per platform: its extra linters would fire on code paths nobody builds, for diminishing returns.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Releasing

Release machinery, the version bump, tags, `Makefile`, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-oauth.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version in `internal/cli/version.go` (D10) to `vX.Y.Z`. It is a source literal the binary reports verbatim, and the deploy refuses a tag that does not match what the built binary's `--version` prints.
2. Commit on `main` and push `main`.
3. Tag the commit `oauth/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux and darwin amd64 and arm64 archives and checksums. A tag with a prerelease part (`oauth/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Wait for the workflow to publish the release (`gh release view oauth/vX.Y.Z` succeeds), then install it on the developer's machine from this directory with `OAUTH_VERSION=vX.Y.Z sh install.sh`. The installer puts the binary in `~/.local/bin`; plain `sh install.sh` installs the newest stable release. A release is not done until this step is.

`oauth --version` then prints `vX.Y.Z`.
