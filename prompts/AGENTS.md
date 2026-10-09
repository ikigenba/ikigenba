# prompts

An app of the Ikigenba platform: the suite's prompt runner, served at
`prompts.<host>`. One Go binary serves on the listening socket it is passed as
descriptor 3 (`/run/ikigenba/prompts.sock` on a host), behind the host's
nginx. A prompt is a catalog record naming a model in agentkit's catalog and
the text the model is given, with an optional system prompt, the tool groups
it allows (`files`, `bash`, `suite`) and an optional output schema; prompts
keeps the catalog and the record of every run in its own SQLite database,
`state/prompts.db` resolved against the working directory and opened through
appkit's `db` package, of which it is the only writer. A run is one agent
session over agentkit: the app starts its own binary as `prompts agent`, a
child in its own process group and control group, with a composed
environment, bounded output and a deadline, hands it the run spec on standard
input, and the child runs the agentkit tool loop in
`state/runs/<prompt id>/<run id>/` and writes the model's final message to
standard output. The run folder keeps the run's `input.json`, `stdout`,
`stderr`, `transcript.jsonl` and `work/`. Every page and `/mcp` is for a
signed-in user of the space; nginx lets no guest through. At `/mcp` it offers
eleven MCP tools, which agents reach through the MCP gateway: `list`, `show`,
`create`, `update`, `delete`, `subscribe`, `unsubscribe`, `run`, `runs`,
`result` and `cancel`. At `/` it serves a catalog of the user's prompts and
their last runs, with a page for each prompt at `/<name>/` and for each run
at `/<name>/runs/<run id>/`, and links to an about screen. On a host it runs
as `/opt/ikigenba/current/prompts/bin/prompts` with
`/var/opt/ikigenba/prompts` as its working directory and its environment from
`/etc/opt/ikigenba/prompts/env`; a developer runs the same binary from the
checkout. The module path is `github.com/ikigenba/ikigenba/prompts`. It
requires appkit (`github.com/ikigenba/ikigenba/appkit`), agentkit
(`github.com/ikigenba/ikigenba/agentkit`) and toolkit
(`github.com/ikigenba/ikigenba/toolkit`), and uses appkit's packages `page`
(the banner, launcher and footer, and the shared static files under
`/_appkit/`), `identity` (the caller nginx authenticated, required on every
route), `mcp` (the server mounted at `/mcp`, the client the tests drive it
with, and the client the child reaches the gateway with), `telemetry` (the
event contract, the request middleware, and the writer prompts' events go
through), `db` (the catalog's handle and its migrations), and `events` (the
event bus). Its packages are `cli`, `server`, `settings`, `store`, `runner`,
`runs`, `agent` (the child's role: spec, tools, loop, transcript), `urls`,
`pages`, `tools` and `web`, under `internal/`. The contract is the documents
in `specs/design/`. This file restates none of it.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, the catalog's migrations under `migrations/`, which the root package
embeds, and everything under `etc/` (`etc/manifest.toml`, and the nginx
fragment `etc/nginx.conf` if design names one). The delivery that created
prompts seeded `go.mod`, `go.sum` and `etc/` from its decisions document; the
build run owns them from then on and changes them as design says. It never
writes `assets/`, `share/`, this `AGENTS.md`, the `Makefile` or
`.golangci.yml`. `state/` is where a running prompts keeps `prompts.db` and
its runs; it is created at run time and never committed. See the `spec` and
`build-spec` skills. Everything below is what the build run computes the gap
and runs the gates against; it is written outside the run and read-only to
it.

## Assets

prompts is an app, so it has no stories: its intent is the decisions document
that delivered it, or the intent agreed in conversation, together with its
templates under `assets/`, and the design in `specs/design/` is the record.

`assets/` holds prompts' markup: the Go `html/template` files `landing.html`,
the catalog (template `landing`), `prompt.html`, one prompt's page (template
`prompt`), `run.html`, one run's page (template `run`), `about.html`, the
about screen (template `about`), `notfound.html`, the not-found page
(template `notfound`), and `unavailable.html`, the unavailable page (template
`unavailable`). Each opens with a comment naming the data it receives; the
hooks it carries are documented there for people and the stylesheet, and
design never names them. Every page is shown only to a signed-in user. The
landing, prompt, run and about pages carry appkit's banner; the not-found and
unavailable pages carry the footer only and never the banner. They follow the
repository's `design/` and are inputs to the spec: the user or the delivering
agent writes them, before design is drafted, and the build run reads them and
never writes them. The code parses them into the set appkit's
`page.Templates` returns and executes them by template name; it never writes
markup of its own, not even a fragment or an error page. A run's answer,
transcript and files are the run's own and are served as they are. Go's
`embed` reaches only files at or below the embedding package's directory, so
the module's root package (the directory holding `go.mod`) embeds `assets/`,
and design names what it exports. Every word a person or an agent reads on a
page, and every class, id and attribute, lives in the templates and nowhere
else; a test and a requirement never spell one. Design names each template
and the data it receives, never its text, hooks, markup or styles. A test
proves a page by executing the named template with the data design states and
comparing, or by checking that a value the test supplied appears in the body;
it never looks for a word or a tag. A change to copy or markup is an edit to
the template alone. A template that is missing or wrong, or one that cannot
show a state design names, is an issue: the run files it in `specs/issues/`
and never edits the asset to close it.

