# D03-serve

The serve path covers what happens between `cli.Run` being called with no arguments and the process being gone:

- the terms accounts serves on
- the listener's life
- the route table its one handler keeps, with the identity each route takes
- the start and the stop of the background work: the operations engine and, in the sandbox, the simulated hosts

It is the suite's app serve path with three changes:

- It adds one route that takes no identity, the daemon's (`/accountsd` and everything below `/accountsd/`).
- It adds the engine and the simulated hosts beside the server.
- It drops what accounts has no use for: no sibling client and no bus paths.

This document declares `internal/server` and `internal/web`. Outcomes cited as outcome N are those of /home/mgreenly/Documents/ikigenba-ssh-hosts.md.

What this document cites from the others:

- `D01-layout-and-run-seam`:
  - the run seam (`cli.Process`, `cli.Run`)
  - the settings (`settings.Settings`, `settings.Read`, `Drain()`, `Cloud()`, `HostsInUse()`, `AccountsURL`, `Binding`)
  - the choice of cloud, the database and daemon binary paths, `pages.ServiceName`, and the identity and display string
- `D02-cli`: the exit codes.
- `D04-store`: the store.
- `D05-cloud`: the cloud seam.
- `D06-pages`: the pages (`pages.Handler`, `pages.Config`, `pages.Load`) and the sign-in redirect.
- `D07-tools`: the tools (`tools.Register`, `tools.Config`, `tools.NotConfigured`).
- `D08-trail`: the trail.
- `D09-on-a-space`: the manifest's `guests = true` and the nginx fragment.
- `D10-operations`: the engine (`ops.New`, `ops.Config`, `(*Engine).Run`, `(*Engine).Wake`).
- `D12-host-identity`: the verifier (`hostauth.NewVerifier`, `hostauth.VerifierConfig`).
- `D13-daemon-endpoints`: the daemon endpoints (`daemonapi.New`, `daemonapi.Config`, `(*Server).Stop`, `(*Server).Digest`).
- `D16-sandbox`: the memory cloud and the simulated hosts (`memory.New`, `sim.New`, `sim.Config`, `(*Sim).Run`).

appkit's packages own their contracts; this document wires them.

## The services file

`IKIGENBA_SERVICES` names the services file. accounts uses three of its entries:

- `auth`: the pages read it afresh per request (`D06-pages`).
- `telemetry`: appkit's sink reads it afresh per delivery.
- `accounts`: read afresh for the accounts URL each time a launch step needs it, and for the binding at each verification (`D01-layout-and-run-seam`).

`Run` also reads the binding once at the start, for the simulated hosts. A services-file change after the start therefore leaves the simulated daemons on the old binding while the verifier reads the new one; accepted, because only the sandbox runs simulated hosts and its `accounts` entry does not change while it runs (a restart picks up a new one).

The start is not refused when `IKIGENBA_SERVICES` is unset, names no file, or names a file `services.Read` refuses. accounts then serves as with a file holding no entry.

## The terms accounts serves on

accounts serves only on a listening socket it inherits, taken as `sd_listen_fds(3)` documents. It reports readiness with `READY=1` to `NOTIFY_SOCKET`, as `sd_notify(3)` documents.

It trusts `X-User-Id`, `X-User-Email` and `X-Request-Id` as nginx sets them. The manifest declares `guests = true` (`D09-on-a-space`). nginx therefore lets a request with no credential through to every path but `/mcp`, so that a daemon reaches `/accountsd` and `/accountsd/…` (outcome 21).

A request with no `X-User-Id`, or an empty one, is a **guest**:

- The pages send a guest to sign in.
- `/mcp` refuses one.
- The daemon routes take no user at all: a daemon proves who it is by its presigned identity header (`D12-host-identity`), which `D13-daemon-endpoints` checks.

## The route table

Every request goes through appkit's `telemetry.Middleware` over `Config.Telemetry`, which records the request pair (`D08-trail`). Then the handler routes by `URL.Path` alone, compared by hand, so nothing is cleaned and nothing is redirected:

- **Exactly `/mcp`**: `identity.Require`, then the MCP server `Config.MCP`. The tools of `D07-tools` are registered on it with `tools.Register` once, when the handler is built.
- **A path beginning `/_appkit/`**: `identity.Optional`, then `page.Static()`.
- **Exactly `/accountsd`, or a path beginning `/accountsd/`**: no identity wrapper, then `Config.Daemon` (`D13-daemon-endpoints`).
- **Every other path**: `identity.Optional`, then `pages.Handler` (`D06-pages`). That includes `/accountsdx`, `/mcp/`, `/events`, `/hosts/<name>`, `/` and `/tools`.

