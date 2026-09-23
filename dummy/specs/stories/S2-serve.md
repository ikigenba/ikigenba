# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits:
dummy never opens one of its own. It takes the socket the way systemd socket
activation passes it — `LISTEN_PID` names dummy's own process, `LISTEN_FDS`
is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*`
variables from its environment once it has taken it. On a host, opsctl
publishes `ikigenba-dummy.socket`, which holds the Unix socket
`/run/ikigenba/dummy.sock`, beside `ikigenba-dummy.service`, which runs
`/opt/dummy/bin/dummy` with no arguments as the `ikigenba` user, with
`/opt/dummy` as its working directory and `/opt/dummy/etc/env` as its
environment file; nginx proxies to `http://unix:/run/ikigenba/dummy.sock:`.
The service is `Type=notify`: dummy tells systemd it is ready, by sending
`READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, dummy drains
for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its
environment, and 5 when that is unset or empty. On a host, opsctl owns this
value and the service unit's stop timeout: both are space-wide settings in
opsctl's configuration, opsctl writes the drain into every app's `etc/env` and
the stop timeout (10 seconds by default, always longer than the drain) into
every service unit, and an app's manifest never sets either. A developer stands in for
the host with `systemd-socket-activate`, which passes a socket on the same
terms. A healthy dummy prints nothing, so under systemd the journal holds only
trouble. A diagnostic dummy writes about a request names that request by its
`X-Request-Id`, as `dummy: request <id>: <reason>` on stderr, so a line in the
journal can be matched to nginx's log of the same request; a request that
carries no `X-Request-Id` is named `-`. The actor in these stories is the host, whether that is systemd or a
developer at a terminal standing in for it.

These are the terms every app of the platform serves on, and a new app copies
them from here. The socket is the app's only way in. Every app runs as the one
`ikigenba` user, so any app can reach any sibling's socket, and nginx reaches
them all; nothing else on the host can. The suite is a closed system that only
we deploy services into, and an app trusts the suite: it trusts the headers
nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated,
and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every
request and overwrites whatever a client sent — and it trusts a sibling that
calls it to have forwarded them. An app that calls a sibling while serving a
request calls it directly at `http://unix:/run/ikigenba/<app>.sock:`, not
through nginx, and copies `X-User-Id`, `X-User-Email`, and `X-Request-Id` from
the request it is serving onto the call, so the sibling cannot tell the call
from one nginx made. dummy has no sibling to call. Work no live request
started, such as a scheduled job, has no caller to forward and is not covered
by these terms.

## The host starts dummy

The socket keeps out every process that is not part of the suite or nginx,
which a port on loopback would not: any process on the host can connect to a
loopback port, and only the `ikigenba` user and nginx can connect to
`/run/ikigenba/dummy.sock`. systemd owns the socket, so it exists, and
accepts connections into its queue, before dummy starts and while it is
stopped; dummy's part is to serve what arrives on it. `systemctl start`
returns once dummy has reported that it is ready.

Command:

```
$ sudo systemctl start ikigenba-dummy.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed dummy: `/opt/dummy/bin/dummy` exists, and
  `ikigenba-dummy.socket` and `ikigenba-dummy.service` are published.
- `ikigenba-dummy.socket` is active, so `/run/ikigenba/dummy.sock` exists and
  accepts connections.
- `ikigenba-dummy.service` is not running.

Postconditions:

- `ikigenba-dummy.service` is `active`, and dummy is serving on
  `/run/ikigenba/dummy.sock`: a connection there, and every connection queued
  before dummy started, is answered by dummy.
- dummy listens on no other socket and no port.
- dummy has written nothing to the journal.
- It keeps running until it is signalled.

## A developer serves dummy on a laptop

A laptop has no `ikigenba-dummy.socket`, so the developer lets
`systemd-socket-activate` hold a socket and pass it to dummy exactly as
systemd would. A TCP socket on loopback serves a browser and `curl` alike, and
the later groups' requests go to `http://127.0.0.1:3000`. The three lines are
`systemd-socket-activate`'s own: it announces the socket, and it starts dummy
only when the first connection arrives, which dummy then answers. dummy adds
nothing to them. There is no `NOTIFY_SOCKET` here, so dummy reports readiness
to nobody.

Command:

```
$ systemd-socket-activate -l 127.0.0.1:3000 dummy
```

Output:

```
Listening on 127.0.0.1:3000 as 3.
Communication attempt on fd 3.
Execing dummy (dummy)
```

Does not exit. The lines are on stderr; stdout is empty. The first line
appears at once, the other two when the first connection arrives.

Preconditions:

- `bin/dummy` exists and is on the developer's `PATH` as `dummy`.
- Nothing is listening on `127.0.0.1:3000`.

Postconditions:

- dummy is serving on `127.0.0.1:3000` and on no other address, and it
  answered the connection that started it.
- It keeps running until it is signalled.

## The host stops dummy

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's
`Ctrl-C` sends `SIGINT`, which dummy treats the same way. dummy stops taking
new connections, finishes the requests it has already accepted, and exits.
It closes its own copy of the socket and nothing more: the socket belongs to
systemd, which keeps it open, and dummy never removes
`/run/ikigenba/dummy.sock`. It waits at most `DRAIN_SECONDS` for requests to
finish, which on a host is always less than the time the service unit allows
before systemd kills it.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

