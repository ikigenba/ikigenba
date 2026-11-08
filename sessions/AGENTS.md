# sessions

An app of the Ikigenba platform: the space's record of agent sessions, served at `sessions.<host>`. One Go binary serves on the listening socket it is passed as descriptor 3 (`/run/ikigenba/sessions.sock` on a host), behind the host's nginx. sessions keeps content-free turns of the agent sessions people run in Claude Code, Codex CLI and Grok CLI, pushed by the `turns` command from each person's machine, built to be used by agents and scripts through MCP, not by a person at a keyboard. A turn is one record: its session, the parent session when it ran as a subagent, the host and the person who pushed it, the working directory, the tool, the model, the moment, whose turn it was, and the token counts the tool reported; no text of any prompt, answer, thought, tool call or tool result is kept, save a session's first command and its argument. A push is idempotent: a turn pushed twice is one turn. From the turns sessions derives attention, wall time, agent time, tokens, dollars and kind of work: attention under an away limit it holds as a setting, dollars from a price table it holds, and kind from a rule table it holds; a session that matches no rule is untagged, and changing the setting, a price or a rule recomputes every figure, since the turns are kept. The person is the account whose credential pushed the turns. Any signed-in user of the space reads every person's sessions and changes the tables, as themselves. Nothing in it names a space.

Among its siblings sessions calls none, and outside the suite it reaches nothing. It keeps its turns, rules, prices and settings in its own SQLite database, `state/sessions.db` resolved against the working directory and opened through appkit's `db` package; it is the only writer of it. It emits nothing on the bus, accepts none, and records every change in the trail: the tool, the caller, what changed, and the result. Its manifest sets `guests = false`: nginx lets no request without a credential through, to the pages and to `/mcp` alike, and its nginx fragment answers the bus's two paths 404 at the public name. At `/mcp` it offers the MCP tools design catalogs, which agents and the `turns` command reach through the MCP gateway; they are its only API. At `/` it serves a landing page showing the business's totals for this week and the eight before it by kind, with the untagged share and nothing per person, a tools page at `/tools`, an about screen at `/about` and a not-found page; the pages carry no form and change no record. On a host it runs as `bin/sessions` of its release with `/var/opt/ikigenba/sessions` as its working directory and its environment from `/etc/opt/ikigenba/sessions/env`; a developer runs the same binary from the checkout. The module path is `github.com/ikigenba/ikigenba/sessions`. It requires appkit (`github.com/ikigenba/ikigenba/appkit`) and uses appkit's packages `page` (the banner and footer, and the shared static files under `/_appkit/`), `identity` (the caller nginx authenticated, required at `/mcp` and optional before the pages), `mcp` (the server mounted at `/mcp`, and the client the tests drive it with), `telemetry` (the event contract, the request middleware and the writer sessions' events go through), `db` (the database's handle and its migrations), `paging` (the listing tokens), `services` (the services file) and `version` (the identity and its display string). It requires no other module. The contract is the documents in `specs/design/`. This file restates none of it. sessions is an app, so it has no stories: its intent is the decisions document that delivered it, or what was agreed in conversation, together with its templates under `assets/`, and the design is the record.

This sub-project is spec-driven: `specs/design/` defines the contract, and the build run writes the Go source, the tests, `go.mod`'s requirements and `go.sum`, the migrations under `migrations/` (`0001_sessions.sql`), which the root package embeds, and everything under `etc/` (`etc/manifest.toml` and the nginx fragment `etc/nginx.conf`). The delivery that created sessions seeded `go.mod` on appkit; the build run owns it and `go.sum` from then on and changes them as design says. It never writes `assets/`, `share/`, this `AGENTS.md`, the `Makefile` or `.golangci.yml`. `state/` is where a running sessions keeps `sessions.db`; it is created at run time and never committed. See the `spec` and `build-spec` skills. Everything below is what the build run computes the gap and runs the gates against; it is written outside the run and read-only to it.

## Assets