Only `/mcp` changes the record on behalf of a person. `/accountsd/v1/report` records the host's report, on behalf of a verified daemon. Nothing else changes anything. No answer on any route carries `Set-Cookie`.

## The start

With `Args` empty, `Run` goes through these steps in order:

1. It checks the settings, then that exactly one socket was passed in, with the diagnostics below.
2. It removes the three `LISTEN_*` variables and takes descriptor 3.
3. It opens the database (`D01-layout-and-run-seam`), and builds the store over `Process.Now` and `Process.Rand`, then the writer.
4. It records `service.started`.
5. It chooses the cloud by `Settings.Cloud()`:
   - `Process.Cloud(ctx, region)` in AWS mode.
   - `memory.New` over `state/sandbox-cloud.json` in sandbox mode.
   - None otherwise.

   A cloud that fails to open is not fatal. accounts writes one line, `accounts: ` followed by `cli.NoCloud` and the reason, and serves as in mode none.
6. It builds the daemon endpoints with `daemonapi.New` over the daemon binary it resolved. `daemonapi.New` streams the binary once to compute its digest.
7. It builds the engine with `ops.New`, and the handler.
8. It sends `READY=1`.
9. It starts the engine's `Run`, and in sandbox mode the simulated hosts' `sim.Run`, each in a goroutine.
10. It serves.

The engine resumes every unsettled operation at once, so an operation interrupted by a restart continues without a tool call (outcome 11). The tools wake the engine through `tools.Config.Wake`, so a new operation's first step does not wait for the engine's timer.

## Stopping

When `ctx` is done, `Run` stops in this order:

1. It calls `daemonapi.Server.Stop()`. Every long poll in progress then answers at once, so a waiting daemon never holds the drain up.
2. It drains the server for `DRAIN_SECONDS`.
3. It waits for the engine and the simulated hosts. Both return on `ctx`.
4. It shuts the writer down with the cause's text as `service.stopping`'s reason.
5. It closes the database.

The engine never blocks the drain. An AWS call in flight is cancelled with `ctx`. Its step was recorded before the call, so it resumes at the next start (outcome 11).

From shortly after the drain deadline on, `Run` hands the sink nothing. A drain that runs out on requests is `accounts: stopped with <n> request(s) unfinished`, exit 1.

## The binary

The one exec'ing test of `cmd/accounts` proves the wiring. `D01-layout-and-run-seam` states what the binary does at the start. This document adds the bare start with no socket and the stop on `SIGINT`.

## Recorded decisions

- **`internal/server` is the suite's, unchanged.**
- **The daemon routes sit outside identity**, so that a daemon with no credential reaches `D13-daemon-endpoints`.
  - Precedent: webhooks' `/in/<slug>` behind `guests = true`.
  - The departure from outcome 21's wording ("a path nginx passes without auth's subrequest") is recorded in `D09-on-a-space`. It takes nginx's `/check/open` subrequest instead.
  - The prefix match `/accountsd/` and the exact `/accountsd` are compared by hand, as `/mcp` is, so `/accountsdx` stays a page path and reaches the not-found page.
- **`web.Config` gains `Cloud`, `Hosts`, `Configured`, `Wake` and `Daemon`, and loses `Host`.**
  - The tools need the cloud for the DNS check, the hosts settings in use, whether the run is configured, and the engine's wake.
  - The pages need whether the run is configured and the hosts domain.
  - The route table needs the daemon endpoints.
- **The pages' clock is the writer's `Now`.** `web.Config` carries no clock of its own, as before. `pages.Config.Now` is `cfg.Telemetry.Now`, which is `Process.Now`.
- **A cloud that fails to open is not fatal**.
  - accounts serves as mode none and says why on standard error.
  - It writes one line, so an operator sees it in the journal, and the tools say "not configured" rather than failing.
  - `cli.NoCloud` is a copy constant, a pick.
  - The sandbox's unreadable memory-cloud file takes the same path.
- **A failing `daemonapi.New` has no stated answer.** `D13-daemon-endpoints` makes a missing binary a 404 rather than a failure, and no permitted test can make reading an existing file fail. This follows the old store-failure decision.
- **Stop order: daemon endpoints, drain, background, writer, database.**
  - Long polls would otherwise run out the drain. Precedent: no sibling long-polls. gmail's and gdrive's workers stop on `ctx` before the writer shuts down.
  - Stopping the background before the writer keeps `service.stopping` the last event, as R-UVI5-53XK requires.
