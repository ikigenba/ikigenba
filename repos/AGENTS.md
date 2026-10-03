# repos

An app of the Ikigenba platform: the suite's home for git repositories,
served at `repos.<host>`. One Go binary serves on the listening socket it is
passed as descriptor 3 (`/run/ikigenba/repos.sock` on a host), behind the
host's nginx. It keeps each of a user's repositories as a bare git repository
under `state/repos/`, catalogued in its own SQLite database, of which it is
the only writer. It serves each repository over git's smart HTTP at
`/<name>.git/` by running the host's own `git http-backend`, bounded by the
slots, queues, deadlines and sizes its environment sets, and tidies each one
on a schedule with git's own collection. At `/mcp` it offers six MCP tools,
which agents reach through the MCP gateway: `list`, `show`, `status`,
`create`, `rename` and `delete`. At `/` it serves a landing page that says
what it is and how to clone, and links to an about screen. The suite's other
apps on the same host read a repository straight from its directory. On a
host it runs as `/opt/repos/bin/repos` with `/opt/repos` as its working
directory and its environment from `/opt/repos/etc/env`; a developer runs the
same binary from the checkout. The module path is
`github.com/ikigenba/ikigenba/repos`. It requires appkit
(`github.com/ikigenba/ikigenba/appkit`) and one SQLite driver (see
Toolchain), runs the host's `git` (see Toolchain), and uses appkit's packages
`page` (the banner, launcher and footer, and the shared static files under
`/_appkit/`), `identity` (the caller nginx authenticated, required on every
request), `mcp` (the server mounted at `/mcp`, and the client the tests drive
it with) and `telemetry` (the event contract, the request middleware, and the
writer repos' events go through). The contract is the documents in
`specs/design/`. This file restates none of it.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, and everything under `etc/` (`etc/manifest.toml`, and the nginx
fragment `etc/nginx.conf` that lets a push stream through to repos). It never
writes `assets/` or `share/`. See the `spec` and `build-spec` skills.
Everything below is what the build run computes the gap and runs the gates
against; it is human-authored and read-only to the run.

## Assets

`assets/` holds repos' markup: the Go `html/template` files `landing.html`,
the landing page (template `landing`), and `about.html`, the about screen
(template `about`). Each opens with a comment naming the data it receives and
the hooks it emits. They are written and approved by a human in interactive
sessions, following the repository's `design/`, and are inputs to the spec:
the build run reads them and never writes them. The code parses them into the
set appkit's `page.Templates` returns and executes them by template name; it
never writes markup of its own, not even a fragment or an error page. Go's
`embed` reaches only files at or below the embedding package's directory, so
the module's root package (the directory holding `go.mod`) embeds `assets/`,
and design names what it exports. Design names each template, the data it
receives, and the hooks it emits; the tests assert on those hooks and on
visible text, never on layout. A needed template that is missing or wrong, a
state a story names that the templates cannot show, or a hook design names
that the templates lack is an issue for a human: the run files it in
`specs/issues/` and never edits the asset to close it.

repos holds no copy of the stylesheet, fonts, or licences; appkit's `page`
package embeds and serves them. `share/icon.svg` is repos' icon in the
service launcher: the Tabler outline `git-branch` from
`design/ikigenba/icons/tabler/`, without its class, width, height, or
invisible bounding path, as `design/README.md` asks of a launcher icon. It is
human-authored; the build run never writes it. `devctl build` packs it beside
`bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- `git` 2.x on the `PATH`, with `git http-backend` (every packaged git carries
  it). repos runs the host's `git` for every repository operation and links
  no git library; on a host opsctl provisions it. The tests run it too (see
  Test discipline), so without it gate 4 fails. It is an approved dependency
  of the repository; no other program is.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is
  committed, and the gates themselves run offline. `go.mod` starts with no
  requirement; the build run sets each one and its `go.sum` lines, and moves
  to another release only when this file names one:
  - appkit, at the release `go.mod` requires: one whose `request.finished`
    carries the request's and the response's body byte counts. See Adopting
    appkit below.
  - `modernc.org/sqlite` `v1.59.0`, the SQLite driver, set with
    `go get modernc.org/sqlite@v1.59.0`. It and the modules it pulls in are
    repos' only other dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is auth's.** repos stores its catalog in SQLite. Its
driver is exactly the one auth uses, `modernc.org/sqlite` at the version
auth's `go.mod` requires, `v1.59.0`: pure Go, so the release build stays
cgo-free (gate 3), already approved for the repository, and already in the
module cache wherever auth builds. Adding any other external dependency — a
Go git library among them — or moving this one to another version, needs
human approval first.

### Adopting appkit

The appkit release `go.mod` requires (written `<version>` below) is released from this repository, and until its tag is pushed
neither the Go module proxy nor the checksum database knows it. Until then,
on the machine that runs the build:

1. After appkit's build, tag its release commit locally:
   `git tag appkit/<version> <commit>`.
2. Seed the module cache once, from the local repository:
   ```
   GOPROXY=direct GONOSUMDB=github.com/ikigenba/ikigenba GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=url./mnt/projects/ikigenba.insteadOf GIT_CONFIG_VALUE_0=https://github.com/ikigenba/ikigenba go mod download github.com/ikigenba/ikigenba/appkit@<version>
   ```
3. The build run sets the requirement with
   `GONOSUMDB=github.com/ikigenba/ikigenba go get github.com/ikigenba/ikigenba/appkit@<version>`;
   the build run's shell must export `GONOSUMDB=github.com/ikigenba/ikigenba`.

Once `go.sum` holds appkit's lines, ordinary builds and the gates work. The
tag `appkit/<version>` must be pushed before any other machine builds repos.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/repos` and everything under `internal/`. This is the file set
the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. repos echoes back what callers give it —
repository names, file contents, commit messages, ref names, header values —
so a test that sends an id-shaped value lands one in the test file. No
fixture carries one: not a repository name, a ref, a file's content or path,
a commit message or author, a tool argument, a header value, or an expected
body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, git config, or real state.

**The run seam carries the process.** repos binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal (the
`PATH` repos finds `git` on included), the pid, the inherited listener, the
output streams, the clock, the random source, the working directory and the
database source come in through the run seam design declares, and tests
inject them. A test never reads or changes the real environment, clock, or
randomness, and never leaves the inherited-listener step unset, since that
would take the test process's real descriptor 3. A test learns that the
server is ready the way systemd does: it binds a Unix datagram socket in a
short temporary directory (`os.MkdirTemp("", ...)`, since a Unix socket path
is limited to 108 bytes and `t.TempDir()` can exceed it), names it in
`NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test.
Drain tests are the one place a test waits on the clock, because the drain
deadline is the behavior, and they keep that wait to a few seconds. The gates
run offline as an ordinary user, with no systemd.

**The state is an isolated directory.** Each test gives repos a working
directory, or the database and repositories paths design names, inside a
test-owned temporary directory, so the catalog and `state/repos/` are the
test's own; an in-memory SQLite DSN is fine where no repository directory is
involved. Tests may create filesystem fixtures inside that temporary tree (an
absent parent, a file that is not a database, a repository directory with its
`HEAD` or `objects/` removed, a directory whose permissions the test
removed); nothing touches `/opt/repos` or a shared file. A database or disk
failure mid-request is produced by making the test's own database or
directory fail after the server has it, for example by closing the store the
test handed it.

**git is real, and its environment is the test's.** repos is a front for
`git http-backend`, and what it proves is that git's own protocol passes
through it, so tests use the host's real `git` and never a stand-in for it. A
test finds it once, with `exec.LookPath("git")` — the one look at the real
environment a test makes — and fails, never skips, when there is none; it
hands repos a `PATH` holding that directory through the run seam, and a
`PATH` without it to exercise the missing-git refusal. Every git that runs
under a test, whether repos starts it or the test does, runs with an
environment the test composed: `HOME` and `XDG_CONFIG_HOME` inside the test's
temporary directory, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` naming a
file in that directory or `/dev/null`, `GIT_TERMINAL_PROMPT=0`, and no
credential helper, so the developer's git config never decides a result. A
test may run `git` itself, with `exec.Command` and that environment, in two
ways only: as the client of a repos it serves on loopback or a Unix socket,
cloning, fetching and pushing as a user's git would, with the identity
headers given as `http.extraHeader` in its own config; and against a
directory in its temporary tree, to make a fixture or read back what a
repository holds (a ref, an object, `ikigenba.*` config). A fixture commit
carries an author and committer date the test fixes, so its sha is the same
on every run. Tests keep repositories and pushes small: a size limit is
reached by starting repos with a small `PUSH_MAX_BYTES` or `REPO_MAX_BYTES`,
never by writing anything near the defaults.

**Limits are driven, not waited out.** Slots, queues, the repository lock,
`QUEUE_SECONDS` and `OPERATION_SECONDS` are proved through the clock and the
seams design names, never by waiting real seconds. A busy slot or lock is
held by an operation the test controls — a push whose request body the test
holds open (an `io.Pipe` it has not closed), or the hook design names for
holding one — and released by the test; a deadline passes because the test
advances the clock or fires the timer design names. A client that goes away
is a request the test cancels or a connection it closes. The scheduled
maintenance cycle is triggered the way design names, never by waiting for a
timer; `MAINTENANCE_HOURS` is proved by what the injected clock makes due. A
test that sleeps to fill a slot, age a wait, or reach a cycle is a bug. A
test waits for git it started to finish with a deadline that fails the test.

**Events come from the test's own sink.** Where a test observes the events
repos records, it hands repos' writer appkit's `telemetry.Capture`, or a
services file whose `telemetry` entry names a Unix socket the test holds, as
design names. Every test builds its own catalog and repositories; no test
depends on state another test made or on the order the tests run in.

**One environment variable, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read repos cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a repos constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. Such a test does not call
`t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity`
middleware wraps every route, so a request reaches `/`, the about screen,
`/mcp` or a git path only with an `X-User-Id` header. A test sets the
identity headers, or omits `X-User-Id` to exercise the missing-identity
answer. What appkit's middleware and writer do is appkit's contract; repos'
tests prove only that its handler is wired to them, by use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing
identity header, `server/discover`), never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns.
repos' tests never re-prove appkit's transport.

