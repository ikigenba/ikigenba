# mcp

mcp is the MCP gateway: one endpoint through which agents reach every service's tools. One Go binary serves `mcp.<host>` on the socket it is passed as descriptor 3, behind the host's nginx. At `/mcp` it offers four tools over the suite's MCP services (`services`, `describe`, `call` and `mutate`), `/mcp/a,b` scopes it to the named services, `/` is a connect page that gives a person the commands that point Claude Code or Codex at it, and the endpoint for any other client, and `/.well-known/oauth-protected-resource` is the protected-resource metadata that tells an MCP client auth issues the tokens `/mcp` accepts. It holds no state: on every request it reads the services file afresh and reaches each backend on the socket that file names with appkit's MCP client, forwarding the caller's identity and giving up after 50 seconds. It is built on appkit for pages, services, identity, MCP and telemetry. The contract is `specs/design/`; this file restates none of it.

## Layout

- `specs/` is the contract: `stories/` and `design/`.
- `assets/` is the page markup, and `share/icon.svg` the launcher icon. The build run never writes them; the user or the delivering agent changes them.
- `assets.go` is the root package, which embeds `assets/`. `cmd/mcp` is the binary. `internal/` is everything else, one package per concern.
- `etc/` is what the host needs: `manifest.toml`.
- The build run writes the Go source, the tests, `go.mod`, `go.sum` and `etc/`. `Makefile`, `.golangci.yml` and this file are its inputs and read-only to it. See the `spec` and `build-spec` skills.

## Assets

`assets/` holds `connect.html`, the connect page, an `html/template` file that follows the repository's `design/`. It is an input to the spec. The root package embeds it, since Go's `embed` reaches only files at or below its own directory, and the code executes it by template name. That each `Copy` button puts its command or the endpoint on the clipboard is checked by hand in a browser against the sandbox, never by the gates. Code never writes markup of its own, not even a fragment or an error page. Design names the template, the data it receives and the hooks it emits; tests assert on those hooks and on visible text, never on layout. A template that is missing or wrong, a state a story names that it cannot show, or a hook design names that it lacks is filed in `specs/issues/`; the run never edits an asset to close one.

mcp holds no stylesheet, fonts or licences; appkit's `page` package serves them. `share/icon.svg` is the Tabler outline `plug-connected` from `design/ikigenba/icons/tabler/`, stripped as `design/README.md` asks of a launcher icon. `devctl build` packs it beside `bin/` and `etc/`.

## Toolchain

- Go 1.26 or later.
- A C compiler cgo can use, such as `gcc`: `go test -race` needs it (gate 4). The release build is cgo-free (gate 3).
- The modules `go.mod` requires, in the module cache; `go.sum` is committed and the gates run offline. The build run sets each requirement and moves to another release only when this file names one: appkit `v0.14.0`, whose `identity` package exports `Optional`, the middleware that lets a guest through with an empty caller (see Adopting appkit).
- `golangci-lint` v2, configured by `.golangci.yml` here.
- A POSIX shell at `/bin/sh`, for the one exec'ing test.
- GNU `make`, for the developer targets; no gate runs through it.

Prefer the standard library, then a widely used public module; adding one needs approval, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, `appkit/<version>` on origin (see the root `AGENTS.md`); the build run sets it with `go get github.com/ikigenba/ikigenba/appkit@v0.14.0`.

## Test files

