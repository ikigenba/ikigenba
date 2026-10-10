# repos

repos is the suite's home for git repositories, served over smart HTTP and MCP. One Go binary serves `repos.<host>` on the socket it is passed as descriptor 3, behind the host's nginx. It keeps each user's repositories as bare git repositories under `state/repos/`, catalogued in its own SQLite database, `state/repos.db`, which it opens through appkit's `db` package and is the only writer of, serves them at `/<name>.git/` through the host's `git http-backend`, and offers six MCP tools at `/mcp` that agents reach through the gateway. It is built on appkit for pages, identity, MCP, telemetry, the event bus (`events`) and the catalog's database (`db`: its handle and its migrations). The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `design/`.
- `assets/` is the page markup and `share/icon.svg` the service's icon, shown in the banner's trail and on home. The build run never writes them; the user or the delivering agent changes them.
- `repos.go` is the root package, which embeds `assets/` and the catalog's migrations under `migrations/`. `cmd/repos` is the binary. `internal/` is everything else, one package per concern.
- `etc/` is what the host needs: `manifest.toml` and the nginx fragment `nginx.conf` that lets a push stream through.
- The build run writes the Go source, the tests, `go.mod`, `go.sum`, `migrations/` and `etc/`. `state/` is where a running repos keeps `repos.db` and its repositories; it is created at run time and never committed. `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

repos is an app, so it has no stories: its intent is the decisions document that delivered it, or conversation, plus its templates under `assets/`, and the design is the record.

`assets/` holds `landing.html` (template `landing`), `about.html` (template `about`) and `tools.html` (template `tools`), each opening with a comment naming the data it receives and the hooks it draws for people and the stylesheet. They follow the repository's `design/` and are inputs to the spec: the build run never writes them; the user or the delivering agent does, before design is drafted. The root package embeds them, since Go's `embed` reaches only files at or below its own directory, and the code parses them into the set appkit's `page.Templates` returns and executes them by name. Code never writes markup of its own, not even a fragment or an error page. Every word a person or an agent reads on a page, and every class, id and attribute, lives in its template and nowhere else; a test, a requirement and the source never spell one. A string the code emits without a template, such as an error line or a description, is a named constant in the source; design declares its name, never its words, and a test references the constant. Design names each template and the data it receives, never its text, hooks, markup or styles. A test proves a page by executing the named template with the data design states and comparing, or by checking that a value the test supplied appears in the body; it never looks for a word or a tag. A change to copy or markup is an edit to the asset alone. A template that is missing or wrong, or that cannot show a state design names, is filed in `specs/issues/`; the run never edits an asset to close one.

repos holds no stylesheet, fonts or licences; appkit's `page` package serves them. `share/icon.svg` is the Tabler outline `git-branch` from `design/ikigenba/icons/tabler/`, stripped as `design/README.md` asks of a service icon. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The release build is cgo-free (gate 3).
- `git` 2.x on the `PATH`, with `git http-backend`. repos runs the host's git for every repository operation and links no git library; opsctl provisions it on a host, and the tests run it too, so gate 4 fails without it.
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline. The build run sets each requirement and moves to another release only when this file names one: appkit at `v0.22.0`, whose `page.Banner` has no service launcher and carries `Release` and `Commit` (see Adopting appkit), and `golang.org/x/sys`.
- `golangci-lint` v2, configured by `.golangci.yml` here.
- A POSIX shell at `/bin/sh`, for the one exec'ing test.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's. The SQLite driver is appkit's: its `db` package brings `modernc.org/sqlite`, pure Go, so the release build stays cgo-free. repos neither requires nor imports it directly.

### Adopting appkit

appkit is required only at a published release, here `v0.22.0`, fetched through the ordinary module proxy and checked against the checksum database (see the root `AGENTS.md`); the build run sets it with `go get github.com/ikigenba/ikigenba/appkit@v0.22.0`. No `replace` directive, no `go.work`, no local module cache stands in for it.

## Test files

The test files are every `*_test.go` in the module: the root package, `cmd/repos` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, and repos echoes back what callers give it, so no fixture carries one: not a repository name, ref, file path or content, commit message or author, tool argument, header value or expected body.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback and Unix sockets only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait; a test that opens a catalog hands it a `Now` it controls), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config, git config or real state.

**The run seam carries the process.** repos binds nothing; it serves on the listener it is passed. Arguments, environment lookup and removal (the `PATH` git is found on included), the pid, the inherited listener, the output streams, the clock, the random source and the working directory all come in through the run seam design declares, and tests inject them. A `Run`-level test sets `Version` to a string of its own and `Dir` to a temporary directory of its own, so the catalog lands there and never in the checkout. A test never reads or changes the real environment, clock or randomness, and never leaves the inherited-listener step unset, which would take the test process's real descriptor 3. The one exception is the working directory: a test that proves what an empty `Dir` or the root package's `Assets`, `Etc` or `Migrations` does whatever the working directory is may `t.Chdir` into a temporary directory of its own, and such a test does not call `t.Parallel`. A test learns the server is ready the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp`, since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test. Drain tests are the one place a test waits on the clock, because the drain deadline is the behavior; they keep it to a few seconds. The gates run offline as an ordinary user with no systemd.

