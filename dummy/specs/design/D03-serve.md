# D03-serve

The serve path: what happens between `cli.Run` being called with no
arguments and the process being gone. `D01-layout-and-run-seam` declares the
names this design behaves through: `cli.Process` with its `Pid`, `Unsetenv`,
`Inherit`, `Banner`, `MCP`, `Telemetry` and `Rand`, `cli.Run`, the exit codes,
`server.Serve` and `server.DrainError`. `D02-cli` decides that an empty `Args`
means serve and that nothing else touches the environment; `D04-panel`
decides what the handler answers and what each request records in the
trail. This design says how `Run`
reads the drain deadline, takes the socket the host passes in, tells systemd
it is ready, serves and stops, and how `Serve` treats the listener it
is handed, in the shapes the serve stories show: a clean start, a stop that
drains, a stop whose drain runs out, and the three ways a start is refused.

## The terms every app serves on

dummy is the platform's reference app, and these terms are the ones every app
copies. An app serves only on a listening socket it inherits and never opens
one of its own. On a host, opsctl publishes `ikigenba-<app>.socket`, which
holds the Unix stream socket `/run/ikigenba/<app>.sock` (owner `ikigenba`,
group `nginx`, mode `0660`), beside `ikigenba-<app>.service`, a
`Type=notify` service that runs the app's binary with no arguments as the
`ikigenba` user; nginx proxies to `http://unix:/run/ikigenba/<app>.sock:`. A
deploy restarts the service alone, so the socket, and the connections queued
on it, outlive every restart.

The app takes the socket the way `sd_listen_fds(3)` documents: `LISTEN_PID`
is the app's own process id, `LISTEN_FDS` counts the sockets passed, and the
first is file descriptor 3 (`SD_LISTEN_FDS_START`). It removes `LISTEN_PID`,
`LISTEN_FDS` and `LISTEN_FDNAMES` from its environment once it has taken the
socket, so nothing it might start inherits a claim meant for it. Once it is
ready it sends the datagram `READY=1` to the Unix datagram socket named by
`NOTIFY_SOCKET`, as `sd_notify(3)` documents, a name beginning with `@` being
in the abstract namespace. On
`SIGTERM` or `SIGINT` it stops accepting, lets the requests it already
accepted finish for at most `DRAIN_SECONDS`, then cuts off whatever is left,
says how many it cut off, and exits 1. It closes its own copy of the socket
and never removes the socket's path. `DRAIN_SECONDS` and the service unit's
`TimeoutStopSec` are space-wide integer-second settings owned by opsctl
(defaults 5 and 10): opsctl writes `DRAIN_SECONDS` into every app's
`etc/env`, and no manifest sets either. An app reads `DRAIN_SECONDS` as a
positive whole number, 5 when it is unset or empty, and sets no upper limit
of its own. The environment opsctl gives an app also carries
`IKIGENBA_SERVICES`, the path of the host's services file, normally
`/var/lib/ikigenba/services.json`; on a laptop it is normally unset. An app reads
it once, at start, through appkit's `page.New` and `mcp.NewServer`, and never
fails to start over it: unset, empty, or naming a file that is missing or
unreadable, the app starts, serves, and says nothing about it; its pages then
carry no launcher and its MCP endpoint gives no instructions
(`D01-layout-and-run-seam` explains why that read happens in `main` and cannot
fail). The telemetry writer's socket sink reads the variable again for every
event it sends, to find the services-file entry named `telemetry`; a
process's environment does not change, so it names the same file, and a file
without that entry, or none at all, only sends the event to standard error
instead.

The socket is the app's only way in, and every app runs as `ikigenba`, so the
suite and nginx can reach it and nothing else on the host can. The suite is a
closed system: an app trusts `X-User-Id`, `X-User-Email` and `X-Request-Id`
as nginx sets them, and trusts a sibling to have forwarded them. An app that
calls a sibling while serving a request calls the sibling's socket directly
and copies those three headers from the request it is serving; appkit's
`telemetry.SiblingClient` does that copying, and records the call, for every
app that calls a sibling through it. dummy calls no sibling, so it states no
requirement of its own about the rule. Work no live request started is outside
these terms.

