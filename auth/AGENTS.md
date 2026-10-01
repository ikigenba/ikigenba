# auth

An app of the Ikigenba platform: one Go binary that serves the auth service on
the socket systemd passes it (`/run/ikigenba/auth.sock` on a host), behind the
host's nginx, which also sends it the identity subrequest for every other app.
On a host it runs as `/opt/auth/bin/auth` with `/opt/auth` as its working
directory and its environment from `/opt/auth/etc/env`; a developer runs the
same binary from the checkout. The module path is `github.com/ikigenba/ikigenba/auth`. It requires appkit
(`github.com/ikigenba/ikigenba/appkit`), which supplies the banner, the
service launcher, and the shared stylesheet, fonts, licences and launcher
script under `/_appkit/`. Its version declaration and run seam are
design D01 (`specs/design/D01-layout-and-run-seam.md`); this file does not
restate them.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the source, the tests, `go.mod`, and `etc/manifest.toml`. See
the `spec` and `build-spec` skills. Everything below is what the build run
computes the gap and runs the gates against; it is human-authored and read-only
to the run.

## Assets

auth holds no copy of the stylesheet, fonts, or licences; appkit embeds and
serves them. `share/icon.svg` is auth's icon in the service launcher: the
Tabler outline `fingerprint` from `design/ikigenba/icons/tabler/`, without its
class, width, height, or invisible bounding path, as `design/README.md` asks of
a launcher icon. It is human-authored; the build run never writes it.
`devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- the appkit module at the version `go.mod` requires, in the Go module cache
  (`go mod download` fetches it once, online); `go.sum` is committed, and the
  gates themselves run offline
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

The toolchain names the tools the gates and the `Makefile` need; it pins no Go
library. Library
release selection is `go.mod`'s job, and the build run writes it.

## Test files

The sub-project's tests are all `*_test.go` files in the module: `cmd/auth` and
everything under `internal/`. This is the file set the canonical gap greps for
requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`.` covers `cmd/` and `internal/`; `specs/` holds no `*_test.go`, so the design
documents never enter the test side of the grep.

**No id-shaped-literal hazard.** A requirement tag has the shape
`R-XXXX-XXXX`: an `R-` prefix and two hyphen-separated groups. auth's opaque
values carry no such shape. Its ids (user, session, login-state, token) are
26-char Crockford base32 (`internal/idcodec`, alphabet excludes I L O U, no
hyphen); its token secrets are `ikp_` + 52 Crockford chars; its stored secret
hashes are 64 lowercase-hex chars. None contains the hyphenated `R-....-....`
pattern the grep matches, so no auth literal — id, secret, or hash — can be
mistaken for a requirement tag.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs. Live
tests follow Live tests below.

- Offline: no network beyond loopback, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**Offline, deterministic, no fixed ports, no sleeping.** The gates run offline
as an ordinary user, with no systemd. Every input reaches the code through the
run seam (`cli.Process`, design D01) — `Args`, `LookupEnv`, `Unsetenv`, `Pid`,
`Stdout`, `Stderr`, `Inherit`, `Now`, `Rand`, `OIDCIssuer`, `DBSource`,
`Banner` — and tests supply each one; a test never reads or changes the real environment,
clock, or randomness, or accesses filesystem or network state outside the
isolated fixtures described below. A test whose result depends on the
developer's machine, wall-clock, environment, or a port already in use is a
bug.

- **Google is faked, never called.** auth talks to Google's OIDC issuer only
  through `Process.OIDCIssuer`. Tests stand up a loopback OIDC issuer (its
  discovery document, keys, and token endpoint on `127.0.0.1:0`) and inject its
  URL through `Process.OIDCIssuer`, so no test reaches `accounts.google.com`.
- **The database is an isolated source.** Tests pass a database path or
  file-backed sqlite DSN confined to a test-owned temporary directory, or an
  in-memory sqlite DSN, through
  `Process.DBSource`. They may create and inspect filesystem fixtures within
  that temporary tree, including an absent database parent, a regular file
  obstructing that parent, a file at the database path that is not a SQLite
  database, and a database file whose write permission the test removed (the
  gates run as an ordinary user, so the permission holds); nothing touches `/opt/auth` or a shared file. A
  database failure mid-request (the `500` of D03) is produced the same way,
  by making the test's own database fail after the server has it — for
  example by closing the `*store.Store` a server-level test handed
  `server.New`.
- **The clock is injected.** `Process.Now` supplies every timestamp. Time-based
  behaviour — session idle (15m) and cap (18h), the 30d token login window,
  token expiry (30d/90d/365d) — is tested by advancing the value `Now` returns,
  never by sleeping. A test that sleeps to age a session or a token is a bug.
- **Randomness is injected.** `Process.Rand` supplies the bytes `idcodec`
  mints ids and secrets from, so id and secret values under test are
  reproducible.
- **The banner source is the test's own.** `Process.Banner` and
  `server.Config.Banner` are a function the test writes, returning whatever
  services the case needs (D01, D05); no test calls `appkit.New`, which reads
  `IKIGENBA_SERVICES` from the real environment. A test may call appkit's
  other exported functions, to render the banner it expects, for instance.
- **No test runs the page's scripts.** The pages carry the Copy button's
  inline script and, when there are services, appkit's launcher script. The
  gates have no browser and no JavaScript engine, and adding one is an
  external dependency no one has approved, so a test asserts what a response
  body carries and never what a script would do with it.
- **Tests read no checkout file.** No test reads this module's source,
  `go.mod`, `go.sum`, `etc/`, or `share/`, walks its directories, or parses
  or reflects over its code. Structure is proven by use: a test imports the
  package, constructs the type, calls the function, or runs the built binary
  (the one exec'ing test below), and asserts the outcome. There is no
  exception.

**auth binds nothing; a test makes its listener.** auth serves on the listener
it is passed (D01, D03). A test that needs a listener makes its own: a
loopback TCP listener on `127.0.0.1:0`, where the kernel chooses the port, or
a Unix socket in a temporary directory — never a fixed port such as 3001. A
`cli.Run`-level test of the serve path hands `Run` that listener through
`Process.Inherit`, sets `LISTEN_PID` to the `Process.Pid` it chose and
`LISTEN_FDS` to `1` in the environment map it passes, alongside the three
Google settings, and records `Unsetenv` calls with a function of its own; it
never leaves `Inherit` nil, since that would take the test process's real
descriptor 3. Failure paths (a descriptor that is not a listener, an `Accept`
that fails) inject an `Inherit` that returns an error or a listener whose
`Accept` fails. A test never sleeps to wait for the server. It learns that
`Run` is serving the way systemd does: it binds a Unix datagram socket in a
short temporary directory (`os.MkdirTemp("", ...)`, since a Unix socket path is
limited to 108 bytes and `t.TempDir()` can exceed it), names it in
`NOTIFY_SOCKET` in the environment map, and waits for the `READY=1` datagram,
with a deadline that fails the test rather than a sleep that hopes. It stops
`Run` by cancelling the context it passed, never by a signal.

**The drain tests wait on the clock, and only they do**, because the drain
deadline is the behavior. A `Serve`-level test passes a drain of a few
milliseconds and a handler that blocks on a channel. The one `Run`-level
overrun test sets `DRAIN_SECONDS=1` and holds a sign-in callback open inside
the fake Google: it starts a sign-in with `GET /login/google`, reads the
`state` from the `Location`, and requests `/login/google/callback` with that
`state` while the fake issuer's token endpoint blocks. It learns that the
handler has begun, without sleeping, when the fake token endpoint receives the
exchange; only then does it cancel the context, and it releases the fake
endpoint after `Run` has returned.

**One exec'ing test, and only one.** Tests under `internal/` never start a real
process; the run seam exists so they need not. The wiring in `cmd/auth` (D01's run
with no command, R-2B1J-WL7R) can be proved no other way, so exactly one kind of test that
execs the binary is admissible, and it lives in `cmd/auth`. It builds the
binary into a temporary directory and runs it with `--version`, with
`manifest`, with a bogus command, and bare with the three Google settings set
and no socket passed in (exit 2). For the serve case it stands in for systemd
without systemd: it makes a Unix socket in a short temporary directory, passes
it to the child as `exec.Cmd.ExtraFiles[0]`, which the child receives as
descriptor 3, and starts the child through
`/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. It sets
`NOTIFY_SOCKET` to a datagram socket it bound and waits for `READY=1`, never a
fixed sleep; then it sends `SIGTERM` and asserts exit 0 and silence on both
streams, and that the socket's path still exists and still accepts a
connection into its queue after the child has exited. It runs the serve case a
second time, with a fresh socket, and stops it with `SIGINT`, asserting the
same, because `main` promises both signals. The child runs in a test-owned
temporary working directory, where it creates `state/auth.db`. Its environment
is one the test composes, never the developer's — the three Google settings
set to placeholder values — and it runs offline like everything else: its
serve case needs no Google, since readiness is the datagram, not a completed
login. In the first serve case that environment also names, in
`IKIGENBA_SERVICES`, a services file the test wrote in its temporary
directory, and before starting the child the test seeds `state/auth.db` in
the child's working directory with a user and a live session through
`internal/store`. Before signalling, it makes one request of the child:
`GET /` with that session's cookie, over the socket. It asserts only that the
page carries the launcher and ends with the footer reading `auth` and the
`Version` that `internal/version` declares, which proves `main` handed
appkit's kit, with auth's version, to the server. Everything else auth answers is decided in process, and the exec'ing
test exists only to prove the wiring. Any other test
that builds, execs, waits on, or signals a process is a bug.