- **The engine resumes on start, where gdrive and gmail fail interrupted work at `Recover`.** Outcome 11 requires resume. The resume is the engine's (`D10-operations`). This document requires only that the engine runs from the start of every configured run.
- **The simulated hosts' diagnostics never reach `Process.Stderr`.** R-H30N-ZNGY allows nothing on standard error beyond the lines it lists, in every mode. `D16-sandbox` gives each simulated daemon a `Stderr` that discards (R-VB9N-YMHD).
- **The daemon endpoints take `Process.After`** as `daemonapi.Config.After` (`D13-daemon-endpoints`, R-R4PD-7V00), so a long poll's 45 s wait is driven by the test's timer source, as the engine's and the simulated hosts' waits are.
- **Tests these requirements need**, each permitted by `accounts/AGENTS.md`, Test discipline:
  - sandbox mode over a temp `Dir`, with an `After` the test controls
  - AWS mode with a recording or blocking fake `cloud.Cloud` handed as `Process.Cloud`
  - long-poll tests that never wait out `protocol.LongPoll`, because the stop answers them at once

## Consumer usage

`Run`, serving:

1. It reads `s, err := settings.Read(p.LookupEnv)`, checks the socket, unsets the three `LISTEN_*` variables and takes `ln, _ := p.Inherit(3)`.
2. It opens `d` with `db.Open` at `state/accounts.db`, builds `st := store.New(d, store.Config{Now: p.Now, Rand: rnd})` and `w := telemetry.New(...)`, and records `service.started`.
3. It opens the cloud `c` by mode. On failure it writes `"accounts: " + cli.NoCloud + ": " + reason` and sets `configured = false`.
4. It builds `v := hostauth.NewVerifier(hostauth.VerifierConfig{Region: hosts.Region, Role: hosts.InstanceProfile, Binding: binding, STS: c, Now: p.Now})`.
5. It builds `ds, _ := daemonapi.New(ctx, daemonapi.Config{Store: st, Verifier: v, Binary: bin, Telemetry: w, Now: p.Now, Configured: configured, After: p.After})`.
6. It builds `eng := ops.New(ops.Config{Store: st, Cloud: c, Hosts: hosts, AccountsURL: accountsURL, Telemetry: w, Now: p.Now, After: p.After})`.
7. It builds `h := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), ServicesPath: path, Store: st, Cloud: c, Hosts: hosts, Configured: configured, Wake: eng.Wake, Daemon: ds, Telemetry: w, Settings: s})`.
8. It sends `READY=1` and starts `go eng.Run(ctx)`.
9. In sandbox mode only, it builds `sm := sim.New(sim.Config{Cloud: mc, Handler: h, Binding: b, Digest: ds.Digest, Now: p.Now, Sleep: p.Sleep, After: p.After})` and starts `go sm.Run(ctx)`.
10. It calls `server.Serve(ctx, ln, h, s.Drain(), stop)`, where `stop` calls `ds.Stop()` first, then waits for the engine and the sim, then calls `w.Shutdown`.
11. It closes `d`.

`web.Handler(cfg)`:

1. It calls `tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Cloud: cfg.Cloud, Hosts: cfg.Hosts, Configured: cfg.Configured, Wake: cfg.Wake, Telemetry: cfg.Telemetry})` once.
2. It builds `pages.Handler` over `pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, MCP: cfg.MCP, Configured: cfg.Configured, Domain: cfg.Hosts.Domain, Now: cfg.Telemetry.Now}`.
3. It mounts `identity.Require(cfg.MCP)` at `/mcp`, `identity.Optional(page.Static())` under `/_appkit/`, `cfg.Daemon` at `/accountsd` and under `/accountsd/`, and `identity.Optional(pages)` everywhere else.
4. It returns `telemetry.Middleware(cfg.Telemetry, mux)`.

A test of the handler:

1. It builds `web.Handler` over a store on its own database, `memory.New` over a temp path, `settings.SandboxHosts`, `Configured: true`, and a `Wake` that counts.
2. It adds `daemonapi.New` over a temp binary, a writer over a capture, and a server from `mcp.NewServer`.
3. It sends raw requests to `/accountsd` with and without `X-User-Id`, and compares each answer with `cfg.Daemon.ServeHTTP`'s.

A test of the stop serves `cli.Run` in sandbox mode with `DRAIN_SECONDS=1`, lets a host reach health `sync`, so a simulated daemon is waiting in a long poll, cancels, and expects `ExitSuccess`.

## REQUIREMENTS

