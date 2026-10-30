# home

An app of the Ikigenba platform: the suite's front door, served at
`home.<host>` and, as the manifest's default app, at the space host itself.
One Go binary serves on the listening socket it is passed as descriptor 3
(`/run/ikigenba/home.sock` on a host), behind the host's nginx. home shows
a signed-in user their own bookmarks and every service on the space: at `/`
it serves a landing page of that user's bookmarks, in the user's order, and
of the services the host's services file names, as tiles, at `/bookmarks` a
page where a signed-in user adds, orders and removes their own bookmarks, and
it links to an about screen at `/about`. It is the home the banner's product
mark links to on every app's pages. home keeps each user's bookmarks, each
with the icon fetched from the bookmarked site when it was added, in its own
SQLite database, `state/home.db` resolved against the
working directory and opened through appkit's `db` package, of which it is the
only writer. It offers no MCP tools, neither emits nor accepts an event on the
suite's event bus, and records in the trail, beyond appkit's request and
service events, only its own `bookmark.created`, `bookmark.moved` and
`bookmark.deleted`. Every
page is for a signed-in user of the space; nginx lets no guest through. On a
host it runs as `/opt/home/bin/home` with `/opt/home` as its working directory
and its environment from `/opt/home/etc/env`; a developer runs the same binary
from the checkout. The module path is `github.com/ikigenba/ikigenba/home`. It
requires appkit (`github.com/ikigenba/ikigenba/appkit`) and uses appkit's
packages `page` (the banner and footer, and the shared static files under
`/_appkit/`), `services` (the services
file, read afresh for auth's links and the landing page's tiles), `version`
(the identity `main` reads), `identity` (the caller
nginx authenticated, required on every route), `telemetry` (the event
contract, the request middleware, and the writer home's events go through),
and `db` (the bookmarks' handle and its migrations). It requires
`golang.org/x/net` and uses its package `html` (the tokenizer that finds a
page's icon links).
The contract is the documents in `specs/design/`. This file restates none of
it. home is an app, so it has no stories: its intent is the decisions document
that delivered it, or what was agreed in conversation, together with its
templates under `assets/`, and the design is the record.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, the migrations under `migrations/`, which the root package embeds,
and everything under `etc/` (`etc/manifest.toml`, and the nginx fragment
`etc/nginx.conf` if design names one). It never writes `assets/`, `share/`,
this `AGENTS.md`, the `Makefile` or `.golangci.yml`. `state/` is where a
running home keeps `home.db`; it is created at run time and never committed.
See the `spec` and `build-spec` skills. Everything below is what the build run
computes the gap and runs the gates against; it is written outside the run and
read-only to it.

## Layout

- `specs/` is the contract: `design/`.
- `assets/` is the page markup, and `share/icon.svg` the service's icon, shown
in the banner's trail and on home. The
  build run never writes them; the user or the delivering agent changes them.
- The root package, in the directory holding `go.mod`, embeds `assets/`,
  `etc/` and `migrations/`: `embed.go` embeds the four templates
  `landing.html`, `about.html`, `notfound.html` and `bookmarks.html`, and
  exports them as `Assets()`, `manifest.toml` as `Etc()` and the migrations as
  `Migrations()`. `cmd/home` is the binary. `internal/` is everything else,
  one package per concern:
  - `internal/cli`: the run seam, the commands and the serve path.
  - `internal/server`: a listener's life, serving and draining.
  - `internal/pages`: the whole HTTP surface, the bookmarks routes included.
  - `internal/bookmarks`: the store over the database.
  - `internal/favicon`: the fetch of a bookmark's icon.
- `migrations/` is the database's schema, one file per migration named with a
  four-digit version: `0001_bookmarks.sql` and `0002_bookmark_order.sql`.
- `etc/` is what the host needs: `manifest.toml`.

## Assets

`assets/` holds home's markup: Go `html/template` files, each opening with a
comment naming the data it receives. Every page is shown only to a signed-in
user. They are written by the user or the delivering agent, following the
repository's `design/`, and are inputs to the spec: the build run reads them
and never writes them. The code parses them into the set appkit's
`page.Templates` returns and executes them by template name; it never writes
markup of its own, not even a fragment or an error page. Go's `embed` reaches
only files at or below the embedding package's directory, so the module's root
package (the directory holding `go.mod`) embeds `assets/`, and design names
what it exports.

Every word a person or an agent reads, and every class, id and attribute,
lives in exactly one place, a template under `assets/` or a named copy constant
in the source; a test and a requirement never spell one. Design names each
template and the data it receives, never its text, hooks, markup or styles. A
test proves a page by executing the named template with the data the design
says and comparing, or by checking that a value the test supplied appears in
the body; it never looks for a word or a tag. A change to copy or markup is an
edit to the asset alone. A needed template that is missing or wrong, or a
template that cannot show a state the design names, is an issue: the run files
it in `specs/issues/` and never edits the asset to close it.

home holds no copy of the stylesheet, fonts, or licences; appkit's `page`
package embeds and serves them. `share/icon.svg` is home's icon, shown in the
banner's trail and on home: the Tabler outline `grid-dots` from
`design/ikigenba/icons/tabler/`,
without its class, width, height, or invisible bounding path, as
`design/README.md` asks of a service icon. The build run never writes it.
`devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is
  committed, and the gates themselves run offline. `go.mod` starts with no
  requirement; the build run sets each one and its `go.sum` lines, and moves
  to another release only when this file names one:
  - appkit `v0.32.0`, set with
    `go get github.com/ikigenba/ikigenba/appkit@v0.32.0`: a release that
    exports the `db` and `version` packages and whose `page.Banner` carries
    the home link, the tools flag, the trail's levels, and `Release` and
    `Commit` but no service launcher. See Adopting appkit below.
  - `golang.org/x/net`, used for its `html` package only, the tokenizer
    `internal/favicon` reads a page's icon links with. Its release is the one
    current when the build run sets it, with `go get golang.org/x/net@latest`;
    this file names none. It, appkit and the modules they pull in are home's
    only dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is appkit's.** home stores its bookmarks in SQLite
through appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so
the release build stays cgo-free (gate 3), and already approved for the
repository. home neither requires nor imports it directly. `golang.org/x/net`
was approved by the decisions document that delivered bookmarks. Adding any
other external dependency needs approval first, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.32.0`, fetched
through the ordinary module proxy and checked against the checksum database
(see the root `AGENTS.md`); the build run sets it with
`go get github.com/ikigenba/ikigenba/appkit@v0.32.0`. No `replace` directive,
no `go.work`, no local module cache stands in for it.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/home` and everything under `internal/`. This is the file set
the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. home echoes back what its services file, its
callers and the sites it fetches give it — service names, URLs, descriptions,
header values, bookmark names, bookmark URLs, a fake site's answers — so a
test that sends an id-shaped value lands one in the test file. No fixture carries
one: not a service name, a URL, a header value, an event's attribute, or an
expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something. Clocks are injected: a test that opens a
  database hands it a `Now` it controls.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**The run seam carries the process.** home binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal, the
pid, the inherited listener, the output streams, the clock, the random source,
the working directory and the HTTP client every request of a favicon fetch
goes through come in through the run seam design declares (`cli.Run` with
`cli.Process`), and tests inject them. A `Run`-level test sets the version
string to one of its own and `Dir` to a temporary directory of its own, so
the database lands there and never in the checkout. A test never reads or
changes the real environment, clock, or randomness, and never leaves the
inherited-listener step unset, since that would take the test process's real
descriptor 3. The working directory is one exception: a test that proves what
an empty `Dir`, or the root package's `Assets`, `Etc` or `Migrations` does
whatever the working directory is may `t.Chdir` into a temporary directory of
its own, and such a test does not call `t.Parallel`. The favicon fetch's
budget is the other: a test of the favicon fetch's budget may read the real
clock around the call, both to build a caller deadline that lies between now
and now plus the budget and to check the deadline the fake transport observes
on the request's context; it never waits on either and nothing sleeps. A test
learns that the server is ready the way systemd
does: it binds a Unix datagram socket in a short temporary directory
(`os.MkdirTemp("", ...)`, since a Unix socket path is limited to 108 bytes and
`t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for
`READY=1` with a deadline that fails the test. Drain tests are the one place a
test waits on the clock, because the drain deadline is the behavior, and they
keep that wait to a few seconds. The gates run offline as an ordinary user,
with no systemd.

**The sites are faked, never called.** No test reaches a bookmarked site or
any other host. A `Run`-level test hands `Process.HTTP` a client whose
transport answers every request as a fake site does, and a test of
`internal/favicon` or of the bookmarks routes hands `favicon.Config.HTTP` such
a client, as design names. The fake answers with the pages, redirects, icons
and errors the test chooses, and records each request it is given, so a test
proves a request by what the fake received. The exec'ing test never lets the
binary fetch, since nothing it asks adds a bookmark.

**The state is an isolated directory.** Each test gives home a `Dir` inside a
test-owned temporary directory, so the database is the test's own. A test
that needs a store opens its own database with appkit's `db.Open` at a path
in its own temporary directory, with `home.Migrations()` and a clock it
controls, closes the handle when it ends, and builds the store over that
handle with `bookmarks.New`. Tests may create filesystem fixtures inside that
temporary tree (a regular file named `state`, a file at `state/home.db` that
is not a database, a database a test's own `db.Open` made and then changed
through `DB.Write`); nothing touches `/opt/home`, the checkout's `state/` or a
shared file. A database failure is provoked with `SetFailing(true)` on the
handle the store the test handed the server was built over, before a call or
from inside the fake transport, as design names, never by corrupting the
file, removing permissions or closing the store. Tests prove home's use of the
database, its schema, its store and its wiring, never SQLite's own guarantees
(atomicity, durability, locking) and never appkit's `db` contract (opening,
migrations, transactions, `db status`), which appkit's own tests prove. Every
test builds its own database.

**The services file is the test's own.** A test that needs services writes a
services file in its own temporary directory, with whatever entries, icons
and flags the case needs, and names it as design says; nothing reads the
host's services file or the developer's. Every test builds its own; no test
depends on state another test made or on the order the tests run in.

**Nothing is waited out.** Deadlines are proved through the seams design
names, never by waiting real seconds; the favicon fetch's budget is proved
through the deadline the fake transport reads on a request's context and a
fake that answers `context.DeadlineExceeded`. A client that goes away is a request the
test cancels or a connection it closes. A test that sleeps to age a wait is a
bug. A test waits for something it started to finish with a deadline that
fails the test.

**Events come from the test's own sink.** Where a test observes the events
home records, it hands home's writer appkit's `telemetry.Capture`, or a
services file whose `telemetry` entry names a Unix socket the test holds, as
design names. A sink that rejects every event returns an error wrapping
`telemetry.ErrRejected`, so nothing is retried.

**Environment variables, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read home cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a home constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. It is the only variable an
in-process test sets for home's own code. The exec'ing test also sets
`IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `testing.T.Setenv`, so it may call
appkit's `version.Read()` and `version.Display()` under the same two values it
composes into the child's
environment; the commit it sets is longer than seven characters, so a `main`
that skipped shortening would fail. Any test
that sets a variable with `testing.T.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity.Require`
wraps every route, so a request reaches any path only with an `X-User-Id`
header. A test sets the identity headers to be a signed-in user, or omits
`X-User-Id` to exercise the missing-identity answer, and sets `Host` and
`X-Forwarded-Proto` the way nginx does, since the absolute URLs home builds
depend on them. What appkit's middleware and writer do is appkit's contract;
home's tests prove only that its handlers are wired to them, by use.

**No test runs the page's scripts.** Every page carries appkit's feedback
script.
The gates have no browser and no JavaScript engine, and adding one is an
external dependency no one has approved, so a test asserts what a response
body carries and never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. It proves what the design
declares by using it. Handing `cmd/home` to `go build` is not the test
reading it.

**One exec'ing test of the binary, and only one.** No test starts a process:
tests under `internal/` never start home. The wiring in `cmd/home` can be
proved no other way, so exactly one test that execs the binary is admissible,
and it lives in `cmd/home`. It builds the binary into a temporary directory and
runs it with `--version`, with `manifest`, with `bogus`, and bare with no
socket passed in. For the serve case it stands in for systemd: it makes a Unix
socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]`
(descriptor 3 in the child), and starts the child through
`/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates
`state/home.db`. Its environment is one
the test composes, never the developer's, with non-empty `IKIGENBA_COMMIT` and
`IKIGENBA_RELEASE`; the test computes the release and commit it expects with
`version.Read()` and the display string with `version.Display()` after setting
the same two values with `t.Setenv`, so no
test spells a version. Separately it runs `--version` with neither variable in
the child's environment and expects exactly one empty line. For each serving run
the test waits for `READY=1`, makes the requests design names for that run
over the socket, and stops the child with a signal, `SIGTERM` in one run and
`SIGINT` in another, asserting what design states; design decides how many
serving runs there are and what each one's environment holds. The binary's
`--version` output and `service.started` all carry that display string, and the
pages' footer the release and commit. Any
other test that builds, execs, waits on, or signals a process is a bug.

## Live tests

There are none. home calls one kind of external service, the sites whose
icons the favicon fetch finds, and design calls for no live test: the unit
tests stand in for those sites with fakes.

## Gates

Run from this directory (`home/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/home`
   — the release build, static and cgo-free, the way `devctl build` builds
   it, which is why the SQLite driver must be pure Go
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`
   — the cache lives in this worktree's git directory, so worktrees never
   share it, and `--allow-parallel-runners` lets gates for several
   sub-projects lint at once; `make lint` runs this form

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

`make` builds `bin/home` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

Deploy machinery, `devctl build` and `devctl deploy`, is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it.

home is an app, not a self-installing CLI: `devctl` builds it into a tarball
named by the commit and pushes it to a space's host, where `opsctl install`
installs it. There is no version to set, no tag to mint and no `--version`
check. Until devctl is next released, use the `devctl` the worktree builds: run
`make build` in `devctl/`, then run the commands below from the repository
root.

1. Commit the change on the branch you are on (push only when asked); the
   working tree must be clean.
2. `devctl/bin/devctl build home` writes `home/dist/home-<sha>.tar.xz`,
   `<sha>` being the 40 lowercase hex digits of `HEAD`. It holds `bin/home`,
   `etc/`, and `share/icon.svg`; it refuses a dirty tree and a binary whose
   `manifest` disagrees with the committed `etc/manifest.toml`.
3. `devctl/bin/devctl deploy <space> home/dist/home-<sha>.tar.xz` uploads the
   tarball to the space's `deploy/` prefix and runs `opsctl install` over ssh;
   the host fetches it, writes `etc/env` (the manifest's `[env]` defaults and
   the space's `DRAIN_SECONDS`), replaces the release, publishes
   `ikigenba-home.socket` (the Unix socket `/run/ikigenba/home.sock`)
   and the `Type=notify` `ikigenba-home.service`, bounded by the manifest's
   `[resources]`, regenerates the host's nginx and litestream configuration,
   and restarts the service alone. The database is kept across releases.

`home --version` prints the host's display string for the code it runs, built
by appkit's `version.Display()` from `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`.
The host does not yet set either variable, so a deployed home prints an empty
line until it does.
