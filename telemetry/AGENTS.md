# telemetry

telemetry keeps the suite's trail of events; every service posts to it and agents query it. One Go binary serves `telemetry.<host>` on the socket it is passed as descriptor 3, behind the host's nginx. Every service on the host posts its events to `/ingest`; telemetry stores each one in its own SQLite database, sweeps out records older than its retention window, and offers four read-only MCP tools over the trail at `/mcp` that agents reach through the gateway. It is built on appkit for pages, identity, MCP and the event contract. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `assets/` is the page markup and `share/icon.svg` the launcher icon. The build run never writes them; an agent changes them only on explicit, direct instruction from a human.
- `assets.go` is the root package, which embeds `assets/`. `cmd/telemetry` is the binary. `internal/` is everything else, one package per concern.
- `etc/` is what the host needs: `manifest.toml` and the nginx fragment `nginx.conf` that makes `/ingest` answer 404 to the public.
- The build run writes the Go source, the tests, `go.mod`, `go.sum` and `etc/`. `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

`assets/` holds `landing.html` (template `landing`) and `about.html` (template `about`), each opening with a comment naming the data it receives and the hooks it emits. They follow the repository's `design/` and are inputs to the spec. The root package embeds them, since Go's `embed` reaches only files at or below its own directory, and the code parses them into the set appkit's `page.Templates` returns and executes them by name. Code never writes markup of its own, not even a fragment or an error page. Tests assert on the hooks design names and on visible text, never on layout. A template that is missing or wrong, a state a story names that it cannot show, or a hook design names that it lacks is filed in `specs/issues/`; the run never edits an asset to close one.

telemetry holds no stylesheet, fonts or licences; appkit's `page` package serves them. `share/icon.svg` is the Tabler outline `heart-rate-monitor` from `design/ikigenba/icons/tabler/`, stripped as `design/README.md` asks of a launcher icon. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The release build is cgo-free (gate 3).
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline. The build run sets each requirement and moves to another release only when this file names one: appkit at the release `go.mod` requires (see Adopting appkit), and `modernc.org/sqlite` `v1.59.0`.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- A POSIX shell at `/bin/sh`, for the one exec'ing test.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs human approval. The SQLite driver is auth's, `modernc.org/sqlite` at the version auth's `go.mod` requires: pure Go, so the release build stays cgo-free, and already cached wherever auth builds.

### Adopting appkit

appkit is required only at a published release, `appkit/<version>` on origin (see the root `AGENTS.md`); the build run sets it with `go get github.com/ikigenba/ikigenba/appkit@<version>`.

## Test files

The test files are every `*_test.go` in the module: the root package, `cmd/telemetry` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, and telemetry stores whatever services send it and echoes it back through its tools, so no fixture carries one: not a service, event, user, request id, attribute key or value, tool argument, header value or expected body.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback and Unix sockets only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**The run seam carries the process.** telemetry binds nothing; it serves on the listener it is passed. Arguments, environment lookup and removal, the pid, the inherited listener, the output streams, the clock, the random source and the database source all come in through the run seam design declares, and tests inject them. A test never reads or changes the real environment, clock or randomness, and never leaves the inherited-listener step unset, which would take the test process's real descriptor 3. A test learns the server is ready the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp`, since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test. Drain tests are the one place a test waits on the clock, because the drain deadline is the behavior; they keep it to a few seconds. The gates run offline as an ordinary user with no systemd.

**The database is an isolated source.** A test passes a database path or file-backed SQLite DSN inside its own temporary directory, or an in-memory DSN, through the run seam or the constructor design names. Filesystem fixtures (an absent parent, a file that is not a database, a file with write permission removed) live in that tree; nothing touches `/opt/telemetry` or a shared file. A mid-request database failure is produced by breaking the test's own database after the server has it, for example by closing the store the test handed it.

**The clock is injected.** Every timestamp telemetry makes and every retention decision the sweep makes comes from the injected clock. Retention is proved by advancing the clock, and a sweep is triggered the way design names, never by waiting for a timer. A test that sleeps to age a record is a bug.

**Events come from the test.** A test that needs records in the trail puts them there itself, by posting to `/ingest` on the handler it built as a sibling would, or by inserting them through telemetry's store or sink, with the times, services, users, request ids and attributes the case needs. A test that observes telemetry's own events hands its writer appkit's `telemetry.Capture` or reads them back from its own store, as design names. Every test builds its own database; none depends on another's records or on test order.

