# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits:
dummy never opens one of its own. It takes the socket the way systemd socket
activation passes it — `LISTEN_PID` names dummy's own process, `LISTEN_FDS` is
`1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*`
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
every service unit, and an app's manifest never sets either. dummy's
environment also carries `IKIGENBA_SERVICES`, the path of the host's services
file, which lists the platform's services for the launcher in every page's
banner (`S3`) and holds the description dummy's MCP endpoint gives its clients
as instructions (`S9-mcp.md`). On a host, opsctl sets it in the environment
the host gives dummy, normally `/var/lib/ikigenba/services.json`; on a
developer's laptop it is normally unset, and dummy's pages then carry no
launcher and its MCP endpoint no instructions. dummy reads the variable once,
when it starts, and never fails to start over it: unset, empty, or naming a
file that is missing or unreadable, dummy starts and serves all the same, and
says nothing about the file itself. The actor in these stories is the host,
whether that is systemd or a developer at a terminal standing in for it.

dummy keeps a trail: it records what it does as events it sends to the
platform's telemetry service, where an operator, or an agent working for one,
follows what happened from one thing they know — a request id, a user, a
widget's id, or a time. dummy finds telemetry in the services file
`IKIGENBA_SERVICES` names, as the entry named `telemetry`, and sends each
event to that entry's socket; it looks the entry up afresh for every event,
so a telemetry installed after dummy started is found without a restart.
What telemetry does with an event is told in telemetry's own stories. The
stories here show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"dummy","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when dummy recorded the event, in UTC to the microsecond, as
`2026-10-02T14:03:07.123456Z`; `service` is always `dummy`; `<request-id>`
and `<user>` are the id of the request that caused the event and the
caller's `X-User-Id`, each empty when there is none, as for an event no
request caused; and `attrs` holds the event's attributes, flat. Attributes
name what happened and the ids of what it touched, never data: no event
carries a widget's name, count, or status, a request's query, a caller's
email, or a tool's arguments. dummy sends its events one at a time, in the
order it recorded them, and an answer never waits for its events to be sent.
Telemetry takes an event by answering `204`. When it cannot be reached —
the services file is unset, unreadable, or has no `telemetry` entry, or
nothing answers on its socket — or it answers anything other than `204` or a
`4xx`, dummy tries the event a few times over a fraction of a second. When it
answers `4xx`, it has refused the event itself, and sending it again cannot
help, so dummy does not retry it. Either way dummy then writes the event to
stderr as one line, `dummy: undelivered event: <event>`, where `<event>` is
the JSON telemetry would have received, and carries on serving, so nothing in
the trail is lost without trace. A developer whose environment names no
services file therefore sees every event on stderr. A developer
stands in for telemetry with a services file whose `telemetry` entry names a
socket that a listener of their own holds and that takes every event it is
sent; a story that says telemetry takes every event means that, or, on a
host, the telemetry service itself.

stderr holds only trouble: a condition dummy cannot go on from — the start-up
refusals and the requests lost to a drain cut short, below — and an event
dummy could not deliver. A failure dummy handles is not trouble: a request
answered 500, like a request answered any other way, is recorded by its
`request.finished` event with the status (`S3`) and earns no line on stderr.
So while telemetry takes every event, a running dummy writes nothing to stdout
or stderr, and under systemd the journal holds only trouble.

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
from one nginx made. dummy has no sibling to call. A request that reaches an
app with no `X-Request-Id`, or an empty one, as a developer's request does, is
given an id of the same shape by the app, a new one for each such request, so
every event about a request names it. Work no live request
started, such as a scheduled job, has no caller to forward and is not covered
by these terms.

## The host starts dummy

The socket keeps out every process that is not part of the suite or nginx,
which a port on loopback would not: any process on the host can connect to a
loopback port, and only the `ikigenba` user and nginx can connect to
`/run/ikigenba/dummy.sock`. systemd owns the socket, so it exists, and
accepts connections into its queue, before dummy starts and while it is
stopped; dummy's part is to serve what arrives on it. `systemctl start`
returns once dummy has reported that it is ready. At that moment dummy
records `service.started`, the first event of its trail, with the version it
is running: a new version in a start event is how a deploy shows in the
trail.

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
- The host's services file lists the telemetry service, which takes every
  event.

Postconditions:

- `ikigenba-dummy.service` is `active`, and dummy is serving on
  `/run/ikigenba/dummy.sock`: a connection there, and every connection queued
  before dummy started, is answered by dummy.
- dummy listens on no other socket and no port.
- telemetry has received one event from dummy, with no request id and no
  user, whose `version` is the version `dummy --version` prints (`S1`):

  ```
  {"time":"<time>","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
  ```

- dummy has written nothing to the journal.
- It keeps running until it is signalled.

## The host stops dummy

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's
`Ctrl-C` sends `SIGINT`, which dummy treats the same way. dummy stops taking
new connections, finishes the requests it has already accepted, and exits.
It closes its own copy of the socket and nothing more: the socket belongs to
systemd, which keeps it open, and dummy never removes
`/run/ikigenba/dummy.sock`. It waits at most `DRAIN_SECONDS` for requests to
finish, which on a host is always less than the time the service unit allows
before systemd kills it. Once its requests have finished, and each has
recorded its `request.finished` (`S3`), dummy records `service.stopping` with
the reason it is stopping, the name of the signal it received, `SIGTERM` or
`SIGINT`. That is the last event of its trail: dummy sends everything it
recorded before exiting, within the same drain deadline. A `service.started`
with no `service.stopping` before the next one is how the trail shows a
dummy that died rather than stopped.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

dummy exits 0. Nothing is on stdout or stderr.

Preconditions:

