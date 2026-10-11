# artifacts

An app of the Ikigenba platform: the suite's blob store, served at
`artifacts.<host>`. One Go binary serves on the listening socket it is passed
as descriptor 3 (`/run/ikigenba/artifacts.sock` on a host), behind the host's
nginx. artifacts keeps files that agents and services share, each named by a
path and holding an id, and takes and serves their bytes at `/files/<path>`
without ever holding a body in memory. It keeps each artifact's record in its
own SQLite database, `state/artifacts.db` resolved against the working
directory and opened through appkit's `db` package, and its bytes in a file
under `state/blobs/`; it is the only writer of both. It emits an event on the
bus for each artifact created, replaced or deleted, and deletes artifacts
whose expiry has passed once before it is ready and then hourly. Its manifest
sets `guests = false`: nginx lets no request without a credential through, to
the pages, to `/files/` and to `/mcp` alike. At `/mcp` it offers four MCP
tools, which agents reach through the MCP gateway: `list`, `show`, `delete`
and `import`. The tools carry metadata only: `import` moves no bytes and
answers an upload plan, where and how to `PUT` the file, which the agent
follows itself. At `/` it serves a page of every
artifact, with a page per artifact at `/<id>`, a tools page at `/tools` and
an about screen at `/about`. On a host it runs as `bin/artifacts` of its
release with `/var/opt/ikigenba/artifacts` as its working directory and its
environment from `/etc/opt/ikigenba/artifacts/env`; a developer runs the same
binary from the checkout. The module path is
`github.com/ikigenba/ikigenba/artifacts`. It requires appkit
(`github.com/ikigenba/ikigenba/appkit`) and uses appkit's packages `page`
(the banner and footer, and the shared static files under
`/_appkit/`), `identity` (the caller nginx authenticated, required on every
route but the event bus's two), `mcp` (the server mounted at `/mcp`, and the
client the tests drive it with), `telemetry` (the event contract, the request
middleware and the writer artifacts' events go through), `db` (the database's handle and its migrations),
`services` (the services file), `version` (the identity and its display
string) and `events` (the event bus).
The contract is the documents in `specs/design/`. This file restates none of
it. artifacts is an app, so it has no stories: its intent is the decisions
document that delivered it, or what was agreed in conversation, together with
its templates under `assets/`, and the design is the record.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, the migrations under `migrations/`, which the root package embeds,
and everything under `etc/` (`etc/manifest.toml`, and the nginx fragment
`etc/nginx.conf` if design names one). The delivery that created artifacts
seeded `go.mod` and `go.sum` on appkit; the build run owns them from then on
and changes them as design says. It never writes `assets/`, `share/`, this
`AGENTS.md`, the `Makefile` or `.golangci.yml`. `state/` is where a running
artifacts keeps `artifacts.db` and `blobs/`; it is created at run time and
never committed.
See the `spec` and `build-spec` skills. Everything below is what the build run
computes the gap and runs the gates against; it is written outside the run and
read-only to it.

## Assets

`assets/` holds artifacts' markup: Go `html/template` files, each opening with a
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

