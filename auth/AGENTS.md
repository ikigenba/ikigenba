# auth

auth signs users in and answers nginx's identity subrequest for every other app. One Go binary serves it on the socket it is passed as descriptor 3, behind the host's nginx; users sign in through Google's OIDC issuer, and it is the OAuth authorization server MCP clients register with and are approved through. Users, sessions, API and MCP client tokens, MCP client registrations and authorization codes live in its own SQLite database, `state/auth.db` resolved against the working directory and opened through appkit's `db` package. In a sandbox it runs behind the sandbox's nginx, which sets `IKIGENBA_PUBLIC_URL` and `IKIGENBA_CALLBACK_URL` (D03); a host sets neither. It is built on appkit for pages, identity, telemetry and the database. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `assets/` is the page markup, and `share/icon.svg` the launcher icon. The build run never writes them; the user or the delivering agent changes them.
- `migrations/` holds the database's migrations, which the root package embeds; the build run writes it.
- The root package `auth` embeds `migrations/` and `assets/`. `cmd/auth` is the binary. `internal/` is everything else, one package per concern.
- `etc/` is what the host needs: `manifest.toml`.
- `state/` is where a running auth keeps `auth.db`; it is created at run time and never committed.
- The build run writes the Go source, the tests, `migrations/`, `go.mod`, `go.sum` and `etc/`. `assets/`, `share/icon.svg`, `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

auth holds no stylesheet, fonts or licences; appkit's `page` package serves them.

`assets/` holds `approve.html`, the approve page, and `mcp-clients.html`, the profile's `MCP clients` card, `html/template` files that follow the repository's `design/`. They are an input to the spec. The root package embeds them, since Go's `embed` reaches only files at or below its own directory, and the code executes them by template name from auth's template set (D01), adding no markup of its own around them. Design names each template, the data it receives and the hooks it emits; tests assert on those hooks and on visible text, never on layout. A template that is missing or wrong, a state a story names that it cannot show, or a hook design names that it lacks is filed in `specs/issues/`; the run never edits an asset to close one.

`share/icon.svg` is the Tabler outline `user-circle` from `design/ikigenba/icons/tabler/`, stripped as `design/README.md` asks of a launcher icon. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The release build is cgo-free (gate 3).
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline: appkit at the release `go.mod` requires (see Adopting appkit), `github.com/coreos/go-oidc/v3` and and `golang.org/x/oauth2` for the Google sign-in.
- `modernc.org/sqlite`, the pure-Go SQLite driver the cgo-free release build depends on, an approved dependency that arrives through appkit's `db` package; auth never imports it directly.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- A POSIX shell at `/bin/sh`, for the one exec'ing test.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's. Release selection is `go.mod`'s job.

### Adopting appkit

appkit is required only at a published release, `appkit/<version>` on origin (see the root `AGENTS.md`); the build run sets it with `go get github.com/ikigenba/ikigenba/appkit@<version>`.

## Test files

The test files are every `*_test.go` in the module: the root package, `cmd/auth` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`.

**No id-shaped literal in a fixture.** No auth value has a requirement tag's shape: user, session and login-state ids are 26 Crockford base32 chars (`internal/idcodec`, no hyphen), authorization codes 26 Crockford base32 chars, token ids `tok_` plus 26, MCP client ids `cli_` plus 26, token secrets `ikp_` plus 52, stored secret hashes 64 lowercase hex chars, and request ids 32 lowercase hex chars. No auth literal can be mistaken for a tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state. Clocks are injected: a test that opens a database hands it a `Now` it controls. The gates run offline as an ordinary user with no systemd.

**Every input comes through the run seam.** `cli.Process` (D01) carries `Args`, `LookupEnv`, `Unsetenv`, `Pid`, `Stdout`, `Stderr`, `Inherit`, `Now`, `Rand`, `OIDCIssuer`, `Dir`, `Banner` and `Sink`, and tests supply each one. A `Run`-level test sets `Dir` to a temporary directory of its own, so the database lands there and never in the checkout. A test never reads or changes the real environment, clock or randomness, and touches no filesystem or network state outside the fixtures below. The one exception is the working directory: a test that proves what an empty `Dir` or `auth.Migrations()` does whatever the working directory is may `t.Chdir` into a temporary directory of its own, and such a test does not run in parallel. A test whose result depends on the developer's machine, wall clock, environment or a port in use is a bug.

**Google is faked, never called.** auth reaches Google's OIDC issuer only through `Process.OIDCIssuer`. Tests stand up a loopback issuer (discovery document, keys and token endpoint on `127.0.0.1:0`) and inject its URL, so no test reaches `accounts.google.com`.