## Live tests

Live tests are the only tests that connect to external services. Every other
test is a unit test and follows Test discipline.

- Minimal: a live test proves lightly that the whole application or library
  is glued together and works end to end, about one per external service,
  never an exhaustive suite. Behavior, edge cases, and error paths are the
  unit tests' job.
- Separate: live tests are `*_live_test.go` files guarded by
  `//go:build live`, with test functions named `TestLive*`. `go test ./...`
  never runs them; `make live` runs
  `go test -tags live -count=1 -run '^TestLive' ./...`.
- Designed: a live test carries the requirement id it proves, and the gap
  counts it like any other test.
- Local: the code under test runs on the developer's machine; only the
  external service is real.
- Credentials: a live test reads its credentials from the environment. It
  never skips: a missing credential fails it. No credential appears in the
  repo or in test output.
- Run: live tests run only as the conditional `make live` gate below.

## Gates

Run from this directory (`auth/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix below the contract seam, or believes is wrong, is
filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/auth` —
   the release build: proves `cmd/auth` builds static for the host without cgo,
   which is why the SQLite driver must be pure Go
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form; since the cache is private, `--allow-parallel-runners` skips
   golangci-lint's machine-wide lock so gates for several sub-projects can lint
   at once)
6. `make live` — **conditional**: run only when the phase's diff (the working
   tree against the last phase commit) adds or modifies a `*_live_test.go`
   file; otherwise it is not run and not counted. When it applies and a
   credential is absent, that is a missing tool: file an issue, do not pass or
   skip.