An app keeps a trail: it records what it does as events sent to the
platform's telemetry service, through one appkit `telemetry.Writer`. Standard
error has exactly two uses, a condition the app cannot go on from and an
event it could not deliver to telemetry. A handled failure, a 500 included,
is not trouble: it is recorded by the request's `request.finished` event with
its status and earns no line. So while telemetry takes every event a running
app writes nothing to either stream, and under systemd the journal holds only
trouble.

## Taking the socket

With `Args` empty, `Run` reads `DRAIN_SECONDS` first, so a bad value is found
at start rather than at the moment dummy is asked to stop, and when both the
drain and the socket are wrong the drain is the one reported. The value is
spelled the way opsctl writes an integer: one or more ASCII digits, no sign,
no leading zero, no unit, no whitespace. There is no upper limit, so a value
too large for a `time.Duration` is the longest drain a `time.Duration` holds
rather than an error or a wrap to a negative duration. A value that is not
acceptable is a usage error, quoted back verbatim.

Then the socket. A socket is passed in only when `LISTEN_PID` is exactly the
decimal spelling of `Process.Pid` and `LISTEN_FDS` is a count of at least one.
Anything else — `LISTEN_FDS` unset, empty, `0` or not a number, `LISTEN_PID`
unset or naming another process — is no socket passed in, and a count above
one is a unit dummy will not guess about. Both are usage errors with the
detail line saying how to run dummy correctly. None of the refused starts
takes the descriptor, unsets a variable, serves anything or tells systemd
anything; a test sees that as `Inherit` never called, no `Unsetenv` call, no
datagram, and no `Accept` on any listener.

With exactly one socket passed in, `Run` removes the three `LISTEN_*`
variables through `Unsetenv` and takes descriptor 3 as its listener: through
`Process.Inherit` when a test supplies one, otherwise with `net.FileListener`
over the real descriptor, whatever kind of listening socket it is.
The Go `net` documentation of `UnixListener.SetUnlinkOnClose` states that a
listener made by `FileListener` does not remove the socket file when it is
closed, and `FileListener` documents that closing the listener does not
affect the file it was made from; closing is all dummy ever does to the
socket, so the path systemd created stays, and systemd's own copy of the
socket keeps queueing connections for the next dummy. If the descriptor
cannot be made into a listener — it is not a listening stream socket, say —
that is trouble on the host rather than a caller's typo: `dummy: ` and the
error, exit 1.

With the listener in hand `Run` tells systemd it is ready and serves on that
listener until `ctx` is done. What a client of a serving `Run` sees is one
widget set, starting from the three fixture widgets every time `Run` starts,
behind both the panel and the MCP tools: a widget created through the form is
in the next `list_widgets`, and one created through `create_widget` is the
last row of the next table and of the next `list_widgets`. That is the whole
of "the widget set is created once, at process start, and dies with the
process" as anything outside dummy can tell it; how many stores or handlers
`Run` builds to get there is not contract. The banner source and the MCP
server are the ones in `Process` (`D01-layout-and-run-seam` states their
roles). `Run` never calls the banner source or the MCP server itself: only
the handler does, the banner source once for each page it draws with the
banner (`D04-panel`) and the server for each request to `/mcp`, so the
services file can neither delay nor fail a start, and a start that is refused
never reads it. Serving registers dummy's tools on `Process.MCP`
(`D04-panel`), which is why the server `main` makes is handed to `Run` fresh. The socket has been listening since systemd made it, so from that
moment every connection is queued and will be answered: ready is true before
the first `Accept`, and sending it first means a failure to send is reported
without anything having been served. That failure is trouble — under
`Type=notify` systemd would otherwise wait out its start timeout — so it is
`dummy: ` and the error, exit 1, and no connection is ever accepted. A test learns
that dummy is up the way systemd does, by binding a datagram socket, naming it
in `NOTIFY_SOCKET`, and waiting for `READY=1`.

