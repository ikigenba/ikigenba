# gdrive

An app of the Ikigenba platform: Google Drive files, pulled into and pushed
from artifacts, served at `gdrive.<host>`. One Go binary serves on the
listening socket it is passed as descriptor 3 (`/run/ikigenba/gdrive.sock` on
a host), behind the host's nginx. gdrive is the suite's first connector: it
moves files between a signed-in user's Google Drive and artifacts on that
user's behalf, so that no byte and no credential passes through an agent's
context. An agent finds a file with the read tools and asks for a transfer;
the tool validates, records the transfer and answers at once, and the work
runs in the background as the caller, reaching Drive over its REST v3 API with
an access token connections hands out over its socket, and artifacts over
its socket. gdrive speaks to Drive with the standard library's `net/http`
alone; no Google library. Bytes stream between Drive and artifacts and are
never kept on disk. It keeps its transfers in its own SQLite database,
`state/gdrive.db` resolved against the working directory and opened through
appkit's `db` package; it is the only writer of it. It emits an event on the
bus for each transfer that begins and each that ends, and records every Drive
call in the trail. Its manifest sets `guests = false`: nginx lets no request
without a credential through, to the pages and to `/mcp` alike, and its
nginx fragment answers the bus's two paths 404 at the public name. At `/mcp`
it offers eight MCP tools, which agents reach through the MCP gateway:
`drives`, `list`, `search`, `show`, `pull`, `push`, `transfer` and
`transfers`. No tool accepts or returns a file's bytes, a token or a
credential. At `/` it serves a page of the caller's transfers, a page per
transfer at `/<id>`, a tools page at `/tools`, an about screen at `/about`
and a not-found page. On a host it runs as `bin/gdrive` of its release with
`/var/opt/ikigenba/gdrive` as its working directory and its environment from
`/etc/opt/ikigenba/gdrive/env`; a developer runs the same binary from the
checkout. The module path is `github.com/ikigenba/ikigenba/gdrive`. It
requires appkit (`github.com/ikigenba/ikigenba/appkit`) and uses appkit's
packages `page` (the banner and footer, and the shared static files under
`/_appkit/`), `identity` (the caller nginx authenticated, required on every
route but the event bus's two), `mcp` (the server mounted at `/mcp`, and the
client the tests drive it with), `telemetry` (the event contract, the request
middleware and the writer gdrive's events go through), `db` (the database's
handle and its migrations), `services` (the services file), `version` (the
identity and its display string), `events` (the event bus), `artifacts` (the
client of artifacts' socket) and `connections` (the client of connections'
socket).
The contract is the documents in `specs/design/`. This file restates none of
it. gdrive is an app, so it has no stories: its intent is the decisions
document that delivered it, or what was agreed in conversation, together with
its templates under `assets/`, and the design is the record.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, the migrations under `migrations/`, which the root package embeds,
and everything under `etc/` (`etc/manifest.toml` and the nginx fragment
`etc/nginx.conf`). The delivery that created gdrive seeded `go.mod` and
`go.sum` on appkit; the build run owns them from then on and changes them as
design says. It never writes `assets/`, `share/`, this `AGENTS.md`, the
`Makefile` or `.golangci.yml`. `state/` is where a running gdrive keeps
`gdrive.db`; it is created at run time and never committed.
See the `spec` and `build-spec` skills. Everything below is what the build run
computes the gap and runs the gates against; it is written outside the run and
read-only to it.

## Assets

`assets/` holds gdrive's markup: Go `html/template` files, each opening with a
comment naming the data it receives: `landing.html`, `transfer.html`,
`about.html`, `tools.html` and `notfound.html`. Every page is shown only to a
signed-in user. They are written by the user or the delivering agent,
following the repository's `design/`, and are inputs to the spec: the build
run reads them and never writes them. The code parses them into the set
appkit's `page.Templates` returns and executes them by template name; it never
writes markup of its own, not even a fragment or an error page. Go's `embed`
reaches only files at or below the embedding package's directory, so the
module's root package (the directory holding `go.mod`) embeds `assets/`, and
design names what it exports.

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

gdrive holds no copy of the stylesheet, fonts, or licences; appkit's `page`
package embeds and serves them. `share/icon.svg` is gdrive's icon, shown in the
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
  one requirement below, set by the delivery that created gdrive; the
  build run sets any other and its `go.sum` lines, and moves to another
  release only when this file names one:
  - appkit `v0.26.0`, set with
    `go get github.com/ikigenba/ikigenba/appkit@v0.26.0`: a release that
    exports the `db`, `paging` and `version` packages, the `artifacts` and
    `connections` clients of those siblings' sockets, and whose
    `page.Banner` carries the trail, the home link and the tools flag the
    templates use, with `Release` and `Commit`. See Adopting appkit below.
    It and the modules it pulls in are gdrive's only dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is appkit's.** gdrive stores its transfers in SQLite
through appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so
the release build stays cgo-free (gate 3), and already approved for the
repository. gdrive neither requires nor imports it directly. Adding any other
external dependency needs approval first, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.26.0`, fetched
through the ordinary module proxy and checked against the checksum database
(see the root `AGENTS.md`); it is set with
`go get github.com/ikigenba/ikigenba/appkit@v0.26.0`. No `replace` directive,
no `go.work`, no local module cache stands in for it.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/gdrive` and everything under `internal/` (`internal/cli`,
`internal/settings`, `internal/server`, `internal/store`, `internal/drive`,
`internal/urls`, `internal/transfers`, `internal/trail`, `internal/tools`,
`internal/pages` and `internal/web`). This is the file set the canonical gap
greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. gdrive echoes back what callers, Drive and its
siblings give it — Drive file, folder and shared drive ids, names, MIME types
and links, upload session URIs, page tokens, artifact paths and URLs, user ids
and emails, tokens and accounts a fake connections issues, the services
file's URLs — and tests echo Drive file ids above all, since nearly every tool
and transfer names one, so a test that sends an id-shaped value lands one in
the test file. No fixture carries one: not a header value, a query parameter,
a tool argument, a fake Drive's or a fake sibling's answer, a file id, a
token, a URL, a services file entry, an event's payload, or an expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something. Clocks and timers are injected: a test hands
  in a `Now`, an `After` and a `Rand` it controls.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**The run seam carries the process.** gdrive binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal, the
pid, the inherited listener, the output streams, the clock, the sweeper's
timer, the random source, the working directory and the HTTP client every
Drive call goes through come in through the run seam design declares
(`cli.Run` with `cli.Process`), and tests inject them. A `Run`-level test sets
the version string to one of its own and `Dir` to a temporary directory of its
own, so the database lands there and never in the checkout. A test never
reads or changes the real environment, clock, or randomness, and never leaves
the inherited-listener step unset, since that would take the test process's
real descriptor 3. The one exception is the working directory: a test that
proves what an empty `Dir`, or the root package's `Assets`, `Etc` or
`Migrations` does whatever the working directory is may `t.Chdir` into a
temporary directory of its own, and such a test does not call `t.Parallel`. A
test learns that the server is ready the way systemd does: it binds a Unix
datagram socket in a short temporary directory (`os.MkdirTemp("", ...)`, since
a Unix socket path is limited to 108 bytes and `t.TempDir()` can exceed it),
names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that
fails the test. Drain tests are the one place a test waits on the clock,
because the drain deadline is the behavior, and they keep that wait to a few
seconds. The gates run offline as an ordinary user, with no systemd.

**Drive is faked, never called.** No test reaches Google. A `Run`-level test
hands `Process.HTTP` a client whose transport answers every request as a fake
Drive does; `internal/drive`'s own tests may instead point the client's base
URL at a loopback fake Drive served with `httptest`, as design names. The
fake answers the files, exports, drives and upload sessions the test chooses,
refuses or fails on demand, holds a body part-written when a streaming test
needs it, and records each request it receives, so a test proves a call by
the request the fake received. The exec'ing test never lets the binary reach
Drive, since nothing it asks makes a Drive call.

**The siblings are faked on loopback.** artifacts and connections are never
real in a test. A `Run`-level test writes a services file whose entries
`artifacts` and `connections` name Unix sockets in a short temporary directory
on which loopback fakes of the two listen; a test of the engine or the tools
hands them appkit's `artifacts` and `connections` clients built over such
entries, as design names. The fake connections answers `GET /token/google`
with the token, refusal or failure the test chooses; the fake artifacts
answers `HEAD`, `PUT` and `GET` of `/files/<path>` as the test chooses. Each
records the requests it receives, identity headers included.

**The state is an isolated directory.** Each test gives gdrive a working
directory inside a test-owned temporary directory, so the database is the
test's own. A test that needs a store opens its own database with appkit's
`db.Open` at a path in its own temporary directory, with `gdrive.Migrations()`
and a clock it controls, closes the handle when it ends, and builds the store
over that handle with `store.New`. Tests may create filesystem fixtures inside
that temporary tree (a regular file named `state`, a file at `state/gdrive.db`
that is not a database, a database a test's own `db.Open` made and then
changed through `DB.Write`, transfers a test's own store recorded, including
another user's and ones left `running` or `queued`); nothing touches
`/var/opt/ikigenba/gdrive`, the checkout's `state/` or a shared file. A
database failure is provoked with `SetFailing(true)` on the handle the store
the test handed the server or engine was built over, before a call or from a
hook design names, never by corrupting the file, removing permissions or
closing the store. Tests prove gdrive's use of the database, its schema, its
store and its wiring, never SQLite's own guarantees (atomicity, durability,
locking) and never appkit's `db` contract (opening, migrations, transactions,
`db status`), which appkit's own tests prove; they prove what a store holds
by reopening its path, never by reading tables.

**Time and the sweep are driven, not waited out.** A pull's expiry check, a
transfer's started and finished times and the prune's cut-off are proved
through the injected `Now`: a test advances the clock it handed in, never
waits real seconds. The sweeper waits on `Process.After`; a test hands in an
`After` whose channels it holds and delivers on one to make the next prune
happen. A test that sleeps to age a transfer or to let a sweep come round is
a bug. A test waits for something it started to finish (a transfer reaching
`done` or `failed`, a fake receiving bytes, a progress update) with a deadline
that fails the test.

