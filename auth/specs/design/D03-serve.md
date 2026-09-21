# D03-serve

The bare `auth` binary serves. On the host, systemd runs it with no arguments
and an environment file that sets `PORT`, the two Google secrets, and
`WORKSPACE_DOMAIN`; a developer at a terminal stands in for systemd. This
design owns the run-time skeleton around the HTTP handlers: reading and
validating the environment, opening the SQLite store, listening on loopback,
constructing the server, and stopping it gracefully. It does not design any
endpoint's HTTP contract (D05/D06/D07), the store's internals (D04), or the
manifest and CLI surface (D02).

Configuration comes from the environment, read through the run seam's
`Process.Getenv` (declared in D01). `PORT` must be an integer from 1 to 65535;
`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `WORKSPACE_DOMAIN` must each be
present. A configuration fault is a usage error: `cli.Run` writes one
`auth: `-prefixed line to `Process.Stderr`, leaves stdout empty, opens nothing,
listens nowhere, and returns exit code 2. Only after the environment is whole
does `auth` touch the disk or the network.

`auth` then opens its store at the source `Process.DBSource` (the host sets it
to `state/auth.db`, relative to the working directory `/opt/auth`) via
`store.Open`. When the file is absent the store creates it and its schema, then
serving proceeds; this is the only difference between a first start and an
ordinary one, and it is D04's `store.Open` that creates the schema — this
design only requires that serving proceeds identically once `store.Open`
succeeds. When the file exists but cannot be opened, `auth` writes a diagnostic
naming the database source and the underlying reason, and returns exit code 1.

A healthy server listens on `127.0.0.1:$PORT` and nowhere else — nginx on the
host terminates TLS and proxies to loopback — prints nothing on either stream,
and does not exit until it is signalled. If the loopback bind fails because the
port is taken, the listener's own error is relayed, prefixed with `auth: `, and
`auth` returns exit code 1 without disturbing the process that holds the port.
`SIGTERM` and `SIGINT` are handled identically: `auth` finishes the requests it
has accepted, closes the listener, and returns 0 (D01 fixes `cli.Run` to 0 on
both signals).

The HTTP server is built in `internal/server` by a one-argument constructor
that takes a single construction struct, `server.Config`, and exposes a
lifecycle to serve on the loopback listener and to shut down gracefully. The
struct carries every process dependency the handlers need: the opened store,
the Google client (its surface is D05's), the clock, the random source from
which handlers mint values such as the PKCE verifier, the diagnostic stream to
which handlers write errors such as a failed token exchange, and the workspace
domain. The Google credentials are not repeated here; the Google client already
holds them. `cli.Run` fills the struct from the `Process` it was given, so a
test that drives the server directly can hand it a deterministic reader and a
buffer and observe exactly what the handlers drew and wrote. The server side
names the random source and diagnostic stream by their `io` interfaces, never
by `cli.Process`, so D01's one-way import direction holds. That struct is the
whole of what the handlers need; D05/D06/D07 attach observable HTTP behavior to
this server, not new construction parameters.

## REQUIREMENTS

- R-KTXG-XSI4: The `internal/server` package MUST export `type Config struct { Store *store.Store; Google *google.Client; Now func() time.Time; Rand io.Reader; Stderr io.Writer; WorkspaceDomain string }`.
- R-KWD9-PBZI: The `internal/server` package MUST export `type Server` and `func New(cfg Config) *Server`.
- R-IEJ9-SABB: `*server.Server` MUST export `func (*Server) Serve(addr string) error` and `func (*Server) Shutdown(ctx context.Context) error`.
- R-IFR6-6220: `cli.Run` MUST read exactly `PORT`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `WORKSPACE_DOMAIN` from the environment through `Process.Getenv`, and MUST validate `PORT` before the three Google settings.
- R-IGZ2-JTSP: When `PORT` is unset or empty, `cli.Run` MUST write the single line `auth: PORT is not set` to `Process.Stderr`, write nothing to `Process.Stdout`, open no store, listen on no address, and return 2.
- R-II6Y-XLJE: When `PORT` is set to a value that is not an integer from 1 to 65535 inclusive, `cli.Run` MUST write the single line `auth: PORT is '<value>', not a port number` to `Process.Stderr` (where `<value>` is the raw `Process.Getenv("PORT")` value), write nothing to `Process.Stdout`, open no store, listen on no address, and return 2.
- R-IJEV-BDA3: When `PORT` is valid but one of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN` (checked in that order) is unset or empty, `cli.Run` MUST write the single line `auth: <NAME> is not set` for the first such name to `Process.Stderr`, write nothing to `Process.Stdout`, open no store, listen on no address, and return 2.
- R-IKMR-P50S: With a whole configuration, `cli.Run` MUST open the store via `store.Open(Process.DBSource, Process.Rand)` before it listens on any address.
- R-ILUO-2WRH: When `store.Open` returns without error, `cli.Run` MUST proceed to serve identically whether or not the database source pre-existed, relying on D04's `store.Open` to have created the schema when the source was absent, and MUST write nothing to `Process.Stdout` or `Process.Stderr` on this path.
- R-IN2K-GOI6: When `store.Open` returns an error, `cli.Run` MUST write the single line `auth: cannot open database <source>: <reason>` to `Process.Stderr` (where `<source>` is `Process.DBSource` and `<reason>` is the `store.Open` error text), write nothing to `Process.Stdout`, listen on no address, and return 1.
- R-IOAG-UG8V: A healthy `auth` MUST listen on `127.0.0.1:<PORT>` (the configured port) and on no other address, serving the HTTP server built by `server.New`.
- R-KXL6-33Q7: While serving with a whole configuration and an opened store, `cli.Run` itself MUST write nothing to `Process.Stdout` or `Process.Stderr`, so that in the absence of any request whose D05 contract writes a diagnostic both streams stay empty for as long as the server runs, and `cli.Run` MUST NOT return until it receives `SIGINT` or `SIGTERM`.
- R-IRY5-ZRGY: When the loopback bind fails because the configured port is already in use, `cli.Run` MUST write the single line `auth: listen tcp 127.0.0.1:<PORT>: bind: address already in use` (the configured port) to `Process.Stderr`, write nothing to `Process.Stdout`, leave the process already holding the port undisturbed, and return 1.
- R-IT62-DJ7N: On `SIGTERM` and on `SIGINT`, `cli.Run` MUST behave identically: let requests already accepted receive their full response, close the listener via `Server.Shutdown`, leave nothing listening on `127.0.0.1:<PORT>` afterward, and return 0.
- R-KYT2-GVGW: `cli.Run` MUST construct the Google client via `google.NewClient` passing `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, and `Process.OIDCIssuer`, and when `google.NewClient` returns an error MUST write an `auth: `-prefixed diagnostic to `Process.Stderr`, listen on nothing, and return 1; otherwise it MUST construct the server via `server.New` with a `server.Config` whose `Store` is the opened `*store.Store`, `Google` is that Google client, `Now` is `Process.Now`, `Rand` is `Process.Rand`, `Stderr` is `Process.Stderr`, and `WorkspaceDomain` is `WORKSPACE_DOMAIN`, then run it via `Server.Serve` on the address `127.0.0.1:<PORT>` and stop it via `Server.Shutdown`.
- R-IVLV-52P1: The `*server.Server` returned by `server.New` MUST serve the HTTP routes whose contracts D05, D06, and D07 define.
- R-UR0L-ZVDJ: The `*Server` returned by `New` MUST mint every random value the `*Server` mints itself (including the PKCE verifier D05 requires of `GET /login/google`) by reading `cfg.Rand`, MUST write every diagnostic its handlers emit (including the token-exchange error D05 requires of `GET /login/google/callback`) to `cfg.Stderr`, and MUST NOT read a global random source or write to a global output stream; given a `Config` whose `Rand` is a deterministic reader and whose `Stderr` is an in-memory buffer, the values minted are a function of the bytes that reader yields and every such diagnostic appears in that buffer.
