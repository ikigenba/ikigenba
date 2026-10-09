# home

An app of the Ikigenba platform: the suite's front door, served at
`home.<host>` and, as the manifest's default app, at the space host itself.
One Go binary serves on the listening socket it is passed as descriptor 3
(`/run/ikigenba/home.sock` on a host), behind the host's nginx. home shows
every service on the space: at `/` it serves a landing page of the services
the host's services file names, as tiles under the launcher's rules, and links
to an about screen at `/about`. It is the home the banner's product mark links
to on every app's pages. It holds no state, keeps no database, offers no MCP
tools and emits no events of its own beyond appkit's request records. Every
page is for a signed-in user of the space; nginx lets no guest through. On a
host it runs as `/opt/home/bin/home` with `/opt/home` as its working directory
and its environment from `/opt/home/etc/env`; a developer runs the same binary
from the checkout. The module path is `github.com/ikigenba/ikigenba/home`. It
requires appkit (`github.com/ikigenba/ikigenba/appkit`) and uses appkit's
packages `page` (the banner, launcher and footer, the services the launcher
draws, and the shared static files under `/_appkit/`), `identity` (the caller
nginx authenticated, required on every route), and `telemetry` (the event
contract, the request middleware, and the writer home's events go through).
The contract is the documents in `specs/design/`. This file restates none of
it. home is an app, so it has no stories: its intent is the decisions document
that delivered it, or what was agreed in conversation, together with its
templates under `assets/`, and the design is the record.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, and everything under `etc/` (`etc/manifest.toml`, and the nginx
fragment `etc/nginx.conf` if design names one). It never writes `assets/`,
`share/`, this `AGENTS.md`, the `Makefile` or `.golangci.yml`. See the `spec`
and `build-spec` skills. Everything below is what the build run computes the
gap and runs the gates against; it is written outside the run and read-only
to it.

## Layout

- `specs/` is the contract: `design/`.
- `assets/` is the page markup, and `share/icon.svg` the launcher icon. The
  build run never writes them; the user or the delivering agent changes them.
- The root package, in the directory holding `go.mod`, embeds `assets/` and
  `etc/`. `cmd/home` is the binary. `internal/` is everything else, one
  package per concern.
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
package embeds and serves them. `share/icon.svg` is home's icon in the service
launcher: the Tabler outline `home` from `design/ikigenba/icons/tabler/`,
without its class, width, height, or invisible bounding path, as
`design/README.md` asks of a launcher icon. The build run never writes it.
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
  - appkit `v0.19.0`, set with
    `go get github.com/ikigenba/ikigenba/appkit@v0.19.0`: a release whose
    `page.Banner` carries the home link, the tools flag and the trail's
    levels. See Adopting appkit below. It and the modules it pulls in are
    home's only dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

Adding any external dependency needs approval first, the user's or a
delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.19.0`, fetched
through the ordinary module proxy and checked against the checksum database
(see the root `AGENTS.md`); the build run sets it with
`go get github.com/ikigenba/ikigenba/appkit@v0.19.0`. No `replace` directive,
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
file counts as a covered id. home echoes back what its services file and its
callers give it — service names, URLs, descriptions, header values — so a test
that sends an id-shaped value lands one in the test file. No fixture carries
one: not a service name, a URL, a header value, an event's attribute, or an
expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**The run seam carries the process.** home binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal, the
pid, the inherited listener and the output streams come in through the run
seam design declares (`cli.Run` with `cli.Process`), and tests inject them. A
`Run`-level test sets the version string to one of its own. A test never reads
or changes the real environment, clock, or randomness, and never leaves the
inherited-listener step unset, since that would take the test process's real
descriptor 3. The one exception is the working directory: a test that proves
what the root package's `Assets` or `Etc` does whatever the working directory
is may `t.Chdir` into a temporary directory of its own, and such a test does
not call `t.Parallel`. A test learns that the server is ready the way systemd
does: it binds a Unix datagram socket in a short temporary directory
(`os.MkdirTemp("", ...)`, since a Unix socket path is limited to 108 bytes and
`t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for
`READY=1` with a deadline that fails the test. Drain tests are the one place a
test waits on the clock, because the drain deadline is the behavior, and they
keep that wait to a few seconds. The gates run offline as an ordinary user,
with no systemd.

**The services file is the test's own.** A test that needs services writes a
services file in its own temporary directory, with whatever entries, icons
and flags the case needs, and names it as design says; nothing reads the
host's services file or the developer's. Every test builds its own; no test
depends on state another test made or on the order the tests run in.

**Nothing is waited out.** Deadlines are proved through the seams design
names, never by waiting real seconds. A client that goes away is a request the
test cancels or a connection it closes. A test that sleeps to age a wait is a
bug. A test waits for something it started to finish with a deadline that
fails the test.

**Events come from the test's own sink.** Where a test observes the events
home records, it hands home's writer appkit's `telemetry.Capture`, or a
services file whose `telemetry` entry names a Unix socket the test holds, as
design names.

**Environment variables, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read home cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a home constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. It is the only variable an
in-process test sets for home's own code. The exec'ing test also sets
`IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `testing.T.Setenv`, only to
compute the expected display string by calling appkit's `version.Display()`
under the same two values it composes into the child's environment. Any test
that sets a variable with `testing.T.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity.Require`
wraps every route, so a request reaches any path only with an `X-User-Id`
header. A test sets the identity headers to be a signed-in user, or omits
`X-User-Id` to exercise the missing-identity answer, and sets `Host` and
`X-Forwarded-Proto` the way nginx does, since the absolute URLs home builds
depend on them. What appkit's middleware and writer do is appkit's contract;
home's tests prove only that its handlers are wired to them, by use.

**No test runs the page's scripts.** The pages carry appkit's launcher script
when there are services, and every page carries appkit's feedback script.
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
child runs in a test-owned temporary working directory. Its environment is one
the test composes, never the developer's, with non-empty `IKIGENBA_COMMIT` and
`IKIGENBA_RELEASE`; the test computes the display string it expects with
`version.Display()` after setting the same two values with `t.Setenv`, so no
test spells a version. Separately it runs `--version` with neither variable in
the child's environment and expects exactly one empty line. The test waits for
`READY=1`, makes the requests design names for the binary, over the socket,
then stops the child with `SIGTERM`, and in a second run with `SIGINT`,
asserting what design states. The binary's `--version` output, the pages'
banner and footer, and `service.started` all carry that display string. Any
other test that builds, execs, waits on, or signals a process is a bug.

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
   it
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
   and restarts the service alone.

`home --version` prints the host's display string for the code it runs, built
by appkit's `version.Display()` from `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`.
The host does not yet set either variable, so a deployed home prints an empty
line until it does.