prompts adds no words to what the model reads: the system prompt is the
author's, and the tools describe themselves. Source carries no framing text
for a run.

prompts holds no copy of the stylesheet, fonts, or licences; appkit's `page`
package embeds and serves them. `share/icon.svg` is prompts' icon in the
service launcher: the Tabler outline `prompt` from
`design/ikigenba/icons/tabler/`, without its class, width, height, or
invisible bounding path, as `design/README.md` asks of a launcher icon. The
build run never writes it. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- `bash` on the `PATH`. toolkit's `Bash` tool, which the `bash` tool group
  offers, finds it there and runs every command the model gives it with it;
  every host has it. The tests run it too (see Test discipline), so without
  it gate 4 fails. It is an approved dependency of the repository; no other
  program is.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is
  committed, and the gates themselves run offline. The build run moves to
  another release only when this file names one:
  - appkit `v0.18.0`, set with
    `go get github.com/ikigenba/ikigenba/appkit@v0.18.0`: a release that
    exports the `db` and `version` packages. See Adopting the libraries
    below.
  - agentkit `v0.13.0`, set with
    `go get github.com/ikigenba/ikigenba/agentkit@v0.13.0`: the model
    catalog and `Lookup`, `APIKeyRotator`, the tool loop, `NewTool` and
    `NewToolFromSchema`, `OutputContract` and `ValidateOutputSchema`,
    `Limits`, and the JSON Lines log `NewLog`.
  - toolkit `v0.3.0`, set with
    `go get github.com/ikigenba/ikigenba/toolkit@v0.3.0`: `Bash`, `Read`,
    `Write`, `Edit`, `Glob` and `Grep` as agentkit tools rooted at a
    directory.
  - `golang.org/x/sys`. It, appkit, agentkit, toolkit and the modules they
    pull in are prompts' only dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is appkit's.** prompts stores its catalog in SQLite
through appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so
the release build stays cgo-free (gate 3), and already approved for the
repository. prompts neither requires nor imports it directly. Adding any other
external dependency needs approval first, the user's or a delivery's.

### Adopting the libraries

appkit, agentkit and toolkit are required only at a published release, here
appkit `v0.18.0`, agentkit `v0.13.0` and toolkit `v0.3.0`, fetched through the
ordinary module proxy and checked against the checksum database (see the root
`AGENTS.md`), each set with `go get` as above. No `replace` directive, no
`go.work`, no local module cache stands in for any of them. A change that
needs a new library feature lands and releases the library first.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/prompts` and everything under `internal/`. This is the file set
the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`, so the design documents never
enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. prompts echoes back what callers give it —
prompt names, a prompt's text, system prompt and schema, a run's input, what
a model answers and the files a run writes, header values — so a test that
sends an id-shaped value lands one in the test file. No fixture carries one:
not a prompt name, a prompt's text, a model's answer, a tool call or its
result, a file's content or path, a run's input or output, a tool argument, a
header value, or an expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something. Clocks are injected: a test that opens a
  catalog hands it a `Now` it controls.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**The run seam carries the process.** prompts binds nothing itself; it serves
