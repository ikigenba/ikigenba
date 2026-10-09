# sites

An app of the Ikigenba platform: the suite's static site host, served at
`sites.<host>`. One Go binary serves on the listening socket it is passed as
descriptor 3 (`/run/ikigenba/sites.sock` on a host), behind the host's
nginx. A site is a catalog record naming a repository that repos holds, the
ref it tracks, the commit published from it, its visibility (public or
private) and whether it is listed; sites keeps the catalog in its own SQLite
database, `state/sites.db` resolved against the working directory and opened
through appkit's `db` package, of which it is the only writer. It serves each site
at `/<slug>/` from a tree it unpacks under `cache/` with the host's own `git`
from repos' bare repository, read-only, by the repository's id and the
published commit's sha; `cache/` is disposable and rebuilt on demand, never
backed up. A public site is served to anyone, including a visitor with no
credential, whom nginx lets through because sites' manifest declares
`guests = true`; a private site sends such a visitor to auth's sign-in with a
return URL, and serves any signed-in user of the space. Every site request
carries a long-lived visitor cookie sites mints, and records a `site.viewed`
event. A request at the space's apex host redirects to the apex site. At
`/mcp` it offers seven MCP tools, which agents reach through the MCP gateway:
`list`, `show`, `create`, `publish`, `update`, `delete` and `apex`. At `/` it
serves a landing page, for a signed-in user, that lists the space's sites and
says how to make one, and links to an about screen. On a host it runs as
`/opt/ikigenba/current/sites/bin/sites` with `/var/opt/ikigenba/sites` as
its working directory and its environment from `/etc/opt/ikigenba/sites/env`;
a developer runs the same binary from the checkout. The module path is `github.com/ikigenba/ikigenba/sites`. It
requires appkit (`github.com/ikigenba/ikigenba/appkit`), runs the host's
`git` (see Toolchain), and uses appkit's
packages `page` (the banner, launcher and footer, and the shared static files
under `/_appkit/`), `identity` (the caller nginx authenticated: required on
`/mcp`, optional everywhere else, so a guest reaches a site with no identity
headers), `mcp` (the server mounted at `/mcp`, and the client the tests drive
it with), `telemetry` (the event contract, the request middleware, and the
writer sites' events go through) and `db` (the catalog's handle and its
migrations). The contract is the documents in
`specs/design/`. This file restates none of it.

sites is an app, so it has no stories: its intent is the decisions
document that delivered it, or the intent agreed in conversation, together
with its templates under `assets/`, and `specs/design/` is the record.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, the catalog's migrations under `migrations/`, which the root package
embeds, and everything under `etc/` (`etc/manifest.toml`, and the nginx
fragment `etc/nginx.conf` if design names one). It never writes `assets/` or
`share/`. `state/` and `cache/` are where a running sites keeps `sites.db`
and its trees; they are created at run time and never committed. See the `spec` and `build-spec` skills. Everything below is what
the build run computes the gap and runs the gates against; it is
written outside the run and read-only to it.

## Assets

`assets/` holds sites' markup: the Go `html/template` files `landing.html`,
the landing page (template `landing`), `about.html`, the about screen
(template `about`), `tools.html`, the tools page (template `tools`),
`notfound.html`, the not-found page (template `notfound`), and
`unavailable.html`, the page shown while a site's published
commit cannot be served (template `unavailable`). Each opens with a comment
naming the data it receives and, for people and the stylesheet, the hooks
it carries. The landing, about and tools pages carry appkit's banner and are shown
only to a signed-in user; the not-found and unavailable pages may be shown
to a guest, so they carry the footer only and never the banner. They follow
the repository's `design/` and are an input to the spec: the user or the
delivering agent writes them, before design is drafted, and the build run
reads them and never writes them. The code parses them into the set
appkit's `page.Templates` returns and executes them by template name; it
never writes markup of its own, not even a fragment or an error page. The
files of a site are the site's own and are served as they are. Go's `embed`
reaches only files at or below the embedding package's directory, so the
module's root package (the directory holding `go.mod`) embeds `assets/`,
and design names what it exports. Every word a person or an agent reads,
and every class, id and attribute, lives in a template, or, for a line the
code sends without one, in a named copy constant in the source, and nowhere
else; a test and a requirement never spell one. Design names each template
and the data it receives, never its text, hooks, markup or styles. A test
proves a page by executing the named template with the data design states
and comparing, or by checking that a value it supplied appears in the body;
it never looks for a word or a tag. A change to copy or markup is an edit
to the asset or the constant alone. A needed template that is missing or
wrong, or one that cannot show a state design names, is an issue: the run
files it in `specs/issues/` and never edits the asset to close it.

