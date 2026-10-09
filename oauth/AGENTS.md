# oauth

oauth is a CLI that runs the OAuth login flow agent-repl's providers need. One Go binary runs the OAuth 2.0 authorization-code plus PKCE flow against any protocol-compliant service and writes the token endpoint's response verbatim to stdout. It holds no provider-specific knowledge: a service is described entirely by flags. It is a command-line program; it has no stories today. The module path is `github.com/ikigenba/ikigenba/oauth`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- The root package `oauth` (`assets.go`) embeds `assets/`. `cmd/oauth` is the binary. `internal/` is everything else, one package per concern.
- `assets/` holds the callback pages. The build run never writes it; the user or the delivering agent changes it.
- The build run writes the Go source and the tests. `assets/`, `Makefile`, `.golangci.yml`, `install.sh`, `.goreleaser.yaml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

`assets/` holds `success.html` and `failure.html`, the callback pages, `html/template` files. They are self-contained, carrying no theme, chrome or reference to any further resource (R-TPZN-04BM), and are an input to the spec. Go's `embed` reaches only files at or below the embedding package's directory, so the root package embeds them, not one under `internal/`; design names it. The code executes them by template name, adding no markup of its own. Every word a person reads, and every class, id and attribute, lives in the template and nowhere else; a test, a requirement and the source never spell one. Design names each template and the data it receives, never its text, hooks, markup or styles. A test proves a page by executing the named template with the data the design says and comparing, or by checking that a value it supplied appears in the body; it never looks for a word or a tag. The one exception is the self-containment check: a test may parse a page's bytes as an HTML document for every external reference it carries, whatever element carries it, never looking for a word, class, id or other hook; the parse is written in the test, adding no dependency. A change to copy or markup is an edit to the asset alone. A template that is missing or wrong, or that cannot show a state the design names, is filed in `specs/issues/`; the run never edits an asset to close one.

## Toolchain

- Go 1.26 or later.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's. `go.mod` requires nothing today.

## Test files

The test files are every `*_test.go` in the module: the root package, `cmd/oauth` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`.

**No id-shaped literal in a fixture.** oauth neither mints nor emits `PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a requirement tag.

**Build-tagged tests count toward the gap but do not all run.** The grep is textual, so an id tagged in `internal/browser/browser_darwin_test.go` (`//go:build darwin`) counts as covered though `go test -race ./...` on linux never runs it. Gates 3 and 4 guarantee those files compile for their platform. A requirement whose only proof is a darwin-only test is proven to that weaker standard; a design that needs stronger proof must not put the id there.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

## Live tests

A live test is the only kind that reaches an external service. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together, about one per external service, leaving behavior, edge cases and error paths to the unit tests; it carries the id it proves and the gap counts it; the code under test runs locally and only the external service is real; it reads credentials from the environment, fails rather than skips when one is missing, and no credential appears in the repository or in test output. It runs only as gate 7.

## Gates

Run from `oauth/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `GOOS=darwin go vet ./...`, which type-checks `internal/browser/browser_darwin.go` and its test
4. `GOOS=windows go vet ./...`, which type-checks `internal/browser/browser_other.go`, the `!linux && !darwin` fallback
5. `go test -race ./...`
6. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)
7. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

Gates 3 and 4 exist because `go build ./...` never compiles `_test.go` files and `golangci-lint` analyzes only the default build configuration, so platform-tagged sources and their tests can rot silently on linux. They use `go vet` because it type-checks the test files too. `golangci-lint` is not run per platform: its extra linters would fire on code paths nobody builds.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` (the default) builds the binary; `make fmt` formats; `make test`, `make vet` and `make lint` run those gates; `make install` installs from the checkout. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the version bump, tags, `install.sh`, `.goreleaser.yaml` and `.github/workflows/release-oauth.yml` at the repo root, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

1. Set the version in `internal/cli/version.go` (D10) to `vX.Y.Z`. The binary reports it verbatim, and the release refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `oauth/vX.Y.Z` and push the tag. The workflow builds with GoReleaser and publishes linux and darwin amd64 and arm64 archives and checksums; a prerelease tag (`oauth/vX.Y.Z-rc.1`) publishes a GitHub prerelease.
4. Once `gh release view oauth/vX.Y.Z` succeeds, install it from this directory with `OAUTH_VERSION=vX.Y.Z sh install.sh`, which puts the binary in `~/.local/bin`. A release is not done until this step is. Plain `sh install.sh` installs the newest stable release.

`oauth --version` then prints `vX.Y.Z`.