`assets/` holds sessions' markup: Go `html/template` files, each opening with a comment naming the data it receives: `landing.html`, `tools.html`, `about.html` and `notfound.html`. Every page is shown only to a signed-in user; a guest is sent to sign in. They are written by the user or the delivering agent, following the repository's `design/`, and are inputs to the spec: the build run reads them and never writes them. The code parses them into the set appkit's `page.Templates` returns and executes them by template name; it never writes markup of its own, not even a fragment or an error page. Go's `embed` reaches only files at or below the embedding package's directory, so the module's root package (the directory holding `go.mod`) embeds `assets/`, and design names what it exports.

Every word a person or an agent reads, and every class, id and attribute, lives in exactly one place, a template under `assets/` or a named copy constant in the source; a test and a requirement never spell one. Design names each template and the data it receives, never its text, hooks, markup or styles. A test proves a page by executing the named template with the data the design says and comparing, or by checking that a value the test supplied appears in the body; it never looks for a word or a tag. A change to copy or markup is an edit to the asset alone. A needed template that is missing or wrong, or a template that cannot show a state the design names, is an issue: the run files it in `specs/issues/` and never edits the asset to close it.

sessions holds no copy of the stylesheet, fonts, or licences; appkit's `page` package embeds and serves them. `share/icon.svg` is sessions' icon, shown in the banner's trail and on home: Tabler's `clock-play` outline, without its class, width, height, or invisible bounding path, as `design/README.md` asks of a service icon. The build run never writes it. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and without one gate 4 fails with `go: -race requires cgo`. The release build itself is cgo-free, which gate 3 proves.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is committed, and the gates themselves run offline. `go.mod` starts with the one requirement below, set by the delivery that created sessions; the build run sets its `go.sum` lines, and moves to another release only when this file names one:
  - appkit `v0.32.0`, set with `go get github.com/ikigenba/ikigenba/appkit@v0.32.0`: a release that exports the `db`, `paging` and `version` packages, and whose `page.Banner` carries the trail, the home link and the tools flag the templates use, with `Release` and `Commit`. See Adopting appkit below.

  appkit and the modules it pulls in are sessions' only dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through it.
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate runs through it

**The SQLite driver is appkit's.** sessions stores its state in SQLite through appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so the release build stays cgo-free (gate 3), and already approved for the repository. sessions neither requires nor imports it directly. Adding any other external dependency needs approval first, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.32.0`, fetched through the ordinary module proxy and checked against the checksum database (see the root `AGENTS.md`); it is set with `go get github.com/ikigenba/ikigenba/appkit@v0.32.0`. No `replace` directive, no `go.work`, no local module cache stands in for it.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root package, `cmd/sessions` and everything under `internal/` (`internal/cli`, `internal/settings`, `internal/server`, `internal/web`, `internal/store`, `internal/trail`, `internal/pages` and `internal/tools`). This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/`, `assets/` and `share/` hold no `*_test.go`, so the design documents never enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape: such a literal anywhere in a `*_test.go` file counts as a covered id. sessions echoes back what callers give it — session ids, hosts, working directories, models, first commands and their arguments, rule prefixes, user ids and emails, request ids, the services file's URLs — and tests echo session ids and working directories above all, since every pushed turn names them and every caller-chosen token comes back, so a test that sends an id-shaped value lands one in the test file. No fixture carries one: not a header value, a query parameter, a tool argument, a session id, a host, a directory, a model, a command or its argument, a URL, a services file entry, an event's payload, or an expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, and no real credentials.
- Deterministic: time, randomness, and environment are injected; no test sleeps to wait for something. Clocks and pauses are injected: a test hands in a `Now`, a `Sleep` and a `Rand` it controls.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a temporary directory.
- Isolated: a test touches only its own temporary directory, never the developer's home, config, or real state.

**Nothing leaves the suite.** No test reaches anything outside loopback and its own Unix sockets. sessions has no HTTP client and calls no sibling, and a test proves the absence by use, as design names: the `Process` shape, the keys `Run` looks up, and a services file naming every sibling on a listener that records each connection, none of which is reached. A test of the pages writes a services file whose `auth` entry and at least one other sibling's each name a Unix socket on which a fake records each connection, and proves by the fakes' records that no page opens one. Telemetry is a capture. No test reads a session file of Claude Code, Codex CLI or Grok CLI; turns reach sessions only in a push the test composes.

