# dummy

dummy is the reference app: a control panel over widgets that shows the patterns. One Go binary serves on the socket systemd passes it (`/run/ikigenba/dummy.sock` on a host), behind the host's nginx. The panel is a page listing widgets, a table fragment the page re-fetches that answers a conditional GET, and a form that creates a widget; the same widgets are offered as MCP tools at `/mcp` under the same rules. The widgets persist in SQLite at `state/dummy.db`, resolved against the working directory and opened through appkit's `db` package, so they outlive the process and requests share mutable state. It is built on appkit for pages, identity, MCP, telemetry and the database. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `assets/` is the page markup and `share/icon.svg` the launcher icon. The build run never writes them; the user or the delivering agent changes them.
- `migrations/` holds the database's migrations, which the root package embeds; the build run writes it.
- `assets.go` is the root package, which embeds `assets/` and `migrations/`. `cmd/dummy` is the binary. `internal/` is everything else, one package per concern.
- `etc/` is what the host needs: `manifest.toml`.
- `state/` is where a running dummy keeps `dummy.db`; it is created at run time and never committed.
- The build run writes the Go source, the tests, `migrations/`, `go.mod`, `go.sum` and `etc/`. `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

`assets/` holds `page.html`, `table.html`, `form.html` and `script.html`. They follow the repository's `design/` and are inputs to the spec. The root package embeds them, since Go's `embed` reaches only files at or below its own directory, and D01 names what it exports; the code executes them by template name. Code never writes markup of its own, not even a fragment or an error page. Tests assert on the hooks design names and on visible text, never on layout. A template that is missing or wrong, a state a story names that it cannot show, or a hook design names that it lacks is filed in `specs/issues/`; the run never edits an asset to close one.

dummy holds no stylesheet, fonts or licences; appkit's `page` package serves them. `share/icon.svg` is the Tabler outline `cube` from `design/ikigenba/icons/tabler/`, stripped as `design/README.md` asks of a launcher icon. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The release build is cgo-free (gate 3).
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline. The build run sets each requirement and moves to another release only when this file names one: appkit at the release `go.mod` requires (see Adopting appkit).
- `modernc.org/sqlite`, the cgo-free SQLite driver, an approved dependency that arrives through appkit's `db` package; dummy never imports it directly.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- A POSIX shell at `/bin/sh`, for the one exec'ing test.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, `appkit/<version>` on origin (see the root `AGENTS.md`); the build run sets it with `go get github.com/ikigenba/ikigenba/appkit@<version>`.

## Test files

The test files are every `*_test.go` in the module: the root package, `cmd/dummy` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, and the 422 re-render echoes a submitted name back in full, so no fixture carries one: not a widget name, a header value or an expected body. The obligation does not lapse because dummy's own output alphabet is narrow.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback and Unix sockets only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state. Clocks are injected: a test that opens a database hands it a `Now` it controls.

**The run seam carries the process.** dummy binds nothing; it serves on the listener it is passed (D01, D03). A `cli.Run`-level test of the serve path hands `Run` that listener through `Process.Inherit`, sets `LISTEN_PID` to the `Process.Pid` it chose and `LISTEN_FDS` to `1` in the environment map it passes, and records `Unsetenv` calls with a function of its own; it never leaves `Inherit` nil, which would take the test process's real descriptor 3. Failure paths (a descriptor that is not a listener, an `Accept` that fails) inject an `Inherit` that returns an error or a listener whose `Accept` fails. Arguments, environment lookup and removal, the pid and the output streams come in through the run seam (`cli.Process`, D01); tests inject buffers, a map and a recorder, never the real environment. A `Run`-level test also sets `Process.Dir` to a directory of its own, so the database lands in its temporary directory and never in the checkout, and `Process.Now` to a clock it controls. A test learns that `Run` is serving the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp`, since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET` in the environment map, and waits for `READY=1` with a deadline that fails the test. Drain tests are the one place a test waits on the clock, because the drain deadline is the behavior: a `Serve`-level test passes a drain of a few milliseconds and a handler that blocks on a channel, and the `Run`-level overrun tests hold a `POST /widgets` open by sending fewer body bytes than its `Content-Length` declares. There are at most five: one sets `DRAIN_SECONDS=1`; one sets `DRAIN_SECONDS=1`, holds no request open, and serves with a writer whose sink blocks until its context is done, to observe that `Run` still returns within a second of the drain deadline; two observe the 5-second default, with the variable unset and with it empty; and one sets a value too large for a `time.Duration`, observes that the request is not cut off within 7 seconds, then completes the body itself. The last three run as parallel subtests of one parent that sets the environment and builds their servers. A test learns the handler has begun, without sleeping, by sending `Expect: 100-continue` with `X-User-Id` and `Content-Type: application/x-www-form-urlencoded` (so the handler reads the body at all) and waiting for the `100 Continue` that `net/http` sends only when the handler first reads the body; only then does it cancel the context. The gates run offline as an ordinary user with no systemd.

**The handler is built over a store the test owns.** A handler-level test opens its own database with appkit's `db.Open` at a path in its own temporary directory, with `dummy.Migrations()` and a clock it controls, closes the handle when it ends, and creates its own store with `widget.NewStore` over that handle and a source of known bytes, so it knows every widget's id in advance, seeds it with the widgets the case needs, hands the handler a banner source, an MCP server and a telemetry writer of its own, and drives it in process; it needs no listener except to reach `/mcp`. The banner source is a function the test writes, returning the services the case needs (D04). No test depends on a widget another test created, on test order, or on a package-level set; there is none. A store failure is provoked with the handle's `SetFailing(true)`, never by corrupting or removing the file. Tests prove dummy's use of the database, its schema, its store and its wiring, never SQLite's own guarantees (atomicity, durability, locking) and never appkit's `db` contract (opening, migrations, transactions, `db status`), which appkit's own tests prove. The set is shared mutable state that concurrent requests touch, which gate 4's race detector is there to catch: a test may exercise it concurrently, and gate 4 is never reduced to a plain `go test`.

**Telemetry through a capturing sink.** Every event dummy records goes through the one `*telemetry.Writer` it is handed, and a test builds that writer itself: `telemetry.New` with `Service` set to `panel.ServiceName`, `Version` to `cli.Version`, a `Sink` that is a `&telemetry.Capture{}` (or a sink of the test's own, below), a buffer as `Stderr`, a `Now` it controls, a `Sleep` that records the pause and returns at once, and a `Rand` of known bytes when it asserts a minted request id. The same writer goes to `mcp.NewServer` (`ServerConfig.Telemetry`, which appkit requires) and to `panel.Handler`, or, for a `Run`-level test, as `Process.Telemetry` with that server as `Process.MCP`. The test calls `Writer.Flush`, asserts on `Capture.Events` in order by `Name`, `RequestID`, `User` and `Attrs`, and asserts the writer's `Stderr` buffer empty wherever dummy is healthy. A test whose `Run` did not shut the writer down calls `Writer.Shutdown` itself before it ends. No test leaves the `Sink` nil below `main`: the socket sink would read the real `IKIGENBA_SERVICES` and dial whatever it names. A sink of the test's own is needed in two cases only: one that answers a `Deliver` whose context is done with that context's error and accepts every other (the drain-overrun test), and one that blocks until its context is done (the test that the stop never outlasts the drain). To see the order of `Run`'s own overrun line against the writer's lines, a test hands `Process.Stderr` and the writer's `Stderr` one writer of its own that holds a mutex around each `Write`; otherwise the two are separate buffers. To see that the `service.stopping` line comes before any cut-off connection is closed, the overrun test's `Inherit` returns a loopback listener of its own whose accepted connections append a `Close` record, under the same mutex, to that writer's log. A `Process.Rand` of known bytes makes the fixture widgets' ids known.

**One environment variable, set by the test.** appkit's `page.New` and `mcp.NewServer` each read `IKIGENBA_SERVICES` (`services.Variable`) from the process environment once, when called; it is the one read dummy cannot route through `cli.Process`, and in the binary only `main` makes it (D01). A test that calls either, directly or through a dummy constructor, first sets that variable with `t.Setenv` to a services file it wrote in its own temporary directory or to the empty string. It is the only variable a test sets, and such a test does not call `t.Parallel`. A test may call appkit's other exported functions, to render the banner it expects, for instance.

**Identity comes from headers the test sets.** appkit's `identity.Require` wraps dummy's whole handler, so a request reaches a page, the fragment, the form or `/mcp` only with an `X-User-Id` header. A test sets the identity headers, or omits `X-User-Id` to exercise the missing-identity answer; code beneath the middleware that needs a caller is handed one with `identity.NewContext`. The middleware's behavior is appkit's contract; dummy's tests prove only that its handler is wrapped in it. appkit's `telemetry.Middleware` wraps `identity.Require` in turn, so a request's events carry the `X-Request-Id` the test sets or, when it sets none, an id minted from the writer's `Rand`, which the test knows when it supplied known bytes.

**MCP through appkit's client, in process.** A test proves the MCP tools as a client would: it serves the handler it built on an `httptest` server on loopback, or on a Unix socket in a short temporary directory with an `http.Client` that dials it, and drives `/mcp` with appkit's `mcp.Client` (`ListTools`, `CallTool`), passing the caller whose identity headers the client forwards, asserting on the `mcp.Result` and `mcp.ToolInfo` returned. Raw HTTP to `/mcp` is only for what the client cannot send, such as a missing identity header. dummy's tests never re-prove appkit's transport.

**No test runs the page's scripts.** The panel page carries an inline script that re-fetches the table fragment and, when there are services, appkit's launcher script, and on every page appkit's feedback script. The gates have no browser or JavaScript engine, and adding one is an unapproved dependency, so a test asserts what a response body carries, never what a script would do with it. Nothing in the gates waits on a timer for a poll to come round.

**No test reads the checkout.** A test opens no file of this directory, not `.go`, `go.mod`, `go.sum`, `etc/`, `share/` or `assets/`, and never parses source. It proves what design declares by using it: importing, calling, constructing, or running the binary the exec'ing test builds. Handing `cmd/dummy` to `go build` is not reading it.

**One exec'ing test, and only one.** Tests under `internal/` never start a process; the run seam exists so they need not. The wiring in `cmd/dummy` (D01's requirements on the binary) can be proved no other way, so exactly one test execs the binary, and it lives in `cmd/dummy`. It builds the binary into a temporary directory and runs it, always with `cmd.Dir` set to an empty temporary directory of its own, with `--version`, with `bogus`, and bare with no socket (exit 2). For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. It sets `NOTIFY_SOCKET` to a datagram socket it bound and waits for `READY=1`; then it sends `SIGTERM` and asserts exit 0, nothing on stdout or stderr, and that the socket's path still exists and still accepts a connection into its queue after the child has exited. It runs the serve case a second time with a fresh socket and stops it with `SIGINT`, asserting the same, because `main` promises both signals. The child's environment is one the test composes, offline like everything else. In the first serve case `IKIGENBA_SERVICES` names a services file the test wrote, whose entries include dummy's own and one named `telemetry` whose socket is a Unix socket in the same short temporary directory that the test serves with `net/http` and appkit's `telemetry.IngestHandler` over a `*telemetry.Capture`: the test stands in for the telemetry service, the child's events reach it over that socket, and the child has nothing to write to stderr. Before signalling, the test makes the requests D01 names, over the socket and with the identity headers: `GET /widgets`, an MCP call made with appkit's `mcp.Client` on behalf of a caller with a request id, and, for what that client cannot send (`server/discover`, whose result carries the instructions), a raw POST to `/mcp`. After the child exits it asserts on the events its stand-in received: `service.started` first, with the version `cli.Version` declares; the `tool.called` of its MCP call; and `service.stopping` last, with the reason `SIGTERM`. The second serve case names no services file, so the child writes every event to stderr: the test asserts that stdout is empty and that every stderr line is `dummy: undelivered event: ` and a JSON object, the first `service.started` and the last `service.stopping` with the reason `SIGINT`. Each such event waits out the writer's real retry pauses (about 150 milliseconds) before its line appears, well inside the 5-second drain, and the test waits on the child's exit, never on a sleep. The second case may make the same raw POST to prove the instructions are then absent. After the serve case the test asserts that `state/dummy.db` exists under the child's working directory. It asserts only what those requirements state, which proves `main` handed appkit's banner kit, MCP server and telemetry writer, with dummy's name and version, to the handler; everything else is proved in process. Any other test that builds, execs, waits on or signals a process is a bug.

## Live tests

dummy calls no external service: its callers are nginx, auth and sibling services, which the unit tests stand in for, so it has no live tests. Should design ever call for one, it is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions; it proves lightly that the whole is glued together, carries the id it proves, reads credentials from the environment, fails rather than skips when one is missing, and runs only as gate 6.

## Gates

Run from `dummy/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/dummy`, the release build as `devctl build` makes it, static and cgo-free
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

`make build` (the default) builds `bin/dummy`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly, and the exec'ing test builds its own binary.

## Deploy

Deploy machinery, the version bump, tags and `devctl build`/`devctl deploy`, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

dummy is an app, not a self-installing CLI: `devctl` builds it into a release tarball and pushes it to a space's host, where `opsctl install` installs it.

1. Set the version in `internal/cli/release.go` (D01) to `vX.Y.Z`; the binary reports it verbatim and the deploy refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `dummy/vX.Y.Z` and push the tag.
4. `devctl build dummy` at that tag writes `dummy/dist/dummy-vX.Y.Z.tar.xz` holding `bin/dummy`, `etc/` and `share/icon.svg`, with no version recorded inside; it refuses a binary whose `manifest` disagrees with the committed `etc/manifest.toml`.
5. `devctl deploy <space> dummy/dist/dummy-vX.Y.Z.tar.xz` uploads it to the space's `deploy/` prefix and runs `opsctl install` over ssh. The host writes `etc/env` (with the space's `DRAIN_SECONDS`), replaces the release, publishes `ikigenba-dummy.socket` (`/run/ikigenba/dummy.sock`) and the `Type=notify` `ikigenba-dummy.service`, regenerates nginx, and restarts the service alone; the socket stays up, so requests queue on it across the restart.

`dummy --version` then prints `vX.Y.Z`, and `devctl space status <space>` reports it.
