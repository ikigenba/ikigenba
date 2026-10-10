# telemetry

telemetry keeps the suite's trail of events; every service posts to it and agents query it. One Go binary serves `telemetry.<host>` on the socket it is passed as descriptor 3, behind the host's nginx. Every service on the host posts its events to `/ingest`; telemetry stores each one in its own SQLite database, `state/telemetry.db` resolved against the working directory and opened through appkit's `db` package, sweeps out records older than its retention window, and offers four read-only MCP tools over the trail at `/mcp` that agents reach through the gateway. It is built on appkit for pages, identity, MCP, the event contract and the database. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- `assets/` is the page markup and `share/icon.svg` the service's icon, shown in the banner's trail and on home. The build run never writes them; the user or the delivering agent changes them.
- `migrations/` holds the database's migrations, which the root package embeds; the build run writes it.
- `assets.go` is the root package, which embeds `assets/` and `migrations/`. `cmd/telemetry` is the binary. `internal/` is everything else, one package per concern.
- `etc/` is what the host needs: `manifest.toml` and the nginx fragment `nginx.conf` that makes `/ingest` answer 404 to the public.
- `state/` is where a running telemetry keeps `telemetry.db`; it is created at run time and never committed.
- The build run writes the Go source, the tests, `migrations/`, `go.mod`, `go.sum` and `etc/`. `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

telemetry is an app: it has no stories. Its intent is the decisions document that delivered it, or the intent agreed in conversation, together with its templates under `assets/`; the design is the record.

`assets/` holds `landing.html` (template `landing`), `about.html` (template `about`) and `tools.html` (template `tools`), each opening with a comment naming the data it receives. They follow the repository's `design/` and are inputs to the spec. The root package embeds them, since Go's `embed` reaches only files at or below its own directory, and the code parses them into the set appkit's `page.Templates` returns and executes them by name. Code never writes markup of its own, not even a fragment or an error page. Every word a person or an agent reads, and every class, id and attribute, lives in the asset and nowhere else; a test, a requirement and the source never spell one. Design names each template and the data it receives, never its text, hooks, markup or styles. A test proves a page by executing the named template with the data the design says and comparing, or by checking that a value the test supplied appears in the body; it never looks for a word or a tag. A change to copy or markup is an edit to the asset alone. A template that is missing or wrong, or one that cannot show a state the design names, is filed in `specs/issues/`; the run never edits an asset to close one.

telemetry holds no stylesheet, fonts or licences; appkit's `page` package serves them. `share/icon.svg` is the Tabler outline `heart-rate-monitor` from `design/ikigenba/icons/tabler/`, stripped as `design/README.md` asks of a service icon. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The release build is cgo-free (gate 3).
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline. The build run sets each requirement and moves to another release only when this file names one: appkit at `v0.22.0`, whose `page.Banner` has no service launcher and carries `Release` and `Commit` (see Adopting appkit).
- `modernc.org/sqlite`, the cgo-free SQLite driver, an approved dependency that arrives through appkit's `db` package; telemetry never imports it directly.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- A POSIX shell at `/bin/sh`, for the one exec'ing test.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.22.0`, fetched through the ordinary module proxy and checked against the checksum database (see the root `AGENTS.md`); the build run sets it with `go get github.com/ikigenba/ikigenba/appkit@v0.22.0`. No `replace` directive, no `go.work`, no local module cache stands in for it.

## Test files

The test files are every `*_test.go` in the module: the root package, `cmd/telemetry` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, and telemetry stores whatever services send it and echoes it back through its tools, so no fixture carries one: not a service, event, user, request id, attribute key or value, tool argument, header value or expected body.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback and Unix sockets only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state. Clocks are injected: a test that opens a database hands it a `Now` it controls.

**The run seam carries the process.** telemetry binds nothing; it serves on the listener it is passed. Arguments, environment lookup and removal, the pid, the inherited listener, the output streams, the clock, the random source and the directory the database lives under (`Process.Dir`) all come in through the run seam design declares, and tests inject them. A test never reads or changes the real environment, clock or randomness, and never leaves the inherited-listener step unset, which would take the test process's real descriptor 3. A `Run`-level test sets `Process.Version` to a string of its own and `Process.Dir` to a directory of its own, so the database lands in its temporary directory and never in the checkout. A test learns the server is ready the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp`, since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test. Drain tests are the one place a test waits on the clock, because the drain deadline is the behavior; they keep it to a few seconds. The gates run offline as an ordinary user with no systemd.

**The database is the test's own.** A test that needs a store opens its own database with appkit's `db.Open` at a path in its own temporary directory, with `telemetry.Migrations()` and a clock it controls, closes the handle when it ends, and builds the store over that handle with `store.New`; nothing touches `/var/opt/ikigenba/telemetry`, the checkout's `state/` or a shared file. A store failure is provoked with the handle's `SetFailing(true)`, never by corrupting the file or removing permissions. A write failure at the `Run` level, where the test holds no handle `Run` uses, is provoked as design names: a `DB.Write` on another handle on the database path creates a trigger that aborts every insert into `records`. Tests prove telemetry's use of the database, its schema, its store and its wiring, never SQLite's own guarantees (atomicity, durability, locking) and never appkit's `db` contract (opening, migrations, transactions, `db status`), which appkit's own tests prove.

