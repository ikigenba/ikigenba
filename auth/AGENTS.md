# auth

auth signs users in and answers nginx's identity subrequest for every other app. One Go binary serves it on the socket it is passed as descriptor 3, behind the host's nginx; users sign in through Google's OIDC issuer, and users, sessions and API tokens live in its own SQLite database. In a sandbox it runs behind the sandbox's nginx, which sets `IKIGENBA_PUBLIC_URL` and `IKIGENBA_CALLBACK_URL` (D03); a host sets neither. It is built on appkit for pages, identity and telemetry. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `share/icon.svg` is the launcher icon. The build run never writes it; an agent changes it only on explicit, direct instruction from a human.
- `cmd/auth` is the binary. `internal/` is everything else, one package per concern.
- `etc/` is what the host needs: `manifest.toml`.
- The build run writes the Go source, the tests, `go.mod`, `go.sum` and `etc/`. `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

auth holds no stylesheet, fonts or licences; appkit's `page` package serves them. `share/icon.svg` is the Tabler outline `fingerprint` from `design/ikigenba/icons/tabler/`, stripped as `design/README.md` asks of a launcher icon. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The release build is cgo-free (gate 3).
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline: appkit at the release `go.mod` requires (see Adopting appkit), `github.com/coreos/go-oidc/v3` and `golang.org/x/oauth2` for the Google sign-in, and `modernc.org/sqlite`, the pure-Go SQLite driver the cgo-free release build depends on.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- A POSIX shell at `/bin/sh`, for the one exec'ing test.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs human approval. Release selection is `go.mod`'s job.

### Adopting appkit

appkit is released from this repository. Until its tag `appkit/<version>` is on origin, neither the module proxy nor the checksum database knows it, so the machine that builds auth tags the release commit locally (`git tag appkit/<version> <commit>`), seeds the module cache once:

```
GOPROXY=direct GONOSUMDB=github.com/ikigenba/ikigenba GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=url./mnt/projects/ikigenba.insteadOf GIT_CONFIG_VALUE_0=https://github.com/ikigenba/ikigenba go mod download github.com/ikigenba/ikigenba/appkit@<version>
```

and sets the requirement with `GONOSUMDB=github.com/ikigenba/ikigenba go get github.com/ikigenba/ikigenba/appkit@<version>`, with that `GONOSUMDB` exported in the build run's shell. The tag must be pushed before any other machine builds auth.

## Test files

The test files are every `*_test.go` in the module: `cmd/auth` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` holds no `*_test.go`.

**No id-shaped literal in a fixture.** No auth value has a requirement tag's shape: user, session and login-state ids are 26 Crockford base32 chars (`internal/idcodec`, no hyphen), token ids `tok_` plus 26, token secrets `ikp_` plus 52, stored secret hashes 64 lowercase hex chars, and request ids 32 lowercase hex chars. No auth literal can be mistaken for a tag.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state. The gates run offline as an ordinary user with no systemd.

**Every input comes through the run seam.** `cli.Process` (D01) carries `Args`, `LookupEnv`, `Unsetenv`, `Pid`, `Stdout`, `Stderr`, `Inherit`, `Now`, `Rand`, `OIDCIssuer`, `DBSource`, `Banner` and `Sink`, and tests supply each one. A test never reads or changes the real environment, clock or randomness, and touches no filesystem or network state outside the fixtures below. A test whose result depends on the developer's machine, wall clock, environment or a port in use is a bug.

**Google is faked, never called.** auth reaches Google's OIDC issuer only through `Process.OIDCIssuer`. Tests stand up a loopback issuer (discovery document, keys and token endpoint on `127.0.0.1:0`) and inject its URL, so no test reaches `accounts.google.com`.

