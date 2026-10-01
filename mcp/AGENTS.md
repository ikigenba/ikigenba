# mcp

An app of the Ikigenba platform: the suite's MCP gateway, served at
`mcp.<host>`. One Go binary serves on the listening socket it is passed as
descriptor 3 (`/run/ikigenba/mcp.sock` on a host), behind the host's nginx.
At `/mcp` it offers MCP clients four tools over the suite's MCP services:
`services` lists them, `describe` shows a service's tools, `call` runs a read
tool and `mutate` runs a write tool. `/mcp/a,b` scopes the gateway to the
named services. At `/` it serves a connect page that tells a person how to
point a client at it. It holds no state: on every request it reads the
services file afresh, and it reaches each backend directly on the socket that
file names, with appkit's MCP client, forwarding the caller's identity and
giving up after 50 seconds. On a host it runs as `/opt/mcp/bin/mcp` with
`/opt/mcp` as its working directory. The module path is
`github.com/ikigenba/ikigenba/mcp`. It requires one other module, appkit
(`github.com/ikigenba/ikigenba/appkit`), and uses its packages `page` (the
banner, launcher and footer, and the shared static files under `/_appkit/`),
`services` (the services file), `identity` (the caller nginx authenticated,
required on every request and forwarded on every backend call), and `mcp` (the
server mounted at `/mcp` and the client that calls the backends). The contract
is the documents in `specs/design/`. This file restates none of it.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, and `etc/manifest.toml`. It never
writes `assets/` or `share/`. See the `spec` and `build-spec` skills.
Everything below is what the build run computes the gap and runs the gates
against; it is human-authored and read-only to the run.

## Assets

`assets/` holds the gateway's markup: the Go `html/template` file
`connect.html`, the connect page. It is written and approved by a human in
interactive sessions, following the repository's `design/`, and is an input
to the spec: the build run reads it and never writes it. The code embeds it
and executes it by template name; it never writes markup of its own, not even
a fragment or an error page. Go's `embed` reaches only files at or below the
embedding package's directory, so the module's root package (the directory
holding `go.mod`) embeds `assets/`, and design names what it exports. Design
names each template, the data it receives, and the hooks it emits; the tests
assert on those hooks and on visible text, never on layout. A needed template
that is missing or wrong, a state a story names that the templates cannot
show, or a hook design names that the templates lack is an issue for a human:
the run files it in `specs/issues/` and never edits the asset to close it.

The gateway holds no copy of the stylesheet, fonts, or licences; appkit's
`page` package embeds and serves them. `share/icon.svg` is the gateway's icon
in the service launcher: the Tabler outline `plug-connected` from
`design/ikigenba/icons/tabler/`, without its class, width, height, or
invisible bounding path, as `design/README.md` asks of a launcher icon. It is
human-authored; the build run never writes it. `devctl build` packs it beside
`bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- the appkit module at the version `go.mod` requires, in the Go module cache
  (`go mod download` fetches it once, online); `go.sum` is committed, and the
  gates themselves run offline. `go.mod` requires appkit `v0.5.0`, the first
  release with the packages `page`, `services`, `identity` and `mcp`; the
  build run writes its `go.sum` lines, and moves to a later appkit release
  only when this file names one
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/mcp` and everything under `internal/`. This is the file set the
canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. The gateway echoes service and tool names back in
its errors, so a test that sends an id-shaped name lands one in the test file.
No fixture carries one: not a service name, a tool name, an argument, a
header value, or an expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**The run seam carries the process.** The gateway binds nothing itself; it
serves on the listener it is passed. Arguments, environment lookup and
removal, the pid, the inherited listener, and the output streams come in
through the run seam design declares, and tests inject them. A test never
reads or changes the real environment and never leaves the inherited-listener
step unset, since that would take the test process's real descriptor 3. A
test learns that the server is ready the way systemd does: it binds a Unix
datagram socket in a short temporary directory (`os.MkdirTemp("", ...)`, since
a Unix socket path is limited to 108 bytes and `t.TempDir()` can exceed it),
names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that
fails the test. Drain tests are the one place a test waits on the clock,
because the drain deadline is the behavior, and they keep that wait to a few
seconds. The gates run offline as an ordinary user, with no systemd.

**Backends are servers the test builds.** A test that needs backends stands
each one up in process: an appkit `mcp.Server` with tools of the test's own,
served on a Unix socket in a short temporary directory. It writes a services
file in its temporary directory naming those sockets, with whatever `mcp`,
`enabled` and `description` values the case needs, and may rewrite it between
requests, since the gateway reads it afresh each time. An unreachable backend
is a socket path nothing listens on. A backend that records the identity
headers it received proves forwarding. Every test builds its own backends and
services file; no test depends on another's. A backend may also be a plain `http.Handler` of the test's own on such a
socket, alone or wrapped around an appkit `mcp.Server`: to record the requests
it receives, to hold one until the test releases it, or to answer with bytes
the test writes (a JSON-RPC error, a tool list an appkit server cannot
produce, a non-MCP status, or a connection closed before any status), since
the gateway must handle answers no appkit server gives.