Gate 5 flags an `http.Server` without `ReadHeaderTimeout` (`gosec` G112); it
is fixed in code, never suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by
id.

## Build

`make` builds `bin/auth` from the checkout (`make build`, the `Makefile` in
this directory). `make fmt` rewrites unformatted files, and `make test` and
`make lint` run the test and lint gates. The gates do not go through `make`:
they call the Go tool directly, and the one test that needs a binary builds
its own into a temporary directory.

## Deploy

Deploy machinery — the version bump, tags, and the `devctl build`/`devctl
deploy` steps — is hand-maintained infrastructure outside the spec system: the
build run never reads, edits, or tests it.

auth is an app, not a self-installing CLI: it is built into a release tarball
and pushed to a space's host by `devctl`, which drives `opsctl install` there.

1. Set the version in `internal/version/version.go` (D01) to `vX.Y.Z`. It is a
   source literal the binary reports verbatim, and the deploy refuses a tag
   that does not match it.
2. Commit that on `main` and push `main`.
3. Tag that commit `auth/vX.Y.Z` and push the tag.
4. `devctl build auth` at that tag writes `auth/dist/auth-vX.Y.Z.tar.xz`,
   holding `bin/auth`, `etc/`, and `share/icon.svg`, with no version recorded anywhere inside.
   It refuses a binary whose `manifest` disagrees with the committed
   `etc/manifest.toml`.
5. `devctl deploy <space> auth/dist/auth-vX.Y.Z.tar.xz` uploads the tarball
   to the space's `deploy/` prefix and runs `opsctl install` over ssh; the host
   fetches it, writes `etc/env` (with the space's `DRAIN_SECONDS`), replaces
   the release, publishes `ikigenba-auth.socket` (the Unix socket
   `/run/ikigenba/auth.sock`) and the `Type=notify` `ikigenba-auth.service`,
   regenerates the host's nginx and litestream configuration, and restarts the
   service alone; the socket stays up, so requests — `/check` subrequests for
   every other app among them — queue on it across the restart.

`auth --version` then prints `vX.Y.Z`, and `space status` reports it.