**No test runs the page's scripts.** The pages carry appkit's launcher script
when there are services. The gates have no browser and no JavaScript engine,
and adding one is an external dependency no one has approved, so a test
asserts what a response body carries and never what a script would do with
it.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. It proves what the design
declares by using it. Handing `cmd/repos` to `go build` is not the test
reading it.

**One exec'ing test of the binary, and only one.** Beyond `git`, as above,
no test starts a process: tests under `internal/` never start repos. The
wiring in `cmd/repos` can be proved no other way, so exactly one test that
execs the binary is admissible, and it lives in `cmd/repos`. It builds the
binary into a temporary directory and runs it with `--version`, with
`manifest`, with `bogus`, and bare with no socket passed in. For the serve
case it stands in for systemd: it makes a Unix socket in a short temporary
directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child),
and starts the child through
`/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates its
database and `state/repos/`. Its environment is one the test composes, never
the developer's: the git environment above, with a `PATH` holding git's
directory. The test waits for `READY=1`, makes the requests design names for
the binary, over the socket, then stops the child with `SIGTERM`, and in a
second run with `SIGINT`, asserting what design states. Any other test that
builds, execs, waits on, or signals a process other than `git` is a bug.

## Live tests

repos calls no external service: its callers are git clients and sibling
services on the same host, which the unit tests stand in for, and the git it
runs is local, so it has no live tests. Should a design ever call for one, it
is a `*_live_test.go` file guarded by `//go:build live` with test functions
named `TestLive*`; it proves lightly that the whole is glued together,
carries the requirement id it proves, reads its credentials from the
environment, fails (never skips) when one is missing, and runs only as gate
6.