**Events come from the test's own sink.** Where a test observes the events
gdrive records or emits, it hands gdrive's writer appkit's `telemetry.Capture`,
or a services file whose `telemetry` or `events` entry names a Unix socket the
test holds, as design names; it hands the emitter appkit's `events.Capture`
and sets an event cause with `events.NewContext`. Every test builds its own
database; no test depends on state another test made or on the order the
tests run in.

**Environment variables, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read gdrive cannot route through the run seam, and in the
binary only `main` makes it. A test that reaches such a read, directly or
through a gdrive constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. It is the only variable an
in-process test sets for gdrive's own code; `DRAIN_SECONDS` and the five
transfer settings reach `Run` through the `LookupEnv` the test gives it. The
exec'ing test also sets `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with
`testing.T.Setenv`, so it may call appkit's `version.Read()` and the
identity's `String()` under the same two values it composes into the child's
environment; the commit it sets is longer than seven characters, so a `main`
that skipped shortening would fail. Any test that sets a variable with
`testing.T.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity.Require`
wraps every route but the event bus's `/events` and `/declarations`, so a
request reaches the pages or `/mcp` only with an `X-User-Id` header. A test
sets the identity headers to be a signed-in user, or omits `X-User-Id` to
exercise the missing-identity answer, and sets `Host` and `X-Forwarded-Proto`
the way nginx does. Where ownership matters it acts as two users, and it
proves that a transfer's work runs as its caller by the identity headers the
fake siblings received. What appkit's middleware and writer do is appkit's
contract; gdrive's tests prove only that its handlers are wired to them, by
use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing
identity header, `server/discover`), never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns.
gdrive's tests never re-prove appkit's transport.

**Pages are raw HTTP.** A test drives `/`, `/<id>`, `/tools`, `/about` and
any other path as a browser would: raw HTTP requests on the handler it built,
served with `httptest` or on a Unix socket, with the identity headers, and
never follows a redirect.

**The addresses are the test's own.** The connect page a `not_connected`
refusal names, an artifact's page and the banner's profile and sign-out
addresses are built on the services file's entries `connections`, `artifacts`
and `auth`, as design names, never on the request's `Host`. A test chooses
them: it writes a services file into its own temporary directory whose
entries name a URL, or none, and hands its path in the config or names it in
`IKIGENBA_SERVICES` through the `LookupEnv` it gives `Run`. The tools page's
`tools.Catalog()` takes no base.

**No test runs the page's scripts.** Every page carries appkit's feedback
script.
The gates have no browser and no JavaScript engine, and adding one is an
external dependency no one has approved, so a test asserts what a response
body carries and never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. It proves what the design
declares by using it. Handing `cmd/gdrive` to `go build` is not the test
reading it.

**One exec'ing test of the binary, and only one.** No test starts a process:
tests under `internal/` never start gdrive. The wiring in `cmd/gdrive` can be
proved no other way, so exactly one test that execs the binary is admissible,
and it lives in `cmd/gdrive`. It builds the binary into a temporary directory
and runs it with `--version`, with `manifest`, with `bogus`, and bare with no
socket passed in. For the serve case it stands in for systemd: it makes a Unix
socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]`
(descriptor 3 in the child), and starts the child through
`/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates
`state/gdrive.db`. Its environment is one the test composes, never the
developer's, with non-empty `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`; the test
computes the release and commit it expects with `version.Read()` and the
display string with its `String()` after setting the same two values with
`t.Setenv`, so no test spells a version. Separately it runs `--version` with
neither variable in the child's environment and expects exactly one empty
line. The test waits for `READY=1`, makes the requests design names for the
binary, over the socket, then stops the child with `SIGTERM`, and in a second
run with `SIGINT`, asserting what design states. The binary's `--version`
output, the MCP `serverInfo` and `service.started` all carry that display
string, and the pages' footer the release and commit. Any other test that
builds, execs, waits on, or signals a process is a bug.

## Live tests

There are none. gdrive calls one external service, Google Drive, and design
calls for no live test: every Drive fact the design states was observed in
probes made while it was drafted, the unit tests stand in for Drive with a
fake, and the sandbox check proves the real transfers.

## Gates

Run from this directory (`gdrive/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/gdrive`
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

`make` builds `bin/gdrive` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

gdrive ships in the suite release: `devctl build <sha|tag>` builds it with
every other app, its part of the release holding `bin/gdrive`, `etc/` and
`share/icon.svg`, and `devctl deploy <space> <sha|tag>` deploys that release.
The host must run events, which gdrive emits to, and artifacts and
connections, which it moves bytes into and out of and takes the caller's
Google token from; its services file must name both, with the `url` the
artifact pages and the connect page are built on. gdrive holds no secret of
its own. The host's backup archives `state/` whole, so `gdrive.db` is taken
with it; it holds transfer records only, no byte of a file and no token.