**The state is an isolated directory.** Each test gives repos a working directory, or the repositories path design names, inside its own temporary directory, so the catalog and the repositories are the test's own. A test that needs a store opens its own catalog with appkit's `db.Open` at a path in its own temporary directory, with `repos.Migrations()` and a clock it controls, closes the handle when it ends, and builds the store over that handle with `store.Open`. Filesystem fixtures (an absent parent, a regular file named `state`, a file at `state/repos.db` that is not a database, a catalog a test's own `db.Open` made and then changed through `DB.Write`, a repository missing its `HEAD` or `objects/`, a directory with permissions removed) live in that tree; nothing touches `/var/opt/ikigenba/repos`, the checkout's `state/` or a shared file. A catalog failure is provoked with `SetFailing(true)` on the handle the store the test handed the server was built over, never by corrupting the file, removing permissions or closing the store; a disk failure on the repositories' directories is still produced by removing a permission from the test's own directory, as design names. Tests prove repos' use of the catalog, its schema, its store and its wiring, never SQLite's own guarantees (atomicity, durability, locking) and never appkit's `db` contract (opening, migrations, transactions, `db status`), which appkit's own tests prove.

**git is real, and its environment is the test's.** repos fronts `git http-backend`, and what it proves is that git's own protocol passes through, so tests use the host's real `git` and never a stand-in. A test finds it once with `exec.LookPath("git")`, the one look at the real environment a test makes, and fails rather than skips without it; it hands repos a `PATH` holding that directory through the run seam, and one without it to exercise the missing-git refusal. Every git that runs under a test, started by repos or by the test, gets an environment the test composed: `HOME` and `XDG_CONFIG_HOME` in the test's temporary directory, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` naming a file there or `/dev/null`, `GIT_TERMINAL_PROMPT=0`, and no credential helper. A test runs `git` itself in two ways only: as the client of a repos it serves on loopback or a Unix socket, with the identity headers as `http.extraHeader` in its own config, or against a directory in its temporary tree to make a fixture or read back a ref, an object or `ikigenba.*` config. A fixture commit fixes its author and committer dates so its sha is stable. Repositories and pushes stay small: a size limit is reached by starting repos with a small `PUSH_MAX_BYTES` or `REPO_MAX_BYTES`, never by writing near the defaults.

**Limits are driven, not waited out.** Slots, queues, the repository lock, `QUEUE_SECONDS` and `OPERATION_SECONDS` are proved through the clock and the seams design names, never by waiting real seconds. A busy slot or lock is held by an operation the test controls, such as a push whose request body it holds open on an unclosed `io.Pipe`, and released by the test; a deadline passes because the test advances the clock or fires the timer design names; a client that goes away is a request the test cancels or a connection it closes. The maintenance cycle is triggered the way design names, and `MAINTENANCE_HOURS` is proved by what the injected clock makes due. A test waits for git it started with a deadline that fails the test. A test that sleeps to fill a slot, age a wait or reach a cycle is a bug.

**Events come from the test's own sink.** A test that observes repos' events hands its writer appkit's `telemetry.Capture` (bus events are observed through `events.Capture` on the run seam's `EventSink`), or a services file whose `telemetry` entry names a Unix socket the test holds. Every test builds its own catalog and repositories; none depends on another's state or on test order.

**Environment variables, set by the test.** appkit's constructors read `IKIGENBA_SERVICES` (`services.Variable`) from the process environment, the one read repos cannot route through the run seam; in the binary only `main` makes it. A test that reaches that read, directly or through a repos constructor, first sets the variable with `t.Setenv` to a services file it wrote or to the empty string. It is the only variable an in-process test sets for repos' own code. The exec'ing test also sets `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with `t.Setenv`, so it may call appkit's `version.Read()` and `version.Display()` under the same two values it composes into the child's environment; the commit it sets is longer than seven characters, so a `main` that skipped shortening would fail. Any test that sets a variable with `t.Setenv` does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity` middleware wraps every route except the two socket-only event-bus paths `/events` and `/declarations`, which nginx hides with 404, so a request reaches any other path only with an `X-User-Id` header. A test sets the identity headers, or omits `X-User-Id` to exercise the missing-identity answer. The middleware's behavior is appkit's contract; repos' tests prove only that its handler is wired to it.

**MCP through appkit's client.** A test drives `/mcp` as a client would: it serves its handler on loopback or a Unix socket and calls it with appkit's `mcp.Client`, which forwards the caller's identity headers, and asserts on the `mcp.Result` and `mcp.ToolInfo` returned. Raw HTTP to `/mcp` is only for what the client cannot send, such as a missing identity header or `server/discover`. repos' tests never re-prove appkit's transport.

**No test runs the page's scripts.** Every page carries appkit's feedback script. The gates have no browser or JavaScript engine, and adding one is an unapproved dependency, so a test asserts what a response body carries, never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory, not `.go`, `go.mod`, `go.sum`, `etc/`, `share/` or `assets/`, and never parses source. It proves what design declares by using it. Handing `cmd/repos` to `go build` is not reading it.

**One exec'ing test of the binary, and only one.** Beyond `git`, no test starts a process, and tests under `internal/` never start repos. The wiring in `cmd/repos` can be proved no other way, so exactly one test execs the binary, and it lives in `cmd/repos`. It builds the binary into a temporary directory and runs it with `--version`, with `manifest`, with `bogus`, and bare with no socket. For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The child runs in a test-owned working directory, where it creates `state/repos.db` and `state/repos/`, with an environment the test composes: the git environment above, with git's directory on `PATH`, and non-empty `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`; the test computes the release and commit it expects with `version.Read()` and the display string with `version.Display()` after setting the same two values with `t.Setenv`, so no test spells a version. Separately it runs `--version` with neither variable in the child's environment and expects exactly one empty line. The test waits for `READY=1`, makes the requests design names over the socket, then stops the child with `SIGTERM`, and in a second run with `SIGINT`, asserting what design states; the binary's `--version` output, the MCP `serverInfo` and `service.started` all carry that display string, and the landing page's body the release and commit. Any other test that builds, execs, waits on or signals a process other than `git` is a bug.

## Live tests

repos calls no external service: its callers are git clients and sibling services, which the unit tests stand in for, and the git it runs is local, so it has no live tests. Should design ever call for one, it is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions; it proves lightly that the whole is glued together, carries the id it proves, reads credentials from the environment, fails rather than skips when one is missing, and runs only as gate 6.

## Gates

Run from `repos/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/repos`, the release build as `devctl build` makes it, static and cgo-free
4. `go test -race ./...`, which needs `git` on the `PATH`
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

`make build` (the default) builds `bin/repos`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly, and the exec'ing test builds its own binary.

## Deploy

repos ships in the suite release: `devctl build <sha|tag>` and `devctl deploy <space> <sha|tag>`.