sites holds no copy of the stylesheet, fonts, or licences; appkit's `page`
package embeds and serves them. `share/icon.svg` is sites' icon in the
service launcher: the Tabler outline `world-www` from
`design/ikigenba/icons/tabler/`, without its class, width, height, or
invisible bounding path, as `design/README.md` asks of a launcher icon. The build
run never writes it. `devctl build` packs it beside
`bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- `git` 2.x on the `PATH`. sites runs the host's `git` for every read of a
  repository (resolving a ref, checking a commit exists, reading a
  repository's `ikigenba.*` config, unpacking a tree with `git archive`) and
  links no git library; on a host opsctl provisions it. The tests run it too
  (see Test discipline), so without it gate 4 fails. It is an approved
  dependency of the repository; no other program is.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is
  committed, and the gates themselves run offline. `go.mod` starts with no
  requirement; the build run sets each one and its `go.sum` lines, and moves
  to another release only when this file names one:
  - appkit at `v0.20.0` (see Adopting appkit below). It and the modules it
    pulls in are sites' only dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is appkit's.** sites stores its catalog in SQLite through
appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so the
release build stays cgo-free (gate 3), and already approved for the
repository. sites neither requires nor imports it directly. Adding any other
external dependency — a Go git library, a markdown renderer, a MIME database
among them — needs approval first, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.20.0`, fetched
through the ordinary module proxy and checked against the checksum database
(see the root `AGENTS.md`); the build run sets it with
`go get github.com/ikigenba/ikigenba/appkit@v0.20.0`. No `replace` directive,
no `go.work`, no local module cache stands in for it.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/sites` and everything under `internal/`. This is the file set
the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. sites echoes back what callers give it — site
names, slugs, file contents and paths, ref names, header values — so a test
that sends an id-shaped value lands one in the test file. No fixture carries
one: not a site or repository name, a ref, a file's content or path, a commit
message or author, a tool argument, a header value, or an expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something. Clocks are injected: a test that opens a
  catalog hands it a `Now` it controls.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, git config, or real state.

**The run seam carries the process.** sites binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal (the
`PATH` sites finds `git` on included), the pid, the inherited listener, the
output streams, the clock, the random source and the working directory come
in through the run seam design declares, and tests inject them. A `Run`-level
test sets `Version` to a string of its own and `Dir` to a temporary directory of its own, so the catalog lands
there and never in the checkout. A test never reads or changes the real environment, clock, or
randomness, and never leaves the inherited-listener step unset, since that
would take the test process's real descriptor 3. The one exception is the
working directory: a test that proves what an empty `Dir`, a relative
repositories path, or the root package's `Assets`, `Etc` or `Migrations` does
whatever the working directory is may `t.Chdir` into a temporary directory of
its own, and such a test does not call `t.Parallel`. A test learns that the
server is ready the way systemd does: it binds a Unix datagram socket in a
short temporary directory (`os.MkdirTemp("", ...)`, since a Unix socket path
is limited to 108 bytes and `t.TempDir()` can exceed it), names it in
`NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test.
Drain tests are the one place a test waits on the clock, because the drain
deadline is the behavior, and they keep that wait to a few seconds. The gates
run offline as an ordinary user, with no systemd.

**The state is an isolated directory.** Each test gives sites a working
directory, or the cache and repositories paths design names, inside a
test-owned temporary directory, so the catalog, `cache/` and the repositories
it reads are the test's own. A test that needs a store opens its own catalog
with appkit's `db.Open` at a path in its own temporary directory, with
`sites.Migrations()` and a clock it controls, closes the handle when it ends,
and builds the store over that handle with `store.New`. Tests may create
filesystem fixtures inside that temporary tree (a regular file named `state`,
a file at `state/sites.db` that is not a database, a catalog a test's own
`db.Open` made and then changed through `DB.Write`, a bare repository with a
commit the test made, a cache tree the test removed or made unwritable);
nothing touches `/var/opt/ikigenba/sites`, `/var/opt/ikigenba/repos`, the
checkout's `state/` or a shared file. A catalog failure is provoked with `SetFailing(true)` on the
handle the store the test handed the server was built over, before a call or
from a hook design names, never by corrupting the file, removing permissions
or closing the store. Tests prove sites' use of the catalog, its schema, its
store and its wiring, never SQLite's own guarantees (atomicity, durability,
locking) and never appkit's `db` contract (opening, migrations, transactions,
`db status`), which appkit's own tests prove.