dummy exits 0. Nothing is on stdout or stderr.

Preconditions:

- dummy is serving as process `<pid>`, on the socket systemd or
  `systemd-socket-activate` passed it.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- Every request dummy has accepted finishes within 5 seconds of the signal.

Postconditions:

- Every request accepted before the signal received its full response.
- `/run/ikigenba/dummy.sock` still exists, and connections made to it after
  dummy exited wait in the socket's queue for the next dummy to answer.

## The host stops dummy while a request outlasts the drain

dummy waits for accepted requests only until its drain deadline,
`DRAIN_SECONDS` after the signal, so that it always exits before the service
unit's stop timeout and is never killed mid-write by systemd. A request still running at
the deadline is cut off: its connection is closed without the rest of its
response. Losing a request is trouble, so dummy says how many it lost and
exits non-zero.

Command:

```
$ kill -TERM <pid>
```

Output:

```
dummy: stopped with <n> requests unfinished
```

dummy exits 1, 5 seconds after the signal. The line is on stderr; stdout is
empty. `<n>` is the number of requests still running at the deadline.

Preconditions:

- dummy is serving as process `<pid>`, on the socket systemd or
  `systemd-socket-activate` passed it.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `<n>` of the requests dummy has accepted are still running 5 seconds after
  the signal.

Postconditions:

- Every request that finished within 5 seconds of the signal received its
  full response; the `<n>` that did not were cut off.
- `/run/ikigenba/dummy.sock` still exists, and connections made to it after
  dummy exited wait in the socket's queue for the next dummy to answer.

## The host restarts dummy during a deploy

A deploy replaces dummy's binary and restarts `ikigenba-dummy.service` alone;
`ikigenba-dummy.socket` stays up throughout. Between the old dummy exiting
and the new one being ready, connections wait in the socket's queue instead
of being refused, so a client never sees dummy missing. That holds because
dummy finishes what it accepted before it exits and leaves the socket where
systemd put it.

Command:

```
$ sudo systemctl restart ikigenba-dummy.service
```

Output:

```
```

Exits 0, once the new dummy has reported that it is ready. Nothing is on
stdout or stderr.

Preconditions:

- dummy is serving on `/run/ikigenba/dummy.sock` under
  `ikigenba-dummy.service`.
- A client is sending requests to `/run/ikigenba/dummy.sock` throughout the
  restart.

Postconditions:

- Every request the client sent was answered, by the old dummy or the new
  one; none was refused and none was cut off.
- A new dummy process is serving on `/run/ikigenba/dummy.sock`.

## The host starts dummy without a socket

Run bare, with no socket passed in, dummy has nothing to serve on, and it
does not open one of its own: there is no port or address it falls back to.
That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or
`LISTEN_PID` naming some other process, counts as no socket passed in. The
detail says how to run dummy correctly.

Command:

```
$ dummy
```

Output:

```
dummy: no socket was passed in

run it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3000 dummy'
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/dummy` exists.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not dummy's
  process id.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. dummy listened on nothing and told systemd nothing.

## The host passes dummy more than one socket

dummy serves on exactly one socket. A unit that passes it several is
misconfigured, and dummy will not guess which one it was meant to serve on.

Command:

```
$ systemd-socket-activate -l 127.0.0.1:3000 -l 127.0.0.1:3001 dummy
```

Output:

```
Listening on 127.0.0.1:3000 as 3.
Listening on 127.0.0.1:3001 as 4.
Communication attempt on fd 3.
Execing dummy (dummy)
dummy: 2 sockets were passed in, expected 1

run it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3000 dummy'
```

Exits 2. The text is on stderr; stdout is empty. The first four lines are
`systemd-socket-activate`'s own; the rest is dummy's. The connection that
started dummy is closed unanswered.

Preconditions:

- `bin/dummy` exists and is on the developer's `PATH` as `dummy`.
- Nothing is listening on `127.0.0.1:3000` or `127.0.0.1:3001`.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- A connection is made to `127.0.0.1:3000`.

Postconditions:

- Nothing has changed. dummy served on neither socket and told systemd
  nothing.

## The host gives dummy a drain deadline that is not a number of seconds

dummy reads `DRAIN_SECONDS` before it serves, so a bad value is found at start
rather than at the moment dummy is asked to stop. A value that is not a
positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's
mistake, so it is a usage error and dummy serves nothing. The value is quoted
back verbatim. dummy sets no upper limit: keeping the drain inside the service
unit's stop timeout is opsctl's to enforce.

Command:

```
$ DRAIN_SECONDS=abc systemd-socket-activate -l 127.0.0.1:3000 dummy
```

Output:

```
Listening on 127.0.0.1:3000 as 3.
Communication attempt on fd 3.
Execing dummy (dummy)
dummy: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The text is on stderr; stdout is empty. The first three lines are
`systemd-socket-activate`'s own; the last is dummy's. The connection that
started dummy is closed unanswered.

Preconditions:

- `bin/dummy` exists and is on the developer's `PATH` as `dummy`.
- Nothing is listening on `127.0.0.1:3000`.
- A connection is made to `127.0.0.1:3000`.

Postconditions:

- Nothing has changed. dummy served nothing and told systemd nothing.
