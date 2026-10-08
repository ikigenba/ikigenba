# appkit

appkit holds what every app shares: page chrome, identity, the MCP server and client, telemetry, and the database open path. It is a Go library with one concern per package and no package privileged; the module root exports nothing. The module path is `github.com/ikigenba/ikigenba/appkit`. It knows nothing about authentication: nginx and auth establish who the caller is, and appkit only carries it. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- `page` is the chrome every app shows a signed-in user: the banner, launcher and footer templates, and the shared stylesheet, fonts, launcher script, button feedback script and favicon served from one fixed path prefix. `page/assets/` is its markup and static files (see Assets).
- `services` is the one reader of the host's services file (`IKIGENBA_SERVICES`, owned by opsctl).
- `version` is the identity the host gives an app in its environment (`IKIGENBA_COMMIT`, `IKIGENBA_RELEASE`) and the display string built from it.
- `identity` is the caller nginx authenticated (`X-User-Id`, `X-User-Email`, `X-Request-Id`): the middleware that requires it, and forwarding it on a call to a sibling service.
- `mcp` is the Model Context Protocol: the server a service mounts at `/mcp` with its tools, and the client the gateway and service tests use.
- `telemetry` is the suite's event trail: the event contract, the writer that queues and delivers a service's events, its sinks, the wire to the telemetry service, and the request middleware and sibling client that record every request and sibling call.
- `db` is a service's SQLite database: opening it, reading and writing it through transactions, the failure seam its tests use, and its migrations and their status.
- The build run writes the Go source and the tests. `page/assets/`, `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

`page/assets/` sits inside the `page` package directory because Go's `embed` reaches only files at or below the embedding package. It holds the banner template (`banner.html`), the launcher script (`launcher.js`), the button feedback script (`feedback.js`), the favicon (`favicon.svg`, served at `/_appkit/favicon.svg` as image/svg+xml), and copies of the repository's `design/` files: the stylesheet, fonts and their licences. The build run never writes it; the user or the delivering agent changes it, and the session that changes `design/` refreshes the copies. The copied stylesheet replaces the Google Fonts import with `@font-face` rules for the files beside it, and its header names the `design/` commit it came from, so that commit lands first. Package `page` embeds `page/assets/` and never writes markup of its own.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's. `go.mod` requires only `modernc.org/sqlite`, the pure-Go SQLite driver, and only `db` imports it.

## Test files

The test files are every `*_test.go` in the module. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, so no test file carries one that is not a genuine tag.

## Test discipline

These rules govern everything `go test ./...` runs.

**Offline.** No network beyond loopback. MCP, identity and telemetry tests talk HTTP to an `httptest` server on loopback, or to a Unix socket in the test's own temporary directory (keep the path short: a Unix socket path is limited to 107 bytes), never to `/run/ikigenba` or a real service. A socket-sink test names its socket in a services file it writes there.

**Deterministic.** Time, randomness and environment are injected; no test sleeps to wait. A telemetry writer under test gets a fixed `Config.Now` (advanced by hand where a duration is checked), a `Config.Rand` of known bytes where a minted request id is checked, and a `Config.Sleep` that records each pause and returns at once. A test waits for delivery with `Writer.Flush` or a sink that signals on a channel, never a timer. Every writer a test builds is shut down before the test ends (`t.Cleanup`), so no sender outlives it.

**No fixed ports.** A test uses `httptest` or binds `127.0.0.1:0`.

**Isolated.** A test touches only its own temporary directory, never the developer's home, config or real state, and never `/var/lib/ikigenba`. A services file a test needs is written there and named through `IKIGENBA_SERVICES` with `t.Setenv`.

**Logs are captured.** Anything appkit writes as a diagnostic goes to an `io.Writer` the test supplies, and the test asserts on it; no test reads the process's real stderr.

**Hooks, not layout.** Tests assert on the hooks design names and on visible text, never on styles, nor on markup structure beyond the containment and order design names as hooks.

## Live tests

There are none.

## Gates

Run from `appkit/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix without changing an exported name, signature or observable behavior, or believes wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` builds the module; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly.

## Releasing

Release machinery, the tags, is hand-maintained and outside the spec system: the build run never reads, edits or tests it. appkit is a library consumed by module path: there is no binary to ship, and the spec fixes its shape, never its version.

1. Tag a green `main` `appkit/vX.Y.Z` and push the tag.
2. A consumer pins it with an ordinary `require github.com/ikigenba/ikigenba/appkit vX.Y.Z` in its own `go.mod`.

The latest release is `git tag --list 'appkit/v*' --sort=-v:refname | head -1`.