**git is real, and its environment is the test's.** sites reads repositories
with the host's real `git`, and what it proves is that a repository's commit
is served as git unpacks it, so tests use the real `git` and never a stand-in
for it. A test finds it once, with `exec.LookPath("git")` — the one look at
the real environment a test makes — and fails, never skips, when there is
none; it hands sites a `PATH` holding that directory through the run seam,
and a `PATH` without it to exercise the missing-git refusal. Every git that
runs under a test, whether sites starts it or the test does, runs with an
environment the test composed: `HOME` and `XDG_CONFIG_HOME` inside the test's
temporary directory, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` naming a
file in that directory or `/dev/null`, `GIT_TERMINAL_PROMPT=0`, and no
credential helper, so the developer's git config never decides a result. A
test may run `git` itself, with `exec.Command` and that environment, against
a directory in its temporary tree only: to make a bare repository fixture
shaped as repos lays one out (a bare repository whose `HEAD` names
`refs/heads/main` and whose config holds the `ikigenba.*` values design
names), to commit files into it, and to read back what it holds. A fixture
commit carries an author and committer date the test fixes, so its sha is
the same on every run. Tests keep repositories small: a size limit is reached
by starting sites with a small `SITE_MAX_BYTES`, never by writing anything
near the default. No test runs repos, pushes to a repository, or reaches a
repository over HTTP.

**Limits and schedules are driven, not waited out.** `OPERATION_SECONDS` and
any other deadline are proved through the clock and the seams design names,
never by waiting real seconds; a deadline passes because the test advances
the clock or fires the timer design names. A client that goes away is a
request the test cancels or a connection it closes. A test that sleeps to
age a wait is a bug. A test waits for git it started to finish with a
deadline that fails the test.

**Events come from the test's own sink.** Where a test observes the events
sites records, it hands sites' writer appkit's `telemetry.Capture`, or a
services file whose `telemetry` entry names a Unix socket the test holds, as
design names. Every test builds its own catalog, cache and repositories; no
test depends on state another test made or on the order the tests run in.

**Environment variables, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read sites cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a sites constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. It is the only variable an
in-process test sets for sites' own code. The exec'ing test also sets
`IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `t.Setenv`, only to compute the
expected display string by calling appkit's `version.Display()` under the same
two values it composes into the child's environment. Any test that sets a
variable with `t.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity`
middleware wraps every route: `Require` on `/mcp`, `Optional` everywhere
else. A test sets the identity headers to be a signed-in user, or omits
`X-User-Id` to be a guest, and sets `Host` and `X-Forwarded-Proto` the way
nginx does, since the sign-in redirect, the apex redirect and the visitor
cookie's attributes depend on them. What appkit's middleware and writer do is
appkit's contract; sites' tests prove only that its handlers are wired to
them, by use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing
identity header, `server/discover`), never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns.
sites' tests never re-prove appkit's transport.

**No test runs the page's scripts.** The pages carry appkit's launcher script
when there are services, and every page carries appkit's feedback script.
The gates have no browser and no JavaScript engine, and adding one is an
external dependency no one has approved, so a test asserts what a response
body carries and never what a script would do with it. A site's own files are bytes sites relays; a test asserts that they are
served unaltered, never what they do in a browser.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. It proves what the design
declares by using it. Handing `cmd/sites` to `go build` is not the test
reading it.

**One exec'ing test of the binary, and only one.** Beyond `git`, as above,
no test starts a process: tests under `internal/` never start sites. The
wiring in `cmd/sites` can be proved no other way, so exactly one test that
execs the binary is admissible, and it lives in `cmd/sites`. It builds the
binary into a temporary directory and runs it with `--version`, with
`manifest`, with `bogus`, and bare with no socket passed in. For the serve
case it stands in for systemd: it makes a Unix socket in a short temporary
directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child),
and starts the child through
`/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates
`state/sites.db` and `cache/`, with the repositories directory design names pointed
at a bare repository fixture in the same temporary tree. Its environment is
one the test composes, never the developer's: the git environment above, with
a `PATH` holding git's directory, and non-empty `IKIGENBA_COMMIT` and
`IKIGENBA_RELEASE`; the test computes the display string it expects with
`version.Display()` after setting the same two values with `t.Setenv`, so no
test spells a version. Separately it runs `--version` with neither variable in
the child's environment and expects exactly one empty line. The test waits for `READY=1`, makes the
requests design names for the binary, over the socket, then stops the child
with `SIGTERM`, and in a second run with `SIGINT`, asserting what design
states; the binary's `--version` output, the page's banner and footer, the MCP
`serverInfo` and `service.started` all carry that display string. Any other test that builds, execs, waits on, or signals a process
other than `git` is a bug.

## Live tests

sites calls no external service: its callers are browsers, agents and
sibling services on the same host, which the unit tests stand in for, and
the git it runs is local, so it has no live tests. Should a design ever call
for one, it is a `*_live_test.go` file guarded by `//go:build live` with test
functions named `TestLive*`; it proves lightly that the whole is glued
together, carries the requirement id it proves, reads its credentials from
the environment, fails (never skips) when one is missing, and runs only as
gate 6.

## Gates

Run from this directory (`sites/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/sites`
   — the release build, static and cgo-free, the way `devctl build` builds
   it, which is why the SQLite driver must be pure Go
4. `go test -race ./...` — needs `git` on the `PATH` (see Toolchain)
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

`make` builds `bin/sites` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

Deploy machinery — `devctl build` and `devctl deploy` — is hand-maintained
infrastructure outside the spec system: the build run never reads, edits, or
tests it. sites ships in the suite release (`devctl build <sha|tag>`,
`devctl deploy <space> <sha|tag>`); it has no tarball, tag or version of its
own.
