# D03-serve

The serve path: what happens between `cli.Run` being called with no arguments
and the process being gone, and the run-time skeleton around the HTTP
handlers. `D01-layout-and-run-seam` declares the names this design behaves
through: `cli.Process` with its `LookupEnv`, `Pid`, `Unsetenv`, `Inherit`,
`Now`, `Rand`, `OIDCIssuer` and `DBSource`, `cli.Run`, `server.Serve`,
`server.DrainError`, and `*server.Server` as a handler. `D02-cli` decides that
an empty `Args` means serve and that nothing else touches the environment.
This design says how `Run` reads the Google settings and the drain deadline,
takes the socket the host passes in, opens the store, builds the server,
tells systemd it is ready and hands off to `Serve`; how `Serve` treats the
listener it is handed; and the one line the server writes for a request that
is trouble. It does not design any endpoint's HTTP contract (D05/D06/D07), the
store's internals (D04), or the manifest and CLI surface (D02).

## The terms every app serves on

auth serves on the terms dummy, the platform's reference app, states for every
app, and they are restated here for auth. auth serves only on a listening
socket it inherits and never opens one of its own. On a host, opsctl publishes
`ikigenba-auth.socket`, which holds the Unix stream socket
`/run/ikigenba/auth.sock` (owner `ikigenba`, group `nginx`, mode `0660`),
beside `ikigenba-auth.service`, a `Type=notify` service that runs
`/opt/auth/bin/auth` with no arguments as the `ikigenba` user, with
`/opt/auth` as its working directory and `/opt/auth/etc/env` as its
environment file. nginx proxies auth's own hostname to
`http://unix:/run/ikigenba/auth.sock:` and makes its identity subrequest for
every other app to `http://unix:/run/ikigenba/auth.sock:/check`. A deploy
restarts the service alone, so the socket, and the connections queued on it —
`/check` subrequests for every app auth guards among them — outlive every
restart. A developer stands in for the host with
`systemd-socket-activate -l 127.0.0.1:3001 auth`, which passes a loopback TCP
socket on the same terms; a browser reaches it at `http://localhost:3001`, the
origin whose callback is registered on the OAuth client for development, and a
request whose `Host` is `localhost:3001` is the local one (D05).

auth takes the socket the way `sd_listen_fds(3)` documents: `LISTEN_PID` is
its own process id, `LISTEN_FDS` counts the sockets passed, and the first is
file descriptor 3 (`SD_LISTEN_FDS_START`). It removes `LISTEN_PID`,
`LISTEN_FDS` and `LISTEN_FDNAMES` from its environment once it has taken the
socket. Once it is ready it sends the datagram `READY=1` to the Unix datagram
socket named by `NOTIFY_SOCKET`, as `sd_notify(3)` documents, a name beginning
with `@` being in the abstract namespace; with `NOTIFY_SOCKET` unset it tells
nobody. On `SIGTERM` or `SIGINT` it stops accepting, lets the requests it
already accepted finish for at most `DRAIN_SECONDS`, then cuts off whatever is
left, says how many it cut off, and exits 1. It closes its own copy of the
socket and never removes the socket's path. `DRAIN_SECONDS` and the service
unit's `TimeoutStopSec` are space-wide integer-second settings owned by opsctl
(defaults 5 and 10): opsctl writes `DRAIN_SECONDS` into every app's
`etc/env`, and no manifest sets either. auth reads `DRAIN_SECONDS` as a
positive whole number, 5 when it is unset or empty, and sets no upper limit of
its own.

The socket is auth's only way in, and every app runs as `ikigenba`, so the
suite and nginx can reach it and nothing else on the host can. The suite is a
closed system: auth trusts `X-Request-Id` as nginx sets it — on every request
it forwards to auth and on every `/check` subrequest, overwriting whatever a
client sent — and trusts a sibling that calls auth directly to have copied it
from the request it is serving. auth decides identity itself, from the session
cookie or a token, and calls no sibling. A healthy auth writes nothing;
server-side trouble gets one line on stderr naming the request by its
`X-Request-Id`, below.