**The clock is injected.** Every timestamp telemetry makes and every retention decision the sweep makes comes from the injected clock. Retention is proved by advancing the clock, and a sweep is triggered the way design names, never by waiting for a timer. A test that sleeps to age a record is a bug.

**Events come from the test.** A test that needs records in the trail puts them there itself, by posting to `/ingest` on the handler it built as a sibling would, or by inserting them through telemetry's store or sink, with the times, services, users, request ids and attributes the case needs. A test that observes telemetry's own events hands its writer appkit's `telemetry.Capture` or reads them back from its own store, as design names. Every test builds its own database; none depends on another's records or on test order.

**Environment variables, set by the test.** appkit's constructors read `IKIGENBA_SERVICES` (`services.Variable`) from the process environment, the one read telemetry cannot route through the run seam; in the binary only `main` makes it. A test that reaches that read, directly or through a telemetry constructor, first sets the variable with `t.Setenv` to a services file it wrote or to the empty string. It is the only variable an in-process test sets for telemetry's own code. The exec'ing test also sets `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `t.Setenv`, so it may call appkit's `version.Read()` and `version.Display()` under the same two values it composes into the child's environment; the commit it sets is longer than seven characters, so a `main` that skipped shortening would fail. Any test that sets a variable with `t.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity` middleware wraps every route but ingest, so a request reaches `/`, the about screen, the tools page or `/mcp` only with an `X-User-Id` header. A test sets the identity headers, or omits `X-User-Id` to exercise the missing-identity answer. A post to `/ingest` carries no identity headers, as a sibling's does. What the middleware, ingest handler and writer do is appkit's contract; telemetry's tests prove only that its handler is wired to them.

**MCP through appkit's client.** A test drives `/mcp` as a client would: it serves its handler on loopback or a Unix socket and calls it with appkit's `mcp.Client`, which forwards the caller's identity headers, and asserts on the `mcp.Result` and `mcp.ToolInfo` returned. Raw HTTP to `/mcp` is only for what the client cannot send, such as a missing identity header or `server/discover`. telemetry's tests never re-prove appkit's transport.

**No test runs the page's scripts.** Every page carries appkit's feedback script. The gates have no browser or JavaScript engine, and adding one is an unapproved dependency, so a test asserts what a response body carries, never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory, not `.go`, `go.mod`, `go.sum`, `etc/`, `share/` or `assets/`, and never parses source. It proves what design declares by using it. Handing `cmd/telemetry` to `go build` is not reading it.

**One exec'ing test, and only one.** Tests under `internal/` never start a process. The wiring in `cmd/telemetry` can be proved no other way, so exactly one test execs the binary, and it lives in `cmd/telemetry`. It builds the binary into a temporary directory and runs it with `--version`, with `manifest`, with `bogus`, and bare with no socket. For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The child runs in a test-owned working directory, where it creates its database under `state/`, with an environment the test composes, carrying non-empty `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`; the test computes the release and commit it expects with `version.Read()` and the display string with `version.Display()` after setting the same two values with `t.Setenv`, so no test spells a version. Separately it runs `--version` with neither variable in the child's environment and expects exactly one empty line. Where design names the binary's trail, the events carry that display string as their version. The test waits for `READY=1`, makes the requests design names over the socket, then stops the child with `SIGTERM`, and in a second run with `SIGINT`, asserting what design states. Any other test that builds, execs, waits on or signals a process is a bug.

## Live tests

telemetry calls no external service: its callers are sibling services, which the unit tests stand in for, so it has no live tests. Should design ever call for one, it is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions; it proves lightly that the whole is glued together, carries the id it proves, reads credentials from the environment, fails rather than skips when one is missing, and runs only as gate 6.

## Gates

Run from `telemetry/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/telemetry`, the release build as `devctl build` makes it, static and cgo-free
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`; the cache lives in this worktree's git directory so worktrees never share it, and parallel runners let several sub-projects lint at once (`make lint` runs this form)
6. `make live`, only when the phase's diff against the last phase commit adds or modifies a `*_live_test.go` file; otherwise it is not run and not counted. A missing credential is then a missing tool: file an issue, never pass or skip.

Gate 5 also flags an undocumented `package main` (`revive`) and an `http.Server` without `ReadHeaderTimeout` (`gosec` G112); both are fixed in code, never suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by id.

## Build

`make build` (the default) builds `bin/telemetry`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly, and the exec'ing test builds its own binary.

## Deploy

telemetry ships in the suite release (`devctl build <sha|tag>`, `devctl deploy <space> <sha|tag>`); it has no build, version or tag of its own.
