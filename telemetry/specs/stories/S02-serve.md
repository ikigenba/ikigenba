# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: telemetry never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names telemetry's own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-telemetry.socket`, which holds the Unix socket `/run/ikigenba/telemetry.sock`, beside `ikigenba-telemetry.service`, which runs `/opt/telemetry/bin/telemetry` with no arguments as the `ikigenba` user, with `/opt/telemetry` as its working directory and `/opt/telemetry/etc/env` as its environment file; nginx proxies telemetry's public name to `http://unix:/run/ikigenba/telemetry.sock:`, and every other service on the host posts its events straight to that same socket, at `/ingest`, never through nginx (`S06`). The service is `Type=notify`: telemetry tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, telemetry drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty. On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's `etc/env` and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

telemetry's environment also carries `RETENTION_DAYS`, how long a record stays in the trail: a positive whole number of days, and 15 when it is unset or empty. Its default is the manifest's (`S01`), which the host writes into `etc/env`, and an operator changes it there. Records older than the window are swept out, once as telemetry starts and then every hour (`S07`). And it carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/var/lib/ikigenba/services.json`, which opsctl sets in the environment the host gives telemetry. The file lists the platform's services: it feeds the launcher in the banner of telemetry's pages (`S03`), and it holds the description telemetry's MCP endpoint gives its clients as instructions (`S05`). telemetry reads the variable once, when it starts, and reads the file it names afresh on every request, so a rewritten file shows on the next request without a restart. telemetry never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, telemetry starts and serves all the same, treats the file as listing no services, and says nothing about it. Unlike every other app, telemetry does not need the file to find the telemetry service: it is the telemetry service, and its own events go straight into its own store (below), so a missing services file or a file with no `telemetry` entry costs it only the launcher and the instructions.

telemetry checks its environment first — `DRAIN_SECONDS`, then `RETENTION_DAYS` — then looks for its socket, and only then opens its SQLite database at `state/telemetry.db`, relative to its working directory, creating `state/` and the database on its first start. So a start refused as a usage error has touched nothing, not even the database. telemetry is the database's only writer, and the host replicates it as the manifest declares (`S01`, `S14`).

telemetry records what it does as a trail of events, exactly as every app of the platform does, and its events are records in the same trail the other services' events land in: the same store its tools search (`S08` to `S11`). They do not go through its socket. Every other app posts each event to the telemetry service's socket; telemetry writes each of its own straight into its store, so no event of its own loops back through `/ingest`, and no services file is needed to find the way. An event is one record: the time, in UTC to the microsecond; the service, always `telemetry`; the event's name; the request id, the `X-Request-Id` of the request the event belongs to; the user, that request's `X-User-Id`, empty when it had none; and its attributes, flat names with string, number, or boolean values. The request id and the user are empty for an event that belongs to no request. Attributes carry what happened and the names of the things it happened to, never what a request or an answer held: no tool arguments, no results, no error text, no query string. Storing its own trail never holds up a request. telemetry records these events and no others (`S12`):

- `service.started`, once telemetry is serving and has told systemd it is ready, with `version`, the version `telemetry --version` prints (`S01`);
- `service.stopping`, when telemetry is told to stop and has finished the requests it accepted, with `reason`, the name of the signal that stopped it, `SIGTERM` or `SIGINT`; it is the last event telemetry records;
- `request.started`, as each request arrives, with `method` and `path`, the request's URL path without its query, for every request but one to `/ingest`;
- `request.finished`, once that request's answer is complete, with `status`, the HTTP status telemetry answered with, and `duration_us`, how long telemetry took to answer, in whole microseconds, for every request but one to `/ingest`;
- `tool.called`, for each call of one of its four tools that is answered with a result (`S05`), with `tool`, `kind`, `outcome`, and `duration_us`.

Any request to `/ingest`, whatever its method, is a sibling's event arriving or a refusal of one, and it adds at most the record the sibling sent and nothing of telemetry's own (`S06`): the trail is of what the suite did, not of telemetry's bookkeeping, and one request per sibling event would double it. A story shows the events a request added to the trail as a block, one event to a line, in the order telemetry recorded them: the event's name, then each attribute as `<key>=<value>`, with `duration_us` left out because it varies; the request id and the user every line of the block carries are stated beside it. A story's `Nothing has changed.` speaks of everything but the trail, which every request but an ingest adds to.

telemetry's stderr holds only trouble, and trouble is exactly two things: a condition telemetry cannot continue from — a start it refuses, a database it cannot open, a stop that cut requests off, each with its own diagnostic below — and an event of its own trail that it could not store. Everything else telemetry does it records in its trail and writes nothing about, a request it answers with a 5xx included: a handled failure is a fact of the trail, recorded with its status, not a line in the journal. A sibling's event that telemetry cannot take, because its store will not take it, is answered 500 and is the sibling's trouble to report (`S06`): the sibling writes its own `undelivered event` line, and telemetry writes nothing. A sweep writes nothing (`S07`). So under systemd the journal holds only trouble, and a healthy telemetry writes nothing at all. Every line telemetry writes to stderr begins `telemetry: `. An event of its own that it cannot store is not lost without trace: telemetry writes it to stderr as one line, `telemetry: undelivered event: ` followed by the event as a JSON object whose members are, in this order, `time`, `service`, `event`, `request_id`, `user`, and `attrs`, with `time` in the form `2026-10-02T14:03:07.123456Z`, and goes on serving. That is the only way an event of telemetry's own reaches stderr. Storing an event of its own is off the request's path: telemetry queues it and writes it into the store in order, shortly after, so a tool answers from what is in the store when it runs (`S12`). While its database takes writes, no event of its own reaches stderr but at a stop whose drain deadline cuts the storing short, when every event still queued is written there instead (`The host stops telemetry while a request outlasts the drain`).

These are the terms every app of the platform serves on, the same as dummy's. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. The mcp gateway calls telemetry that way, at its socket, forwarding the caller's three headers (`S05`). A post to `/ingest` is the one call that carries no identity: a sibling's writer, not a caller, makes it, and the event it carries names its own request id and user (`S06`). Every other request telemetry serves has a request id: a request that arrives with no `X-Request-Id`, or an empty one — a developer's request with no nginx in front, say — is given one in nginx's shape, 32 lowercase hexadecimal characters telemetry makes up, before anything else in telemetry sees the request, and from then on that id is the request's id in telemetry's trail. telemetry calls no sibling.

## The host starts telemetry

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/telemetry.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before telemetry starts and while it is stopped; telemetry's part is to serve what arrives on it, the events siblings posted while it was down included. `systemctl start` returns once telemetry has reported that it is ready. At that moment telemetry records `service.started`, the first event of its own trail, with the version it is running: a new version in a start event is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-telemetry.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed telemetry: `/opt/telemetry/bin/telemetry` exists, and `ikigenba-telemetry.socket` and `ikigenba-telemetry.service` are published.
- `/opt/telemetry/etc/env` sets `DRAIN_SECONDS` and `RETENTION_DAYS`, each to a valid value, and `IKIGENBA_SERVICES` to the host's services file.
- `ikigenba-telemetry.socket` is active, so `/run/ikigenba/telemetry.sock` exists and accepts connections.
- `/opt/telemetry/state/telemetry.db` exists, from an earlier start.
- `ikigenba-telemetry.service` is not running.

Postconditions:

- `ikigenba-telemetry.service` is `active`, and telemetry is serving on `/run/ikigenba/telemetry.sock`: a connection there, and every connection queued before telemetry started, is answered by telemetry.
- telemetry listens on no other socket and no port.
- `/opt/telemetry/state/telemetry.db` is the database it opened; it existed already, and every record it held that is inside the retention window is still there. Records older than the window have been swept out (`S07`).
- telemetry has written nothing to the journal.
- The trail holds one event from this start, the first this telemetry records, before the `request.started` of any request it answers, with an empty request id and an empty user, `v<semver>` being the version `telemetry --version` prints; `search` with `services` `["telemetry"]` finds it (`S09`, `S12`):

  ```
  service.started version=v<semver>
  ```

- It keeps running until it is signalled.

## The host starts telemetry for the first time

The database file does not yet exist. telemetry creates the `state/` directory if it is absent, then creates `state/telemetry.db` and its schema and serves. The same start succeeds when `state/` already exists and only the database is absent. Neither a fresh deployment nor any other first start needs the directory created beforehand. The paths are relative to telemetry's working directory.

Command:

```
$ telemetry
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so telemetry reports readiness to nobody.
- `DRAIN_SECONDS` and `RETENTION_DAYS` are each unset, or a positive whole number.
- `state/telemetry.db` does not exist.
- Either `state/` is absent and telemetry can create it in its working directory, or `state/` is an existing directory in which telemetry can create the database.