- dummy is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- Every request dummy has accepted finishes within 5 seconds of the signal.
- telemetry takes every event.

Postconditions:

- Every request accepted before the signal received its full response.
- telemetry has received every event dummy recorded. The last is
  `service.stopping`, after the `request.finished` of every request accepted
  before the signal:

  ```
  {"time":"<time>","service":"dummy","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  With `SIGINT` the `reason` is `SIGINT`.
- `/run/ikigenba/dummy.sock` still exists, and connections made to it after
  dummy exited wait in the socket's queue for the next dummy to answer.

## The host stops dummy while a request outlasts the drain

dummy waits for accepted requests only until its drain deadline,
`DRAIN_SECONDS` after the signal, so that it always exits before the service
unit's stop timeout and is never killed mid-write by systemd. A request still running at
the deadline is cut off: its connection is closed without the rest of its
response. Losing a request is trouble, so dummy says how many it lost and
exits non-zero. The drain has used the whole deadline, so dummy has no time
left to send `service.stopping`, or any other event not yet sent, to
telemetry: each goes to stderr as an `undelivered event` line instead.
telemetry never receives a `request.finished` for a request cut off: its
`request.started` with no finish is how the trail shows it was cut off.

Command:

```
$ kill -TERM <pid>
```

Output:

```
dummy: undelivered event: {"time":"<time>","service":"dummy","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
dummy: stopped with <n> requests unfinished
```

dummy exits 1, 5 seconds after the signal. The text is on stderr; stdout is
empty. `<n>` is the number of requests still running at the deadline. When
`<n>` is 1 the line reads `dummy: stopped with 1 request unfinished`. The
`service.stopping` line above is always written. Any other event dummy had
recorded and not yet delivered when the deadline came is written as an
`undelivered event` line too; those lines are in the order the events were
recorded, `service.stopping` last of them. The position of the
`stopped with` line among them is not fixed. stderr may also hold, for a
cut-off request, an `undelivered event` line carrying its
`request.finished`, recorded after the deadline and before dummy exited;
whether it does, and where that line falls, is not fixed. stderr holds no
other line.

Preconditions:

- dummy is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `<n>` of the requests dummy has accepted are still running 5 seconds after
  the signal.
- telemetry takes every event.

Postconditions:

- Every request that finished within 5 seconds of the signal received its
  full response; the `<n>` that did not were cut off.
- Each of the `<n>` cut-off requests has its `request.started` recorded
  (delivered to telemetry, written to stderr as undelivered, or both, when it
  was being delivered as the deadline came). telemetry has received no
  `request.finished` for any of them, and no `service.stopping`.
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
- The host's services file lists the telemetry service, which takes every
  event.

Postconditions:

- Every request the client sent was answered, by the old dummy or the new
  one; none was refused and none was cut off.
- A new dummy process is serving on `/run/ikigenba/dummy.sock`.
- telemetry has received the old dummy's `service.stopping`, with `reason`
  `SIGTERM`, and after it the new dummy's `service.started`, whose `version`
  is the version the new binary's `dummy --version` prints. Every request the
  old dummy answered is recorded before its `service.stopping`, and every
  request the new one answered after its `service.started`.

## The host starts dummy where telemetry cannot be reached

The trail is not a reason to stop serving. When dummy cannot deliver its
events — telemetry is not installed yet, is stopped, or the services file
names no `telemetry` entry — dummy starts and serves exactly as it does
otherwise, and its events go to the journal as `undelivered event` lines
(above). No answer waits on telemetry, so a client sees no difference.
dummy keeps looking for telemetry with every event, so once telemetry takes
events again, dummy's next events go to it without a restart; an event
already written to the journal is not sent again.

Command:

```
$ sudo systemctl start ikigenba-dummy.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed dummy, and `ikigenba-dummy.socket` is
  active, as in `The host starts dummy`.
- `ikigenba-dummy.service` is not running.
- The host's services file lists no `telemetry` entry, or nothing accepts
  connections on the socket that entry names.

Postconditions:

- `ikigenba-dummy.service` is `active`, and dummy is serving on
  `/run/ikigenba/dummy.sock`, as in `The host starts dummy`.
- The journal holds one line from dummy, written after it reported that it
  was ready:

  ```
  dummy: undelivered event: {"time":"<time>","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}
  ```

- Every event dummy records while telemetry cannot be reached is written to
  the journal the same way, one line each, and every request is answered as
  it would be with telemetry taking events.

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

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/dummy` exists.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not dummy's
  process id.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. dummy listened on nothing, told systemd nothing, and
  sent telemetry nothing.

## The host passes dummy more than one socket

dummy serves on exactly one socket. A unit that passes it several is
misconfigured, and dummy will not guess which one it was meant to serve on.

Command:

```
$ dummy
```

Output:

```
dummy: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/dummy` exists and is on the `PATH` as `dummy`.
- `LISTEN_PID` is dummy's process id and `LISTEN_FDS` is `2`: two listening
  sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. dummy served on neither socket, told systemd
  nothing, and sent telemetry nothing.

## The host gives dummy a drain deadline that is not a number of seconds

dummy reads `DRAIN_SECONDS` before it serves, so a bad value is found at start
rather than at the moment dummy is asked to stop. A value that is not a
positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's
mistake, so it is a usage error and dummy serves nothing. The value is quoted
back verbatim. dummy sets no upper limit: keeping the drain inside the service
unit's stop timeout is opsctl's to enforce.

Command:

```
$ DRAIN_SECONDS=abc dummy
```

Output:

```
dummy: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/dummy` exists and is on the `PATH` as `dummy`.
- `LISTEN_PID` is dummy's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. dummy served nothing, told systemd nothing, and sent
  telemetry nothing.
