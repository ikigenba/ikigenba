# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits:
dummy never opens one of its own. It takes the socket the way systemd socket
activation passes it — `LISTEN_PID` names dummy's own process, `LISTEN_FDS` is
`1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*`
variables from its environment once it has taken it. On a host, opsctl
publishes `ikigenba-dummy.socket`, which holds the Unix socket
`/run/ikigenba/dummy.sock`, beside `ikigenba-dummy.service`, which runs
`/opt/ikigenba/current/dummy/bin/dummy` with no arguments as the `ikigenba`
user, with `/var/opt/ikigenba/dummy` as its working directory and
`/etc/opt/ikigenba/dummy/env` as its environment file; nginx proxies to `http://unix:/run/ikigenba/dummy.sock:`.
The service is `Type=notify`: dummy tells systemd it is ready, by sending
`READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, dummy drains
for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its
environment, and 5 when that is unset or empty. On a host, opsctl owns this
value and the service unit's stop timeout: both are space-wide settings in
opsctl's configuration, opsctl writes the drain into every app's env file and
the stop timeout (10 seconds by default, always longer than the drain) into
every service unit, and an app's manifest never sets either. dummy's
environment also carries `IKIGENBA_SERVICES`, the path of the host's services
file, which lists the platform's services for the launcher in every page's
banner (`S3`) and holds the description dummy's MCP endpoint gives its clients
as instructions (`S9-mcp.md`). On a host, opsctl sets it in the environment
the host gives dummy, normally `/run/ikigenba/services.json`; on a
developer's laptop it is normally unset, and dummy's pages then carry no
launcher and its MCP endpoint no instructions. dummy reads the variable once,
when it starts, and never fails to start over it: unset, empty, or naming a
file that is missing or unreadable, dummy starts and serves all the same, and
says nothing about the file itself. The actor in these stories is the host,
whether that is systemd or a developer at a terminal standing in for it.

The widgets are kept in dummy's database, `state/dummy.db` under its working
directory, and every request shares them. A database dummy creates holds no
widgets; every widget created since is kept across restarts and deploys, with
the id it was given. dummy reads `DRAIN_SECONDS` and takes its socket before
it opens the database, so a start refused as a usage error has touched
nothing, not even the database. Then it creates `state/` if it is absent and
`state/dummy.db` if it is absent, brings the database up to date by applying,
in order, every migration it carries that the database has not had (`S1`),
and only then serves and tells systemd it is ready. A database it cannot open
is a start it refuses, with one line on stderr,
`dummy: cannot open database state/dummy.db: <reason>`, and exit status 1. A
database that records a migration it does not carry is one a newer dummy has
upgraded; dummy applies nothing to it, warns on stderr that it is ahead, and
serves it as usual (below). dummy is the database's only writer, and the host
replicates it as the manifest declares (`S1`).

dummy's environment may also carry `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`,
from which it builds `<display>`, the display string `dummy --version` prints
under the same environment (`S1`). dummy reads them once, when it starts, and
shows that string as its version wherever it shows one: in its
`service.started` event (below), its pages' footer (`S3`), and its MCP
`serverInfo` (`S9-mcp.md`). With neither set the string is empty, and dummy
starts and serves all the same.

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
answered 500, or 503 because dummy cannot reach the widgets in its database
(`S3`), like a request answered any other way, is recorded by its
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
records `service.started`, the first event of its trail, with `<display>` as
its `version`: a new value there is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-dummy.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl activate` has activated a release holding dummy:
  `/opt/ikigenba/current/dummy/bin/dummy` exists, and `ikigenba-dummy.socket`
  and `ikigenba-dummy.service` are published.
- `ikigenba-dummy.socket` is active, so `/run/ikigenba/dummy.sock` exists and
  accepts connections.
- `ikigenba-dummy.service` is not running.
- `/var/opt/ikigenba/dummy/state/dummy.db` exists, from an earlier start, and records no
  migration this dummy does not carry.
- The host's services file lists the telemetry service, which takes every
  event.

Postconditions:

- `ikigenba-dummy.service` is `active`, and dummy is serving on
  `/run/ikigenba/dummy.sock`: a connection there, and every connection queued
  before dummy started, is answered by dummy.
- dummy listens on no other socket and no port.
- `/var/opt/ikigenba/dummy/state/dummy.db` is the database it opened, now up to date, and
  every widget it held is still there.