## Gates

Run from this directory (`repos/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/repos`
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

`make` builds `bin/repos` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

Deploy machinery — the version bump, tags, and the `devctl build`/`devctl
deploy` steps — is hand-maintained infrastructure outside the spec system: the
build run never reads, edits, or tests it.

repos is an app, not a self-installing CLI: `devctl` builds it into a release
tarball and pushes it to a space's host, where `opsctl install` installs it.
The host must provide `git`, which opsctl provisions. The tag
`appkit/<version>` must be on `origin` before the first release (see
Adopting appkit).

1. Set the version literal design declares to `vX.Y.Z`. The binary reports it
   verbatim, and the deploy refuses a tag that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `repos/vX.Y.Z` and push the tag.
4. `devctl build repos` at that tag writes `repos/dist/repos-vX.Y.Z.tar.xz`,
   holding `bin/repos`, `etc/`, and `share/icon.svg`, with no version
   recorded anywhere inside. It refuses a binary whose `manifest` disagrees
   with the committed `etc/manifest.toml`.
5. `devctl deploy <space> repos/dist/repos-vX.Y.Z.tar.xz` uploads the tarball
   to the space's `deploy/` prefix and runs `opsctl install` over ssh; the
   host fetches it, writes `etc/env` (the manifest's `[env]` defaults and the
   space's `DRAIN_SECONDS`), replaces the release, publishes
   `ikigenba-repos.socket` (the Unix socket `/run/ikigenba/repos.sock`) and
   the `Type=notify` `ikigenba-repos.service`, bounded by the manifest's
   `[resources]`, regenerates the host's nginx (including repos'
   `etc/nginx.conf`) and litestream configuration, and restarts the service
   alone. The database and `state/repos/` are kept across releases.

`repos --version` then prints `vX.Y.Z`, and `devctl space status <space>`
reports it.
