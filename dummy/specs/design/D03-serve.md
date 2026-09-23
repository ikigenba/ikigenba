# D03-serve

The serve path: what happens between `cli.Run` being called with no
arguments and the process being gone. `D01-layout-and-run-seam` declares the
names this design behaves through: `cli.Process` with its `Pid`, `Unsetenv`
and `Inherit`, `cli.Run`, the exit codes, `server.Serve` and
`server.DrainError`. `D02-cli` decides that an empty `Args` means serve and
that nothing else touches the environment; `D04-panel` decides what the
handler answers and the line it writes for a 500. This design says how `Run`
reads the drain deadline, takes the socket the host passes in, tells systemd
it is ready and hands off to `Serve`, and how `Serve` treats the listener it
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
on it, outlive every restart. A developer stands in for the host with
`systemd-socket-activate -l 127.0.0.1:3000 <app>`, which passes a loopback TCP
socket on the same terms.

The app takes the socket the way `sd_listen_fds(3)` documents: `LISTEN_PID`
is the app's own process id, `LISTEN_FDS` counts the sockets passed, and the
first is file descriptor 3 (`SD_LISTEN_FDS_START`). It removes `LISTEN_PID`,
`LISTEN_FDS` and `LISTEN_FDNAMES` from its environment once it has taken the
socket, so nothing it might start inherits a claim meant for it. Once it is
ready it sends the datagram `READY=1` to the Unix datagram socket named by
`NOTIFY_SOCKET`, as `sd_notify(3)` documents, a name beginning with `@` being
in the abstract namespace; with `NOTIFY_SOCKET` unset it tells nobody. On
`SIGTERM` or `SIGINT` it stops accepting, lets the requests it already
accepted finish for at most `DRAIN_SECONDS`, then cuts off whatever is left,
says how many it cut off, and exits 1. It closes its own copy of the socket
and never removes the socket's path. `DRAIN_SECONDS` and the service unit's
`TimeoutStopSec` are space-wide integer-second settings owned by opsctl
(defaults 5 and 10): opsctl writes `DRAIN_SECONDS` into every app's
`etc/env`, and no manifest sets either. An app reads `DRAIN_SECONDS` as a
positive whole number, 5 when it is unset or empty, and sets no upper limit
of its own.

The socket is the app's only way in, and every app runs as `ikigenba`, so the
suite and nginx can reach it and nothing else on the host can. The suite is a
closed system: an app trusts `X-User-Id`, `X-User-Email` and `X-Request-Id`
as nginx sets them, and trusts a sibling to have forwarded them. An app that
calls a sibling while serving a request calls the sibling's socket directly
and copies those three headers from the request it is serving. A healthy app
writes nothing; a 5xx, any status from 500 through 599, is trouble and gets
one line on stderr naming the request by its `X-Request-Id`, and a 4xx is the
caller's to fix and writes nothing (`D04-panel` states dummy's, whose only 5xx
is a 500). Work no live
request started is outside these terms. dummy calls no sibling.

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
over the real descriptor, which serves a Unix or a TCP stream socket alike.
The Go `net` documentation of `UnixListener.SetUnlinkOnClose` states that a
listener made by `FileListener` does not remove the socket file when it is
closed, and `FileListener` documents that closing the listener does not
affect the file it was made from; closing is all dummy ever does to the
socket, so the path systemd created stays, and systemd's own copy of the
socket keeps queueing connections for the next dummy. If the descriptor
cannot be made into a listener — it is not a listening stream socket, say —
that is trouble on the host rather than a caller's typo: `dummy: ` and the
error, exit 1.

With the listener in hand `Run` builds the process's widget store, the
panel's handler over it, and then tells systemd it is ready, before it calls
`Serve`. The socket has been listening since systemd made it, so from that
moment every connection is queued and will be answered: ready is true before
the first `Accept`, and sending it first means a failure to send is reported
without anything having been served. That failure is trouble — under
`Type=notify` systemd would otherwise wait out its start timeout — so it is
`dummy: ` and the error, exit 1, and `Serve` is never called. A test learns
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
a request yet, and is closed at once like an idle one — `http.Server.Shutdown`
alone leaves a new connection open for several seconds, so the build tracks
connection state itself. `Run` reports that error like any other `Serve` failure:
`dummy: stopped with <n> requests unfinished` (`1 request` when `<n>` is 1), exit 1. The drain is bounded
so that dummy always exits before the service unit's stop timeout and is
never killed by systemd mid-write; keeping `DRAIN_SECONDS` below that timeout
is opsctl's to enforce. `Run` never passes a drain that is not positive, and
`Serve`'s behavior for one is not contract.

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

