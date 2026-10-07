# events

An app of the Ikigenba platform: the suite's internal event bus, served at
`events.<host>`. Services emit events to it and it delivers them to the
services that accept them. One Go binary serves on the listening socket it is
passed as descriptor 3 (`/run/ikigenba/events.sock` on a host), behind the
host's nginx. events keeps the retained log of events and each subscriber's
place in it in its own SQLite database, `state/events.db` resolved against the
working directory and opened through appkit's `db` package, of which it is the
only writer. A service emits to events at `/emit`, a path reached over the
Unix socket only, never through nginx; events delivers each event to the
`/events` socket path of every service that accepts it. Every page and `/mcp`
is for a signed-in user of the space; nginx lets no guest through. At `/mcp`
it offers five MCP tools, which agents reach through the MCP gateway:
`catalog`, `search`, `subscribers`, `skip` and `resume`. At `/` it
serves a landing page of its subscribers and links to an about screen. On a
host it runs as `/opt/events/bin/events` with `/opt/events` as its working
directory and its environment from `/opt/events/etc/env`; a developer runs the
same binary from the checkout. The module path is
`github.com/ikigenba/ikigenba/events`. It requires appkit
(`github.com/ikigenba/ikigenba/appkit`) and uses appkit's packages `page` (the
banner, launcher and footer, and the shared static files under `/_appkit/`),
`identity` (the caller nginx authenticated, required on every route nginx
serves), `mcp` (the server mounted at `/mcp`, and the client the tests drive
it with), `telemetry` (the event contract, the request middleware, and the
writer events' own telemetry goes through), `db` (the log's handle and its
migrations) and `events` (the suite's event contract that emitters and
subscribers share). The contract is the documents in `specs/design/`. This
file restates none of it.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, the log's migrations under `migrations/`, which the root package
embeds, and everything under `etc/` (`etc/manifest.toml`, and the nginx
fragment `etc/nginx.conf`). It never writes `assets/`, `share/`, this file,
the `Makefile` or `.golangci.yml`. `state/` is where a running events keeps
`events.db`; it is created at run time and never committed. See the `spec`
and `build-spec` skills. Everything below is what the build run computes the
gap and runs the gates against; it is written outside the run and read-only
to it.

## Assets

`assets/` holds events' markup: the Go `html/template` files design names,
among them the landing page, the about screen, the not-found page and the
unavailable page. Each opens with a comment naming the data it receives and
the hooks it emits. Every page is shown only to a signed-in user. The landing
and about pages carry appkit's banner; the not-found and unavailable pages
carry the footer only and never the banner. They are written by the user or
the delivering agent, following the repository's `design/`, and are inputs to
the spec: the build run reads them and never writes them. The code parses
them into the set appkit's `page.Templates` returns and executes them by
template name; it never writes markup of its own, not even a fragment or an
error page. Go's `embed` reaches only files at or below the embedding
package's directory, so the module's root package (the directory holding
`go.mod`) embeds `assets/`, and design names what it exports. Design names
each template, the data it receives, and the hooks it emits; the tests assert
on those hooks and on visible text, never on layout. A needed template that
is missing or wrong, a state a story names that the templates cannot show, or
a hook design names that the templates lack is an issue: the run files it in
`specs/issues/` and never edits the asset to close it.

events holds no copy of the stylesheet, fonts, or licences; appkit's `page`
package embeds and serves them. `share/icon.svg` is events' icon in the
service launcher: the Tabler outline `broadcast` (`@tabler/icons` 3.48.0, the
release `design/ikigenba/icons/tabler/` records), without its class, width,
height, or invisible bounding path, as `design/README.md` asks of a launcher
icon. The build run never writes it. `devctl build` packs it beside `bin/`
and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is
  committed, and the gates themselves run offline. `go.mod` starts with no
  requirement; the build run sets each one and its `go.sum` lines, and moves
  to another release only when this file names one:
  - appkit `v0.14.0`, set with
    `go get github.com/ikigenba/ikigenba/appkit@v0.14.0`: a release that
    exports the `db` and `events` packages. It and the modules it pulls in
    are events' only dependencies. See Adopting appkit below.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is appkit's.** events stores its log in SQLite through
appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so the
release build stays cgo-free (gate 3), and already approved for the
repository. events neither requires nor imports it directly. Adding any other
external dependency needs approval first, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, `appkit/<version>`
on origin (see the root `AGENTS.md`); the build run sets it with
`go get github.com/ikigenba/ikigenba/appkit@<version>`.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/events` and everything under `internal/`. This is the file set
the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. events echoes back what callers give it —
service and event names, an event's attributes, user and request ids, an
error a subscriber returned, header values — so a test that sends an
id-shaped value lands one in the test file. No fixture carries one: not a
service or event name, an attribute, a user or request id, a cause, a
subscriber's answer, a tool argument, a header value, or an expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something. Clocks are injected: a test that opens a
  log hands it a `Now` it controls.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**The run seam carries the process.** events binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal, the
pid, the inherited listener, the output streams, the clock, the sleep, the
random source and the working directory come in through the run seam design
declares, and tests inject them. A `Run`-level test sets `Dir` to a temporary
directory of its own, so the log lands there and never in the checkout. A
test never reads or changes the real environment, clock, or randomness, and
never leaves the inherited-listener step unset, since that would take the
test process's real descriptor 3. The one exception is the working
directory: a test that proves what an empty `Dir` or the root package's
`Assets`, `Etc` or `Migrations` does whatever the working directory is may
`t.Chdir` into a temporary directory of its own, and such a test does not
call `t.Parallel`. A test learns that the server is ready the way systemd
does: it binds a Unix datagram socket in a short temporary directory
(`os.MkdirTemp("", ...)`, since a Unix socket path is limited to 108 bytes
and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for
`READY=1` with a deadline that fails the test. Drain tests are the one place
a test waits on the clock, because the drain deadline is the behavior, and
they keep that wait to a few seconds. The gates run offline as an ordinary
user, with no systemd.

**The state is an isolated directory.** Each test gives events a working
directory inside a test-owned temporary directory, so the log is the test's
own. A test that needs a store opens its own log with appkit's `db.Open` at a
path in its own temporary directory, with `events.Migrations()` and a clock
it controls, closes the handle when it ends, and builds the store over that
handle with `store.New`. Tests may create filesystem fixtures inside that
temporary tree (an absent parent, a regular file named `state`, a file at
`state/events.db` that is not a database, a log a test's own `db.Open` made
and then changed through `DB.Write`); nothing touches `/opt/events`, the
checkout's `state/` or a shared file. A store failure is provoked with
`SetFailing(true)` on the handle the store the test handed the server was
built over, before a call or from a hook design names, never by corrupting
the file, removing permissions or closing the store. Tests prove events' use
of the log, its schema, its store and its wiring, never SQLite's own
guarantees (atomicity, durability, locking) and never appkit's `db` contract
(opening, migrations, transactions, `db status`), which appkit's own tests
prove.

**Emitters and subscribers are the test's own servers.** events takes events
at `/emit` over its Unix socket and delivers them to other services'
`/events` socket paths. A test drives both ends with `httptest` servers
listening on Unix sockets it binds in a short temporary directory
(`os.MkdirTemp("", ...)`, every socket path under 107 bytes), named to events
through a services file the test wrote or the seam design names: it posts to
`/emit` as an emitter would, and stands in for each subscriber with a handler
that records what arrives and answers as the case requires (accepts, fails,
stalls until the test releases it, or is gone). No test reaches a real
service, and no test reaches `/emit` or `/events` over TCP.

**Retries and schedules are driven, not waited out.** Every deadline, retry
and backoff is proved through the injected clock, sleep and the seams design
names, never by waiting real seconds; a deadline passes because the test
advances the clock or fires the timer design names. A client that goes away
is a request the test cancels or a connection it closes. A test that sleeps
to age a wait is a bug. A test waits for a delivery it expects with a
deadline that fails the test.

**Events come from the test's own sink.** Where a test observes the telemetry
events records, it hands events' writer appkit's `telemetry.Capture`, or a
services file whose `telemetry` entry names a Unix socket the test holds, as
design names. Every test builds its own log and subscribers; no test depends
on state another test made or on the order the tests run in.

**One environment variable, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read events cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through an events constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. Such a test does not call
`t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity.Require`
wraps every route nginx serves, so a request reaches such a path only with an
`X-User-Id` header. A test sets the identity headers to be a signed-in user,
or omits `X-User-Id` to exercise the missing-identity answer, and sets `Host`
and `X-Forwarded-Proto` the way nginx does, since the absolute URLs events
builds depend on them. What appkit's middleware and writer do is appkit's
contract; events' tests prove only that its handlers are wired to them, by
use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing
identity header, `server/discover`), never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns.
events' tests never re-prove appkit's transport.

**No test runs the page's scripts.** The pages carry appkit's launcher script
when there are services, and every page carries appkit's feedback script.
The gates have no browser and no JavaScript engine, and adding one is an
external dependency no one has approved, so a test asserts what a response
body carries and never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. It proves what the design
declares by using it. Handing `cmd/events` to `go build` is not the test
reading it.

**One exec'ing test of the binary, and only one.** No test starts a process:
tests under `internal/` never start events. The wiring in `cmd/events` can be
proved no other way, so exactly one test that execs the binary is
admissible, and it lives in `cmd/events`. It builds the binary into a
temporary directory and runs it with `--version`, with `manifest`, with
`bogus`, and bare with no socket passed in. For the serve case it stands in
for systemd: it makes a Unix socket in a short temporary directory, passes it
as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child
through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates
`state/events.db`. Its environment is one the test composes, never the
developer's. The test waits for `READY=1`, makes the requests design names
for the binary, over the socket, then stops the child with `SIGTERM`, and in
a second run with `SIGINT`, asserting what design states. Any other test that
builds, execs, waits on, or signals a process is a bug.

## Live tests

events calls no external service: its callers and subscribers are browsers,
agents and sibling services on the same host, which the unit tests stand in
for, so it has no live tests. Should a design ever call for one, it is a
`*_live_test.go` file guarded by `//go:build live` with test functions named
`TestLive*`; it proves lightly that the whole is glued together, carries the
requirement id it proves, reads its credentials from the environment, fails
(never skips) when one is missing, and runs only as gate 6.