Postconditions:

- `state/` exists, created by telemetry if it was absent.
- `state/telemetry.db` now exists, with its schema, created by this start.
- telemetry is serving on the socket it was passed, and on no other.
- The trail holds exactly one record, telemetry's own `service.started` with `version=v<semver>`, the version `telemetry --version` prints, under an empty request id and an empty user.
- It keeps running until it is signalled.

## The host starts telemetry with no services file

A developer running telemetry at a terminal, or a host whose services file is not there, gives telemetry no list of services. It serves all the same, and unlike its siblings it loses nothing of its trail: its own events go into its own store, not to a socket the file would name, so nothing reaches stderr. What the file would have given — the launcher in its pages' banner (`S03`) and the description its MCP endpoint gives as instructions (`S05`) — is simply absent. A services file that exists but has no entry named `telemetry` is the same for the trail and the launcher, and only the instructions differ as `S05` tells. Siblings posting to `/ingest` are unaffected either way: they find telemetry through their own services file, not telemetry's.

Command:

```
$ telemetry
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `IKIGENBA_SERVICES` is unset, or names a file that does not exist, or names a services file that has no entry named `telemetry`.
- `state/telemetry.db` exists, from an earlier start, or can be created as in `The host starts telemetry for the first time`.

Postconditions:

- telemetry is serving on the socket it was passed, and answers every request as it would with a services file: its pages have no launcher, and its MCP endpoint gives no instructions when the file names no `telemetry` entry.
- The trail holds telemetry's own `service.started` with `version=v<semver>`, under an empty request id and an empty user, and every event it records from now on.
- Nothing reached stderr.

## The host starts telemetry where its state directory cannot be created

A regular file named `state` occupies the path where telemetry needs its state directory. telemetry has taken its socket, but it reports the database setup failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1.

Command:

```
$ telemetry
```

Output:

```
telemetry: cannot open database state/telemetry.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and `RETENTION_DAYS` are each unset, or a positive whole number.
- `state` is an existing regular file in telemetry's working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database was created. telemetry served nothing, told systemd nothing, and recorded no event.