## Configuration and taking the socket

With `Args` empty, `Run` first reads the three required settings through
`Process.LookupEnv`, in the order `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
`WORKSPACE_DOMAIN`; the first one unset or empty is named and the start is a
usage error. Then it reads `DRAIN_SECONDS`, so a bad value is found at start
rather than at the moment auth is asked to stop. The value is spelled the way
opsctl writes an integer: one or more ASCII digits, no sign, no leading zero,
no unit, no whitespace. There is no upper limit, so a value too large for a
`time.Duration` is the longest drain a `time.Duration` holds rather than an
error or a wrap to a negative duration. A value that is not acceptable is a
usage error, quoted back verbatim. Only then does it look for its socket, so
when several things are wrong the earliest in that order is the one reported:
a missing Google setting is named whether or not a socket was passed in and
whatever `DRAIN_SECONDS` holds.

A socket is passed in only when `LISTEN_PID` is exactly the decimal spelling
of `Process.Pid` and `LISTEN_FDS` is a count of at least one. Anything else —
`LISTEN_FDS` unset, empty, `0` or not a number, `LISTEN_PID` unset or naming
another process — is no socket passed in, and a count above one is a unit auth
will not guess about. Both are usage errors with the detail line saying how to
run auth correctly. None of the refused starts opens the database, takes the
descriptor, unsets a variable, serves anything or tells systemd anything; a
test sees that as no store created at `DBSource`, `Inherit` never called, no
`Unsetenv` call, no datagram, and no `Accept` on any listener.

With exactly one socket passed in, `Run` removes the three `LISTEN_*`
variables through `Unsetenv` and takes descriptor 3 as its listener: through
`Process.Inherit` when a test supplies one, otherwise with `net.FileListener`
over the real descriptor, which serves a Unix or a TCP stream socket alike.
The Go `net` documentation of `UnixListener.SetUnlinkOnClose` states that a
listener made by `FileListener` does not remove the socket file when it is
closed, and `FileListener` documents that closing the listener does not affect
the file it was made from; closing is all auth ever does to the socket, so the
path systemd created stays, and systemd's own copy of the socket keeps
queueing connections for the next auth. If the descriptor cannot be made into
a listener, that is trouble on the host rather than a caller's typo: `auth: `
and the error, exit 1.

## Opening the store and becoming ready

With the listener in hand, and not before, `Run` opens its store at
`Process.DBSource` (the host sets it to `state/auth.db`, relative to the
working directory `/opt/auth`) via `store.Open`, so a start refused as a usage
error has touched nothing, not even the database. When the file is absent the
store creates any missing parent directories, the file, and its schema, then
serving proceeds; D04 owns that filesystem preparation. When directory
creation or database opening fails, `auth` writes a diagnostic naming the
database source and the underlying reason and exits 1 before it serves or
reports ready; the connection that started it is closed unanswered when the
process exits.

`Run` then builds the Google client and the server. The client is built
without contacting Google — discovery is deferred to the first sign-in (D05) —
so starting touches no network, and auth starts and serves even while Google
is unreachable. The server is built in `internal/server` by a one-argument
constructor that takes a single construction struct, `server.Config`. The
struct carries every process dependency the handlers need: the opened store,
the Google client (its surface is D05's; `google.NewClient` does no I/O and
cannot fail, so building it has no failure path), the clock, the random
source from which handlers mint values such as the PKCE verifier, the
diagnostic stream to which handlers write their lines, and the workspace
domain. The Google credentials are not repeated here; the Google client
already holds them. The server side names the random source and diagnostic
stream by their `io` interfaces, never by `cli.Process`, so D01's one-way
import direction holds. That struct is the whole of what the handlers need;
D05/D06/D07 attach observable HTTP behavior to this server, not new
construction parameters.

Then `Run` tells systemd it is ready, before it calls `Serve`. The socket has
been listening since systemd made it, so from that moment every connection is
queued and will be answered: ready is true before the first `Accept`, and
sending it first means a failure to send is reported without anything having
been served. That failure is trouble — under `Type=notify` systemd would
otherwise wait out its start timeout — so it is `auth: ` and the error, exit
1, and `Serve` is never called. A test learns that auth is up the way systemd
does, by binding a datagram socket, naming it in `NOTIFY_SOCKET`, and waiting
for `READY=1`.

## Serving and stopping

`Serve` owns `ln` from the call. It serves HTTP/1.1 with `h` and blocks: plain
HTTP/1.1, no h2c, no TLS. `net/http` answers an HTTP/1.0 request with an
HTTP/1.0 status line, and the HTTP/1.1 requirement does not forbid that. On a
space the visitor's HTTP/2 and TLS are nginx's, terminated before auth is
reached.

When `ctx` is done, which is how `SIGTERM` or `SIGINT` reaches it through
`main`, `Serve` closes the listener so nothing new is accepted — new
connections wait in the socket's queue for the next auth — drops connections
that carry no request, and lets every request already being handled finish and
receive its whole response, for at most `drain`. If they all finish, it
returns nil as soon as they have. If some are still being handled when `drain`
runs out — a sign-in callback still waiting on Google, say — it closes their
connections without the rest of their responses and returns a `*DrainError`
counting them, without waiting for the handlers themselves to return. The
count is of calls to `h.ServeHTTP` that had begun and not returned; a
connection still sending its request headers is not a request yet, and is
closed at once like an idle one — `http.Server.Shutdown` alone leaves a new
connection open for several seconds, so the build tracks connection state
itself. `Run` reports that error like any other `Serve` failure:
`auth: stopped with <n> requests unfinished` (`1 request` when `<n>` is 1),
exit 1. The drain is bounded so that auth always exits before the service
unit's stop timeout and is never killed by systemd mid-write; keeping
`DRAIN_SECONDS` below that timeout is opsctl's to enforce. `Run` never passes
a drain that is not positive, and `Serve`'s behavior for one is not contract.

`Serve` is silent in its own right. Go's `net/http` documents at
`Server.ErrorLog` that, when that field is nil, errors accepting connections
and unexpected behaviour from handlers are logged through the `log` package's
standard logger, which writes to the real standard error and never passes
through `Process.Stderr`. So `Serve` gives the server an `ErrorLog` that goes
nowhere; the silence requirement fixes the outcome, not the constructor.

No read, write, or idle timeouts are contract. The build is nonetheless
expected to set `ReadHeaderTimeout`: the lint gate's gosec `G112` demands it
and suppression is forbidden, and it affects only a connection that has not
yet delivered a request header. The build must not set a write timeout that
could cut a response short inside the drain.

## What auth writes

A healthy run is silent from start to finish: no startup banner, no request
log, nothing on either stream when it stops, so that under systemd the
journal holds only trouble. The one thing that reaches `Stderr` while auth
serves is the server's line for a request that is trouble, written through
the `Config.Stderr` writer `Run` hands the server. A request is trouble when
auth answers it with a 5xx, any status from 500 through 599. auth's own are a
500 — its own database failed the read or write the request needs, or auth
otherwise could not do its part — and a 502, when Google fails a sign-in (D05).
Each such request gets exactly one line,
`auth: request <id>: <reason>`, where `<id>` is the request's `X-Request-Id`
so that a line in the journal can be matched to nginx's log of the same
request, `-` when the request carries none, and `<reason>` is the underlying
error. auth does not check the id's shape; it trusts nginx and its siblings.
Every other answer, 4xx included, writes nothing: a 4xx is the caller's to
fix, not trouble.

A store failure is the same trouble on every route: whatever the route, a
store operation that fails with anything other than `store.ErrNotFound` —
which each route already answers as its own 400, 401, 403 or 404 — is
answered 500 with a single plain-text line saying the server failed, and no
identity headers. So nginx, which treats any `/check` answer other than 200,
401 and 403 as its own failure, shows the visitor an error and never reaches
the app.

`Run` makes sure no two writes to `Stderr` are ever in progress at once, the
server's included — requests are concurrent, a handler cut off by the drain
may still be running when `Run` reports the overrun, and a test's `Stderr` is
a `bytes.Buffer` that the race detector watches.

## REQUIREMENTS

- R-KTXG-XSI4: The `internal/server` package MUST export `type Config struct { Store *store.Store; Google *google.Client; Now func() time.Time; Rand io.Reader; Stderr io.Writer; WorkspaceDomain string }`.
- R-KWD9-PBZI: The `internal/server` package MUST export `type Server` and `func New(cfg Config) *Server`.
- R-MALT-945W: `Serve` MUST accept connections on `ln` and answer every request received on them over HTTP/1.1 with the response `h` produces for that request, and MUST NOT return while `ctx` is not done and serving has not failed.
- R-MBTP-MVWL: When `ctx` is done, `Serve` MUST stop accepting connections and close `ln`, and MUST close every connection that carries no request.
- R-MD1M-0NNA: When `ctx` is done, `Serve` MUST let every call to `h.ServeHTTP` already in progress continue and MUST deliver the complete response of every such call that returns within `drain` after `ctx` was done; when every such call has returned and its response has been delivered before `drain` has elapsed, `Serve` MUST return nil without waiting for `drain` to elapse.
- R-ME9I-EFDZ: When `n` calls to `h.ServeHTTP`, `n` at least 1, are still in progress once `drain` has elapsed after `ctx` was done, `Serve` MUST close the connections those requests arrived on without writing the rest of their responses and MUST return a `*DrainError` whose `Unfinished` is `n`, without waiting for those calls to return.
- R-MFHE-S74O: `(*DrainError).Error` MUST return exactly `"stopped with 1 request unfinished"` when `e.Unfinished` is 1, and exactly `"stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"` for every other value of `e.Unfinished`.
- R-MGPB-5YVD: When serving fails while `ctx` is not done, `Serve` MUST return a non-nil error, and `Serve` MUST NOT return nil for any reason other than `ctx` being done.
- R-MJ53-XICR: `Serve` MUST write nothing to the `log` package's default logger and nothing to any process stream, whatever `ln` and `h` do: the diagnostics `net/http`'s server writes through its `ErrorLog`, such as an `Accept` error it retries or a handler that panics, MUST be discarded, so that a test which points the `log` package's output at a buffer and drives `Serve` with a listener whose `Accept` returns a temporary error and a handler that panics finds the buffer empty when `Serve` returns.
- R-MKD0-BA3G: When `Args` is empty, `Run` MUST decide whether each of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `WORKSPACE_DOMAIN` is set before it decides whether `DRAIN_SECONDS` is acceptable, and MUST decide whether `DRAIN_SECONDS` is acceptable before it calls `LookupEnv` for `LISTEN_PID` or `LISTEN_FDS`, so that when several of these are wrong the diagnostic `Run` writes is the one for the earliest in that order.
- R-MLKW-P1U5: When `Args` is empty and one of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, taken in that order, is the first for which `LookupEnv` returns `false` or the empty string, `Run` MUST write exactly `"auth: " + name + " is not set\n"` to `Stderr` for that `name`, write nothing to `Stdout`, call none of `store.Open`, `Inherit`, or `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-MMST-2TKU: `Run` MUST accept a non-empty `DRAIN_SECONDS` value if and only if it consists of one or more ASCII decimal digits, the first of which is not `0`, and no other characters, whatever the magnitude of the integer those digits denote, so that `0`, `-1`, `2.5`, `5s`, `05`, ` 5`, and `abc` are refused and no value is refused for being large.
- R-MO0P-GLBJ: When `Args` is empty, the three Google settings are set, and `LookupEnv("DRAIN_SECONDS")` returns `true` with a non-empty value `v` that `Run` does not accept, `Run` MUST write exactly `"auth: DRAIN_SECONDS is '" + v + "', not a positive whole number of seconds\n"` to `Stderr` with `v` verbatim, write nothing to `Stdout`, call none of `store.Open`, `Inherit`, or `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-MP8L-UD28: The `drain` `Run` passes to `server.Serve` MUST be `5 * time.Second` when `LookupEnv("DRAIN_SECONDS")` returns `false` or the empty string, and otherwise, for the accepted value denoting the integer `n`, MUST be `n` seconds when `n` seconds is at most the largest `time.Duration` and the largest `time.Duration` when it is not.
- R-MQGI-84SX: `Run` MUST treat a socket as passed in if and only if `LookupEnv("LISTEN_PID")` returns `true` with a value equal to `strconv.Itoa(p.Pid)` and `LookupEnv("LISTEN_FDS")` returns `true` with a value of one or more ASCII decimal digits and no other characters denoting an integer of at least 1, that integer being the number of sockets passed in.
- R-MROE-LWJM: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, and no socket is passed in, `Run` MUST write exactly `"auth: no socket was passed in\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3001 auth'\n"` to `Stderr`, write nothing to `Stdout`, call none of `store.Open`, `Inherit`, or `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-MSWA-ZOAB: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, and more than one socket is passed in, `Run` MUST write exactly `"auth: " + v + " sockets were passed in, expected 1\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3001 auth'\n"` to `Stderr` with `v` the value of `LISTEN_FDS` verbatim, write nothing to `Stdout`, call none of `store.Open`, `Inherit`, or `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-MU47-DG10: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, and exactly one socket is passed in, `Run` MUST call `Unsetenv`, when it is not nil, once with each of `LISTEN_PID`, `LISTEN_FDS`, and `LISTEN_FDNAMES` before it returns, and MUST NOT call it with any other key.
- R-MVC3-R7RP: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, and exactly one socket is passed in, `Run` MUST take file descriptor 3, and no other descriptor, as its listener, by calling `Inherit(3)` exactly once when `Inherit` is not nil, and by calling `net.FileListener` on a file for the process's file descriptor 3 when `Inherit` is nil.
- R-MWK0-4ZIE: When taking file descriptor 3 as a listener fails with an error `err`, `Run` MUST write exactly `"auth: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, not call `store.Open`, send nothing to a notification socket, and return `1`.
- R-MXRW-IR93: `Run` MUST call `store.Open(p.DBSource, p.Rand)` only after it has taken file descriptor 3 as a listener, exactly once, and before it sends anything to a notification socket or calls `server.Serve`.
- R-NII7-0UUW: When `store.Open` returns without error, `Run` MUST proceed identically whether or not the database source pre-existed, relying on D04's `store.Open` to have created the schema when the source was absent.
- R-MYZS-WIZS: When `store.Open` returns an error, `Run` MUST write exactly `"auth: cannot open database " + p.DBSource + ": " + err.Error() + "\n"` to `Stderr`, where `err` is that error, write nothing to `Stdout`, send nothing to a notification socket, accept no connection on the listener it took, and return `1`.
- R-A6NL-V77Q: When `store.Open` succeeds, `Run` MUST construct the Google client via `google.NewClient` with the values of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, and `p.OIDCIssuer`, construct the server via `server.New` with a `server.Config` whose `Store` is the opened `*store.Store`, `Google` is that client, `Now` is `p.Now`, `Rand` is `p.Rand`, `Stderr` is a writer `w` each of whose `Write` calls results in exactly one call to `p.Stderr.Write` with the same bytes, and `WorkspaceDomain` is the value of `WORKSPACE_DOMAIN`, and, unless notifying fails, call `server.Serve` exactly once with `Run`'s own `ctx`, the listener it took, that `*server.Server` as the handler, and the `drain` of R-MP8L-UD28, without contacting the issuer `p.OIDCIssuer` names before a request needs it. This wiring MUST be observable through the served listener: with `p.OIDCIssuer` set to a reachable loopback OIDC issuer, `p.Rand` a deterministic reader, and `p.Now` a fixed clock, a `GET /login/google` served by `Run` MUST return `302` whose `Location` is addressed to that issuer's discovered `authorization_endpoint` and whose `state` and `code_challenge` are derived from `p.Rand`; the sign-in handler's own contract is D05's (R-TTTA-C8HY, R-KY4E-8B7G), not this requirement's.
- R-N1FL-O2H6: When `store.Open` has succeeded and `LookupEnv("NOTIFY_SOCKET")` returns `true` with a non-empty value `a`, `Run` MUST, after `store.Open` returns and before calling `server.Serve`, send exactly one datagram, whose content is exactly `READY=1`, to the Unix datagram socket whose address is `a`, an `a` beginning with `@` naming a socket in the abstract namespace; `Run` MUST send nothing to a notification socket on any other occasion.
- R-N3VE-FLYK: When `LookupEnv("NOTIFY_SOCKET")` returns `false` or the empty string, `Run` MUST send no datagram and MUST serve as it otherwise would.
- R-N53A-TDP9: When sending `READY=1` fails with an error `err`, `Run` MUST write exactly `"auth: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, accept no connection on the listener it took, not call `server.Serve`, and return `1`.
- R-N6B7-75FY: A `Run` that calls `server.Serve` and to which `Serve` returns nil MUST return `0`.
- R-GYUP-OF53: A `Run` that calls `server.Serve` MUST write nothing to `Stdout`, and MUST write nothing to `Stderr` other than what the server writes through the writer of R-A6NL-V77Q and, when `Serve` returns a non-nil error, the one line R-N8QZ-YOXC states.
- R-N8QZ-YOXC: When `server.Serve` returns a non-nil error to `Run`, `Run` MUST write to `Stderr` exactly `auth: `, that error's `Error()` text, and a newline, MUST write nothing to `Stdout`, and MUST return `1`.
- R-H2IE-TQD6: `Run` MUST NOT let two calls to `Stderr.Write` be in progress at the same time, those made through the writer of R-A6NL-V77Q included, so that a `Stderr` that is not safe for concurrent use is never written concurrently.
- R-NB6S-Q8EQ: When `Inherit` is nil and file descriptor 3 is a listening Unix-domain stream socket bound to a filesystem path, `Run` MUST leave that path in place and MUST NOT shut the socket down, so that after `Run` returns the socket still accepts connections into its queue for another process that holds it.
- R-IVLV-52P1: The `*server.Server` returned by `server.New` MUST serve the HTTP routes whose contracts D05, D06, and D07 define.
- R-UR0L-ZVDJ: The `*Server` returned by `New` MUST mint every random value the `*Server` mints itself (including the PKCE verifier D05 requires of `GET /login/google`) by reading `cfg.Rand`, MUST write every diagnostic its handlers emit (including the token-exchange error D05 requires of `GET /login/google/callback`) to `cfg.Stderr`, and MUST NOT read a global random source or write to a global output stream; given a `Config` whose `Rand` is a deterministic reader and whose `Stderr` is an in-memory buffer, the values minted are a function of the bytes that reader yields and every such diagnostic appears in that buffer.
- R-CCQE-EHNR: When a store operation the `*Server` calls while handling a request on any route D05, D06, or D07 defines returns an error that does not satisfy `errors.Is(err, store.ErrNotFound)`, the `*Server` MUST answer that request with status `500`, `Content-Type: text/plain; charset=utf-8`, a body that is a single line of plain text, and neither `HeaderUserID` nor `HeaderUserEmail` set, whatever response another requirement states for that request, except that a `GET /login/google` already being answered `502` under R-XXPJ-ZJU1 stays `502` when the `ConsumeLoginState` it makes to discard its login state fails.
- R-XV9R-80CN: For every request the `*Server` answers with a status from `500` through `599`, it MUST write exactly `"auth: request " + id + ": " + reason + "\n"` to `cfg.Stderr` in a single call to `cfg.Stderr.Write`, where `id` is the value `r.Header.Get("X-Request-Id")` returns when that value is non-empty and `-` when it is empty, and `reason` is the `Error()` text of the error that caused that status.
- R-XWHN-LS3C: The `*Server` MUST write nothing to `cfg.Stderr` for a request it answers with a status outside `500` through `599`, and MUST write exactly one line to `cfg.Stderr` for each request it answers with a status from `500` through `599`.