## Gates

Run from this directory (`events/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/events`
   — the release build, static and cgo-free, the way `devctl build` builds
   it, which is why the SQLite driver must be pure Go
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`
   — the cache lives in this worktree's git directory, so worktrees never
   share it, and `--allow-parallel-runners` lets gates for several
   sub-projects lint at once; `make lint` runs this form
6. `make live` — **conditional**: run only when the phase's diff (the working
   tree against the last phase commit) adds or modifies a `*_live_test.go`
   file; otherwise it is not run and not counted. When it applies and a
   credential is absent, that is a missing tool: file an issue, do not pass or
   skip.

Gate 5 also flags an undocumented `package main` (`revive`) and an
`http.Server` without `ReadHeaderTimeout` (`gosec` G112); both are fixed in
code, never suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Build

`make` builds `bin/events` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

Deploy machinery — the version bump, tags, and the `devctl build`/`devctl
deploy` steps — is hand-maintained infrastructure outside the spec system: the
build run never reads, edits, or tests it.

events is an app, not a self-installing CLI: `devctl` builds it into a release
tarball and pushes it to a space's host, where `opsctl install` installs it.

1. Set the version literal design declares to `vX.Y.Z`. The binary reports it
   verbatim, and the deploy refuses a tag that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `events/vX.Y.Z` and push the tag.
4. `devctl build events` at that tag writes
   `events/dist/events-vX.Y.Z.tar.xz`, holding `bin/events`, `etc/`, and
   `share/icon.svg`; the version is in the file's name and in the binary,
   never in a member's path. It refuses a binary whose `manifest` disagrees
   with the committed `etc/manifest.toml`.
5. `devctl deploy <space> events/dist/events-vX.Y.Z.tar.xz` uploads the
   tarball to the space's `deploy/` prefix and runs `opsctl install` over ssh;
   the host fetches it, writes `etc/env` (the manifest's `[env]` defaults and
   the space's `DRAIN_SECONDS`), replaces the release, publishes
   `ikigenba-events.socket` (the Unix socket `/run/ikigenba/events.sock`)
   and the `Type=notify` `ikigenba-events.service`, regenerates the host's nginx and litestream
   configuration,
   and restarts the service alone. The database is kept across releases.

`events --version` then prints `vX.Y.Z`, and `devctl space status <space>`
reports it.
