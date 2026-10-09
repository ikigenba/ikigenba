# toolkit

toolkit is agentkit's standard local tools: Bash, Read, Write, Edit, Glob and Grep. It is a Go library that hands consumers of agentkit each tool as a ready-made `agentkit.Tool` built against an explicit root directory, behaving like the Claude Code tool of the same name so a model already knows how to use it. There is no binary; the module path is `github.com/ikigenba/ikigenba/toolkit`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`. toolkit is a library and has no stories.
- The module root is the one package, `toolkit`; source and tests sit flat beside `go.mod`.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `README.md` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- `bash` on the `PATH`: the `Bash` tool shells out to it, and its tests run real commands.
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline. The build run sets each requirement and moves to another release only when this file names one: agentkit at the release `go.mod` requires, `github.com/bmatcuk/doublestar/v4` and `github.com/boyter/gocodewalker`.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

## Test files

The test files are every `*_test.go` in the module. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, so no test file carries one that is not a genuine tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

## Live tests

A live test is the only kind that reaches an external service. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together, about one per external service, leaving behavior, edge cases and error paths to the unit tests; it carries the id it proves and the gap counts it; the code under test runs locally and only the external service is real; it reads credentials from the environment, fails rather than skips when one is missing, and no credential appears in the repository or in test output. It runs only as gate 5.

## Gates

Run from `toolkit/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix without changing an exported name, signature or observable behavior, or believes wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)
5. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` builds the package; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the tags, is hand-maintained and outside the spec system: the build run never reads, edits or tests it. toolkit is a library consumed by module path: there is no binary to ship, and the spec fixes its shape, never its version.

1. Tag a green `main` `toolkit/vX.Y.Z` and push the tag.
2. A consumer pins it with an ordinary `require github.com/ikigenba/ikigenba/toolkit vX.Y.Z` in its own `go.mod`.
3. toolkit pins agentkit the same way, by its `agentkit/v*` tags. The pin lives only in `go.mod`, never in a design document: D1 names the module, and the build run moves the pin to whatever release carries the surface the current designs use.

The latest release is `git tag --list 'toolkit/v*' --sort=-v:refname | head -1`.