## Serving and stopping

`Serve` owns `ln` from the call. It serves HTTP/1.1 with `h` and blocks:
plain HTTP/1.1, no h2c, no TLS. `net/http` answers an HTTP/1.0 request with
an HTTP/1.0 status line, and the HTTP/1.1 requirement does not forbid that.
On a space the visitor's HTTP/2 and TLS are nginx's, terminated before dummy
is reached.

When `ctx` is done, which is how `SIGTERM` or `SIGINT` reaches it through
`main`, `Serve` closes the listener so nothing new is accepted — new
connections wait in the socket's queue for the next dummy — drops connections
that carry no request, and lets every request already being handled finish
and receive its whole response, for at most `drain`. If they all finish, it
returns nil as soon as they have. If some are still being handled when
`drain` runs out, it closes their connections without the rest of their
responses and returns a `*DrainError` counting them, without waiting for the
handlers themselves to return. The count is of calls to `h.ServeHTTP` that had
begun and not returned; a connection still sending its request headers is not
a request yet, and is closed at once like an idle one. A `Run` whose request is cut off this way says so:
`dummy: stopped with <n> requests unfinished` (`1 request` when `<n>` is 1), exit 1.
`Serve` also takes a `stop` function, which it calls once, when the drain is
over — as soon as every request has finished, or at the deadline before it
cuts anything off — with a context that ends at the deadline; `Serve` returns
only after `stop` does. That is the one moment that is after every finished
request and before any cut-off request can finish, which is what `Run` needs
to close its trail (below). `internal/server` still knows nothing of
telemetry: `stop` is just a function. The drain is bounded
so that dummy always exits before the service unit's stop timeout and is
never killed by systemd mid-write; keeping `DRAIN_SECONDS` below that timeout
is opsctl's to enforce. `Serve`'s behavior for a drain that is not positive
is not contract.

The drain is stated twice because it is observed at two seams. At `Serve` a
test hands in the duration itself. At `Run` the duration exists only as
`DRAIN_SECONDS`, so the requirement is the outcome a client sees: a request
still being handled when `ctx` is done is given the drain deadline — 5
seconds for an unset or empty value, `n` seconds for an accepted `n`, and the
longest `time.Duration` for a value too large to hold, never anything
shorter — and is cut off less than a second after it, with the overrun line
and exit 1. A test holds a `POST /widgets` open and times the cut-off with
`DRAIN_SECONDS=1`, with it unset and with it empty, and with a value too large
for a `time.Duration` watches the request survive seven seconds, past the
default, and then completes it itself.

A test reaches the overrun through the real handler by sending a `POST
/widgets` whose declared `Content-Length` exceeds the bytes it sends: the
handler is then blocked reading the body, which is a request being handled.
At the `Serve` level a test uses a handler that blocks on a channel and a
drain of milliseconds.

`Serve` is silent in its own right. Go's `net/http` documents at
`Server.ErrorLog` that, when that field is nil, errors accepting connections
and unexpected behaviour from handlers are logged through the `log` package's
standard logger, which writes to the real standard error and never passes
through `Process.Stderr`. So `Serve` gives the server an `ErrorLog` that goes
nowhere; the silence requirement fixes the outcome, not the constructor. It is
tested as `D01` tests its stream rule: a test points the `log` package at a
buffer, drives `Serve` with a listener whose first `Accept` returns a
temporary error and a handler that panics, and finds the buffer empty.

## The trail of a run

