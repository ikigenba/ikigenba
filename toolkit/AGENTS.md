# toolkit

toolkit is agentkit's standard local tools: Bash, Read, Write, Edit, Glob and Grep. It is a Go library that hands consumers of `github.com/ikigenba/ikigenba/agentkit` each tool as a ready-made `agentkit.Tool` value built against an explicit root directory, behaving like the Claude Code tool of the same name so a model already knows how to use it. There is no binary; consumers require the module path `github.com/ikigenba/ikigenba/toolkit`. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- The module root is the one package, `toolkit`: its source and tests sit flat beside `go.mod`.
- The build run writes the Go source, the tests, `go.mod` and `go.sum`. `Makefile`, `.golangci.yml`, `README.md` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Toolchain

- Go 1.26 or later.
- `bash` on the `PATH`: the `Bash` tool shells out to it, and its tests run real commands.
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline. The build run sets each requirement and moves to another release only when this file names one: agentkit at the release `go.mod` requires, `github.com/bmatcuk/doublestar/v4` and `github.com/boyter/gocodewalker`.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs human approval.

## Test files

The test files are every `*_test.go` in the module. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

No test file carries an id-shaped literal that is not a genuine requirement tag.

## Test discipline

These rules govern everything `go test ./...` runs; live tests follow Live tests below. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

## Live tests

Live tests are the only tests that reach an external service; every other test is a unit test under Test discipline.

- Minimal: a live test proves lightly that the whole is glued together end to end, about one per external service. Behavior, edge cases and error paths are the unit tests' job.
- Separate: a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions. `go test ./...` never runs them; `make live` runs `go test -tags live -count=1 -run '^TestLive' ./...`.
- Designed: it carries the requirement id it proves, and the gap counts it like any other test.
- Local: the code under test runs on the developer's machine; only the external service is real.
- Credentials: read from the environment. A missing one fails the test, never skips it, and none appears in the repository or in test output.
- Run: only as the conditional `make live` gate below.

## Gates

Run from `toolkit/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding that cannot be fixed below the contract seam without changing an exported name, signature or observable behavior, or that is wrong, is filed as an issue under `specs/issues/` for a human to adjudicate.

1. `test -z "$(gofmt -l .)"` (`go fmt` always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and stops applying `//nolint` and `.golangci.yml` suppressions), and since it is private, `--allow-parallel-runners` skips golangci-lint's machine-wide lock so several sub-projects can lint at once (`make lint` runs this form)
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

Release machinery, the tags, is hand-maintained and outside the spec system: the build run never reads, edits or tests it. toolkit is a library consumed by module path, so there is no binary to ship, and the spec fixes its shape, never its version number.

1. Tag a green `main` `toolkit/vX.Y.Z` and push the tag.
2. A consumer pins it with an ordinary `require github.com/ikigenba/ikigenba/toolkit vX.Y.Z` in its own `go.mod`.
3. toolkit pins agentkit the same way, by its `agentkit/v*` tags. The pin lives only in `go.mod`, never in a design document: D1 names the module, and the build run moves the pin to whatever release carries the surface the current designs use.

The latest release is `git tag --list 'toolkit/v*' --sort=-v:refname | head -1`.