**The database is the test's own.** A test that needs a store opens its own database with appkit's `db.Open` at a path in its own temporary directory, with `auth.Migrations()` and a clock it controls, closes the handle when it ends, and builds the store over that handle with `store.New`; nothing touches `/opt/auth`, the checkout's `state/` or a shared file. A `Run`-level fixture lives under the test's `Dir`: a regular file named `state`, a file at `state/auth.db` that is not a database, or a database a test's own `db.Open` made and then changed through `DB.Write`, such as one recording a version `auth.Migrations()` does not hold. A store failure, D03's `500` among them, is provoked with `SetFailing(true)` on the handle the store a server-level test handed `server.New` was built over, never by corrupting the file, removing permissions or closing the store; where only one statement must fail, the test creates through `DB.Write` on that handle a trigger that aborts it. A store test builds the old-shape fixtures for D04's migrations in its own tree as D04 names: `db.Open` with `auth.Migrations()`, `CreateToken` through a store over that handle, then through `DB.Write` drop `clients`, `auth_codes` and `tokens.kind`/`tokens.host`, and either delete version 3's `schema_migrations` row or also strip the `tok_` prefix and drop `schema_migrations`; then close the handle and open the file again. Tests prove auth's use of the database, its schema, its store and its wiring, never SQLite's own guarantees (atomicity, durability, locking) and never appkit's `db` contract (opening, migrations, transactions, `db status`), which appkit's own tests prove.

**The clock is injected.** `Process.Now` supplies every timestamp. Session idle (15m) and cap (18h), the 30d token login window, token expiry (30d/90d/365d), the 90d MCP client token, the 10m authorization code and the 24h unused client registration are tested by advancing what `Now` returns. A test that sleeps to age a session or token is a bug.

**Randomness is injected.** `Process.Rand` supplies the bytes `idcodec` mints ids and secrets from, so values under test are reproducible.

**The banner source is the test's own.** `Process.Banner` and `server.Config.Banner` are a function the test writes, returning the services the case needs (D01, D05); no test calls `page.New`, which reads `IKIGENBA_SERVICES` from the real environment. A test may call appkit's other exported functions, to render the banner it expects, say.

**The trail is captured, never sent.** Events reach a test through a sink it supplies, never a socket (the exec'ing test excepted), and `Process.Sink` is never nil. A `cli.Run`-level test passes `&telemetry.Capture{}` as `Process.Sink` and reads `Capture.Events` after `Run` returns, or a sink of its own: one returning an error wrapping `telemetry.ErrRejected`, so each event goes to `Stderr` at once with no retry or pause, or one holding `Deliver` until its context is done, for the drain-window test. A `server.New`-level test builds its own writer (`telemetry.New` with `Service` `auth`, a `*telemetry.Capture` sink, its own `Now` and `Rand`, and a buffer as `Stderr`), passes it as `server.Config.Telemetry`, drives the handler, calls `Writer.Flush`, asserts on `Capture.Events`, and calls `Writer.Shutdown` before it ends. A test that wants a particular stop reason cancels `Run`'s context with `context.WithCancelCause`.

**stderr is asserted only for trouble.** A handled failure, a 500 from a failed store or a 502 from a failed Google, is asserted through the `request.finished` status, the domain events, and `Stderr` staying empty. Exact `Stderr` bytes are asserted only for a condition auth cannot continue from (a refused start, a database that will not open, a failed `db status`, a failed `READY=1`, a failed or overrun `Serve`) and for the writer's `auth: undelivered event: ` lines.

**No test runs the page's scripts.** The pages carry appkit's feedback script and, when there are services, appkit's launcher script. The gates have no browser or JavaScript engine, and adding one is an unapproved dependency, so a test asserts what a response body carries, never what a script would do with it.

**No test reads the checkout.** A test opens no file of this module, not source, `go.mod`, `go.sum`, `etc/`, `share/` or `assets/`, walks no directory, and never parses or reflects over code. It proves structure by use: importing the package, constructing the type, calling the function, or running the built binary in the one exec'ing test.

**auth binds nothing; a test makes its listener.** auth serves on the listener it is passed (D01, D03). A test makes its own, a loopback TCP listener on `127.0.0.1:0` or a Unix socket in a temporary directory, never a fixed port. A `cli.Run`-level serve test hands it to `Run` through `Process.Inherit`, sets `LISTEN_PID` to the `Process.Pid` it chose and `LISTEN_FDS` to `1` in the environment map, alongside the three Google settings, and records `Unsetenv` calls with a function of its own; it never leaves `Inherit` nil, which would take the test process's real descriptor 3. Failure paths inject an `Inherit` that errors or a listener whose `Accept` fails. A test learns `Run` is serving the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp`, since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test. It stops `Run` by cancelling the context, never by a signal.

**The drain tests wait on the clock, and only they do**, because the drain deadline is the behavior, and it also bounds how long auth waits to deliver its last events. The one `Run`-level drain-window test sets `DRAIN_SECONDS=1`, passes a sink whose `Deliver` returns only when its context is done, cancels `Run` with no request in flight, and asserts `Run` returns `0` with `service.stopping` written to `Stderr` as undelivered, within the drain plus a margin of real time. A `Serve`-level test passes a drain of a few milliseconds and a handler that blocks on a channel. The one `Run`-level overrun test sets `DRAIN_SECONDS=1` and holds a sign-in callback open inside the fake Google: it starts `GET /login/google`, reads `state` from the `Location`, requests `/login/google/callback` with it while the fake token endpoint blocks, cancels the context once that endpoint has received the exchange (never after a sleep), and releases the endpoint after `Run` has returned.

**One exec'ing test, and only one.** Tests under `internal/` never start a process; the run seam exists so they need not. The wiring in `cmd/auth` (D01's run with no command, R-LPHC-TKY0) can be proved no other way, so exactly one kind of test execs the binary, and it lives in `cmd/auth`. It builds the binary into a temporary directory and runs it with `--version`, with `manifest`, with a bogus command, and bare with the three Google settings set and no socket (exit 2). For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's, sets `NOTIFY_SOCKET` to a datagram socket it bound, and waits for `READY=1`. Then it sends `SIGTERM` and asserts exit 0, silence on both streams, that the socket path still exists and accepts a connection into its queue after the child has exited, and that the working directory holds nothing but `state/`, `state/auth.db` and SQLite's `auth.db-journal`, `auth.db-wal` and `auth.db-shm`. It runs the serve case a second time with a fresh socket and `SIGINT`, asserting the same, because `main` promises both signals. The child runs in a test-owned temporary working directory, offline, with an environment the test composes: the three Google settings set to placeholders (readiness is the datagram, not a login), and `IKIGENBA_SERVICES` naming a services file the test wrote, holding a `telemetry` entry (all six members) whose `socket` is a Unix socket in a short temporary directory outside the child's working directory, on which the test serves appkit's `telemetry.IngestHandler` in process over a sink that records each event and signals a channel. The child delivers its trail there, so its stderr stays empty, and the test reads it after the child exits: `service.started` first, carrying the `Version` `internal/version` declares, and `service.stopping` last, with `reason` `SIGTERM` or `SIGINT`. In the first serve case the services file also lists a service with an icon, and before starting the child the test seeds `state/auth.db` with a user and a live session: `db.Open` with `auth.Migrations()`, a store over that handle with `store.New`, and the handle closed before the child starts. Once its sink has received `service.started` (a channel receive, not a sleep), the test atomically replaces the services file with one identical but for the `telemetry` socket, which names a second socket it serves the same way (the banner reads the file afresh per page, so the icon entry stays), and makes one request of the child, `GET /` with that session's cookie over the socket. It asserts the page carries the launcher and ends with the footer reading `auth` and that `Version`, proving `main` handed appkit's kit with auth's version to the server, and that the request's events and `service.stopping` reached the second socket and not the first, proving `main` handed `Run` appkit's socket sink, which reads the services file at each delivery. Any other test that builds, execs, waits on or signals a process is a bug.