**One environment variable, set by the test.** appkit's constructors read `IKIGENBA_SERVICES` (`services.Variable`) from the process environment, the one read telemetry cannot route through the run seam; in the binary only `main` makes it. A test that reaches that read, directly or through a telemetry constructor, first sets the variable with `t.Setenv` to a services file it wrote or to the empty string, and does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity` middleware wraps every route but ingest, so a request reaches `/`, the about screen or `/mcp` only with an `X-User-Id` header. A test sets the identity headers, or omits `X-User-Id` to exercise the missing-identity answer. A post to `/ingest` carries no identity headers, as a sibling's does. What the middleware, ingest handler and writer do is appkit's contract; telemetry's tests prove only that its handler is wired to them.

**MCP through appkit's client.** A test drives `/mcp` as a client would: it serves its handler on loopback or a Unix socket and calls it with appkit's `mcp.Client`, which forwards the caller's identity headers, and asserts on the `mcp.Result` and `mcp.ToolInfo` returned. Raw HTTP to `/mcp` is only for what the client cannot send, such as a missing identity header or `server/discover`. telemetry's tests never re-prove appkit's transport.

**No test runs the page's scripts.** Every page carries appkit's feedback script. The gates have no browser or JavaScript engine, and adding one is an unapproved dependency, so a test asserts what a response body carries, never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory, not `.go`, `go.mod`, `go.sum`, `etc/`, `share/` or `assets/`, and never parses source. It proves what design declares by using it. Handing `cmd/telemetry` to `go build` is not reading it.

**One exec'ing test, and only one.** Tests under `internal/` never start a process. The wiring in `cmd/telemetry` can be proved no other way, so exactly one test execs the binary, and it lives in `cmd/telemetry`. It builds the binary into a temporary directory and runs it with `--version`, with `manifest`, with `bogus`, and bare with no socket. For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The child runs in a test-owned working directory, where it creates its database under `state/`, with an environment the test composes. The test waits for `READY=1`, makes the requests design names over the socket, then stops the child with `SIGTERM`, and in a second run with `SIGINT`, asserting what design states. Any other test that builds, execs, waits on or signals a process is a bug.

## Live tests

telemetry calls no external service: its callers are sibling services, which the unit tests stand in for, so it has no live tests. Should design ever call for one, it is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions; it proves lightly that the whole is glued together, carries the id it proves, reads credentials from the environment, fails rather than skips when one is missing, and runs only as gate 6.

## Gates

Run from `telemetry/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/telemetry`, the release build as `devctl build` makes it, static and cgo-free
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)
6. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

Gate 5 also flags an undocumented `package main` (`revive`) and an `http.Server` without `ReadHeaderTimeout` (`gosec` G112); both are fixed in code, never suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` (the default) builds `bin/telemetry`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly, and the exec'ing test builds its own binary.

## Deploy

Deploy machinery, the version bump, tags and `devctl build`/`devctl deploy`, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

telemetry is an app, not a self-installing CLI: `devctl` builds it into a release tarball and pushes it to a space's host, where `opsctl install` installs it. The tag `appkit/<version>` must be on `origin` first (see Adopting appkit).

1. Set the version literal design declares to `vX.Y.Z`; the binary reports it verbatim and the deploy refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `telemetry/vX.Y.Z` and push the tag.
4. `devctl build telemetry` at that tag writes `telemetry/dist/telemetry-vX.Y.Z.tar.xz` holding `bin/telemetry`, `etc/` and `share/icon.svg`, with no version recorded inside; it refuses a binary whose `manifest` disagrees with the committed `etc/manifest.toml`.
5. `devctl deploy <space> telemetry/dist/telemetry-vX.Y.Z.tar.xz` uploads it to the space's `deploy/` prefix and runs `opsctl install` over ssh. The host writes `etc/env` with the space's `DRAIN_SECONDS`, replaces the release, publishes `ikigenba-telemetry.socket` (`/run/ikigenba/telemetry.sock`) and the `Type=notify` `ikigenba-telemetry.service`, regenerates nginx (with telemetry's `etc/nginx.conf`) and litestream, and restarts the service alone. The socket stays up, so events siblings post queue on it across the restart.

`telemetry --version` then prints `vX.Y.Z`, and `devctl space status <space>` reports it.
