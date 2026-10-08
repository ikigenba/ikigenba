# D03-serve

The serve path: what happens between `cli.Run` being called with no arguments
and the process being gone, and the run-time skeleton around the HTTP
handlers. `D01-layout-and-run-seam` declares the names this design behaves
through: `cli.Process` with its `LookupEnv`, `Pid`, `Unsetenv`, `Inherit`,
`Now`, `Rand`, `OIDCIssuer`, `Dir`, `Banner` and `Sink`, `cli.Run`, the
database path, `auth.Migrations`, `server.Serve`, `server.DrainError`, and
`*server.Server` as a handler; appkit's `db` package (its D15 and D16) owns
the database handle and the migrations, and D04 owns the store over it. `D02-cli` decides that
an empty `Args` means serve and that nothing else touches the environment.
This design says how `Run` reads the Google settings, the drain deadline and
the two optional origins, takes the socket the host passes in, opens the
database and builds the store over it, builds the server,
tells systemd it is ready, records that it started, hands off to `Serve`,
and records that it is stopping; how `Serve` treats the
listener it is handed; how the server divides requests between appkit's
shared files and its own routes; and the trail auth records, and the little
it writes to stderr, while it serves. It does not design any endpoint's HTTP contract
(D05/D06/D07/D09, and D08 for appkit's shared files under `/_appkit/`), the
store's internals (D04), or the manifest and CLI surface (D02).

## The terms every app serves on

auth serves on the terms dummy, the platform's reference app, states for every
app, and they are restated here for auth. auth serves only on a listening
socket it inherits and never opens one of its own. What kind of socket that
is, and where it lives, is the host's business: auth serves whatever it is
passed the same way, and nothing here depends on its kind. On a host, opsctl
publishes `ikigenba-auth.socket` beside `ikigenba-auth.service`, a
`Type=notify` service that runs `/opt/auth/bin/auth` with no arguments as the
`ikigenba` user, with `/opt/auth` as its working directory and
`/opt/auth/etc/env` as its environment file. The host's nginx sends auth the
requests for auth's own hostname and the identity subrequest, `/check`, for
every other app, or `/check/open` for an app that serves guests; to auth each
is an ordinary HTTP request. A deploy restarts the service alone, so the
socket, and the connections queued on it — `/check` and `/check/open`
subrequests for every app auth guards among them — outlive every restart.

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
socket and nothing more: it never shuts the socket down or removes it.
`DRAIN_SECONDS` and the service unit's `TimeoutStopSec` are space-wide
integer-second settings owned by opsctl (defaults 5 and 10): opsctl writes
`DRAIN_SECONDS` into every app's `etc/env`, and no manifest sets either. auth
reads `DRAIN_SECONDS` as a positive whole number, 5 when it is unset or empty,
and sets no upper limit of its own. The environment opsctl gives auth also
carries `IKIGENBA_SERVICES`, the path of the host's services file, normally
`/var/lib/ikigenba/services.json`; on a host that has no services file it is
unset. auth reads
it once, at start, through `page.New` in `main` (`D01-layout-and-run-seam`
explains why that read happens there and cannot fail), and auth's own code
never reads it; each banner reads the file through appkit's `services.Read`,
in the format opsctl publishes. Unset, empty, not a clean path, or naming a
file that is missing, unreadable or malformed, auth starts, serves its
pages without a launcher, and says nothing about it.

Two more variables are optional and independent of each other, and a host
sets neither: `IKIGENBA_PUBLIC_URL`, auth's own public origin, and
`IKIGENBA_CALLBACK_URL`, the origin Google sends a browser back to after a
sign-in. A sandbox, the local runner a developer brings the platform up in
over plain HTTP on a loopback port, sets both, for instance
`IKIGENBA_PUBLIC_URL=http://auth.wip.localhost:7400` and
`IKIGENBA_CALLBACK_URL=http://localhost:7400`; that is the sandbox's published
environment contract, and auth reads nothing else of it, `IKIGENBA_SANDBOX`
included. What each changes once auth serves is D05's: the Google
`redirect_uri`, auth's own origin, and which sign-out origins are on the
space. With both unset auth behaves exactly as it does on a host.

The socket is auth's only way in. Only nginx and the suite's own apps can
reach it; keeping everything else out is the host's job, not auth's. The suite
is a closed system: auth trusts `X-Request-Id` as nginx sets it — on every
request it forwards to auth and on every `/check` and `/check/open` subrequest, overwriting
whatever a client sent — and trusts a sibling that calls auth directly to have
copied it from the request it is serving. auth decides identity itself, from
the session cookie or a token, and calls no sibling. Every request auth
serves is recorded in the trail under that request id, and stderr holds only
trouble, below.

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
usage error, quoted back verbatim. Then it reads `IKIGENBA_PUBLIC_URL`, then
`IKIGENBA_CALLBACK_URL`, so a bad value is found at start rather than at the
first request that needs it. Each must be unset or an **origin value**: `http`
or `https` (lowercase), `://`, a host of lowercase ASCII letters, digits, `-`
and `.` with no empty label (no leading or trailing `.`, no `..`), and
optionally `:` and a port from 1 through 65535 with no leading zero and never
the scheme's default (`80` for `http`, `443` for `https`), with nothing else —
no user part, no path (not even a trailing `/`, since auth appends
`/login/google/callback` itself), no query, no fragment. That is the form a
browser writes a domain origin in: a lowercase host, and the port as a plain
integer only when it is not the scheme's default (RFC 6454 §6.2; the WHATWG
URL standard's host and port serialization). auth compares the value exactly
against what browsers send, so a value spelled any other way would never
match. The grammar does not refuse every such spelling: a shorthand IPv4 host
such as `http://127.1:7400` is accepted. The host rule is stricter than the one D05's return-URL rule uses for
an in-space host, which allows either case, any placement of `.`, and any
port. A variable present with an empty value is
not unset, and like any other value that is not an origin it is a usage error,
`auth: <NAME> is '<value>', not an origin`, the value quoted back verbatim in
the style of the `DRAIN_SECONDS` line. Only then does it look for its socket,
so when several things are wrong the earliest in that order is the one
reported: a missing Google setting is named whether or not a socket was passed
in and whatever `DRAIN_SECONDS`, `IKIGENBA_PUBLIC_URL`, and
`IKIGENBA_CALLBACK_URL` hold.

A socket is passed in only when `LISTEN_PID` is exactly the decimal spelling
of `Process.Pid` and `LISTEN_FDS` is a count of at least one. Anything else —
`LISTEN_FDS` unset, empty, `0` or not a number, `LISTEN_PID` unset or naming
another process — is no socket passed in, and a count above one is a unit auth
will not guess about. Both are usage errors with the detail line saying how to
run auth correctly. None of the refused starts opens the database, takes the
descriptor, unsets a variable, serves anything or tells systemd anything; a
test sees that as an empty `Dir` still empty, `Inherit` never called, no
`Unsetenv` call, no datagram, and no `Accept` on any listener.

With exactly one socket passed in, `Run` removes the three `LISTEN_*`
variables through `Unsetenv` and takes descriptor 3 as its listener: the one
`Process.Inherit` returns when a test supplies it, otherwise one made from the
real descriptor. `Run` uses that listener only through the `net.Listener`
interface: it accepts connections on it and closes it when it is done, and
nothing else. The code names no socket kind and inspects the listener no
further: no type assertion or type switch on it and no setting beyond what the
interface offers. None is needed. Go documents that a listener made from an
already open descriptor (`net.FileListener`) leaves the socket in place when
it is closed, so the socket systemd made stays where it is, and systemd's own
copy of it keeps queueing connections for the next auth. If the
descriptor cannot be made into a listener, that is trouble on the host rather
than a caller's typo: `auth: ` and the error, exit 1.

## Opening the database and becoming ready

With the listener in hand, and not before, `Run` opens its database with
appkit's `db.Open` (its D15 and D16): the path is the database path,
`state/auth.db` resolved against `Process.Dir`, which `Run` passes as
`filepath.Join(Dir, "state", "auth.db")`, so with an empty `Dir` it is the
relative path appkit resolves against the process's working directory; the
migrations are `auth.Migrations()`; the clock is `Process.Now`; the service
is `auth`; and the writer for appkit's warning is `Process.Stderr`. So a start
refused as a usage error has touched nothing, not even the database. appkit
creates the missing directories and the file, applies the migrations the
database has not had and stamps them with that clock, and refuses a database
it cannot open; how it does each is appkit's contract, and auth's tests do not
re-prove it. They
prove the wiring instead: after a start with a fixed clock, appkit's
`db.Status` reports `Dir`'s `state/auth.db` exactly as it reports a database
the test made itself with `db.Open`, `auth.Migrations()` and that clock. That
holds for a first start in an empty `Dir`, and equally for a start over a
database an earlier auth wrote before it carried migrations, which `0001`
adopts unchanged, `0002` brings to the prefixed token ids, and `0003` makes
every token a personal token bound to no host (D04), every user, session,
login state and token it held kept. `Run` builds its store
over the handle with `store.New`, handing it `Process.Rand`, and closes the
handle before it returns; the store neither opens nor closes it (D04).

When `db.Open` fails, the start is refused as trouble on the host: one line,
`auth: cannot open database state/auth.db: ` and the error's text, exit 1,
before anything is served, before systemd is told anything, and before any
event is recorded. The path in the line is the relative one the manifest
declares, whatever `Dir` is, so an operator reads the same line on every host;
the error's text names the absolute path where appkit's does. A newline in that
text is replaced by a space, so the line is one line whatever appkit's error
holds. Under systemd the start fails, and the socket keeps queueing for an
auth that can serve. A missing database is not this error; it is the first
start.

A database a newer auth has migrated is not this error either. A rollback
deploy runs the older binary over the newer schema, and each release's
migrations only expand the schema the release before it uses, so the older
auth runs on it. appkit's `db.Open` accepts a database that records a
version auth does not carry: it applies nothing, not even a migration auth
carries that the database lacks, and returns the handle, having written one
warning line to the `Stderr` auth gave it, beginning with the service name
auth gave it, `auth: `, and naming the lowest version it does not know. That
line is appkit's, and auth states nothing about its text: a test compares
`Stderr` with what its own `db.Open` call, with the service `auth`, writes for
the same database. auth adds nothing to it and otherwise starts and serves as
it always does, telling systemd it is ready and answering requests over that
database.

`Run` then builds the Google client and the server. The client is built
without contacting Google — discovery is deferred to the first sign-in (D05) —
so starting touches no network, and auth starts and serves even while Google
is unreachable. The server is built in `internal/server` by a one-argument
constructor that takes a single construction struct, `server.Config`. The
struct carries every process dependency the handlers need: the opened store,
the Google client (its surface is D05's; `google.NewClient` does no I/O and
cannot fail, so building it has no failure path), the clock, the random
source from which handlers mint values such as the PKCE verifier, the
telemetry writer through which the server records every event, the workspace
domain, the public origin and the callback origin (each the variable's value,
or empty when it is unset, so an empty field means "as on a host"), and, last,
the banner source `Process.Banner` carries. `Run` never
calls the banner source itself: only the handlers do, once for each page
they draw with the banner (D05, D07), so the services file can neither delay
nor fail a start, and a start that is refused never reads it. The Google credentials are not repeated here; the Google client
already holds them. The server side names the random source by its `io` interface and the
writer as appkit's `*telemetry.Writer`, never by `cli.Process`; it has no
diagnostic stream of its own, because nothing it does is trouble that belongs
on stderr. That struct is the whole of what the handlers need;
D05/D06/D07/D08/D09 attach observable HTTP behavior to this server, not new
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

Once ready, `Run` records `service.started`, with auth's version, as the
first event of the trail; it is the `Ready` of the one `telemetry.Writer`
`Run` builds (`D01-layout-and-run-seam`, "The trail"), whose sink is
`Process.Sink`. A start that is refused, or that fails before `Serve` is
called — a usage error, a descriptor that is not a listener, a database that
will not open, a `READY=1` that cannot be sent — records no event at all, and
hands nothing to the sink.

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

The trail closes inside the same drain window. When `Serve` returns nil, every
request auth accepted has finished and recorded its `request.finished`, so
`Run` then records `service.stopping`, the last event, with the reason the
context's cancellation cause gives (`SIGTERM` or `SIGINT` from `main`), and
waits for the writer to deliver what it holds — but only until `drain` has
elapsed since the context was done; whatever is still undelivered then goes to
stderr as undelivered events, and `Run` returns `0` all the same. When `Serve`
returns a `*DrainError`, the window has already closed: `Run` does not wait to
deliver `service.stopping`, which goes to stderr after any other event still
undelivered, and only then writes the `stopped with` line. A cut-off request
whose handler ends after `Serve` has returned records its `request.finished`
too late for the trail: the sink is never left to store it, and it appears
as one more undelivered line, in no fixed place among the others, if the
process is still running to write it. When `Serve` fails while
the context is not done, `Run` records `service.stopping` with the reason
`failed`, waits for delivery for at most `drain`, and then writes the failure's
line and exits 1.

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

## Routing

Every request the server receives, whatever its path, first passes through
appkit's request middleware, `telemetry.Middleware` with the server's writer,
the outermost layer of auth's handler tree: auth's own pages, `/check`,
`/check/open`, `/me`, the OAuth routes MCP clients use (D09),
the shared files and a path no route defines alike. It records
`request.started` when the request arrives and `request.finished`, with the
status appkit's D14 defines for auth's answer — the status auth answered,
but for cases such as a handler that panics before writing anything,
recorded as 500 — how long it took, how many bytes of the request's body
auth read, and how many bytes of body its answer carried, once it has
answered, both under
the request's `X-Request-Id` and the user its `X-User-Id` names; a request
without an id is given one, 32 lowercase hexadecimal digits minted from the
writer's random source, which is `Process.Rand`, or from `crypto/rand` when
reading that source fails. Every other event auth
records for the request — a check event (D06), a sign-in event (D05), a token
event (D07), a client event (D09) — comes between the two and carries the same request id. The
middleware is inside the `*Server`, so a test that drives `server.New`'s
handler directly sees the same events a test that drives `Run` does.

Inside it, the server divides every request by its path before anything else. A path
beginning with `/_appkit/`, `page.StaticPrefix`, is handed unchanged to
the handler `page.Static()` returns, ahead of every route of auth's own:
that handler compares the whole `URL.Path` itself, so nothing is stripped, and
what it answers — the nine shared files, their 404s and 405s — is D08's. No
identity is decided and the store is never called for such a request, so a
failed store changes nothing there, which is why the store-failure rule below
names only the routes D05, D06, D07 and D09 define. Every other path goes to auth's
own routes. `/assets/` is no longer special: the style files auth used to
serve there are appkit's now, under `/_appkit/`, and a path under `/assets/`
is a path no route defines, answered 404 like any other. The page templates
auth now keeps in its `assets/` directory (D01) are drawn into pages, never
served as files, so they change nothing here.

auth's own routes are those D05, D06 and D07 define — the sign-in and profile
pages, `/check`, `/check/open`, `/me`, and the token actions, among them
`POST /tokens/<id>/revoke` for an MCP client token — and the OAuth
authorization server's, which D09 defines: `GET
/.well-known/oauth-authorization-server`, `POST /register`, `GET /authorize`,
`POST /authorize`, and `POST /token`. The metadata document is one exact
route among auth's own, reached after the `/_appkit/` split like every other,
and like `/check` it needs no credential. RFC 8414 §3.1 places it at
`/.well-known/oauth-authorization-server` with nothing after it because auth's
issuer, its own origin, has no path component; so nothing beneath that path is
a route, and neither is any other path under `/.well-known/`. Each of these
routes answers the methods its design states; what auth answers for another
method on one of them follows the precedent of auth's other routes and is not
fixed here.

## What auth records and writes

auth's trail is its record of what it did; its stderr holds only trouble, so
that under systemd the journal shows nothing else. Trouble is of two kinds.
One is a condition auth cannot continue from: a start it refuses, a database
it cannot open, a `READY=1` it cannot send, a `Serve` that fails or a drain
that cuts requests off; each has its own diagnostic line, stated with its
case. The other is an event the writer could not deliver — the telemetry
service is missing from the services file or not accepting, it rejected the
event, the queue was full, or the drain window closed first — which appkit's
writer writes as one line, `auth: undelivered event: ` and the event's JSON
(and, for an event auth formed wrongly, a bug, `auth: malformed event: `).
Nothing else reaches `Stderr` but appkit's one warning when the database is
ahead of the binary (above): no startup message, no request log, nothing
when auth stops cleanly. Telemetry being unreachable is never a reason to
stop: auth answers every request the same, and exits as it otherwise would.

A request auth answers is not trouble, whatever its status. auth's own 5xx
are a 500 — its own database failed the read or write the request needs, or
auth otherwise could not do its part — and a 502, when Google fails a sign-in
(D05). Each is a handled failure: it writes nothing to stderr, and the
request's `request.finished` records the status, under the request id that
ties it to nginx's log of the same request.

A store failure is the same failure on every route: whatever the route, a
store operation that fails with anything other than `store.ErrNotFound` —
which each route already answers as its own 400, 401, 403 or 404, or as
D09's redirect or OAuth error — is
answered 500 with a single plain-text line saying the server failed, and no
identity headers. A test makes the store fail at the server, where it holds
the handle: it builds the store with `store.New` over a handle of its own and
calls `SetFailing(true)` on that handle (appkit's failure seam, D04), or,
where only one statement must fail, creates through `DB.Write` on that handle
a trigger that aborts it, such as the `DELETE` that discards a login state. So nginx, which treats any `/check` or `/check/open` answer
other than 200, 401 and 403 as its own failure, shows the visitor an error and never reaches
the app.

`Run` makes sure no two writes to `Stderr` are ever in progress at once, the
writer's included — the writer's sender runs beside the requests, an event
emitted after the drain may be written while `Run` reports the overrun, and a
test's `Stderr` is a `bytes.Buffer` that the race detector watches.

## REQUIREMENTS

- R-B3GY-XRZO: The `internal/server` package MUST export `type Config struct { Store *store.Store; Google *google.Client; Now func() time.Time; Rand io.Reader; Telemetry *telemetry.Writer; WorkspaceDomain string; PublicURL string; CallbackURL string; Banner func(u page.User) page.Banner }`, with exactly those fields in that order, where `page` is the package `github.com/ikigenba/ikigenba/appkit/page` and `telemetry` is the package `github.com/ikigenba/ikigenba/appkit/telemetry`, and `Telemetry` is the writer through which the `*Server` records every event.
- R-KWD9-PBZI: The `internal/server` package MUST export `type Server` and `func New(cfg Config) *Server`.
- R-SO0Q-POPR: `IKIGENBA_PUBLIC_URL`, auth's own public origin, MUST be an optional environment variable of auth's serve path: `Run` reads it through `LookupEnv` when `Args` is empty, and its being unset never stops `Run` from serving.
- R-SP8N-3GGG: `IKIGENBA_CALLBACK_URL`, the origin Google returns a browser to, MUST be an optional environment variable of auth's serve path: `Run` reads it through `LookupEnv` when `Args` is empty, and its being unset never stops `Run` from serving.
- R-GDKV-4MH2: auth's design defines an **origin value** as a string that is exactly `http://` or `https://`, followed by a non-empty **host** made only of ASCII lowercase letters, ASCII digits, `-`, and `.`, which neither begins nor ends with `.` and contains no `..`, optionally followed by `:` and a **port** of one or more ASCII digits, the first of which is not `0`, denoting an integer from 1 through 65535 that is not `80` when the value begins `http://` and not `443` when it begins `https://`, and nothing else; its **scheme** is the text before its `://`; so that `http://auth.wip.localhost:7400`, `http://localhost:7400`, `https://localhost:80`, `http://localhost:443`, `http://localhost:1`, and `http://localhost:65535` are origin values and the empty string, `http://localhost:7400/` (a trailing `/`), a value with a path, a query, a fragment, a user part, whitespace, an empty host, a `:` with no digits or with non-digits after it, a scheme other than lowercase `http` or `https`, `http://auth.green.example.:7400` (a trailing `.`), `http://.wip.localhost:7400` (a leading `.`), `http://auth..localhost:7400` (an empty label), `http://Auth.wip.localhost:7400` (an uppercase letter), `http://localhost:07400` (a leading zero), `http://localhost:0`, `http://localhost:65536`, `http://localhost:80`, and `https://localhost:443` (the scheme's default port) is not; every requirement of auth's design that names an origin value MUST denote that.
- R-T3VF-OPCS: `Run` MUST NOT call `LookupEnv` with the key `IKIGENBA_SANDBOX`.
- R-MALT-945W: `Serve` MUST accept connections on `ln` and answer every request received on them over HTTP/1.1 with the response `h` produces for that request, and MUST NOT return while `ctx` is not done and serving has not failed.
- R-MBTP-MVWL: When `ctx` is done, `Serve` MUST stop accepting connections and close `ln`, and MUST close every connection that carries no request.
- R-MD1M-0NNA: When `ctx` is done, `Serve` MUST let every call to `h.ServeHTTP` already in progress continue and MUST deliver the complete response of every such call that returns within `drain` after `ctx` was done; when every such call has returned and its response has been delivered before `drain` has elapsed, `Serve` MUST return nil without waiting for `drain` to elapse.
- R-ME9I-EFDZ: When `n` calls to `h.ServeHTTP`, `n` at least 1, are still in progress once `drain` has elapsed after `ctx` was done, `Serve` MUST close the connections those requests arrived on without writing the rest of their responses and MUST return a `*DrainError` whose `Unfinished` is `n`, without waiting for those calls to return.
- R-MFHE-S74O: `(*DrainError).Error` MUST return exactly `"stopped with 1 request unfinished"` when `e.Unfinished` is 1, and exactly `"stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"` for every other value of `e.Unfinished`.
- R-MGPB-5YVD: When serving fails while `ctx` is not done, `Serve` MUST return a non-nil error, and `Serve` MUST NOT return nil for any reason other than `ctx` being done.
- R-MJ53-XICR: `Serve` MUST write nothing to the `log` package's default logger and nothing to any process stream, whatever `ln` and `h` do: the diagnostics `net/http`'s server writes through its `ErrorLog`, such as an `Accept` error it retries or a handler that panics, MUST be discarded, so that a test which points the `log` package's output at a buffer and drives `Serve` with a listener whose `Accept` returns a temporary error and a handler that panics finds the buffer empty when `Serve` returns.
- R-GESR-IE7R: When `Args` is empty, `Run` MUST decide whether each of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `WORKSPACE_DOMAIN` is set before it decides whether `DRAIN_SECONDS` is acceptable, MUST decide whether `DRAIN_SECONDS` is acceptable before it decides whether `IKIGENBA_PUBLIC_URL` is unset or an origin value (R-GDKV-4MH2), MUST decide that of `IKIGENBA_PUBLIC_URL` before it decides whether `IKIGENBA_CALLBACK_URL` is unset or an origin value, and MUST decide that of `IKIGENBA_CALLBACK_URL` before it calls `LookupEnv` for `LISTEN_PID` or `LISTEN_FDS`, so that when several of these are wrong the diagnostic `Run` writes is the one for the earliest in that order.
- R-7WK6-E33F: When `Args` is empty and one of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, taken in that order, is the first for which `LookupEnv` returns `false` or the empty string, `Run` MUST write exactly `"auth: " + name + " is not set\n"` to `Stderr` for that `name`, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, create, remove or change nothing under `p.Dir` — so that a `p.Dir` that names an empty directory is still empty when `Run` returns — send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-MMST-2TKU: `Run` MUST accept a non-empty `DRAIN_SECONDS` value if and only if it consists of one or more ASCII decimal digits, the first of which is not `0`, and no other characters, whatever the magnitude of the integer those digits denote, so that `0`, `-1`, `2.5`, `5s`, `05`, ` 5`, and `abc` are refused and no value is refused for being large.
- R-7XS2-RUU4: When `Args` is empty, the three Google settings are set, and `LookupEnv("DRAIN_SECONDS")` returns `true` with a non-empty value `v` that `Run` does not accept, `Run` MUST write exactly `"auth: DRAIN_SECONDS is '" + v + "', not a positive whole number of seconds\n"` to `Stderr` with `v` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, create, remove or change nothing under `p.Dir` — so that a `p.Dir` that names an empty directory is still empty when `Run` returns — send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-7YZZ-5MKT: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, and `LookupEnv("IKIGENBA_PUBLIC_URL")` returns `true` with a value `v` that is not an origin value (R-GDKV-4MH2), the empty string included, `Run` MUST write exactly `"auth: IKIGENBA_PUBLIC_URL is '" + v + "', not an origin\n"` to `Stderr` with `v` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, create, remove or change nothing under `p.Dir` — so that a `p.Dir` that names an empty directory is still empty when `Run` returns — send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-807V-JEBI: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, `IKIGENBA_PUBLIC_URL` is unset or an origin value (R-GDKV-4MH2), and `LookupEnv("IKIGENBA_CALLBACK_URL")` returns `true` with a value `v` that is not an origin value, the empty string included, `Run` MUST write exactly `"auth: IKIGENBA_CALLBACK_URL is '" + v + "', not an origin\n"` to `Stderr` with `v` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, create, remove or change nothing under `p.Dir` — so that a `p.Dir` that names an empty directory is still empty when `Run` returns — send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-MP8L-UD28: The `drain` `Run` passes to `server.Serve` MUST be `5 * time.Second` when `LookupEnv("DRAIN_SECONDS")` returns `false` or the empty string, and otherwise, for the accepted value denoting the integer `n`, MUST be `n` seconds when `n` seconds is at most the largest `time.Duration` and the largest `time.Duration` when it is not.
- R-MQGI-84SX: `Run` MUST treat a socket as passed in if and only if `LookupEnv("LISTEN_PID")` returns `true` with a value equal to `strconv.Itoa(p.Pid)` and `LookupEnv("LISTEN_FDS")` returns `true` with a value of one or more ASCII decimal digits and no other characters denoting an integer of at least 1, that integer being the number of sockets passed in.
- R-81FR-X627: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, `IKIGENBA_PUBLIC_URL` and `IKIGENBA_CALLBACK_URL` are each unset or an origin value (R-GDKV-4MH2), and no socket is passed in, `Run` MUST write exactly `"auth: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"` to `Stderr`, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, create, remove or change nothing under `p.Dir` — so that a `p.Dir` that names an empty directory is still empty when `Run` returns — send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-82NO-AXSW: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, `IKIGENBA_PUBLIC_URL` and `IKIGENBA_CALLBACK_URL` are each unset or an origin value (R-GDKV-4MH2), and more than one socket is passed in, `Run` MUST write exactly `"auth: " + v + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"` to `Stderr` with `v` the value of `LISTEN_FDS` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, create, remove or change nothing under `p.Dir` — so that a `p.Dir` that names an empty directory is still empty when `Run` returns — send nothing to a notification socket, and accept no connection on any listener, and return `2`.
- R-GM45-T0NX: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, `IKIGENBA_PUBLIC_URL` and `IKIGENBA_CALLBACK_URL` are each unset or an origin value (R-GDKV-4MH2), and exactly one socket is passed in, `Run` MUST call `Unsetenv`, when it is not nil, once with each of `LISTEN_PID`, `LISTEN_FDS`, and `LISTEN_FDNAMES` before it returns, and MUST NOT call it with any other key.
- R-GNC2-6SEM: When `Args` is empty, the three Google settings are set, `DRAIN_SECONDS` is unset, empty, or accepted, `IKIGENBA_PUBLIC_URL` and `IKIGENBA_CALLBACK_URL` are each unset or an origin value (R-GDKV-4MH2), and exactly one socket is passed in, `Run` MUST take file descriptor 3, and no other descriptor, as its listener, whatever kind of listening socket it is: when `Inherit` is not nil, the listener returned by `Inherit(3)`, `Inherit` being called exactly once and with no argument other than 3; when `Inherit` is nil, the listening socket the process holds as file descriptor 3; so that a request sent to that socket's address is answered by `Run`.
- R-FO7L-15H1: Once `Run` has taken a listener, it MUST close that listener before it returns, whether it returns after serving or because a later step failed.
- R-83VK-OPJL: When taking file descriptor 3 as a listener fails with an error `err`, `Run` MUST write exactly `"auth: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, create, remove or change nothing under `p.Dir` — so that a `p.Dir` that names an empty directory is still empty when `Run` returns — send nothing to a notification socket, and return `1`.
- R-SYN4-TBT1: When `Run` has taken file descriptor 3 as a listener and, while `ctx` is not done, `db.Open` (package `github.com/ikigenba/ikigenba/appkit/db`) returns a non-nil error `err` for a `db.Config` whose `Path` is the database path (R-7GPH-F2GE), whose `Migrations` is `auth.Migrations()`, whose `Now` is `p.Now`, whose `Service` is `auth` and whose `Stderr` is `p.Stderr`, `Run` MUST write exactly `"auth: cannot open database state/auth.db: " + r + "\n"` to `Stderr`, where `r` is `err.Error()` with every newline character replaced by one space, whatever `p.Dir` holds, write nothing else to `Stderr` and nothing to `Stdout`, send nothing to a notification socket, accept no connection on the listener it took, make no call to `p.Banner`, and return `1`; so that a `p.Dir` holding a regular file named `state` is refused with that line, and so is a `p.Dir` whose `state/auth.db` is a regular file that is not a SQLite database.
- R-LO9G-FT7B: When `p.Dir` holds a database file `state/auth.db` that a `db.Open` call with `auth.Migrations()` created and whose handle has been closed, and to whose `schema_migrations`, through `DB.Write` on a handle on it, a row was then added recording as applied the version one greater than the greatest version among the files of `auth.Migrations()`, as a newer auth's migration leaves it, `Run` MUST start and serve as it does over a database that records no such version, sending `READY=1` as R-T3IQ-CERT requires and answering requests on the listener it took, so that a `GET /check` carrying no `Authorization` header and the `ikigenba_session` cookie of a session live at `p.Now` that a store over that database held before `Run` was called is answered `200`; and before it sends `READY=1`, `Run` MUST have written to `Stderr`, in a single call to `Stderr.Write`, exactly the bytes that appkit's `db.Open`, called before `Run` with a `db.Config` whose `Path` names that file, whose `Migrations` is `auth.Migrations()`, whose `Service` is `auth` and whose `Stderr` is a writer of the test's own, writes to that writer, and MUST write nothing else to `Stderr` before it sends `READY=1`; and appkit's `db.Status`, called with a `db.Config` whose `Path` is that file's path and whose `Migrations` is `auth.Migrations()`, MUST write after `Run` has sent `READY=1` exactly the bytes it wrote for that file before `Run` was called, so that starting over such a database applies no migration and changes no recorded version.
- R-GHCN-OPOF: When `Run` sends `READY=1` with a `p.Now` that returns the same time on every call and a `p.Dir` that holds no entry named `state`, holds an empty directory named `state`, or holds a database file `state/auth.db` that a `db.Open` call with `auth.Migrations()` created and whose handle has been closed and from which, through `DB.Write` on a handle on it, the tables `schema_migrations`, `clients`, and `auth_codes` and the columns `kind` and `host` of `tokens` were then dropped, as from a database an auth that carried no migrations wrote, `p.Dir` MUST by then hold a regular file `state/auth.db` for which appkit's `db.Status`, called with a `db.Config` whose `Path` is that file's path and whose `Migrations` is `auth.Migrations()`, writes exactly the bytes it writes, with those same `Migrations`, for a database that a `db.Open` call with a `db.Config` whose `Path` names a file in another empty directory, whose `Migrations` is `auth.Migrations()` and whose `Now` is `p.Now` has created and whose handle has been closed; so that the database `Run` serves is the one at the database path, brought up to date with `auth.Migrations()` and stamped with `p.Now`; and when `p.Dir` instead holds a database file `state/auth.db` that a `db.Open` call with `auth.Migrations()` created and whose handle has been closed, `db.Status` called as above MUST write after `Run` has sent `READY=1` exactly the bytes it wrote for that file before `Run` was called, so that a restart applies nothing to a database already up to date.
- R-T2AT-YN14: When the `db.Open` call R-SYN4-TBT1 describes returns a non-nil `*db.DB` `h`, `Run` MUST build its store with `store.New(h, p.Rand)`, construct the Google client via `google.NewClient` with the values of `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `WORKSPACE_DOMAIN`, and `p.OIDCIssuer`, construct the server via `server.New` with a `server.Config` whose `Store` is that `*store.Store`, `Google` is that client, `Now` is `p.Now`, `Rand` is `p.Rand`, `Telemetry` is the one `*telemetry.Writer` through which `Run` records every event (R-BEG2-DPNX), `WorkspaceDomain` is the value of `WORKSPACE_DOMAIN`, `PublicURL` is the value of `IKIGENBA_PUBLIC_URL` and the empty string when it is unset, `CallbackURL` is the value of `IKIGENBA_CALLBACK_URL` and the empty string when it is unset, and `Banner` is `p.Banner`, and, unless notifying fails, call `server.Serve` exactly once with `Run`'s own `ctx`, the listener it took, that `*server.Server` as the handler, and the `drain` of R-MP8L-UD28, without contacting the issuer `p.OIDCIssuer` names before a request needs it. This wiring MUST be observable through the served listener: with `p.OIDCIssuer` set to a reachable loopback OIDC issuer, `p.Rand` a deterministic reader, and `p.Now` a fixed clock, a `GET /login/google` served by `Run` MUST return `302` whose `Location` is addressed to that issuer's discovered `authorization_endpoint` and whose `state` and `code_challenge` are derived from `p.Rand`; the sign-in handler's own contract is D05's, not this requirement's.
- R-GOJY-KK5B: When `Run` serves with `IKIGENBA_CALLBACK_URL` set to an origin value `o` (R-GDKV-4MH2) and `p.OIDCIssuer` set to a reachable loopback OIDC issuer, a `GET /login/google` served by `Run` MUST respond `302` whose `Location` carries a `redirect_uri` query parameter whose decoded value is `o` followed by `/login/google/callback`, whatever `Host` the request carries.
- R-GPRU-YBW0: When `Run` serves with `IKIGENBA_PUBLIC_URL` set to an origin value `o` (R-GDKV-4MH2), a `POST /logout` served by `Run` whose `Host` is `o` with its scheme and `://` removed, which carries the `SessionCookieName` cookie of a live session and exactly one `Origin` header field whose value is `o`, MUST respond `302`.
- R-T4RU-LM2C: When no request reaches the handler `Run` passes to `server.Serve`, `Run` MUST return having made no call to `p.Banner`, whatever it returns.
- R-T3IQ-CERT: When the `db.Open` call R-SYN4-TBT1 describes has returned a non-nil `*db.DB` and `LookupEnv("NOTIFY_SOCKET")` returns `true` with a non-empty value `a`, `Run` MUST, after that `db.Open` call returns and before calling `server.Serve`, send exactly one datagram, whose content is exactly `READY=1`, to the Unix datagram socket whose address is `a`, an `a` beginning with `@` naming a socket in the abstract namespace; `Run` MUST send nothing to a notification socket on any other occasion.
- R-N3VE-FLYK: When `LookupEnv("NOTIFY_SOCKET")` returns `false` or the empty string, `Run` MUST send no datagram and MUST serve as it otherwise would.
- R-N53A-TDP9: When sending `READY=1` fails with an error `err`, `Run` MUST write exactly `"auth: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, accept no connection on the listener it took, not call `server.Serve`, and return `1`.
- R-N6B7-75FY: A `Run` that calls `server.Serve` and to which `Serve` returns nil MUST return `0`.
- R-5RXL-FWKQ: A `Run` that calls `server.Serve` MUST write nothing to `Stdout`, and MUST write nothing to `Stderr` other than lines each of which begins `auth: undelivered event: ` or `auth: malformed event: ` and holds one event it recorded, the one line appkit's `db.Open` writes to the `Stderr` R-SYN4-TBT1 gives it when the database records a version `auth.Migrations()` does not hold, and, when `Serve` returns a non-nil error, the one line R-N8QZ-YOXC states.
- R-N8QZ-YOXC: When `server.Serve` returns a non-nil error to `Run`, `Run` MUST write to `Stderr` exactly `auth: `, that error's `Error()` text, and a newline, MUST write nothing to `Stdout`, and MUST return `1`.
- R-2J42-BC4C: When the handler `Run` passes to `server.Serve` answers a request for which appkit's `telemetry.Middleware` records a status (appkit D14, R-3HP0-MB1R) from `500` through `599`, `Run` MUST write nothing to `Stderr` for that request, and the `request.finished` it records for that request MUST carry that status.
- R-BEG2-DPNX: When `Run` serves, every event it records MUST be handed to `p.Sink`'s `Deliver`, unless it is written to `Stderr` as an undelivered event, as an `Event` whose `Service` is `auth` and whose `Time` is a value `p.Now` returned while the event was recorded, converted to UTC and truncated to a whole microsecond.
- R-2MRR-GNCF: When `Run` serves and `p.Sink`'s `Deliver` returns, for every event, an error for which `errors.Is(err, telemetry.ErrRejected)` is true, `Run` MUST write to `Stderr`, for each event it records, exactly one line, in a single call to `Stderr.Write`: `auth: undelivered event: `, the bytes the event's `MarshalJSON` returns, and a newline, those lines coming in the order the events were recorded, except that where the line of an event appkit's writer does not queue — one recorded while its queue is full or after `Writer.Shutdown` began (appkit D12, R-W9YY-0F39 and R-WDMN-5QBC) — falls among them is not fixed; and `Run` MUST otherwise answer every request and return exactly as it does when every delivery succeeds.
- R-BGVV-595B: When `Run` serves with a `p.Rand` that yields only zero bytes, every event `Run` records for a request that carries no `X-Request-Id` header MUST carry the request id `00000000000000000000000000000000`, 32 `0`s.
- R-T4QM-Q6II: When `Run` calls `server.Serve`, the first event it records MUST be one named `service.started`, with an empty request id, an empty user, and attributes exactly `version`, whose value is the value of `Version` from `internal/version`, recorded only after `Run` has sent the `READY=1` datagram R-T3IQ-CERT requires, when it requires one, and before `Run` records any event for a request.
- R-8B6Y-ZBZR: When `Run` returns without having called `server.Serve` — whatever `Args` holds, and whether it returns after a command, a usage error, a failure to take file descriptor 3, a failure of `db.Open`, or a failure to send `READY=1` — it MUST NOT have called `p.Sink`'s `Deliver`, and MUST NOT have written to `Stderr` any line beginning `auth: undelivered event: ` or `auth: malformed event: `.
- R-BKJK-AKDE: When `ctx` is done and `server.Serve` returns nil to `Run`, `Run` MUST record an event named `service.stopping`, with an empty request id, an empty user, and attributes exactly `reason`, whose value is the `Error()` text of `context.Cause(ctx)`, after every other event it records, the `request.finished` of every request served included; and `Run` MUST NOT return before every event it recorded has been handed to `p.Sink`'s `Deliver` and that call has returned, or the event has been written to `Stderr` as an undelivered event.
- R-2P7K-86TT: Once `ctx` is done and `server.Serve` has returned nil, `Run` MUST NOT wait for a `Deliver` call to return, or for an event to be delivered, beyond the moment `drain` (R-MP8L-UD28) has elapsed since `ctx` was done: given a `p.Sink` whose `Deliver` returns only once its context is done, `Run` MUST return `0` once that moment has passed, having written to `Stderr`, in the form R-2MRR-GNCF states, the line of every event it recorded and did not deliver, `service.stopping` included.
- R-KON7-OYQU: When `server.Serve` returns a `*server.DrainError` to `Run`, and `p.Sink`'s `Deliver` answers every call whose context is done with a non-nil error and every other call with a nil error, that sink MUST NOT have answered nil for `service.stopping` or for any event recorded after `Serve` returned, so that a sink that fails on a done context never stores them; `Run` MUST write to `Stderr`, in the form R-2MRR-GNCF states and in the order recorded, except that where the line of an event appkit's writer does not queue (appkit D12, R-W9YY-0F39 and R-WDMN-5QBC) falls among them is not fixed, the line of every event it recorded before `Serve` returned and the sink did not answer nil for, then the line of `service.stopping`, with its `reason` as R-BKJK-AKDE states, and MUST write the line R-N8QZ-YOXC states after the line of `service.stopping`; where the line of an event recorded after `Serve` returned falls among these lines is not fixed.
- R-BO79-FVLH: When `server.Serve` returns a non-nil error to `Run` while `ctx` is not done, `Run` MUST record an event named `service.stopping`, with an empty request id, an empty user, and attributes exactly `reason`, whose value is `failed`, after every other event it records, and MUST write the line R-N8QZ-YOXC states only after every event it recorded has been handed to `p.Sink`'s `Deliver` and that call has returned, or the event has been written to `Stderr` as an undelivered event, waiting for delivery no longer than `drain` after `Serve` returned.
- R-5T5H-TOBF: `Run` MUST NOT let two calls to `Stderr.Write` be in progress at the same time, those that write the lines R-2MRR-GNCF and R-5RXL-FWKQ state included, whatever goroutine makes them, so that a `Stderr` that is not safe for concurrent use is never written concurrently.
- R-NI60-D0O6: When `Inherit` is nil and file descriptor 3 is a listening socket that another process also holds, a connection made, after `Run` returns, to the address that socket was listening on when `Run` took it MUST still be accepted into that socket's queue for the other process.
- R-GCH2-5MPN: The `*server.Server` returned by `server.New` MUST serve the HTTP routes whose contracts D05, D06, D07, D08, and D09 define.
- R-2LJV-2VLQ: The `*Server` returned by `New` MUST mint every random value the `*Server` mints itself (including the PKCE verifier D05 requires of `GET /login/google`) by reading `cfg.Rand`, other than the request id appkit's `telemetry.Middleware` mints for a request, which comes from the random source of `cfg.Telemetry` (appkit D14, R-DWO7-CX19) or, when that read fails, as appkit's R-DRSL-TU2H states, and MUST NOT read a global random source, other than for such a request id after such a failed read, or write to a global output stream or through the `log` package's default logger; given a `Config` whose `Rand` is a deterministic reader, the values minted from it are a function of the bytes that reader yields.
- R-ORBB-5N5F: For every request the `*Server` returned by `server.New` serves, whatever its method and path — an appkit path, a route D05, D06, or D07 defines, and a path no route defines included — it MUST record through `cfg.Telemetry` exactly one event named `request.started`, before every other event it records for that request, whose attributes are exactly `method`, the request's method, and `path`, its `URL.Path`; and exactly one event named `request.finished`, after every other event it records for that request, whose attributes are exactly `status`, the status appkit's `telemetry.Middleware` records for the request (appkit D14, R-3HP0-MB1R), `duration_us`, `request_bytes`, and `response_bytes`; both MUST carry as their request id the request's first `X-Request-Id` value when that is present and not empty, and otherwise one id of 32 lowercase hexadecimal digits that the `*Server` minted for that request, and as their user the request's first `X-User-Id` value, empty when absent.
- R-OTR3-X6MT: The `response_bytes` attribute of the `request.finished` event R-ORBB-5N5F requires MUST be, for a request whose method is not `HEAD`, the number of bytes of body in the `*Server`'s answer to that request; its `request_bytes` attribute MUST be the number of bytes of the request's body the `*Server` read before answering, as appkit's `telemetry.Middleware` counts them (appkit D14): 0 for a request answered without reading its body, a `GET` of `/`, `/check`, or `/me` carrying a non-empty body included, whatever its `Content-Length`, and the whole body's length for a `POST /tokens` R-N5RR-K5GT requires to create a token.
- R-BD85-ZXX8: Every event other than `request.started` and `request.finished` that the `*Server` records while answering a request MUST carry the request id of that request's `request.started`, and MUST be recorded after that `request.started` and before that request's `request.finished`.
- R-GDOY-JEGC: When a store operation the `*Server` calls while handling a request on any route D05, D06, D07, or D09 defines returns an error that does not satisfy `errors.Is(err, store.ErrNotFound)`, the `*Server` MUST answer that request with status `500`, `Content-Type: text/plain; charset=utf-8`, a body that is a single line of plain text, and neither `HeaderUserID` nor `HeaderUserEmail` set, whatever response another requirement states for that request, except that a `GET /login/google` already being answered `502` because the endpoints of the issuer `cfg.Google` names could not be discovered (D05) stays `502` when the `ConsumeLoginState` it makes to discard its login state fails.
- R-TQGG-VYEV: The `*Server` returned by `server.New` MUST answer with status `404`, whatever its method, every request whose `URL.Path` consists of `/assets/` followed by a non-empty name that contains no `/` and is neither `.` nor `..`, `/assets/theme.css` included.