A healthy run is silent from start to finish: no startup banner, no request
log, nothing on either stream when it stops, so that under systemd the
journal holds only trouble. The one thing that reaches `Stderr` while dummy
serves is the handler's line for a 500, written through a writer `Run` hands
the handler. `Run` makes sure no two writes to `Stderr` are ever in progress
at once, the handler's included — requests are concurrent, a handler cut off
by the drain may still be running when `Run` reports the overrun, and a test's
`Stderr` is a `bytes.Buffer` that the race detector watches.

No read, write, or idle timeouts are contract. The build is nonetheless
expected to set `ReadHeaderTimeout`: the lint gate's gosec `G112` demands it
and suppression is forbidden, and it affects only a connection that has not
yet delivered a request header. The build must not set a write timeout that
could cut a response short inside the drain.

## REQUIREMENTS

- R-QLRL-RC1B: `Serve` MUST accept connections on `ln` and answer every request received on them over HTTP/1.1 with the response `h` produces for that request, and MUST NOT return while `ctx` is not done and serving has not failed.
- R-LSZH-BHHX: When `ctx` is done, `Serve` MUST stop accepting connections and close `ln`, and MUST close every connection that carries no request.
- R-LU7D-P98M: When `ctx` is done, `Serve` MUST let every call to `h.ServeHTTP` already in progress continue and MUST deliver the complete response of every such call that returns within `drain` after `ctx` was done; when every such call has returned and its response has been delivered before `drain` has elapsed, `Serve` MUST return nil without waiting for `drain` to elapse.
- R-LVFA-30ZB: When `n` calls to `h.ServeHTTP`, `n` at least 1, are still in progress once `drain` has elapsed after `ctx` was done, `Serve` MUST close the connections those requests arrived on without writing the rest of their responses and MUST return a `*DrainError` whose `Unfinished` is `n`, without waiting for those calls to return.
- R-LURX-UT17: `(*DrainError).Error` MUST return exactly `"stopped with 1 request unfinished"` when `e.Unfinished` is 1, and exactly `"stopped with " + strconv.Itoa(e.Unfinished) + " requests unfinished"` for every other value of `e.Unfinished`.
- R-QO7E-IVIP: When serving fails while `ctx` is not done, `Serve` MUST return a non-nil error, and `Serve` MUST NOT return nil for any reason other than `ctx` being done.
- R-IBR4-T2PU: `Serve` MUST write nothing to the `log` package's default logger and nothing to any process stream, whatever `ln` and `h` do: the diagnostics `net/http`'s server writes through its `ErrorLog`, such as an `Accept` error it retries or a handler that panics, MUST be discarded, so that a test which points the `log` package's output at a buffer and drives `Serve` with a listener whose `Accept` returns a temporary error and a handler that panics finds the buffer empty when `Serve` returns.
- R-LXV2-UKGP: When `Args` is empty, `Run` MUST decide whether `DRAIN_SECONDS` is acceptable before it calls `LookupEnv` for `LISTEN_PID` or `LISTEN_FDS`, so that when `DRAIN_SECONDS` is not acceptable and no socket was passed in, the `DRAIN_SECONDS` diagnostic is the one `Run` writes.
- R-LZ2Z-8C7E: `Run` MUST accept a non-empty `DRAIN_SECONDS` value if and only if it consists of one or more ASCII decimal digits, the first of which is not `0`, and no other characters, whatever the magnitude of the integer those digits denote, so that `0`, `-1`, `2.5`, `5s`, `05`, ` 5`, and `abc` are refused and no value is refused for being large.
- R-W59L-WD8G: When `Args` is empty and `LookupEnv("DRAIN_SECONDS")` returns `true` with a non-empty value `v` that `Run` does not accept, `Run` MUST write exactly `"dummy: DRAIN_SECONDS is '" + v + "', not a positive whole number of seconds\n"` to `Stderr` with `v` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `ExitUsage`.
- R-M1IR-ZVOS: The `drain` `Run` passes to `server.Serve` MUST be `5 * time.Second` when `LookupEnv("DRAIN_SECONDS")` returns `false` or the empty string, and otherwise, for the accepted value denoting the integer `n`, MUST be `n` seconds when `n` seconds is at most the largest `time.Duration` and the largest `time.Duration` when it is not.
- R-M2QO-DNFH: `Run` MUST treat a socket as passed in if and only if `LookupEnv("LISTEN_PID")` returns `true` with a value equal to `strconv.Itoa(p.Pid)` and `LookupEnv("LISTEN_FDS")` returns `true` with a value of one or more ASCII decimal digits and no other characters denoting an integer of at least 1, that integer being the number of sockets passed in.
- R-W6HI-A4Z5: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and no socket is passed in, `Run` MUST write exactly `"dummy: no socket was passed in\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3000 dummy'\n"` to `Stderr`, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `ExitUsage`.
- R-W7PE-NWPU: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and more than one socket is passed in, `Run` MUST write exactly `"dummy: " + v + " sockets were passed in, expected 1\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3000 dummy'\n"` to `Stderr` with `v` the value of `LISTEN_FDS` verbatim, write nothing to `Stdout`, call neither `Inherit` nor `Unsetenv`, send nothing to a notification socket, and accept no connection on any listener, and return `ExitUsage`.
- R-M6ED-IYNK: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and exactly one socket is passed in, `Run` MUST call `Unsetenv`, when it is not nil, once with each of `LISTEN_PID`, `LISTEN_FDS`, and `LISTEN_FDNAMES` before it returns, and MUST NOT call it with any other key.
- R-M7M9-WQE9: When `Args` is empty, `DRAIN_SECONDS` is unset, empty, or accepted, and exactly one socket is passed in, `Run` MUST take file descriptor 3, and no other descriptor, as its listener, by calling `Inherit(3)` exactly once when `Inherit` is not nil, and by calling `net.FileListener` on a file for the process's file descriptor 3 when `Inherit` is nil.
- R-W8XB-1OGJ: When taking file descriptor 3 as a listener fails with an error `err`, `Run` MUST write exactly `"dummy: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, send nothing to a notification socket, and return `ExitServerFailed`.
- R-MA22-O9VN: When `Run` has taken file descriptor 3 as a listener `ln` and does not fail to notify, it MUST call `widget.NewStore` exactly once and then `server.Serve` exactly once with `Run`'s own `ctx`, `ln`, `panel.Handler(store, w)`, and the `drain` of R-M1IR-ZVOS, where `store` is the value `widget.NewStore` returned and `w` is a writer each of whose `Write` calls results in exactly one call to `Stderr.Write` with the same bytes.
- R-MCHV-FTD1: When `Run` has taken file descriptor 3 as a listener and `LookupEnv("NOTIFY_SOCKET")` returns `true` with a non-empty value `a`, `Run` MUST, after taking the listener and before calling `server.Serve`, send exactly one datagram, whose content is exactly `READY=1`, to the Unix datagram socket whose address is `a`, an `a` beginning with `@` naming a socket in the abstract namespace; `Run` MUST send nothing to a notification socket on any other occasion.
- R-MDPR-TL3Q: When `LookupEnv("NOTIFY_SOCKET")` returns `false` or the empty string, `Run` MUST send no datagram and MUST serve as it otherwise would.
- R-WA57-FG78: When sending `READY=1` fails with an error `err`, `Run` MUST write exactly `"dummy: " + err.Error() + "\n"` to `Stderr`, write nothing to `Stdout`, accept no connection on the listener it took, and return `ExitServerFailed`.
- R-MG5K-L4L4: A `Run` that calls `server.Serve` and to which `Serve` returns nil MUST return `ExitSuccess`.
- R-MHDG-YWBT: A `Run` that calls `server.Serve` MUST write nothing to `Stdout`, and MUST write nothing to `Stderr` other than what the handler writes through the writer of R-MA22-O9VN and, when `Serve` returns a non-nil error, the one line R-QVIS-THYV states.
- R-QVIS-THYV: When `server.Serve` returns a non-nil error to `Run`, `Run` MUST write to `Stderr` exactly `dummy: `, that error's `Error()` text, and a newline, MUST write nothing to `Stdout`, and MUST return `ExitServerFailed`.
- R-MILD-CO2I: `Run` MUST NOT let two calls to `Stderr.Write` be in progress at the same time, those made through the writer of R-MA22-O9VN included, so that a `Stderr` that is not safe for concurrent use is never written concurrently.
- R-MJT9-QFT7: When `Inherit` is nil and file descriptor 3 is a listening Unix-domain stream socket bound to a filesystem path, `Run` MUST leave that path in place and MUST NOT shut the socket down, so that after `Run` returns the socket still accepts connections into its queue for another process that holds it.