on the listener it is passed. Arguments, environment lookup and removal (the
`PATH` included, and the five provider keys), the pid, the inherited
listener, the output streams, the clock, the random source and the working
directory come in through the run seam design declares, `cli.Process`, with
`ScriptAfter` and `Cgroup`, and tests inject them: a test drives the deadline
through `ScriptAfter` and stands a temporary directory in for the
control-group tree through `Cgroup`. A `Run`-level test sets the version
string to one of its own and `Dir` to a temporary directory of its own, so
the catalog lands there and never in the checkout. A test never reads or
changes the real environment, clock, or randomness, and never leaves the
inherited-listener step unset, since that would take the test process's real
descriptor 3. The one exception is the working directory: a test that proves
what an empty `Dir`, or the root package's `Assets`, `Etc` or `Migrations`
does whatever the working directory is may `t.Chdir` into a temporary
directory of its own, and such a test does not call `t.Parallel`. A test
learns that the server is ready the way systemd does: it binds a Unix datagram
socket in a short temporary directory (`os.MkdirTemp("", ...)`, since a Unix
socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it
in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the
test. Drain tests are the one place a test waits on the clock, because the
drain deadline is the behavior, and they keep that wait to a few seconds. The
gates run offline as an ordinary user, with no systemd.

**The state is an isolated directory.** Each test gives prompts a working
directory, or the runs path design names, inside a test-owned temporary
directory, so the catalog and the runs under `state/runs/` are the test's
own. A test that needs a store opens its own catalog with appkit's `db.Open`
at a path in its own temporary directory, with `prompts.Migrations()` and a
clock it controls, closes the handle when it ends, and builds the store over
that handle with `store.New`. Tests may create filesystem fixtures inside that
temporary tree (an absent parent, a regular file named `state`, a file at
`state/prompts.db` that is not a database, a catalog a test's own `db.Open`
made and then changed through `DB.Write`, a run directory the test removed or
made unwritable); nothing touches `/var/opt/ikigenba/prompts`, the checkout's
`state/` or a shared file. A catalog failure is provoked with
`SetFailing(true)` on the handle the store the test handed the server was
built over, before a call or from a hook design names, never by corrupting
the file, removing permissions or closing the store. The one exception is the
test of a failed `Recover` through `Run`, where the store is `Run`'s own: from
a hook design names it opens a second handle of its own on the same file with
`db.Open` and `prompts.Migrations()`, drops the `runs` table inside that
handle's `Write`, and closes it. A disk failure on the runs directory is still
produced by removing a permission from the test's own directory, as design
names. Tests prove prompts' use of the catalog, its schema, its store and its
wiring, never SQLite's own guarantees (atomicity, durability, locking) and
never appkit's `db` contract (opening, migrations, transactions, `db
status`), which appkit's own tests prove.

**The child is real, and the provider is the test's.** A run is the binary's
own `agent` role started through the runner, and what prompts proves is that
a run is the child's agentkit session as the child runs it, so tests run the
real child and never a stand-in for it; the one exception is the probe
program `internal/runner`'s own tests run in its place, which prove the runner
and not the child. A test builds nothing and execs
nothing but the test binary's own `agent` role, through the runner, the way
design names; it starts no other program. The provider is an
`httptest` server the test serves on loopback, found through the endpoint base
URL the run spec carries for that reason and the app sets only under test; no
test reaches a real provider or holds a real key, and a key a test hands
prompts is a value of its own. The gateway a `suite` run reaches is an MCP
server the test serves on a Unix socket in its temporary tree, named by a
services file the test wrote. Every child that runs under a test runs with an
environment the test composed, through the run seam: `HOME` inside the test's
temporary directory and a `PATH` the test chose, so the developer's
environment never decides a result. A test that offers the `bash` group finds
`bash` once with `exec.LookPath`, the one look at the real environment such a
test makes, and fails, never skips, when there is none. Tests keep runs small
and quick: an output limit is reached by starting prompts with a small
`OUTPUT_MAX_BYTES`, never by writing anything near the default, and a tool-call
limit by a small `RUN_MAX_TOOL_CALLS`.

**Limits and schedules are driven, not waited out.** `PROMPT_SECONDS` and any
other deadline are proved through the clock and the seams design names, never
by waiting real seconds; a deadline passes because the test advances the clock
or fires the timer design names. A client that goes away is a request the test
cancels or a connection it closes. A test that sleeps to age a wait is a bug.
A test waits for a child it started to finish with a deadline that fails the
test.

**Events come from the test's own sink.** Where a test observes the events
prompts records, it hands prompts' writer appkit's `telemetry.Capture`, or a
services file whose `telemetry` entry names a Unix socket the test holds, as
design names. Every test builds its own catalog and runs; no test depends on
state another test made or on the order the tests run in.

**Environment variables, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read prompts cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a prompts constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. It is the only variable an
in-process test sets for prompts' own code. The exec'ing test also sets
`IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `testing.T.Setenv`, only to
compute the expected display string by calling appkit's `version.Display()`
under the same two values it composes into the child's environment. Any test
that sets a variable with `testing.T.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity.Require`
wraps every route, so a request reaches any path only with an `X-User-Id`
header. A test sets the identity headers to be a signed-in user, or omits
`X-User-Id` to exercise the missing-identity answer, and sets `Host` and
`X-Forwarded-Proto` the way nginx does, since the absolute URLs prompts builds
depend on them. What appkit's middleware and writer do is appkit's contract;
prompts' tests prove only that its handlers are wired to them, by use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing
identity header, `server/discover`), never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns.
prompts' tests never re-prove appkit's transport, agentkit's loop or
toolkit's tools.