The test files are every `*_test.go` in the module: the root package, `cmd/mcp` and everything under `internal/`. The canonical gap greps them for ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/` and `assets/` hold no `*_test.go`.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag from any other string of that shape, and the gateway echoes service and tool names back in its errors, so no fixture carries one: not a service name, tool name, argument, header value or expected body.

## Test discipline

These rules govern everything `go test ./...` runs. Tests are offline (loopback and Unix sockets only, no real credentials), deterministic (time, randomness and environment are injected; nothing sleeps to wait), bind no fixed port (`127.0.0.1:0` or a Unix socket in a temporary directory), and touch only their own temporary directory, never the developer's home, config or real state.

**The run seam carries the process.** The gateway binds nothing; it serves on the listener it is passed. Arguments, environment lookup and removal, the pid, the inherited listener, the output streams and the sink the trail is delivered to all come in through the run seam design declares, and tests inject them. A test never reads or changes the real environment, and never leaves the inherited-listener step unset, which would take the test process's real descriptor 3. A test learns the server is ready the way systemd does: it binds a Unix datagram socket in a short temporary directory (`os.MkdirTemp`, since a socket path is limited to 108 bytes and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for `READY=1` with a deadline that fails the test. Drain tests are the one place a test waits on the clock, because the drain deadline is the behavior; they keep it to a few seconds. The gates run offline as an ordinary user with no systemd.

**The trail is a capture the test holds.** No test reaches a live telemetry service. A test of the run seam passes a `*telemetry.Capture` as the seam's sink and reads `Capture.Events` once `Run` has returned. A test of the handler builds its own writer with `telemetry.New` over a `*telemetry.Capture`, calls `Writer.Flush` before reading `Capture.Events`, and calls `Writer.Shutdown` on every writer it builds. A test asserts on events by name, attributes, request id and user, never on `time`, and on `duration_us` only with a clock it injected through `telemetry.Config.Now` and advances by hand. Undelivered events are proved with a sink of the test's own that rejects every event (its error wrapping `telemetry.ErrRejected`, so nothing is retried); a sink that blocks until the test releases it proves that no answer waits on the trail and that the drain bounds delivery. A test that leaves the seam's sink nil stands in for the telemetry service: it serves appkit's `telemetry.IngestHandler` over a `*telemetry.Capture` on a Unix socket in a short temporary directory and names that socket as the `telemetry` entry of its own services file.

**Backends are servers the test builds.** A test that needs backends stands each one up in process: an appkit `mcp.Server` with tools of the test's own, and a telemetry writer over a `*telemetry.Capture`, served on a Unix socket in a short temporary directory. It writes a services file in its temporary directory naming those sockets, with whatever `mcp`, `enabled` and `description` values the case needs, and may rewrite it between requests, since the gateway reads it afresh each time. An unreachable backend is a socket path nothing listens on. A backend that records the identity headers it received proves forwarding. A backend may also be a plain `http.Handler` on such a socket, alone or wrapped around an appkit `mcp.Server`, to record requests, hold one until the test releases it, or answer with bytes the test writes (a JSON-RPC error, a tool list an appkit server cannot produce, a non-MCP status, or a connection closed before any status), since the gateway must handle answers no appkit server gives. Every test builds its own backends and services file; none depends on another's.

**No test waits out the backend timeout.** The 50-second limit is injected: a test that proves the timeout sets a short one and a backend that blocks on a channel until the test releases it; a test that proves cancellation cancels the request's context while the backend blocks. That the default is fifty seconds is proved by `DefaultBudget`'s value; a test proves only that a zero `Budget` is not shorter than the few seconds it waits. A test that proves the budget is shared by a call's backend requests, or is not cut short, may release a held answer on a timer within the short budget, keeping that wait to a few seconds.

**One environment variable, set by the test.** appkit's constructors read `IKIGENBA_SERVICES` (`services.Variable`) from the process environment, and appkit's socket sink reads it on every delivery; it is the one read the gateway cannot route through the run seam, and in the binary only code `main` supplies, and the socket sink when the seam's sink is nil, makes it. A test that reaches that read, directly, through a gateway constructor, or by leaving the seam's sink nil, first sets the variable with `t.Setenv` to a services file it wrote or to the empty string, and does not call `t.Parallel`.

**Identity comes from headers the test sets.** appkit's `identity` middleware wraps every route: `Require` on `/mcp` and every path beneath it, `Optional` everywhere else, so a guest reaches the connect page's sign-in redirect, the protected-resource metadata, the shared files, the 405s and the 404 with an empty caller. A test sets the identity headers to be a signed-in user, or omits `X-User-Id`, or sends it empty, to be a guest, or, on `/mcp`, to exercise the missing-identity answer; it sets `Host` and `X-Forwarded-Proto` the way nginx does, since the endpoint, the metadata and the sign-in redirect depend on them. The middleware's behavior is appkit's contract; the gateway's tests prove only that its routes are wrapped in the one design names.

**MCP through appkit's client.** A test drives `/mcp` as a client would: it serves its handler on loopback or a Unix socket and calls it with appkit's `mcp.Client`, which forwards the caller's identity headers. Raw HTTP to `/mcp` is only for what the client cannot send (a missing, duplicated or empty identity header, a malformed scope path, a method other than POST, a request on an earlier protocol revision, `initialize` or `server/discover`) and for comparing the gateway's answer with appkit's server's answer to the same request. Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns, on the events the test's capture holds, and on what the test captured in a buffer of its own. The gateway's tests never re-prove appkit's transport.

**No test runs the page's scripts.** Every page carries appkit's feedback script. The gates have no browser or JavaScript engine, and adding one is an unapproved dependency, so a test asserts what a response body carries, never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory, not `.go`, `go.mod`, `go.sum`, `etc/`, `share/` or `assets/`, and never parses source. Handing `cmd/mcp` to `go build` is not reading it.

**One exec'ing test, and only one.** Tests under `internal/` never start a process. The wiring in `cmd/mcp` can be proved no other way, so exactly one test execs the binary, and it lives in `cmd/mcp`. It builds the binary into a temporary directory and runs it with `--version`, with `bogus`, and bare with no socket. For the serve case it stands in for systemd: it makes a Unix socket in a short temporary directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the child), and starts the child through `/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because `LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. It waits for `READY=1`, makes the requests design names over the socket with the identity headers, then stops the child with `SIGTERM`, and in a second run with `SIGINT`, asserting what design states. Where design names the binary's trail, the test's services file names as its `telemetry` entry a socket on which the test serves appkit's `telemetry.IngestHandler` over a `*telemetry.Capture`, and the test reads the events delivered there once the child has exited. The child's environment is one the test composes, never the developer's. Any other test that builds, execs, waits on or signals a process is a bug.

## Live tests

The gateway's backends are sibling services on the same host, which the unit tests stand in for, so it has no live tests. Should design ever call for one, it is a `*_live_test.go` file behind `//go:build live` with `TestLive*` functions; it proves lightly that the whole is glued together, carries the id it proves, reads credentials from the environment, fails rather than skips when one is missing, and runs only as gate 6.