artifacts holds no copy of the stylesheet, fonts, or licences; appkit's `page`
package embeds and serves them. `share/icon.svg` is artifacts' icon, shown in the
banner's trail and on home: a Tabler outline from
`design/ikigenba/icons/tabler/`, without its
class, width, height, or invisible bounding path, as `design/README.md` asks of
a service icon. The build run never writes it. `devctl build` packs it beside
`bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is
  committed, and the gates themselves run offline. `go.mod` starts with the
  one requirement below, set by the delivery that created artifacts; the
  build run sets any other and its `go.sum` lines, and moves to another
  release only when this file names one:
  - appkit `v0.22.0`, set with
    `go get github.com/ikigenba/ikigenba/appkit@v0.22.0`: a release that
    exports the `db` and `version` packages and whose `page.Banner` carries
    the trail, the home link and the tools flag the templates use, with
    `Release` and `Commit` but no service launcher. See
    Adopting appkit below. It and the modules it pulls in are artifacts' only
    dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is appkit's.** artifacts stores its artifacts' records in SQLite through
appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so the
release build stays cgo-free (gate 3), and already approved for the
repository. artifacts neither requires nor imports it directly. Adding any other
external dependency needs approval first, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.22.0`, fetched
through the ordinary module proxy and checked against the checksum database
(see the root `AGENTS.md`); it is set with
`go get github.com/ikigenba/ikigenba/appkit@v0.22.0`. No `replace` directive,
no `go.work`, no local module cache stands in for it.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/artifacts` and everything under `internal/`. This is the file set
the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. artifacts echoes back what callers give it —
paths, bodies, content types, expiry headers, the services file's URL — so a test that
sends an id-shaped value lands one in the test file. No fixture carries one:
not a path, a body, a tool argument, a header value, a URL, a services file
entry, an event's payload, or an expected body.

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

**The run seam carries the process.** artifacts binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal, the
pid, the inherited listener, the output streams, the clock, the sweeper's
timer, the random source and the working directory come in through the run seam design declares
(`cli.Run` with `cli.Process`), and tests inject them. A `Run`-level test sets
the version string to one of its own and `Dir` to a temporary directory of its
own, so the database and the blob directory land there and never in the
checkout. A test never reads or changes the real environment,
clock, or randomness, and never leaves the inherited-listener step unset,
since that would take the test process's real descriptor 3. The one exception
is the working directory: a test that proves what an empty `Dir`, or the root
package's `Assets`, `Etc` or `Migrations` does whatever the working directory
is may `t.Chdir` into a temporary directory of its own, and such a test does
not call `t.Parallel`. A test learns that the server is ready the way systemd
does: it binds a Unix datagram socket in a short temporary directory
(`os.MkdirTemp("", ...)`, since a Unix socket path is limited to 108 bytes and
`t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for
`READY=1` with a deadline that fails the test. Drain tests are the one place a
test waits on the clock, because the drain deadline is the behavior, and they
keep that wait to a few seconds. The gates run offline as an ordinary user,
with no systemd.

**The state is an isolated directory.** Each test gives artifacts a working
directory inside a test-owned temporary directory, so the database and the
blob directory are the test's own. A test that needs a store opens its own database with appkit's
`db.Open` at a path in its own temporary directory, with `artifacts.Migrations()`
and a clock it controls, closes the handle when it ends, makes a blob
directory beside it, and builds the store over that handle and that directory
with `store.New`. Tests may create filesystem fixtures inside
that temporary tree (a regular file named `state` or `state/blobs`, a file at
`state/artifacts.db` that is not a database, a database a test's own `db.Open`
made and then changed through `DB.Write`, artifacts a test's own store put and
whose files it then removed, stray files in `state/blobs`); nothing touches
`/var/opt/ikigenba/artifacts`, the checkout's `state/` or a shared file. A
test may list the blob directory with `os.ReadDir` and read its files, since
design states what lies there, but no blob's name is contract. A database failure is provoked with `SetFailing(true)` on the
handle the store the test handed the server was built over, before a call or
from a hook design names, never by corrupting the file, removing permissions
or closing the store. Tests prove artifacts' use of the database, its schema, its
store and its wiring, never SQLite's own guarantees (atomicity, durability,
locking) and never appkit's `db` contract (opening, migrations, transactions,
`db status`), which appkit's own tests prove. No test measures memory: that
no body is held whole is carried by the streaming requirements design names,
never by inspecting a heap.

**Sweeps are driven, not waited out.** The expiry sweep is proved through
the injected clock and the timer design names, never by waiting real
seconds. A sweep runs because the test advances its `Now` and delivers on the
channel its `After` returned, which tells the test each wait as it begins;
the sweep before ready runs without one. A client that
goes away is a request the test cancels, a connection it closes, or a body
reader of its own that fails mid-body. A test that sleeps to
age a wait is a bug. A test waits for something it started to finish with a
deadline that fails the test.

**Events come from the test's own sink.** Where a test observes the events
artifacts records or emits, it hands artifacts' writer appkit's `telemetry.Capture`, or
a services file whose `telemetry` or `events` entry names a Unix socket the
test holds, as design names; it hands the emitter appkit's `events.Capture`
and sets an event cause with `events.NewContext`. Every test builds its own database; no test
depends on state another test made or on the order the tests run in.