**The run seam carries the process.** sessions binds nothing itself; it serves on the listener it is passed. Arguments, environment lookup and removal, the pid, the inherited listener, the output streams, the clock, the pause between delivery attempts, the random source and the working directory come in through the run seam design declares (`cli.Run` with `cli.Process`), and tests inject them. A `Run`-level test sets the version string to one of its own and `Dir` to a temporary directory of its own, so the database lands there and never in the checkout. A test never reads or changes the real environment, clock, or randomness, and never leaves the inherited-listener step unset, since that would take the test process's real descriptor 3. The one exception is the working directory: a test that proves what an empty `Dir`, or the root package's `Assets`, `Etc` or `Migrations` does whatever the working directory is may `t.Chdir` into a temporary directory of its own, and such a test does not call `t.Parallel`. A test learns that the server is ready the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp("", ...)`, since a Unix socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test. A test waits on the clock in one place only, because there the deadline is the behavior: a drain test, which keeps that wait to a few seconds. The gates run offline as an ordinary user, with no systemd.

**The state is an isolated directory.** Each test gives sessions a working directory inside a test-owned temporary directory, so the database is the test's own. A test that needs a store opens its own database with appkit's `db.Open` at a path in its own temporary directory, with the root package's `Migrations()` and a clock it controls, closes the handle when it ends, and builds the store over that handle with `store.New`. Tests may create filesystem fixtures inside that temporary tree (a regular file named `state`, a file at `state/sessions.db` that is not a database, a database a test's own `db.Open` made and then changed through `DB.Write`, turns, rules and prices a test's own store recorded); nothing touches `/var/opt/ikigenba/sessions`, the checkout's `state/` or a shared file. A database failure is provoked with `SetFailing(true)` on the handle the store the test handed the server or handler was built over, or by closing that handle, as design names, never by corrupting the file or removing permissions. Tests prove sessions' use of the database, its schema, its store and its wiring, never SQLite's own guarantees (atomicity, durability, locking) and never appkit's `db` contract (opening, migrations, transactions, `db status`), which appkit's own tests prove; they prove what a store holds by reopening its path, never by reading tables.

**Time is driven, not waited out.** The times sessions keeps are the ones pushed to it; the one it reads itself, the present moment that places the current week, is proved through the injected `Now`: a test advances the clock it handed in, never waits real seconds. The writer's pause between delivery attempts is the injected `Sleep`. A test that sleeps to age a record is a bug. A test waits for something it started to finish (an event reaching a capture) with a deadline that fails the test.

**Events come from the test's own sink.** Where a test observes the events sessions records, it hands sessions' writer appkit's `telemetry.Capture`, or a services file whose `telemetry` entry names a Unix socket the test holds, as design names. Every test builds its own database; no test depends on state another test made or on the order the tests run in.

**Environment variables, set by the test.** appkit's constructors read `IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is the one environment read sessions cannot route through the run seam, and in the binary only `main` makes it. A test that reaches such a read, directly or through a sessions constructor, first sets that variable with `testing.T.Setenv` to a services file it wrote or to the empty string, so the developer's environment never decides a result. It is the only variable an in-process test sets for sessions' own code; `DRAIN_SECONDS` reaches `Run` through the `LookupEnv` the test gives it. The exec'ing test also sets `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `testing.T.Setenv`, so it may call appkit's `version.Read()` and the identity's `String()` under the same two values it composes into the child's environment; the commit it sets is longer than seven characters, so a `main` that skipped shortening would fail. Any test that sets a variable with `testing.T.Setenv` does not call `t.Parallel`.

**Process-wide state is not shared.** A test that changes process-wide state, such as the standard `log` package's output, restores it when it ends and does not call `t.Parallel`, so no other test's line lands in its buffer.