**No test runs the page's scripts.** The pages carry appkit's launcher script
when there are services, and every page carries appkit's feedback script.
The gates have no browser and no JavaScript engine, and adding one is an
external dependency no one has approved, so a test asserts what a response
body carries and never what a script would do with it. A run's answer,
transcript and files are bytes prompts relays; a test asserts that they are
served unaltered, never what they do in a browser.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or
`assets/` — and never parses or inspects source. It proves what the design
declares by using it. Handing `cmd/prompts` to `go build` is not the test
reading it.

**One exec'ing test of the binary, and only one.** Beyond the child the runner
starts and `bash`, as above, no test starts a process: tests under
`internal/` never start prompts' server. The wiring in `cmd/prompts` can be
proved no other way, so exactly one test that execs the binary is admissible,
and it lives in `cmd/prompts`. It builds the binary into a temporary
directory and runs it with `--version`, with `manifest`, with `bogus`, with
`prompts agent` and a run spec on standard input, and bare with no socket
passed in. For the serve case it stands in for systemd:
it makes a Unix socket in a short temporary directory, passes it as
`exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child
through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates
`state/prompts.db` and its run directories under `state/runs/`. Its
environment is one the test composes, never the developer's: a `PATH` the
test chose, the five provider keys set to values of the test's own, and
non-empty `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`; the test computes the
display string it expects with `version.Display()` after setting the same two
values with `t.Setenv`, so no test spells a version. Separately it runs
`--version` with neither variable in the child's environment and expects
exactly one empty line. The test waits for `READY=1`, makes the requests
design names for the binary, over the socket, then stops the child with
`SIGTERM`, and in a second run with `SIGINT`, asserting what design states.
The binary's `--version` output, the pages' banner and footer, the MCP
`serverInfo` and `service.started` all carry that display string. Any other
test that builds, execs, waits on, or signals a process other than the
runner's child or `bash` is a bug.

## Live tests

prompts' one external call is the model provider, which the unit tests stand
in for with an `httptest` server; its other callers are browsers, agents and
sibling services on the same host. It has no live tests: the sandbox check
the delivery runs is where a real provider answers. Should a design ever call
for one, it is a `*_live_test.go` file guarded by `//go:build live` with test
functions named `TestLive*`; it proves lightly that the whole is glued
together, carries the requirement id it proves, reads its credentials from
the environment, fails (never skips) when one is missing, and runs only as
gate 6.

## Gates

Run from this directory (`prompts/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/prompts`
   — the release build, static and cgo-free, the way `devctl build` builds
   it, which is why the SQLite driver must be pure Go
4. `go test -race ./...` — needs `bash` on the `PATH` (see Toolchain)
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

`make` builds `bin/prompts` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

prompts ships in the suite release: `devctl build <sha|tag>` builds it with
every other app, and `devctl deploy <space> <sha|tag>` deploys that release.
The five provider keys its manifest names as secrets must exist on the space
(`devctl secrets push`) or in the shell that runs `sandbox up`, or the app is
refused.