## Gates

Run from `mcp/`, in order; every command must exit 0. No skipped tests and no disabled linters. A per-finding suppression (`//nolint` and the like) is a skip the run never adds; a finding it cannot fix or believes wrong is filed as an issue.

1. `test -z "$(gofmt -l .)"` (fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/mcp`, the release build as `devctl build` makes it, static and cgo-free
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

`make build` (the default) builds `bin/mcp`; `make fmt` formats; `make test` and `make lint` run those gates. The gates themselves call the Go tool directly, and the exec'ing test builds its own binary.

## Deploy

Deploy machinery, the version bump, tags and `devctl build`/`devctl deploy`, is hand-maintained and outside the spec system: the build run never reads, edits or tests it.

mcp is an app, not a self-installing CLI: `devctl` builds it into a release tarball and pushes it to a space's host, where `opsctl install` installs it. The tag `appkit/<version>` must be on `origin` first (see Adopting appkit).

1. Set the version literal design declares to `vX.Y.Z`; the binary reports it verbatim and the deploy refuses a tag that does not match.
2. Commit on `main` and push `main`.
3. Tag the commit `mcp/vX.Y.Z` and push the tag.
4. `devctl build mcp` at that tag writes `mcp/dist/mcp-vX.Y.Z.tar.xz` holding `bin/mcp`, `etc/` and `share/icon.svg`; it refuses a binary whose `manifest` disagrees with the committed `etc/manifest.toml`.
5. `devctl deploy <space> mcp/dist/mcp-vX.Y.Z.tar.xz` uploads it and runs `opsctl install` on the host, which publishes `ikigenba-mcp.socket` and `ikigenba-mcp.service`, regenerates nginx, and restarts the service.

`mcp --version` then prints `vX.Y.Z`, and `devctl space status <space>` reports it.