- R-QFFD-6WFO: The `internal/server` package, imported from the path `github.com/ikigenba/ikigenba/accounts/internal/server` with the package name `server`, MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(ctx context.Context)) error`, where `context`, `net`, `net/http` and `time` are the standard library's packages.
- R-QGN9-KO6D: The `internal/server` package MUST export `type DrainError struct { Unfinished int }`, with exactly this field, with the method `func (e *DrainError) Error() string`.
- R-QHV5-YFX2: `server.Serve` MUST answer every request received on `ln` with the response `h` produces, MUST NOT return while `ctx` is not done and serving has not failed, and once `ctx` is done MUST stop accepting, let every call to `h.ServeHTTP` in progress finish within `drain`, call `stop` once with a context that is done once `drain` has elapsed, and return nil when every such call finished in time, or a `*server.DrainError` whose `Unfinished` is the number still in progress when `drain` elapsed.
- R-MY4W-C8QN: When `Accept` on `ln` returns an error that is not a temporary `net.Error` while `ctx` is not done, `server.Serve` MUST return a non-nil error for which `errors.As` with a `*server.DrainError` target is false and whose text holds the text of the error `Accept` returned; so that a listener whose `Accept` returns at once, on its first call, an error whose text is `listener gone` makes `Serve` return such an error holding `listener gone`.
- R-QJ32-C7NR: `(*server.DrainError).Error` MUST return exactly `"stopped with 1 request unfinished"` when `Unfinished` is 1, and exactly `"stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"` otherwise.
- R-GC6V-KP5O: The `internal/web` package, imported from the path `github.com/ikigenba/ikigenba/accounts/internal/web` with the package name `web`, MUST export `type Config struct { Banner func(u page.User) page.Banner; MCP *mcp.Server; ServicesPath string; Store *store.Store; Cloud cloud.Cloud; Hosts settings.Hosts; Configured bool; Wake func(); Daemon *daemonapi.Server; Telemetry *telemetry.Writer; Settings settings.Settings }`, with exactly these fields in this order, where `page`, `mcp` and `telemetry` are appkit's packages `github.com/ikigenba/ikigenba/appkit/page`, `github.com/ikigenba/ikigenba/appkit/mcp` and `github.com/ikigenba/ikigenba/appkit/telemetry`, and `store`, `cloud`, `settings` and `daemonapi` are accounts' `internal/store`, `internal/cloud`, `internal/settings` and `internal/daemonapi`; `Banner` is the pages' banner source, `MCP` the server mounted at `/mcp`, `ServicesPath` the path of the services file (empty for none), `Store` the store every route reads and writes, `Cloud` the cloud the tools reach (nil when the run is not configured), `Hosts` the hosts settings in use, `Configured` whether the run is configured, `Wake` the function that wakes the operations engine, `Daemon` the daemon endpoints, `Telemetry` the writer every request's events go through, and `Settings` the settings the run read.
- R-QMQR-HIVU: The `internal/web` package MUST export `func Handler(cfg Config) http.Handler`, where `http` is the standard library's `net/http`.
- R-GEMO-C8N2: accounts' design states the behaviour of a handler `web.Handler(cfg)` only for a `web.Config` whose `Banner`, `MCP`, `Store`, `Wake`, `Daemon` and `Telemetry` are not nil, whose `Cloud` is not nil when `Configured` is true, and whose `Settings` is one `settings.Read` (`D01-layout-and-run-seam`) returned with a nil error; `ServicesPath` may be empty; every requirement of accounts' design naming such a handler MUST denote one built so.
- R-QP6K-92D8: accounts' design defines the **request id** of a request answered by a handler `web.Handler(cfg)` as the request's first `X-Request-Id` value when that header is present and its first value is not empty, and otherwise as the id appkit's `telemetry.Middleware`, given `cfg.Telemetry`, mints for that request; its **caller** as the `identity.Caller` whose `UserID` is its first `X-User-Id` value, whose `Email` is its first `X-User-Email` value, each empty when absent, and whose `RequestID` is its request id; and a **guest** as a request whose `X-User-Id` is absent or whose first value is empty; every requirement of accounts' design naming a request's request id, its caller or a guest MUST denote that string, that `Caller` or that request.
- R-GFUK-Q0DR: A handler `web.Handler(cfg)` MUST answer every request from a guest whose `URL.Path` is exactly `/mcp`, whatever its method, other headers, query and body, as `identity.Require(h)` answers it for any handler `h`: status 500, exactly one `Content-Type` header whose value is `text/plain; charset=utf-8`, and the body `identity.MissingBody`, or empty when the method is `HEAD`; calling no tool, calling no method of `cfg.Cloud` or `cfg.Wake`, recording nothing in `cfg.Store`, and recording no event through `cfg.Telemetry` but `request.started` and `request.finished`.
- R-EUVX-0DY8: A handler `web.Handler(cfg)` MUST answer every request whose `URL.Path` is exactly `/mcp` and that is not from a guest as `cfg.MCP` answers it for that request's caller (R-QP6K-92D8), with the tools of `D07-tools` registered on `cfg.MCP` by `tools.Register` once, when the handler is built, over a `tools.Config` whose `Store`, `Cloud`, `Hosts`, `Configured`, `Wake` and `Telemetry` are `cfg`'s; so that once `web.Handler(cfg)` has returned, `cfg.MCP.Tools()` holds each of the nineteen tools `D07-tools` declares exactly once, and, with `cfg.Cloud` a cloud `memory.New` (`D16-sandbox`) returned, `cfg.Hosts` equal to `settings.SandboxHosts`, `cfg.Configured` true, `cfg.Wake` a function that counts its calls and `cfg.Store` built by `store.New` with a `Now` that always returns 2026-11-17 14:03:07 UTC, a `create_host` call made through an appkit `mcp.Client` on behalf of a caller with a non-empty `UserID` returns a `Result` whose `IsError()` is false, `cfg.Wake` has been called at least once, and a `host` call naming that host answers `created` `2026-11-17T14:03:07Z`.
- R-QSU9-EDLB: A handler `web.Handler(cfg)` MUST answer every request whose `URL.Path` begins `/_appkit/`, from a guest or not, whatever its method, as `page.Static()` answers it; so that `GET /_appkit/theme.css` is answered 200 by both with the same body, with or without `X-User-Id`.
- R-GIAD-HJV5: A handler `web.Handler(cfg)` MUST answer every request whose `URL.Path` is exactly `/accountsd` or begins `/accountsd/`, from a guest or not, whatever its method, headers, query and body, with the status, headers and body `cfg.Daemon` (`D13-daemon-endpoints`) answers it with; so that a guest's `GET /accountsd` and a signed-in `GET /accountsd` are each answered as `cfg.Daemon.ServeHTTP` answers that request, neither with the redirect to sign in `D06-pages` states nor with `identity.MissingBody`, and a guest's `GET /accountsd/v1/state` is answered as `cfg.Daemon` answers a request with no identity header.
- R-GJI9-VBLU: A handler `web.Handler(cfg)` MUST answer every request whose `URL.Path` is not exactly `/mcp`, does not begin `/_appkit/`, is not exactly `/accountsd` and does not begin `/accountsd/`, from a guest or not, whatever its method, headers, query and body, as the page handler (`D06-pages`) answers it for the `pages.Config` whose `Banner` is `cfg.Banner`, whose `Pages` is a set a `pages.Load` call returned, whose `ServicesPath` is `cfg.ServicesPath`, whose `Store` is `cfg.Store`, whose `MCP` is `cfg.MCP`, whose `Configured` is `cfg.Configured`, whose `Domain` is `cfg.Hosts.Domain` and whose `Now` is `cfg.Telemetry.Now`; so that `/`, `/about`, `/tools`, `/hosts/host01`, `/nope`, `/mcp/`, `/events`, `/declarations`, `/_appkit` and `/accountsdx` reach the page handler, and a guest's `GET /` and a guest's `GET /hosts/host01` are each answered with the redirect to sign in `D06-pages` states.
- R-QVA2-5X2P: A handler `web.Handler(cfg)` MUST NOT send a `Set-Cookie` header in any response, whatever the request's path, method and headers, a guest's included.
- R-GKQ6-93CJ: A handler `web.Handler(cfg)` MUST call no method of `cfg.Cloud`, MUST NOT call `cfg.Wake`, and MUST record nothing in `cfg.Store`, in answer to any request whose `URL.Path` is not exactly `/mcp`, is not exactly `/accountsd` and does not begin `/accountsd/`, from a guest or not, whatever its method, headers, query and body; so that, with `cfg.Cloud` a fake that records every call, the signed-in and guest `GET`, `POST` and `DELETE` requests of `/`, `/tools`, `/about`, `/hosts/host01`, `/nope`, `/mcp/` and `/_appkit/theme.css` leave the fake's record empty, and a store reopened at the database's path afterwards holds no host, account, user or report it did not hold before.
- R-GLY2-MV38: When `Args` is empty, `settings.Read(p.LookupEnv)` returns a nil error and no socket is passed in, which is when `LISTEN_PID` is unset or is not `Pid` written in decimal, or `LISTEN_FDS` is unset, empty, `0` or not decimal digits, `Run` MUST write exactly `"accounts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"` to `Stderr` in one write, write nothing to `Stdout`, and return `ExitUsage` (`D02-cli`), without calling `Inherit`, `Cloud` or `Executable` and without creating anything under `Dir`; and when `LISTEN_PID` is `Pid` and `LISTEN_FDS` counts more than one, it MUST write exactly `"accounts: " + v + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"`, `v` being `LISTEN_FDS` verbatim, and return `ExitUsage` the same way.
- R-QYXR-B8AS: When exactly one socket is passed in and the settings are accepted, `Run` MUST pass each of `LISTEN_PID`, `LISTEN_FDS` and `LISTEN_FDNAMES` to `Unsetenv`, and no other key, and MUST serve on the listener `Inherit` returns for the argument 3, calling `Inherit` exactly once.
- R-GN5Z-0MTX: When `db.Open` fails with an error `err` for `state/accounts.db` under `Dir`, `Run` MUST write exactly `"accounts: cannot open database state/accounts.db: " + r + "\n"` to `Stderr` in one write, `r` being `err.Error()` with every newline replaced by a space, write nothing to `Stdout`, send nothing to the notification socket, accept no connection, call neither `Cloud` nor `Executable`, record no event, and return `ExitServerFailed` (`D02-cli`); so that a regular file named `state` under `Dir` gets that line.
- R-R1DK-2RS6: When `NOTIFY_SOCKET` is not empty, `Run` MUST send exactly one datagram, `READY=1`, to it, before accepting any connection; and when `NOTIFY_SOCKET` is unset or empty, `Run` MUST send nothing to any notification socket.
- R-GODV-EEKM: When `Run` serves, it MUST call `MCP` exactly once, before sending `READY=1`, with a writer through which every event emitted while it serves is handed to `Sink`, and MUST answer every request whose `URL.Path` is exactly `/mcp` and that is not from a guest with the server `MCP` returned; so that, `MCP` returning `mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: p.Version, Telemetry: w})` over the writer `w` it is given, a `CallTool` call for the tool `hosts` (`D07-tools`) made by an appkit `mcp.Client` on behalf of a caller with a non-empty `UserID` returns a `Result` whose `_meta` member `io.modelcontextprotocol/serverInfo` is exactly `{"name":<n>,"version":<v>}`, `<n>` being `pages.ServiceName` and `<v>` being `p.Version`, and an event the test emits through `w` after `READY=1` is handed to `Sink`.
- R-R519-8309: The first event recorded through the run's writer MUST be `service.started` (`D08-trail`) with an empty `RequestID` and `User` and `Attrs` exactly `version`, equal to `p.Version`, and it MUST precede every `request.started`.
- R-GPLR-S6BB: When `Run` serves with `IKIGENBA_SERVICES` unset, empty, naming no file, naming a file appkit's `services.Read` refuses, or naming a services file that holds no entry, in any mode `settings.Settings.Cloud` selects, it MUST send `READY=1` and serve; and a `CallTool` call for the tool `hosts` (`D07-tools`) made by an appkit `mcp.Client` on behalf of a caller with a non-empty `UserID` MUST return a `Result` whose `IsError()` is false, and a signed-in `GET /` made over the same run MUST be answered 200; so that a space whose services file is missing still serves accounts.
- R-R7H1-ZMHN: When `Run` serves with `IKIGENBA_SERVICES` naming a services file, a guest's `GET /` MUST be answered as a handler `web.Handler(cfg)` built from a `web.Config` whose `ServicesPath` is that file's path answers a guest's `GET /`, the file being as it is when the request is received; so that, the file's entry `auth` having been rewritten after `READY=1` from `https://auth.one.test` to `https://auth.two.test`, the answer's `Location` is the sign-in address `D06-pages` builds for that request over the rewritten file and begins `https://auth.two.test/`.
- R-GQTO-5Y20: The `internal/cli` package MUST export the string constant `NoCloud`, a non-empty copy constant holding no newline: what accounts says on standard error when the cloud its settings select could not be opened.
- R-GS1K-JPSP: When `Run` serves and the cloud the settings select cannot be opened, because `Cloud` returns a non-nil error `err` in AWS mode or `memory.New` (`D16-sandbox`) returns one in sandbox mode, `Run` MUST write exactly `"accounts: " + cli.NoCloud + ": " + r + "\n"` to `Stderr` in one write, `r` being `err.Error()` with every newline replaced by a space, before sending `READY=1`, and MUST then serve as it serves when the settings select `settings.ModeNone` (`D01-layout-and-run-seam`); so that, with `Cloud` returning an error whose text is `no credentials`, the run sends `READY=1`, writes that line holding `no credentials`, a `create_host` call answers a `Result` whose `IsError()` is true and whose text holds `tools.NotConfigured`, and `GET /accountsd` is answered 404; and so that in sandbox mode a directory at `state/sandbox-cloud.json` under `Dir` gets that line and the same answers.
- R-GT9G-XHJE: When `Run` serves and the settings it read select `settings.ModeAWS` or `settings.ModeSandbox`, it MUST have called `Cloud` (AWS mode) or opened the memory cloud (sandbox mode) after creating `state/accounts.db` under `Dir` and before sending `READY=1`, and every operation the store holds with status `running` when `Run` starts MUST continue (`D10-operations`) without any tool call; so that a host whose `create_host` call answered state `provisioning` in one sandbox run that was then stopped before the host was answered `running` is answered `running` by a `host` call in a later sandbox run given the same `Dir`, in which no tool but `host` is called, once the memory cloud's delays (`D16-sandbox`) have passed by `Now`.
- R-EW3T-E5OX: When `Run` serves with an `After` whose channels never deliver and the settings select `settings.ModeAWS`, the first step of an operation a tool records MUST be made without waiting on any channel `After` returned, so that, `Cloud` returning a fake that records every call, a `create_host` call (`D07-tools`) is followed, before `Run` returns and while `Run` still serves, by the fake receiving a call to `FindAddresses` or `AllocateAddress` within a deadline shorter than `ops.PollInterval` (`D10-operations`) from the tool's answer.
- R-GVP9-P10S: When `Run` serves and the settings it read select `settings.ModeNone`, no operation the store holds MUST advance, so that a host a sandbox run over the same `Dir` left in state `provisioning` is still answered `provisioning` by every `host` call of the run, and the run hands `Sink` no `host.changed` and no `operation.finished` event (`D08-trail`).
- R-GY52-GKI6: When `Run` serves and the settings it read select `settings.ModeSandbox`, every host whose instance is running in the memory cloud MUST be served by a simulated daemon (`D16-sandbox`) that reaches the run's handler, verifies through the run's verifier (`D12-host-identity`) with the binding `Settings.Binding` answers, and reports, with no process outside `Run`; so that, `Now` advancing past the memory cloud's delays, a host a `create_host` call created is answered by a later `host` call in state `running` with health `sync` (`D04-store`), and a user an `add_user` call added to an account on that host is answered by a later `users` call with `pending` false.
- R-GZCY-UC8V: When `ctx` is done while `Run` serves, every request to `/accountsd/v1/state` (`D13-daemon-endpoints`) that is waiting for a change MUST be answered before the drain deadline, `settings.Settings.Drain()` of the settings it read, has elapsed since `ctx` was done, without counting toward the requests a `*server.DrainError` reports; so that, in sandbox mode with `DRAIN_SECONDS` set to `1` and a host in health `sync` whose simulated daemon is waiting on that path, and `Sink`'s `Deliver` returning nil without delay for every event, `Run` returns `ExitSuccess` (`D02-cli`) with `service.stopping` the last event handed to `Sink`.
- R-H0KV-83ZK: When `ctx` is done while `Run` serves, a call the run's engine is making to the cloud MUST NOT keep `Run` from returning: the context of that call MUST be done once `ctx` is done, and `Run` MUST return within the drain deadline plus a margin that does not depend on that call; so that, in AWS mode with `Cloud` returning a fake whose `AllocateAddress` blocks until the test releases it, and `DRAIN_SECONDS` set to `1`, `Run` returns `ExitSuccess` (`D02-cli`) before the test releases the call, with no request in progress, and the host a `create_host` call created is answered `provisioning` by a `host` call in a later run given the same `Dir` whose settings select `settings.ModeNone`.
- R-UVI5-53XK: When `ctx` is done while `Run` serves, `Run` MUST record exactly one `service.stopping` (`D08-trail`) through the run's writer, with an empty `RequestID` and `User` and `Attrs` exactly `reason`, the text of `context.Cause(ctx)`, and MUST hand `Sink` no event after it; and when no request is still being handled once the drain deadline, `settings.Settings.Drain()` of the settings it read, has elapsed since `ctx` was done, and `Sink`'s `Deliver` returns nil without delay for every event, `service.stopping` MUST be the last event handed to `Sink` and `Run` MUST return `ExitSuccess` (`D02-cli`).
- R-UWQ1-IVO9: When `ctx` is done while `Run` serves, `Run` MUST begin no `Deliver` call on `Sink` once the drain deadline plus a margin that does not depend on `Sink` or on the requests still being handled has elapsed since `ctx` was done, so that an event recorded through the run's writer and not handed to `Sink` by then is never handed to it, and reaches `Stderr`, if at all, only as the `undelivered event` line appkit's writer writes for it; and when every request has ended before the drain deadline and `Sink`'s `Deliver` returns nil without delay for every event, every event recorded through the run's writer before it is shut down MUST have been handed to `Sink` before `Run` returns.
- R-H1SR-LVQ9: When `k` requests are still being handled once the drain deadline has elapsed since `ctx` was done, the context of each of those requests MUST be done by that deadline; and `Run` MUST return within the deadline plus a margin that depends neither on `Sink` nor on how long those requests would otherwise go on, MUST write exactly `"accounts: " + e + "\n"` to `Stderr`, `e` being the text of `server.DrainError{Unfinished: k}`, MUST write nothing to `Stderr` after that line, neither before nor after it returns, and MUST return `ExitServerFailed` (`D02-cli`); so that, in AWS mode with `DRAIN_SECONDS` set to `1`, a host the run's store holds and one `create_account` call (`D07-tools`) naming it in progress whose check calls `FindRecords` on a fake `Cloud` that blocks until the test releases it, whatever its context, `Run` returns `ExitServerFailed` before the test releases the call, the last line on `Stderr` is that line with `e` the text of `server.DrainError{Unfinished: 1}`, and, after `Run` has returned and the test has released the call and emitted an event through the writer its `MCP` was given, `Stderr` holds nothing more.
- R-H30N-ZNGY: When `Run` has taken file descriptor 3, has opened its database, serving does not fail before `ctx` is done, and `Sink`'s `Deliver` returns nil without delay for every event, it MUST write nothing to `Stdout`, neither before nor after it returns, and nothing to `Stderr` but the line appkit's `db.Open` writes when the database is ahead of this binary, when it is, the line R-GS1K-JPSP states, when the cloud cannot be opened, and, when a request is still being handled once the drain deadline has elapsed, `undelivered event` lines written before the drain line and the drain line R-H1SR-LVQ9 states; and it MUST write nothing to `Stderr` after it returns, even when an event is then recorded through the writer its `MCP` was given; whatever mode its settings select and whatever requests it answers, guests' requests, daemon requests, tool refusals, tool failures, failed operations and the simulated hosts' work included.
- R-AESO-HDDX: When serving fails before `ctx` is done, as when `Accept` on the listener `Inherit` returned returns an error that is not a temporary `net.Error`, and `Sink`'s `Deliver` returns nil without delay for every event, `Run` MUST stop serving, write to `Stderr` nothing but, when the database is ahead of this binary, the line appkit's `db.Open` writes for it, followed by exactly `"accounts: " + r + "\n"` in one write, `r` being a text that holds the text of the error `Accept` returned with every newline replaced by a space, neither before nor after it returns, even when an event is then recorded through the writer its `MCP` was given, write nothing to `Stdout`, and return `ExitServerFailed` (`D02-cli`); so that a listener whose `Accept` returns at once, on its first call, an error whose text is `listener gone` makes `Run`, over a database it created, return `ExitServerFailed` with `Stderr` holding that one line, beginning `accounts: ` and holding `listener gone`, and, after `Run` has returned and the test has emitted an event through the writer its `MCP` was given, nothing more.
- R-RDKJ-WH74: When the `accounts` binary starts with no arguments, `DRAIN_SECONDS` unset and neither `LISTEN_PID` nor `LISTEN_FDS` in its environment, from a working directory that holds no entry, it MUST write exactly `"accounts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"` to its standard error, write nothing to its standard output, exit with the value of `ExitUsage` (`D02-cli`), and leave its working directory holding no entry.
- R-H48K-DF7N: When the `accounts` binary is serving, started with no arguments, `IKIGENBA_SANDBOX` set, no hosts variable set, a listening socket passed as file descriptor 3 by the socket-activation protocol, `NOTIFY_SOCKET` naming a Unix datagram socket and `IKIGENBA_SERVICES` naming a services file whose entry `telemetry` names a Unix socket on which a listener answers every `POST /ingest` with 204, and is sent `SIGINT` with no request in progress, it MUST exit 0, having written nothing to its standard output or standard error, the last event the `telemetry` listener receives being `service.stopping` with `attrs` exactly `{"reason":"SIGINT"}`.