## The host starts telemetry with a database it cannot open

`state/telemetry.db` exists but telemetry cannot open it — the file is not writable by the service user, or its contents are not a valid SQLite database. telemetry writes a diagnostic naming the database problem and exits before it serves or reports ready. A missing file is not this error: an absent `state/telemetry.db` is created on first start (see `The host starts telemetry for the first time`). Under systemd the start fails, and `systemctl start` reports it; the socket stays up, and the events siblings post wait in its queue for a telemetry that can take them.

Command:

```
$ telemetry
```

Output:

```
telemetry: cannot open database state/telemetry.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying open failure.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and `RETENTION_DAYS` are each unset, or a positive whole number.
- `state/telemetry.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. telemetry served nothing, told systemd nothing, and recorded no event.

## The host stops telemetry

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which telemetry treats the same way. telemetry stops taking new connections, finishes the requests it has already accepted — a sibling's post to `/ingest` among them, so an event that reached telemetry is stored before it exits — and exits. It closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and telemetry never removes `/run/ikigenba/telemetry.sock`. It waits at most `DRAIN_SECONDS` for requests to finish, which on a host is always less than the time the service unit allows before systemd kills it. Once its requests have finished, and each has recorded its `request.finished`, telemetry records `service.stopping` with the reason it is stopping, the name of the signal it received, `SIGTERM` or `SIGINT`. That is the last event of its own trail, and it is in the store before telemetry exits. A `service.started` with no `service.stopping` before the next one is how the trail shows a telemetry that died rather than stopped.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

telemetry exits 0. Nothing is on stdout or stderr.

Preconditions:

- telemetry is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- Every request telemetry has accepted finishes within 5 seconds of the signal.

Postconditions:

- Every request accepted before the signal received its full response, and every event a sibling posted before the signal is in the store.
- The trail holds every event telemetry recorded. The last of its own is `service.stopping`, after the `request.finished` of every request accepted before the signal, with an empty request id and an empty user:

  ```
  service.stopping reason=SIGTERM
  ```

  With `SIGINT` the `reason` is `SIGINT`.
- `/run/ikigenba/telemetry.sock` still exists, and connections made to it after telemetry exited — a sibling's next event among them — wait in the socket's queue for the next telemetry to answer.

## The host stops telemetry while a request outlasts the drain

telemetry waits for accepted requests only until its drain deadline, `DRAIN_SECONDS` after the signal, so that it always exits before the service unit's stop timeout and is never killed mid-write by systemd. A request still running at the deadline — a `search` over a large trail, say — is cut off: its connection is closed without the rest of its response. Losing a request is trouble, so telemetry says how many it lost and exits non-zero. The drain deadline bounds the trail too: storing what is left of its own trail happens inside the same `DRAIN_SECONDS`, so once the requests have used it all, nothing is left to store in, and every event of its own not yet in the store goes to stderr instead, `service.stopping` among them. The trail then ends with the cut-off requests' `request.started` and no `request.finished` for them, which is how it shows a request that never finished.

Command:

```
$ kill -TERM <pid>
```

Output:

```
telemetry: undelivered event: {"time":"<time>","service":"telemetry","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
telemetry: stopped with <n> requests unfinished
```