- telemetry has received one event from dummy, with no request id and no
  user, whose `version` is `<display>`, the string `dummy --version` prints
  under the environment the host gives dummy (`S1`), the empty string when
  that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`:

  ```
  {"time":"<time>","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- dummy has written nothing to the journal.
- It keeps running until it is signalled.

## The host starts dummy for the first time

The database file does not yet exist. dummy creates the `state/` directory if
it is absent, then creates `state/dummy.db`, applies every migration it
carries, and serves. The same start succeeds when `state/` already exists and
only the database is absent. Neither a fresh deployment nor any other first
start needs the directory created beforehand. The paths are relative to
dummy's working directory. A database dummy creates holds no widgets, so the
panel it serves lists none until one is created.

Command:

```
$ dummy
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/dummy` exists and is on the `PATH` as `dummy`.
- `LISTEN_PID` is dummy's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so dummy reports readiness to nobody.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry takes
  every event.
- `state/dummy.db` does not exist.
- Either `state/` is absent and dummy can create it in its working directory,
  or `state/` is an existing directory in which dummy can create the
  database.

Postconditions:

- `state/` exists, created by dummy if it was absent.
- `state/dummy.db` now exists, created by this start, and is up to date:
  `dummy db status` prints `0001 applied <time>`, `<time>` being the moment
  this start applied it (`S1`).
- dummy is serving on the socket it was passed, and on no other.
- The database holds no widgets: the panel lists none (`S3`).
- telemetry has received dummy's `service.started`, whose `version` is
  `<display>`.
- It keeps running until it is signalled.

## The host starts dummy over an existing database

A database an earlier dummy created is where the widgets live, so a start, a
restart, or a new binary serves the widgets that were there, each with the id
it was given when it was created. Nothing about a start adds, removes, or
changes a widget.

Command:

```
$ dummy
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/dummy` exists and is on the `PATH` as `dummy`.
- `LISTEN_PID` is dummy's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so dummy reports readiness to nobody.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry takes
  every event.
- `state/dummy.db` exists, from an earlier start, and records no migration
  this dummy does not carry. It holds exactly these widgets, in creation
  order: `alpha` 3 `active`, `beta` 0 `paused`, `gamma` 12 `retired`, with
  the ids `<alpha-id>`, `<beta-id>`, and `<gamma-id>` an earlier dummy gave
  them.

Postconditions:

- dummy is serving on the socket it was passed, over the same
  `state/dummy.db`, now up to date.
- The panel lists exactly `alpha` 3 `active`, `beta` 0 `paused`, and `gamma`
  12 `retired`, in that order, with the ids `<alpha-id>`, `<beta-id>`, and
  `<gamma-id>` (`S3`).
- telemetry has received dummy's `service.started`, whose `version` is
  `<display>`.
- It keeps running until it is signalled.

## The host starts dummy where its state directory cannot be created

A regular file named `state` occupies the path where dummy needs its state
directory. dummy has taken its socket, but it reports the database failure
and exits before it serves or reports ready. The failure is not the caller's
usage, so it exits 1.

Command:

```
$ dummy
```

Output:

```
dummy: cannot open database state/dummy.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying
directory-creation failure.

Preconditions:

- `bin/dummy` exists and is on the `PATH` as `dummy`.
- `LISTEN_PID` is dummy's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `state` is an existing regular file in dummy's working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database was created. dummy served nothing, told systemd nothing, and
  recorded no event.

## The host starts dummy with a database it cannot open

`state/dummy.db` exists but dummy cannot open it: the file is not writable by
the user dummy runs as, or its contents are not a SQLite database. dummy
writes a diagnostic naming the database problem and exits before it serves or
reports ready. A missing file is not this error: an absent `state/dummy.db` is
created on first start (see `The host starts dummy for the first time`).
Under systemd the start fails, and `systemctl start` reports it; the socket
stays up, and connections wait in its queue for a dummy that can serve them.

Command:

```
$ dummy
```

Output:

```
dummy: cannot open database state/dummy.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying
open failure.

Preconditions:

- `bin/dummy` exists and is on the `PATH` as `dummy`.
- `LISTEN_PID` is dummy's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `state/dummy.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. dummy served nothing, told systemd nothing, and
  recorded no event.

## The host starts dummy with a database a newer dummy has upgraded

A deploy rolled back to an older binary leaves it over a database a newer
dummy has upgraded: the database records a migration this dummy does not
carry. Data never rolls back, so older code must run on newer data, and this
dummy serves the database as it stands. It applies nothing, not even a
migration it carries that the database lacks, and writes one line to stderr
naming the lowest version it does not carry, zero-padded to four digits as
`dummy db status` prints it; on a host that line goes to the journal. Then it
serves and tells systemd it is ready, as in any start. The warning is a line
on stderr only, not an event in the trail. `dummy db status` lists every
version the database records, the unknown ones as `unknown` (`S1`).

Command:

```
$ dummy
```

Output:

```
dummy: unknown migration version 0002: database is ahead of this binary
```

Does not exit. The line is on stderr, written before dummy serves; stdout is
empty.

Preconditions:

- `bin/dummy` exists and is on the `PATH` as `dummy`, carrying only migration
  `0001`.
- `LISTEN_PID` is dummy's process id and `LISTEN_FDS` is `1`: one listening
  socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry takes
  every event.
- `state/dummy.db` exists and records versions `0001` and `0002` as applied.
  It holds exactly these widgets, in creation order: `alpha` 3 `active`,
  `beta` 0 `paused`, `gamma` 12 `retired`.

Postconditions:

- The database still records `0001` and `0002`, and no other version; this
  start applied nothing.
- dummy is serving on the socket it was passed, over that `state/dummy.db`.
- The panel lists exactly `alpha` 3 `active`, `beta` 0 `paused`, and `gamma`
  12 `retired`, in that order (`S3`).
- telemetry has received dummy's `service.started`, whose `version` is
  `<display>`, and no event about the warning.
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
recorded before exiting, within the same drain deadline. Sending takes time,
so a stop is silent when the requests leave dummy at least a second of the
drain for it; a request that finishes later still gets its whole response,
but an event dummy has not sent when the deadline comes goes to stderr as an
`undelivered event` line instead. A `service.started`
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
- Every request dummy has accepted finishes at least a second before the
  drain deadline, within 4 seconds of the signal, so dummy has that second
  left to send what it recorded.
- telemetry takes every event as soon as it is sent.

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

A deploy activates a new release, which switches dummy's binary and restarts
`ikigenba-dummy.service`, one app at a time; `ikigenba-dummy.socket` stays up
throughout. Between the old dummy exiting
and the new one being ready, connections wait in the socket's queue instead
of being refused, so a client never sees dummy missing. That holds because
dummy finishes what it accepted before it exits and leaves the socket where
systemd put it. The widgets outlive the deploy: the new dummy opens the same
`state/dummy.db`, bringing it up to date first when the new binary carries a
migration the database has not had, and serves every widget the old one kept.

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
  `ikigenba-dummy.service`, over `/var/opt/ikigenba/dummy/state/dummy.db`.
- The database holds widgets, among them ones created through the old dummy.
- A client is sending requests to `/run/ikigenba/dummy.sock` throughout the
  restart, and every request the old dummy accepted finishes at least a
  second before its drain deadline.
- The host's services file lists the telemetry service, which takes every
  event.

Postconditions:

- Every request the client sent was answered, by the old dummy or the new
  one; none was refused and none was cut off.
- A new dummy process is serving on `/run/ikigenba/dummy.sock`, over the same
  `state/dummy.db`.
- Every widget the old dummy held, those created through it included, is
  served by the new one with the same id, name, count, and status, in the
  same order.
- telemetry has received the old dummy's `service.stopping`, with `reason`
  `SIGTERM`, and after it the new dummy's `service.started`, whose `version`
  is the `<display>` of the new dummy's environment. Every request the
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

- `opsctl activate` has activated a release holding dummy, and
  `ikigenba-dummy.socket` is active, as in `The host starts dummy`.
- `ikigenba-dummy.service` is not running.
- `/var/opt/ikigenba/dummy/state/dummy.db` exists and records no migration this dummy
  does not carry, or can be created as in `The host starts dummy for the
  first time`.
- The host's services file lists no `telemetry` entry, or nothing accepts
  connections on the socket that entry names.

Postconditions:

- `ikigenba-dummy.service` is `active`, and dummy is serving on
  `/run/ikigenba/dummy.sock`, as in `The host starts dummy`.
- The journal holds one line from dummy, written after it reported that it
  was ready:

  ```
  dummy: undelivered event: {"time":"<time>","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
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

- Nothing has changed. dummy opened no database, listened on nothing, told
  systemd nothing, and sent telemetry nothing; an absent `state/dummy.db` is
  still absent.

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

- Nothing has changed. dummy opened no database, served on neither socket,
  told systemd nothing, and sent telemetry nothing.

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

- Nothing has changed. dummy opened no database, served nothing, told systemd
  nothing, and sent telemetry nothing.