`Process.Telemetry` is the writer every event of the run goes through; `main`
builds it, because the MCP server it also hands to `Run` must be built over
the same writer, and a test builds its own over appkit's capturing sink.
`Run` does three things with it, all on the serve path. Once it holds the
listener and has told systemd it is ready, it calls `Ready`, which records
`service.started` with the version the writer was built with, so that event
comes before anything a request records. When `ctx` is done it lets the
drain run and then, through `Serve`'s `stop`, calls `Shutdown` with the reason
and the drain's own context: the reason is the text of `context.Cause(ctx)`,
which `main` sets to the name of the signal it received, `SIGTERM` or
`SIGINT`. Every request that finished has by then recorded its
`request.finished`, so `service.stopping` is the last event out, and
`Shutdown` delivers what is queued within the drain deadline and writes what
is left to the writer's standard error when the deadline comes. A refused
start, a command, and a failure to send `READY=1` touch the writer not at
all, so they record nothing, and `Telemetry` may be nil for them.

When the drain runs out, its deadline has passed, so `Shutdown` has no time
left: `service.stopping`, and anything else not yet delivered, goes to the
writer's standard error as an `undelivered event` line at once. Because
`stop` runs before `Serve` cuts the remaining requests off, a cut-off request
cannot finish while the writer still delivers: its `request.finished`, if it
is recorded at all before the process exits, arrives after `Shutdown` began
and goes to standard error too, so telemetry never receives a finish for a
request that did not finish. Only after `Shutdown` has returned does `Run`
write its own `dummy: stopped with <n> requests unfinished` line, so on the
process's standard error that line follows the `service.stopping` line and
every other line for an event recorded before the deadline; a cut-off
request's late `request.finished` line, when there is one, may fall before or
after it. A test sees the order by handing `Process.Stderr` and the writer's
`Stderr` one writer that serialises its callers. That telemetry receives no
finish for a cut-off request follows from one order `Run` keeps: the
`service.stopping` line, which `Shutdown` writes before it returns when the
drain's context is already done, is written before any cut-off connection is
closed, and an event emitted after `Shutdown` began is never delivered
(appkit's writer). A test observes that order deterministically with an
`Inherit` listener whose accepted connections log their `Close` into the same
serialised log the test's stderr writer writes to.

A healthy run is silent from start to finish on `Process.Stderr` and
`Process.Stdout`: no startup message, no request log, nothing when it stops.
The lines `Run` itself writes there are the overrun line and the
`Accept`-failure line, each one write from one goroutine, so `Run` never has
two writes to `Stderr` in progress. The writer's lines go to its own
`Stderr`, which `main` points at the same standard error.

No read, write, or idle timeouts are contract. The build is nonetheless
expected to set `ReadHeaderTimeout`: the lint gate's gosec `G112` demands it
and suppression is forbidden, and it affects only a connection that has not
yet delivered a request header. The build must not set a write timeout that
could cut a response short inside the drain.

## REQUIREMENTS

- R-QLRL-RC1B: `Serve` MUST accept connections on `ln` and answer every request received on them over HTTP/1.1 with the response `h` produces for that request, and MUST NOT return while `ctx` is not done and serving has not failed.
- R-LSZH-BHHX: When `ctx` is done, `Serve` MUST stop accepting connections and close `ln`, and MUST close every connection that carries no request.
- R-KA1B-WBAE: When `ctx` is done, `Serve` MUST let every call to `h.ServeHTTP` already in progress continue and MUST deliver the complete response of every such call that returns within `drain` after `ctx` was done; when every such call has returned and its response has been delivered before `drain` has elapsed, `Serve` MUST then call `stop`, when it is not nil, and MUST return nil once that call has returned, without waiting for `drain` to elapse.
- R-KB98-A313: When `n` calls to `h.ServeHTTP`, `n` at least 1, are still in progress once `drain` has elapsed after `ctx` was done, `Serve` MUST call `stop`, when it is not nil, and only after that call has returned close the connections those requests arrived on without writing the rest of their responses, and MUST then return a `*DrainError` whose `Unfinished` is `n`, without waiting for those calls to `h.ServeHTTP` to return.
- R-KCH4-NURS: `Serve` MUST call `stop` at most once, MUST NOT call it while `ctx` is not done or when it returns because serving failed while `ctx` was not done, and MUST pass it a context that is done once `drain` has elapsed after `ctx` was done, and not before.
- R-LURX-UT17: `(*DrainError).Error` MUST return exactly `"stopped with 1 request unfinished"` when `e.Unfinished` is 1, and exactly `"stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"` for every other value of `e.Unfinished`.
- R-QO7E-IVIP: When serving fails while `ctx` is not done, `Serve` MUST return a non-nil error, and `Serve` MUST NOT return nil for any reason other than `ctx` being done.
- R-IBR4-T2PU: `Serve` MUST write nothing to the `log` package's default logger and nothing to any process stream, whatever `ln` and `h` do: the diagnostics `net/http`'s server writes through its `ErrorLog`, such as an `Accept` error it retries or a handler that panics, MUST be discarded, so that a test which points the `log` package's output at a buffer and drives `Serve` with a listener whose `Accept` returns a temporary error and a handler that panics finds the buffer empty when `Serve` returns.
- R-ONGQ-VBKW: When `Args` is empty, `Run` MUST decide whether `DRAIN_SECONDS` is acceptable before it calls `LookupEnv` for `LISTEN_PID` or `LISTEN_FDS`, so that when `DRAIN_SECONDS` is not acceptable and no socket was passed in, the `DRAIN_SECONDS` diagnostic is the one `Run` writes.
- R-P9EX-R6XE: `Run` MUST accept a non-empty `DRAIN_SECONDS` value if and only if it consists of one or more ASCII decimal digits, the first of which is not `0`, and no other characters, whatever the magnitude of the integer those digits denote, so that `0`, `-1`, `2.5`, `5s`, `05`, ` 5`, and `abc` are refused and no value is refused for being large.
- R-1XHC-TE94: When `Args` is empty and `LookupEnv("DRAIN_SECONDS")` returns `true` with a non-empty value `v` that `Run` does not accept, `Run` MUST write exactly `"dummy: DRAIN_SECONDS is '" + v + "', not a positive whole number of seconds\n"` to `Stderr` with `v` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `ExitUsage`.
- R-PE6U-PF31: When `ctx` is done while `Run` is handling a request that arrived on the listener it took, `Run` MUST NOT close that request's connection, and MUST NOT return, before the drain deadline has elapsed since `ctx` was done, unless that request's handling has ended and its complete response has been delivered, and MUST deliver the complete response of that request when its handling ends before the drain deadline has elapsed; the **drain deadline** is 5 seconds when `LookupEnv("DRAIN_SECONDS")` returns `false` or the empty string, and otherwise, for the accepted value denoting the integer `n`, `n` seconds when `n` seconds is at most the largest `time.Duration` and the largest `time.Duration` when it is not, so that no accepted value, however large, makes the drain shorter.
- R-PFER-36TQ: When `k` requests that arrived on the listener `Run` took, `k` at least 1, are still being handled once the drain deadline of R-PE6U-PF31 has elapsed since `ctx` was done, `Run` MUST, less than one second after the drain deadline has elapsed, close the connections those requests arrived on without writing the rest of their responses, write to `Stderr` exactly `dummy: `, the `Error()` text of a `*server.DrainError` whose `Unfinished` is `k`, and a newline, write nothing to `Stdout`, and return `ExitServerFailed`.
- R-PU58-9AJ7: `Run` MUST treat a socket as passed in if and only if `LookupEnv("LISTEN_PID")` returns `true` with a value equal to `strconv.Itoa(p.Pid)` and `LookupEnv("LISTEN_FDS")` returns `true` with a value of one or more ASCII decimal digits and no other characters denoting an integer of at least 1, that integer being the number of sockets passed in.
- R-JI4Z-UR8P: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and no socket is passed in, `Run` MUST write exactly `"dummy: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"` to `Stderr`, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `ExitUsage`.
- R-JJCW-8IZE: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and more than one socket is passed in, `Run` MUST write exactly `"dummy: " + v + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"` to `Stderr` with `v` the value of `LISTEN_FDS` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `ExitUsage`.
- R-QEVI-RE50: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and exactly one socket is passed in, `Run` MUST call `Unsetenv`, when it is not nil, once with each of `LISTEN_PID`, `LISTEN_FDS`, and `LISTEN_FDNAMES` before it returns, and MUST NOT call it with any other key.
- R-DPQ2-9T5Q: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and exactly one socket is passed in, `Run` MUST take file descriptor 3, and no other descriptor, as its listener: when `Inherit` is not nil, by calling `Inherit(3)` exactly once and serving on the listener it returns, and when `Inherit` is nil, by serving on the listening socket that is the process's file descriptor 3.
- R-3OW4-PGT8: When taking file descriptor 3 as a listener fails with an error `err`, `Run` MUST write exactly `"dummy: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, send nothing to a notification socket, and return `ExitServerFailed`.
- R-KDP1-1MIH: When `Run` has taken file descriptor 3 as a listener and has not failed to send `READY=1`, it MUST answer every request that arrives on that listener while `ctx` is not done, MUST NOT return while `ctx` is not done unless a call to that listener's `Accept` has returned an error that `Run` does not retry (R-ERVL-5OIC), and, once `ctx` is done and no request that arrived on that listener is being handled, MUST return `ExitSuccess` less than one second after the drain deadline of R-PE6U-PF31 has elapsed since `ctx` was done, whatever `p.Telemetry`'s sink does, and before that deadline has elapsed when that sink answers every `Deliver` call with a nil error at once.
- R-P9B9-6C49: Every `tools/call` of `list_widgets` that a `Run` answers at `/mcp` on the listener it took, before that `Run` has accepted any widget through its form (`D07-form`) or through `create_widget`, MUST return a result whose `widgets` holds the widget objects of exactly the three widgets R-EYW8-QIPA lists, in the order it lists them, whatever widgets earlier calls to `Run` in the same process accepted.
- R-PAJ5-K3UY: After a `tools/call` of `create_widget` that `Run` answers at `/mcp` on the listener it took returns a result with no `isError` member, the next `GET /widgets/table` carrying a non-empty `X-User-Id` header and no `If-None-Match` header that `Run` answers on that listener MUST show the created widget as the last row of the widgets table, as `D06-table` renders a widget's row, and the next `tools/call` of `list_widgets` that `Run` answers there MUST hold the widget object of the created widget as the last element of `widgets`.
- R-PBR1-XVLN: After `Run` accepts a widget submitted through the form (`D07-form`) in a `POST /widgets` that arrived on the listener it took, the next `tools/call` of `list_widgets` that `Run` answers at `/mcp` on that listener MUST hold the widget object of that widget as the last element of `widgets`.
- R-QFU0-CZZ9: When no request arrives on the listener `Run` took, `Run` MUST return having made no call to `p.Banner`, whatever it returns.
- R-QH1W-QRPY: When `Run` has taken file descriptor 3 as a listener and `LookupEnv("NOTIFY_SOCKET")` returns `true` with a non-empty value `a`, `Run` MUST, after taking the listener and before accepting any connection on it, send exactly one datagram, whose content is exactly `READY=1`, to the Unix datagram socket whose address is `a`, an `a` beginning with `@` naming a socket in the abstract namespace; `Run` MUST send nothing to a notification socket on any other occasion.
- R-EQYT-T2RY: When sending `READY=1` fails with an error `err`, `Run` MUST write exactly `"dummy: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, accept no connection on the listener it took, and return `ExitServerFailed`.
- R-KEWX-FE96: When `Run` has taken file descriptor 3 as a listener and has not failed to send `READY=1`, it MUST write nothing to `Stdout`, and MUST write nothing to `Stderr` other than the line R-PFER-36TQ states and the line R-ERVL-5OIC states.
- R-DOI5-W1F1: `Run` MUST write nothing to the `log` package's default logger, whatever happens while it serves, so that a test which points the `log` package's output at a buffer, serves with a banner source that panics when called and a listener whose first `Accept` returns an error `Run` retries, requests a page, and cancels `ctx` finds the buffer empty when `Run` returns.
- R-ERVL-5OIC: When `Run` has taken file descriptor 3 as a listener, has not failed to send `READY=1`, and a call to that listener's `Accept` returns, while `ctx` is not done, an error that `Run` does not retry, `Run` MUST write to `Stderr` exactly one line, beginning `dummy: ` and ending in a newline, MUST write nothing to `Stdout`, and MUST return `ExitServerFailed`; an error that is not a `net.Error` MUST be among the errors `Run` does not retry.
- R-5ZTA-PV8G: When `Inherit` is nil and file descriptor 3 is a listening Unix-domain stream socket bound to a filesystem path, `Run` MUST leave that path in place and MUST NOT shut the socket down, so that after `Run` returns the socket still accepts connections into its queue for another process that holds it.
- R-KG4T-T5ZV: When `Run` has taken file descriptor 3 as a listener and has not failed to send `READY=1`, the first event `p.Telemetry` records after `Run` is called MUST be one `service.started` event, the event `Writer.Ready` records, recorded before any event of any request that arrived on that listener.
- R-KHCQ-6XQK: When `Run` returns without having taken a listener — `Args` not empty, or a start R-1XHC-TE94, R-JI4Z-UR8P, R-JJCW-8IZE or R-3OW4-PGT8 refuses — or after failing to send `READY=1` (R-EQYT-T2RY), it MUST NOT have called any method of `p.Telemetry`, so that `Run` does not panic when `p.Telemetry` is nil, a writer passed as `p.Telemetry` has recorded no event from that call, and that writer's `Ready` called afterwards records `service.started`.
- R-KIKM-KPH9: When `Run` returns `ExitSuccess` after `ctx` was done, and `p.Telemetry`'s sink answers every `Deliver` call with a nil error at once, the events that sink has received when `Run` returns MUST end with exactly one `service.stopping` event, whose `reason` attribute is the string `context.Cause(ctx).Error()` returns, received after the `request.finished` event of every request that arrived on the listener `Run` took.
- R-WGRO-GGMY: When `Run` cuts off requests as R-PFER-36TQ states, and `p.Telemetry`'s sink answers every `Deliver` call whose context is done with a non-nil error and every other `Deliver` call with a nil error, that sink MUST NOT have answered nil for a `service.stopping` event; `p.Telemetry` MUST write to its `Stderr` the `undelivered event` line (appkit's telemetry writer) of a `service.stopping` event whose `reason` attribute is the string `context.Cause(ctx).Error()` returns, and MUST have written it before any connection of a cut-off request is closed, so that a cut-off request's `request.finished`, recorded only after its connection closes, is recorded after `Shutdown` began and never reaches the sink; and `Run` MUST write the line R-PFER-36TQ states to `Stderr` only after that `service.stopping` line has been written and after the line of every other event recorded before the drain deadline elapsed that the sink did not answer nil for.
- R-KL0F-C8YN: The widgets `Run` serves on the listener it took MUST start as the store `widget.NewStore(p.Rand)` (`D05-widgets`) makes, their ids included, so that a `tools/call` of `list_widgets` that `Run` answers there before accepting any widget reports as the `id` of `alpha`, `beta` and `gamma` the `ID` R-KYFB-JQ4A states for the first, second and third 8 bytes read from `p.Rand`, and so that `p.Rand` may be nil.