telemetry exits 1, 5 seconds after the signal. The lines are on stderr; stdout is empty. The output is one or more `telemetry: undelivered event: ` lines and then the `stopped with` line, always last; the `service.stopping` line shown is always among them, but other lines may come before or after it. `<n>` is the number of requests still running at the deadline. When `<n>` is 1 the last line reads `telemetry: stopped with 1 request unfinished`. `<time>` is when telemetry recorded the event. The undelivered lines are every event that was still waiting to be stored at the deadline, `service.stopping` included, in the order telemetry recorded them, followed by any event telemetry records after the deadline, such as a cut-off request's `request.finished`, which is written the same way, never stored, before the last line or not at all.

Preconditions:

- telemetry is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `<n>` of the requests telemetry has accepted are still running 5 seconds after the signal.

Postconditions:

- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off.
- The trail holds no `service.stopping` from this telemetry, and no `request.finished` for any of the `<n>` requests cut off; each of them has its `request.started` in the trail.
- `/run/ikigenba/telemetry.sock` still exists, and connections made to it after telemetry exited wait in the socket's queue for the next telemetry to answer.

## The host restarts telemetry during a deploy

A deploy replaces telemetry's binary and restarts `ikigenba-telemetry.service` alone; `ikigenba-telemetry.socket` stays up throughout. Between the old telemetry exiting and the new one being ready, connections wait in the socket's queue instead of being refused — the events every sibling posts among them — so no sibling sees telemetry missing and no event is turned away for the deploy. That holds because telemetry finishes what it accepted before it exits and leaves the socket where systemd put it.

Command:

```
$ sudo systemctl restart ikigenba-telemetry.service
```

Output:

```
```

Exits 0, once the new telemetry has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- telemetry is serving on `/run/ikigenba/telemetry.sock` under `ikigenba-telemetry.service`.
- Siblings are posting events to `/run/ikigenba/telemetry.sock`, and a client is sending requests to it, throughout the restart.

Postconditions:

- Every request sent was answered, by the old telemetry or the new one; none was refused and none was cut off. Every event a sibling posted is in the store, taken by whichever telemetry answered it.
- A new telemetry process is serving on `/run/ikigenba/telemetry.sock`, over the same `state/telemetry.db`.
- The trail holds the old telemetry's `service.stopping`, with `reason` `SIGTERM`, and after it the new telemetry's `service.started`, whose `version` is the version the new binary's `telemetry --version` prints, so the trail shows the deploy as a new version in a start event. Every request the old telemetry answered is recorded before its `service.stopping`, and every request the new one answered after its `service.started`.

## The host starts telemetry without a socket

Run bare, with no socket passed in, telemetry has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run telemetry correctly.

Command:

```
$ telemetry
```

Output:

```
telemetry: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not telemetry's process id.
- `DRAIN_SECONDS` and `RETENTION_DAYS` are each unset, or a positive whole number.

Postconditions:

- Nothing has changed. telemetry opened no database, listened on nothing, told systemd nothing, and recorded no event; an absent `state/telemetry.db` is still absent.

## The host passes telemetry more than one socket

telemetry serves on exactly one socket. A unit that passes it several is misconfigured, and telemetry will not guess which one it was meant to serve on.

Command:

```
$ telemetry
```

Output:

```
telemetry: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` and `RETENTION_DAYS` are each unset, or a positive whole number.

Postconditions:

- Nothing has changed. telemetry opened no database, served on neither socket, told systemd nothing, and recorded no event.

## The host gives telemetry a drain deadline that is not a number of seconds

telemetry reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment telemetry is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and telemetry serves nothing. The value is quoted back verbatim. telemetry sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce. It checks `DRAIN_SECONDS` before `RETENTION_DAYS`, so it is the one named when both are bad, and before it looks for its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ DRAIN_SECONDS=abc telemetry
```

Output:

```
telemetry: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. telemetry opened no database, served nothing, told systemd nothing, and recorded no event.

## The host gives telemetry a retention window that is not a number of days

telemetry reads `RETENTION_DAYS` before it serves, so a bad value is found at start rather than at the first sweep. A value that is not a positive whole number of days — `0`, `-1`, `2.5`, `15d`, or `abc` — is the caller's mistake, so it is a usage error and telemetry serves nothing: a window it cannot read is not a window it can guess, and sweeping by a guess would lose records. The value is quoted back verbatim. Only an unset or empty variable means the default, 15. telemetry checks it after `DRAIN_SECONDS` and before it looks for its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ RETENTION_DAYS=abc telemetry
```

Output:

```
telemetry: RETENTION_DAYS is 'abc', not a positive whole number of days
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/telemetry` exists and is on the `PATH` as `telemetry`.
- `LISTEN_PID` is telemetry's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. telemetry opened no database, served nothing, told systemd nothing, and recorded no event; no record was swept.