## Live tests

A live test is the only kind that reaches an external service. It is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions, which `go test ./...` never runs and `make live` runs as `go test -tags live -count=1 -run '^TestLive' ./...`. It proves lightly that the whole is glued together, about one per external service, leaving behavior, edge cases and error paths to the unit tests; it carries the id it proves and the gap counts it; the code under test runs locally and only the external service is real; it reads credentials from the environment, fails rather than skips when one is missing, and no credential appears in the repository or in test output. It runs only as gate 6.

## Gates

Run from `auth/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/auth`, the release build, static and cgo-free, which is why the SQLite driver must be pure Go
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)
6. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

Gate 5 flags an `http.Server` without `ReadHeaderTimeout` (`gosec` G112); it is fixed in code, never suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` (the default) builds `bin/auth`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly, and the exec'ing test builds its own binary.

## Deploy

Deploy machinery, the version bump, tags and `devctl build`/`devctl deploy`, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

auth is an app, not a self-installing CLI: `devctl` builds it into a release tarball and pushes it to a space's host, where `opsctl install` installs it.

1. Set the version in `internal/version/version.go` (D01) to `vX.Y.Z`; the binary reports it verbatim and the deploy refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `auth/vX.Y.Z` and push the tag.
4. `devctl build auth` at that tag writes `auth/dist/auth-vX.Y.Z.tar.xz` holding `bin/auth`, `etc/` and `share/icon.svg`, with no version recorded inside; it refuses a binary whose `manifest` disagrees with the committed `etc/manifest.toml`.
5. `devctl deploy <space> auth/dist/auth-vX.Y.Z.tar.xz` uploads it to the space's `deploy/` prefix and runs `opsctl install` over ssh. The host writes `etc/env` with the space's `DRAIN_SECONDS`, replaces the release, publishes `ikigenba-auth.socket` (`/run/ikigenba/auth.sock`) and the `Type=notify` `ikigenba-auth.service`, regenerates nginx and litestream, and restarts the service alone. The socket stays up, so requests, the `/check` subrequests for every other app among them, queue on it across the restart.

`auth --version` then prints `vX.Y.Z`, and `devctl space status <space>` reports it.