**Identity comes from headers the test sets.** appkit's `identity.Require` wraps `/mcp`, and `identity.Optional` the pages and `/_appkit/`. A test sets the identity headers to be a signed-in user, or omits `X-User-Id` to be a guest: at `/mcp` it exercises the missing-identity answer, and on a page the redirect to sign in. It sets `Host` and `X-Forwarded-Proto` the way nginx does. Where it matters it acts as two users, and it proves that each change's event in the trail names its caller, by the capture, and that a turn it pushed is recorded as its caller's, by the record. What appkit's middleware and writer do is appkit's contract; sessions' tests prove only that its handlers are wired to them, by use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would: it serves the handler it built on loopback or a Unix socket and calls it with appkit's `mcp.Client`, passing the caller whose identity headers the client forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing identity header, `server/discover`), never a substitute for the client. Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns. sessions' tests never re-prove appkit's transport.

**Pages are raw HTTP.** A test drives `/`, `/tools`, `/about` and any other path as a browser would: raw HTTP requests on the handler it built, served with `httptest` or on a Unix socket, with the identity headers where the route takes them, and never follows a redirect.

**The addresses are the test's own.** The banner's profile and sign-out addresses and a guest's sign-in address are built on the services file's entry `auth`, as design names, or, with none, on the request's scheme and host. A test chooses them: it writes a services file into its own temporary directory whose entry names a URL, or none, and hands its path in the config or names it in `IKIGENBA_SERVICES` through the `LookupEnv` it gives `Run`.

**No test runs the page's scripts.** Every page carries appkit's feedback script. The gates have no browser and no JavaScript engine, and adding one is an external dependency no one has approved, so a test asserts what a response body carries and never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory — no `.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/` or `assets/` — and never parses or inspects source. It proves what the design declares by using it. Handing `cmd/sessions` to `go build` is not the test reading it.

**One exec'ing test of the binary, and only one.** No test starts a process: tests under `internal/` never start sessions. The wiring in `cmd/sessions` can be proved no other way, so exactly one test that execs the binary is admissible, and it lives in `cmd/sessions`. It builds the binary into a temporary directory and runs it with `--version`, with `manifest`, with `bogus`, and bare with no socket passed in. For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The child runs in a test-owned temporary working directory, where it creates `state/sessions.db`. Its environment is one the test composes, never the developer's, with non-empty `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`; the test computes the release and commit it expects with `version.Read()` and the display string with its `String()` after setting the same two values with `t.Setenv`, so no test spells a version. Separately it runs `--version` with neither variable in the child's environment and expects exactly one empty line. The test waits for `READY=1`, makes the requests design names for the binary, over the socket, then stops the child with `SIGTERM`, and in a second run with `SIGINT`, asserting what design states. It calls the tools design names. The binary's `--version` output, the MCP `serverInfo` and `service.started` all carry that display string, and the pages' footer the release and commit. Any other test that builds, execs, waits on, or signals a process is a bug.

## Live tests

There are none. No test reaches outside the suite: sessions calls no sibling and reads no session file, and turns reach it only in a push. The sandbox walk drives every page and tool end to end, and the `turns` command's real pushes are proved on a space, at the delivery's finish line.

## Gates

Run from this directory (`sessions/`), in order; every command must exit 0. No skipped tests, no disabled linters laundering a failure. A per-finding suppression comment (`//nolint` and the like) is a skip: the run never adds one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/sessions` — the release build, static and cgo-free, the way `devctl build` builds it, which is why the SQLite driver must be pure Go
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners` — the cache lives in this worktree's git directory, so worktrees never share it, and `--allow-parallel-runners` lets gates for several sub-projects lint at once; `make lint` runs this form

Gate 5 also flags an undocumented `package main` (`revive`) and an `http.Server` without `ReadHeaderTimeout` (`gosec` G112); both are fixed in code, never suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make` builds `bin/sessions` from the checkout (`make build`). `make fmt` rewrites unformatted files, and `make test` and `make lint` run the test and lint gates. The gates do not go through `make`: they call the Go tool directly, and the one test that needs a binary builds its own into a temporary directory.

## Deploy

sessions ships in the suite release: `devctl build <sha|tag>` builds it with every other app, its part of the release holding `bin/sessions`, `etc/` and `share/icon.svg`, and `devctl deploy <space> <sha|tag>` deploys that release. Its services file names telemetry and auth. sessions is served at `sessions.<host>`, needs no outbound access and holds no secret of its own. The host's backup archives `state/` whole, so `sessions.db` is taken with it; it holds the turns, rules, prices and settings, and no text of any session and no secret.