**No test waits out the backend timeout.** The 50-second limit is injected: a
test that proves the timeout sets a short one and a backend that blocks on a
channel until the test releases it. A test that proves cancellation cancels
the request's context while the backend blocks. That the default budget is fifty seconds is proved by `DefaultBudget`'s value;
a test proves only that a zero `Budget` is not shorter than the few seconds it
waits. A test that proves the budget is shared by a gateway call's backend
requests, or is not cut short, may release a held backend answer on a timer
within the short budget; like a drain test, it keeps that wait to a few
seconds.

**One environment variable, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read the gateway cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a gateway constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. Such a test does not call
`t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity` middleware
wraps the whole handler, so a request reaches `/`, `/mcp` or `/mcp/...` only
with an `X-User-Id` header. A test sets the identity headers, or omits
`X-User-Id` to exercise the missing-identity answer. What the middleware does
is appkit's contract; the gateway's tests prove only that its handler is
wrapped in it, by use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing, duplicated
or empty identity header, a malformed scope path, a method other than POST, a
request on an earlier protocol revision, `initialize` or `server/discover`)
and for comparing the gateway's answer with appkit's server's answer to the
same request; never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns, and
on the diagnostics the test captured in a buffer of its own. The gateway's
tests never re-prove appkit's transport.

**No test runs the page's scripts.** The gates have no browser and no
JavaScript engine, and adding one is an external dependency no one has
approved, so a test asserts what a response body carries and never what a
script would do with it.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. Handing `cmd/mcp` to
`go build` is not the test reading it.

**One exec'ing test, and only one.** Tests under `internal/` never start a
real process. The wiring in `cmd/mcp` can be proved no other way, so exactly
one test that execs the binary is admissible, and it lives in `cmd/mcp`. It
builds the binary into a temporary directory and runs it with `--version`,
with `bogus`, and bare with no socket passed in. For the serve case it stands
in for systemd: it makes a Unix socket in a short temporary directory, passes
it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the
child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`,
because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's.
It waits for `READY=1`, makes the requests design names for the binary, over
the socket and with the identity headers, then stops the child with `SIGTERM`,
and in a second run with `SIGINT`, asserting what design states. The child's
environment is one the test composes, never the developer's. Any other test
that builds, execs, waits on, or signals a process is a bug.

## Live tests

The gateway's backends are sibling services on the same host, which the unit
tests stand in for, so it has no live tests. Should a design ever call for
one, it is a `*_live_test.go` file guarded by `//go:build live` with test
functions named `TestLive*`; it proves lightly that the whole is glued
together, carries the requirement id it proves, reads its credentials from the
environment, fails (never skips) when one is missing, and runs only as gate 6.

## Gates

Run from this directory (`mcp/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/mcp` —
   the release build, static and cgo-free, the way `devctl build` builds it
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

`make` builds `bin/mcp` from the checkout (`make build`). `make fmt` rewrites
unformatted files, and `make test` and `make lint` run the test and lint
gates. The gates do not go through `make`: they call the Go tool directly, and
the one test that needs a binary builds its own into a temporary directory.

## Deploy

Deploy machinery — the version bump, tags, and the `devctl build`/`devctl
deploy` steps — is hand-maintained infrastructure outside the spec system: the
build run never reads, edits, or tests it.

The gateway is an app, not a self-installing CLI: `devctl` builds it into a
release tarball and pushes it to a space's host, where `opsctl install`
installs it.

1. Set the version literal design declares to `vX.Y.Z`. The binary reports it
   verbatim, and the deploy refuses a tag that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `mcp/vX.Y.Z` and push the tag.
4. `devctl build mcp` at that tag writes `mcp/dist/mcp-vX.Y.Z.tar.xz`, holding
   `bin/mcp`, `etc/`, and `share/icon.svg`. It refuses a binary whose
   `manifest` disagrees with the committed `etc/manifest.toml`.
5. `devctl deploy <space> mcp/dist/mcp-vX.Y.Z.tar.xz` uploads the tarball and
   runs `opsctl install` on the host, which publishes `ikigenba-mcp.socket`
   and `ikigenba-mcp.service`, regenerates nginx, and restarts the service.

`mcp --version` then prints `vX.Y.Z`, and `space status` reports it.