**Environment variables, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read artifacts cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a artifacts constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. It is the only variable an
in-process test sets for artifacts' own code. The exec'ing test also sets
`IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `testing.T.Setenv`, so it may call
appkit's `version.Read()` and the identity's `String()` under the same two values it
composes into the child's
environment; the commit it sets is longer than seven characters, so a `main`
that skipped shortening would fail. Any test
that sets a variable with `testing.T.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity.Require`
wraps every route but the event bus's `/events` and `/declarations`, so a
request reaches the pages, `/files/` or `/mcp` only with an `X-User-Id`
header. A test sets the identity headers to be a signed-in user, or omits
`X-User-Id` to exercise the missing-identity answer, and sets `Host` and
`X-Forwarded-Proto` the way nginx does, since the absolute URLs artifacts builds
depend on them. What appkit's middleware and writer do is appkit's contract;
artifacts' tests prove only that its handlers are wired to them, by use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing
identity header, `server/discover`), never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns.
artifacts' tests never re-prove appkit's transport.

**Files are raw HTTP.** A test drives `/files/<path>` as curl would: raw
HTTP requests on the handler it built, served with `httptest` or on a Unix
socket, as two users where ownership matters, with the method, the escaped
path, the headers and a body from a reader it controls (one that fails, one
past a small `ARTIFACT_MAX_BYTES`, one with no `Content-Length`). It proves a
`GET` by comparing with what `http.ServeContent` writes into an
`httptest.ResponseRecorder` for the same request, and lists the blob
directory after each call.

**The plan's base is the test's own.** `import`'s `url` is built on the
base its config carries, never on the request's `Host`. A test that registers
the tools chooses that base, and the tools page's `tools.Catalog()` takes
none. A test through `web.Handler` writes a services file into its own
temporary directory whose entry `artifacts` names a URL, or none, and hands
its path in the config, or names it in `IKIGENBA_SERVICES` through the
`LookupEnv` it gives `Run`.

**No test runs the page's scripts.** Every page carries appkit's feedback
script.
The gates have no browser and no JavaScript engine, and adding one is an
external dependency no one has approved, so a test asserts what a response
body carries and never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. It proves what the design
declares by using it. Handing `cmd/artifacts` to `go build` is not the test
reading it.

**One exec'ing test of the binary, and only one.** No test starts a process:
tests under `internal/` never start artifacts. The wiring in `cmd/artifacts` can be
proved no other way, so exactly one test that execs the binary is admissible,
and it lives in `cmd/artifacts`. It builds the binary into a temporary directory and
runs it with `--version`, with `manifest`, with `bogus`, and bare with no
socket passed in. For the serve case it stands in for systemd: it makes a Unix
socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]`
(descriptor 3 in the child), and starts the child through
`/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates
`state/artifacts.db` and `state/blobs/`. Its environment is one the test composes, never the
developer's, with non-empty `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`; the test
computes the release and commit it expects with `version.Read()` and the display
string with its `String()` after setting
the same two values with `t.Setenv`, so no test spells a version. Separately
it runs `--version` with neither variable in the child's environment and
expects exactly one empty line. The test waits for `READY=1`, makes the
requests design names for the binary, over the socket, then stops the child
with `SIGTERM`, and in a second run with `SIGINT`, asserting what design
states. The binary's `--version` output, the MCP `serverInfo` and
`service.started` all carry that display string, and the pages' footer the
release and commit. Any other
test that builds, execs, waits on, or signals a process is a bug.

## Live tests

artifacts calls no external service: its callers are browsers, agents and
sibling services on the same host, all of which the unit tests stand in
for, so it has no live tests. Should a design ever call for one, it is a
`*_live_test.go` file guarded by `//go:build live` with test functions named
`TestLive*`; it proves lightly that the whole is glued together, carries the
requirement id it proves, reads its credentials from the environment, fails
(never skips) when one is missing, and runs only as gate 6.

## Gates

Run from this directory (`artifacts/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/artifacts`
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

`make` builds `bin/artifacts` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

artifacts ships in the suite release: `devctl build <sha|tag>` builds it with
every other app, its part of the release holding `bin/artifacts`, `etc/` and
`share/icon.svg`, and `devctl deploy <space> <sha|tag>` deploys that release.
The host must run events, which artifacts emits to, and its services file
must name artifacts itself, with the `url` `import`'s plan is built on. The host's backup archives
`state/` whole, so `artifacts.db` and `state/blobs/` are taken together.