**The database is an isolated source.** A test passes `Process.DBSource` a database path or file-backed SQLite DSN inside its own temporary directory, or an in-memory DSN. Filesystem fixtures live in that tree: an absent database parent, a regular file obstructing it, a file at the path that is not a database, or a database file with write permission removed (the gates run as an ordinary user, so it holds); nothing touches `/opt/auth` or a shared file. A mid-request database failure (D03's `500`) is produced by breaking the test's own database after the server has it, for example by closing the `*store.Store` a server-level test handed `server.New`. A store test may build the old-shape fixture for D04's token-id migration in its own tree: open a store with `store.Open`, create a token with `CreateToken`, close it, strip the `tok_` prefix from that id wherever the database stores it (tables and columns found through `sqlite_master`) through the SQLite driver, and open the store again.

**The clock is injected.** `Process.Now` supplies every timestamp. Session idle (15m) and cap (18h), the 30d token login window and token expiry (30d/90d/365d) are tested by advancing what `Now` returns. A test that sleeps to age a session or token is a bug.

**Randomness is injected.** `Process.Rand` supplies the bytes `idcodec` mints ids and secrets from, so values under test are reproducible.

**The banner source is the test's own.** `Process.Banner` and `server.Config.Banner` are a function the test writes, returning the services the case needs (D01, D05); no test calls `page.New`, which reads `IKIGENBA_SERVICES` from the real environment. A test may call appkit's other exported functions, to render the banner it expects, say.

**The trail is captured, never sent.** Events reach a test through a sink it supplies, never a socket (the exec'ing test excepted), and `Process.Sink` is never nil. A `cli.Run`-level test passes `&telemetry.Capture{}` as `Process.Sink` and reads `Capture.Events` after `Run` returns, or a sink of its own: one returning an error wrapping `telemetry.ErrRejected`, so each event goes to `Stderr` at once with no retry or pause, or one holding `Deliver` until its context is done, for the drain-window test. A `server.New`-level test builds its own writer (`telemetry.New` with `Service` `auth`, a `*telemetry.Capture` sink, its own `Now` and `Rand`, and a buffer as `Stderr`), passes it as `server.Config.Telemetry`, drives the handler, calls `Writer.Flush`, asserts on `Capture.Events`, and calls `Writer.Shutdown` before it ends. A test that wants a particular stop reason cancels `Run`'s context with `context.WithCancelCause`.

**stderr is asserted only for trouble.** A handled failure, a 500 from a failed store or a 502 from a failed Google, is asserted through the `request.finished` status, the domain events, and `Stderr` staying empty. Exact `Stderr` bytes are asserted only for a condition auth cannot continue from (a refused start, a database that will not open, a failed `READY=1`, a failed or overrun `Serve`) and for the writer's `auth: undelivered event: ` lines.

**No test runs the page's scripts.** The pages carry the Copy button's inline script and, when there are services, appkit's launcher script. The gates have no browser or JavaScript engine, and adding one is an unapproved dependency, so a test asserts what a response body carries, never what a script would do with it.

**No test reads the checkout.** A test opens no file of this module, not source, `go.mod`, `go.sum`, `etc/` or `share/`, walks no directory, and never parses or reflects over code. It proves structure by use: importing the package, constructing the type, calling the function, or running the built binary in the one exec'ing test.

**auth binds nothing; a test makes its listener.** auth serves on the listener it is passed (D01, D03). A test makes its own, a loopback TCP listener on `127.0.0.1:0` or a Unix socket in a temporary directory, never a fixed port. A `cli.Run`-level serve test hands it to `Run` through `Process.Inherit`, sets `LISTEN_PID` to the `Process.Pid` it chose and `LISTEN_FDS` to `1` in the environment map, alongside the three Google settings, and records `Unsetenv` calls with a function of its own; it never leaves `Inherit` nil, which would take the test process's real descriptor 3. Failure paths inject an `Inherit` that errors or a listener whose `Accept` fails. A test learns `Run` is serving the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp`, since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test. It stops `Run` by cancelling the context, never by a signal.

**The drain tests wait on the clock, and only they do**, because the drain deadline is the behavior, and it also bounds how long auth waits to deliver its last events. The one `Run`-level drain-window test sets `DRAIN_SECONDS=1`, passes a sink whose `Deliver` returns only when its context is done, cancels `Run` with no request in flight, and asserts `Run` returns `0` with `service.stopping` written to `Stderr` as undelivered, within the drain plus a margin of real time. A `Serve`-level test passes a drain of a few milliseconds and a handler that blocks on a channel. The one `Run`-level overrun test sets `DRAIN_SECONDS=1` and holds a sign-in callback open inside the fake Google: it starts `GET /login/google`, reads `state` from the `Location`, requests `/login/google/callback` with it while the fake token endpoint blocks, cancels the context once that endpoint has received the exchange (never after a sleep), and releases the endpoint after `Run` has returned.

**One exec'ing test, and only one.** Tests under `internal/` never start a process; the run seam exists so they need not. The wiring in `cmd/auth` (D01's run with no command, R-AUXO-9DST) can be proved no other way, so exactly one kind of test execs the binary, and it lives in `cmd/auth`. It builds the binary into a temporary directory and runs it with `--version`, with `manifest`, with a bogus command, and bare with the three Google settings set and no socket (exit 2). For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's, sets `NOTIFY_SOCKET` to a datagram socket it bound, and waits for `READY=1`. Then it sends `SIGTERM` and asserts exit 0, silence on both streams, that the socket path still exists and accepts a connection into its queue after the child has exited, and that the working directory holds nothing but `state/`, `state/auth.db` and SQLite's `auth.db-journal`, `auth.db-wal` and `auth.db-shm`. It runs the serve case a second time with a fresh socket and `SIGINT`, asserting the same, because `main` promises both signals. The child runs in a test-owned temporary working directory, offline, with an environment the test composes: the three Google settings set to placeholders (readiness is the datagram, not a login), and `IKIGENBA_SERVICES` naming a services file the test wrote, holding a `telemetry` entry (all six members) whose `socket` is a Unix socket in a short temporary directory outside the child's working directory, on which the test serves appkit's `telemetry.IngestHandler` in process over a sink that records each event and signals a channel. The child delivers its trail there, so its stderr stays empty, and the test reads it after the child exits: `service.started` first, carrying the `Version` `internal/version` declares, and `service.stopping` last, with `reason` `SIGTERM` or `SIGINT`. In the first serve case the services file also lists a service with an icon, and before starting the child the test seeds `state/auth.db` with a user and a live session through `internal/store`. Once its sink has received `service.started` (a channel receive, not a sleep), the test atomically replaces the services file with one identical but for the `telemetry` socket, which names a second socket it serves the same way (the banner reads the file afresh per page, so the icon entry stays), and makes one request of the child, `GET /` with that session's cookie over the socket. It asserts the page carries the launcher and ends with the footer reading `auth` and that `Version`, proving `main` handed appkit's kit with auth's version to the server, and that the request's events and `service.stopping` reached the second socket and not the first, proving `main` handed `Run` appkit's socket sink, which reads the services file at each delivery. Any other test that builds, execs, waits on or signals a process is a bug.

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
