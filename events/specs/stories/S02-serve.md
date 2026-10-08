# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: events never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names events' own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-events.socket`, which holds the Unix socket `/run/ikigenba/events.sock`, beside `ikigenba-events.service`, which runs `/opt/ikigenba/current/events/bin/events` with no arguments as the `ikigenba` user, with `/var/opt/ikigenba/events` as its working directory and `/etc/opt/ikigenba/events/env` as its environment file; nginx proxies events' public name, `events.<space>`, to `http://unix:/run/ikigenba/events.sock:`, and every other service on the host emits its events straight to that same socket, at `/emit`, never through nginx (`S07`). The service is `Type=notify`: events tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, events drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty (`S15`). On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's environment file, `/etc/opt/ikigenba/<app>/env`, and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

events' environment also carries its six settings, each a positive whole number, and each the manifest's default when it is unset or empty; the host writes the defaults into `/etc/opt/ikigenba/events/env` on every activate, and an operator who changes one there and restarts events has it until the next activate rewrites the file. Only an unset or empty variable means the default. `EVENTS_DEPTH_MAX`, 8, is the deepest an event's chain of causes may go: an event whose `depth` is greater is refused (`S07`). `EVENTS_DELIVERY_TIMEOUT_SECONDS`, 5, a number of seconds, is how long events waits for a subscriber to answer one delivery (`S11`). `EVENTS_DELIVERY_ATTEMPTS`, 10, is how many times events tries to deliver one event to a subscriber that answers with an error before it pauses that subscriber (`S11`). `EVENTS_INFLIGHT_MAX`, 4, is how many deliveries events has in flight at once, across all its subscribers (`S11`). `EVENTS_RETENTION_DAYS`, 2, a number of days, is how long an event stays in the log (`S13`). `EVENTS_DECLARATIONS_SECONDS`, 60, a number of seconds, is how often events asks every enabled service again what it emits and accepts (`S06`). And it carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/run/ikigenba/services.json`, which opsctl sets in the environment the host gives events. The file lists the platform's services: it feeds the launcher in the banner of events' pages (`S03`), it holds the description events' MCP endpoint gives its clients as instructions (`S05`), its enabled entries are the services events asks for their declarations and delivers to (`S06`, `S11`), and its entry named `telemetry` is where events sends its trail (below). events reads the variable once, when it starts, and reads the file it names afresh whenever it needs it, so a rewritten file shows without a restart. events never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, events starts and serves all the same, treats the file as listing no services at start, and says nothing about the file itself; at a later declarations refresh a file it cannot read counts as no answer, so every held declaration stays (`S06`).

events checks its environment first — `DRAIN_SECONDS`, then `EVENTS_DEPTH_MAX`, `EVENTS_DELIVERY_TIMEOUT_SECONDS`, `EVENTS_DELIVERY_ATTEMPTS`, `EVENTS_INFLIGHT_MAX`, `EVENTS_RETENTION_DAYS` and `EVENTS_DECLARATIONS_SECONDS`, in that order — then looks for its socket, and only then opens its SQLite database, the retained log of events and each subscriber's place in it, at `state/events.db`, relative to its working directory. So a start refused as a usage error has touched nothing, not even the database. It creates `state/` if it is absent and `state/events.db` if it is absent, brings the database up to date by applying, in order, every migration it carries that the database has not had (`S01`), sweeps the log once (`S13`), asks every service the services file enables for its declarations (`S06`), and only then serves and tells systemd it is ready. A database it cannot open is a start it refuses, with one line on stderr, `events: cannot open database state/events.db: <reason>`, and exit status 1. A database that records a migration it does not carry is one a newer events has upgraded; events applies nothing to it, warns on stderr that it is ahead, and serves it as usual (below). events is the database's only writer, and the host replicates it (`S17`).

events' environment may also carry `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`, from which it builds `<display>`, the display string `events --version` prints under the same environment (`S01`). events reads them once, when it starts, and shows that string as its version wherever it shows one: in its `service.started` event (below), its pages' footer and its about screen (`S03`), and its MCP `serverInfo` (`S05`). With neither set the string is empty, and events starts and serves all the same.

events keeps a trail: it records what it does as events it sends to the platform's telemetry service, exactly as every app of the platform does, and the events it records are told in `S14`. They are the trail's events, not the bus's: nothing events records in the trail passes through its own log, and an event a sibling emits at `/emit` is kept in the log and never forwarded to telemetry, though events records in its trail what it did with it, its `event.accepted` among them (`S14`). events finds telemetry in the services file `IKIGENBA_SERVICES` names, as the entry named `telemetry`, and sends each event to that entry's socket; it looks the entry up afresh for every event, so a telemetry installed after events started is found without a restart. The stories here show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"events","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when events recorded the event, in UTC to the microsecond, as `2026-10-05T09:14:02.123456Z`; `service` is always `events`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`, each empty when there is none, as for a start or a stop; and `attrs` holds the event's attributes, flat, their keys in alphabetical order. The events of this group are `service.started`, once events is serving and has told systemd it is ready, with `version`, the display string `<display>` (`S01`), and `request.started` and `request.finished` for each request to a page, to `/_appkit/`, or to `/mcp` (`S03`, `S04`, `S05`); a sibling's emit at `/emit` adds neither (`S07`, `S14`). Among the events of a start is also a `sibling.called` for each enabled service events asks for its declarations before it is ready, recorded before `service.started` (`S06`); every event events records, these among them, is told in `S14`. events sends its events one at a time, in the order it recorded them, and an answer never waits for its events to be sent. Telemetry takes an event by answering `204`. When it cannot be reached — the services file is unset, unreadable, or has no `telemetry` entry, or nothing answers on its socket — or it answers anything other than `204` or a `4xx`, events tries the event a few times over a fraction of a second. When it answers `4xx`, it has refused the event itself, and sending it again cannot help, so events does not retry it. Either way events then writes the event to stderr as one line, `events: undelivered event: <event>`, where `<event>` is the JSON telemetry would have received, and carries on serving, so nothing in the trail is lost without trace. A developer whose environment names no services file therefore sees every event on stderr. A developer stands in for telemetry with a services file whose `telemetry` entry names a socket that a listener of their own holds and that takes every event it is sent; a story that says telemetry takes every event means that, or, on a host, the telemetry service itself.

stderr holds only trouble: a condition events cannot go on from — the start-up refusals below and the requests lost to a drain cut short (`S15`) — an event of its trail events could not deliver to telemetry, and the warning that its database is ahead of the binary (below). A failure events handles is not trouble: an emitted event refused, a subscriber that answers with an error or not at all, a subscriber paused, a tool call refused, a path answered `404`, a request answered 500, like a request answered any other way, is recorded in the trail, the log or the tool's result and earns no line on stderr. So while telemetry takes every event, a running events writes nothing to stdout or stderr, and under systemd the journal holds only trouble. Every line events writes to stderr begins `events: `.

These are the terms every app of the platform serves on. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. events' manifest declares no `guests`, so on a host with an authenticator nginx passes events no request from a visitor with no credential: it sends one to sign in at a page (`S03`) and challenges one at `/mcp` (`S05`). The mcp gateway calls events at its socket (`S05`). An emit at `/emit` is the one request that carries no identity: a sibling, not a caller, makes it, and the event it carries names its own request id and user (`S07`). A request that reaches events with no `X-Request-Id`, or an empty one, as a developer's request does, is given an id of the same shape by events, a new one for each such request, so every event about a request names it. events calls its siblings only at their own sockets: for their declarations (`S06`) and to deliver to them (`S11`).

## The host starts events

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/events.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before events starts and while it is stopped; events' part is to serve what arrives on it, the events siblings emitted while it was down included. `systemctl start` returns once events has opened its log, swept it (`S13`), asked its siblings for their declarations (`S06`), and reported that it is ready. At that moment events records `service.started`, with `<display>` as its `version`: a new value there is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-events.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- The active release includes events: `/opt/ikigenba/current/events/bin/events` exists, and `ikigenba-events.socket` and `ikigenba-events.service` are published.
- `/etc/opt/ikigenba/events/env` sets `DRAIN_SECONDS` and each of the six settings to a valid value, and `IKIGENBA_SERVICES` to the host's services file.
- `ikigenba-events.socket` is active, so `/run/ikigenba/events.sock` exists and accepts connections.
- `/var/opt/ikigenba/events/state/events.db` exists, from an earlier start, and records no migration this events does not carry.
- `ikigenba-events.service` is not running.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `ikigenba-events.service` is `active`, and events is serving on `/run/ikigenba/events.sock`: a connection there, and every connection queued before events started, is answered by events.
- events listens on no other socket and no port.
- `/var/opt/ikigenba/events/state/events.db` is the database it opened, now up to date; it existed already, and every event it held that the sweep did not take is still there, as is every subscriber's place in the log (`S10`, `S13`).
- telemetry has received from events a `sibling.called` for each enabled service events asked for its declarations before it was ready (`S06`, `S14`), and after them one `service.started`, with no request id and no user, whose `version` is `<display>`, the string `events --version` prints under the environment the host gives events (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`:

  ```
  {"time":"<time>","service":"events","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- events has written nothing to the journal.
- It keeps running until it is signalled.

## The host starts events for the first time

Nothing of events' state exists yet. events creates the `state/` directory if it is absent, then creates `state/events.db`, applies every migration it carries, and serves with a log that holds no event and no subscriber. The same start succeeds when `state/` already exists and only the database is absent. Neither a fresh deployment nor any other first start needs a directory created beforehand. The paths are relative to events' working directory.

Command:

```
$ events
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so events reports readiness to nobody.
- `DRAIN_SECONDS` and each of the six settings are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry is enabled and names a socket a listener holds that takes every event.
- `state/events.db` does not exist.
- Either `state/` is absent and events can create it in its working directory, or `state/` is an existing directory in which events can create the database.

Postconditions:

- `state/` exists, created by events if it was absent.
- `state/events.db` now exists, created by this start, and is up to date: `events db status` prints `0001 applied <time>`, `<time>` being the moment this start applied it (`S01`). Its log holds no event and it records no subscriber.
- events is serving on the socket it was passed, and on no other.
- telemetry has received from events a `sibling.called` for each enabled service events asked for its declarations before it was ready, the `telemetry` entry's among them (`S06`, `S14`), and after them exactly one `service.started` whose `version` is `<display>`, under an empty request id and an empty user.
- It keeps running until it is signalled.

## The host starts events with no services file

A developer running events at a terminal, or a host whose services file is not there, gives events no list of services. It serves all the same, and every page request and tool call is answered as it would be with the file. What the file would have given is gone: the launcher in its pages' banner (`S03`), the instructions its MCP endpoint gives (`S05`), and the services events asks for their declarations and delivers to, so with no file no service has declared anything, every event emitted to it is refused as undeclared (`S06`, `S07`), and nothing is delivered (`S11`). And with no `telemetry` entry there is nowhere to send the trail, so every event events records goes to stderr as an `undelivered event` line (above); with no services there is no service to ask for declarations, so the first line is `service.started`. A services file that exists but has no entry named `telemetry` sends the trail to stderr the same way, but events asks the services it does enable for their declarations before it is ready, so the `undelivered event` lines of those asks' `sibling.called` records come first, as in `The host starts events where telemetry cannot be reached`.

Command:

```
$ events
```

Output:

```
events: undelivered event: {"time":"<time>","service":"events","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
```

Does not exit. The line is on stderr, written once events is serving; stdout is empty. Every event events records from then on is written the same way, one line each.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the six settings are unset, or valid.
- `IKIGENBA_SERVICES` is unset, or names a file that does not exist.
- `state/events.db` exists, from an earlier start, and records no migration this events does not carry, or can be created as in `The host starts events for the first time`.

Postconditions:

- events is serving on the socket it was passed, and answers every request as it would with a services file: its pages have no launcher, and its MCP endpoint gives no instructions.
- Every event events records is on stderr as an `undelivered event` line; none was sent anywhere.

## The host starts events where its state directory cannot be created

A regular file named `state` occupies the path where events needs its state directory. events has taken its socket, but it reports the database setup failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1.

Command:

```
$ events
```

Output:

```
events: cannot open database state/events.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the six settings are unset, or valid.
- `state` is an existing regular file in events' working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database was created. events served nothing, took no event, delivered nothing, told systemd nothing, and sent telemetry nothing.

## The host starts events with a database it cannot open

`state/events.db` exists but events cannot open it — the file is not writable by the service user, or its contents are not a valid SQLite database. events writes a diagnostic naming the database problem and exits before it serves or reports ready. A missing file is not this error: an absent `state/events.db` is created, empty, as on a first start. events does not rebuild its log from anywhere: the events and the subscribers' places a lost database held come back only from its replica (`S17`). Under systemd the start fails, and `systemctl start` reports it; the socket stays up, and the events siblings emit wait in its queue for an events that can take them.

Command:

```
$ events
```

Output:

```
events: cannot open database state/events.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying open failure.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the six settings are unset, or valid.
- `state/events.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. `state/events.db` is as it was. events served nothing, took no event, delivered nothing, told systemd nothing, and sent telemetry nothing.

## The host starts events with a database a newer events has upgraded

A deploy rolled back to an older binary leaves it over a database a newer events has upgraded: the database records a migration this events does not carry. Data never rolls back, so older code must run on newer data, and this events serves the database as it stands. It applies nothing, not even a migration it carries that the database lacks, and writes one line to stderr naming the lowest version it does not carry, zero-padded to four digits as `events db status` prints it; on a host that line goes to the journal. Then it goes on as in any start: it sweeps the log (`S13`), asks its siblings for their declarations (`S06`), serves, and tells systemd it is ready. The warning is a line on stderr only, not an event in the trail. `events db status` lists every version the database records, the unknown ones as `unknown` (`S01`).

Command:

```
$ events
```

Output:

```
events: unknown migration version 0002: database is ahead of this binary
```

Does not exit. The line is on stderr, written before events serves; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`, carrying only migration `0001`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and each of the six settings are unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry takes every event.
- `state/events.db` exists and records versions `0001` and `0002` as applied.

Postconditions:

- The database still records `0001` and `0002`, and no other version; this start applied nothing. Every event it held that the sweep did not take is still there, as is every subscriber's place in the log (`S10`, `S13`).
- events is serving on the socket it was passed, over that `state/events.db`.
- telemetry has received events' `service.started`, whose `version` is `<display>`, and no event about the warning.
- It keeps running until it is signalled.

## The host restarts events during a deploy

A deploy replaces events' binary and restarts `ikigenba-events.service` alone; `ikigenba-events.socket` stays up throughout. Between the old events exiting and the new one being ready, connections wait in the socket's queue instead of being refused — the events every sibling emits among them — so no sibling sees events missing and no event is turned away for the deploy. That holds because events finishes what it accepted before it exits and leaves the socket where systemd put it (`S15`). The log outlives the deploy: the new events opens the same `state/events.db`, bringing it up to date first when the new binary carries a migration the database has not had, keeps every event the old one took and every subscriber's place in the log, and delivers on from each subscriber's cursor (`S11`); a delivery the old events had not finished when it stopped is made again by the new one (`S15`).

Command:

```
$ sudo systemctl restart ikigenba-events.service
```

Output:

```
```

Exits 0, once the new events has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- events is serving on `/run/ikigenba/events.sock` under `ikigenba-events.service`.
- Siblings are emitting events to `/run/ikigenba/events.sock`, each one an event its producer has declared (`S06`), with a `depth` within `EVENTS_DEPTH_MAX` (`S07`), and users and the gateway are sending requests to it, throughout the restart, and every request the old events accepted finishes at least a second before its drain deadline.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- Every request sent was answered, by the old events or the new one; none was refused and none was cut off. Every event a sibling emitted is in the log, taken by whichever events answered it, with the next `seq` after the ones before it.
- A new events process is serving on `/run/ikigenba/events.sock`, over the same `state/events.db`; every subscriber's cursor is where the old events left it, or further on.
- telemetry has received the old events' `service.stopping`, with `reason` `SIGTERM`, and after it the new events' `service.started`, whose `version` is the `<display>` of the new events' environment. Every request the old events answered, but for an emit at `/emit`, which adds no request events (`S14`), is recorded before its `service.stopping`, and every such request the new one answered after its `service.started`.

## The host starts events where telemetry cannot be reached

The trail is not a reason to stop serving. When events cannot deliver its trail — telemetry is not installed yet, is stopped, or the services file names no `telemetry` entry — events starts and serves exactly as it does otherwise, and its trail's events go to the journal as `undelivered event` lines (above). No answer, no emit and no delivery waits on telemetry, so a user, the gateway, an emitting sibling and a subscriber see no difference. events keeps looking for telemetry with every event, so once telemetry takes events again, events' next events go to it without a restart; an event already written to the journal is not sent again.

Command:

```
$ sudo systemctl start ikigenba-events.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- The active release includes events, and `ikigenba-events.socket` is active, as in `The host starts events`.
- `ikigenba-events.service` is not running.
- The host's services file lists no `telemetry` entry, or nothing accepts connections on the socket that entry names.

Postconditions:

- `ikigenba-events.service` is `active`, and events is serving on `/run/ikigenba/events.sock`, as in `The host starts events`.
- The journal holds an `undelivered event` line for each `sibling.called` events recorded asking the enabled services for their declarations before it was ready (`S06`, `S14`), and after them this line, written after it reported that it was ready:

  ```
  events: undelivered event: {"time":"<time>","service":"events","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- Every event events records in its trail while telemetry cannot be reached is written to the journal the same way, one line each, and every request is answered, every emitted event taken or refused, and every delivery made, as it would be with telemetry taking events.

## The host starts events without a socket

Run bare, with no socket passed in, events has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run events correctly.

Command:

```
$ events
```

Output:

```
events: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not events' process id.
- `DRAIN_SECONDS` and each of the six settings are unset, or valid.

Postconditions:

- Nothing has changed. events opened no database, listened on nothing, told systemd nothing, and sent telemetry nothing; an absent `state/events.db` is still absent.

## The host passes events more than one socket

events serves on exactly one socket. A unit that passes it several is misconfigured, and events will not guess which one it was meant to serve on.

Command:

```
$ events
```

Output:

```
events: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` and each of the six settings are unset, or valid.

Postconditions:

- Nothing has changed. events opened no database, served on neither socket, told systemd nothing, and sent telemetry nothing.

## The host gives events a drain deadline that is not a number of seconds

events reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment events is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and events serves nothing. The value is quoted back verbatim. events sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce. It checks `DRAIN_SECONDS` before each of its six settings, so it is the one named when several are bad, and before it looks for its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ DRAIN_SECONDS=abc events
```

Output:

```
events: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. events opened no database, served nothing, told systemd nothing, and sent telemetry nothing.

## The host gives events a depth limit that is not a number of levels

events reads `EVENTS_DEPTH_MAX` before it serves, so a bad value is found at start rather than at the first event that carries a cause. A value that is not a positive whole number of levels — `0`, `-1`, `2.5`, `8x`, or `abc` — is the caller's mistake, so it is a usage error and events serves nothing. The value is quoted back verbatim. events checks it after `DRAIN_SECONDS` and before its other settings and its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ EVENTS_DEPTH_MAX=abc events
```

Output:

```
events: EVENTS_DEPTH_MAX is 'abc', not a positive whole number of levels
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. events opened no database, served nothing, took no event, told systemd nothing, and sent telemetry nothing.

## The host gives events a delivery timeout that is not a number of seconds

events reads `EVENTS_DELIVERY_TIMEOUT_SECONDS` before it serves, so a bad value is found at start rather than at the first delivery. A value that is not a positive whole number of seconds — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and events serves nothing. The value is quoted back verbatim. events checks it after `DRAIN_SECONDS` and `EVENTS_DEPTH_MAX` and before its other settings and its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ EVENTS_DELIVERY_TIMEOUT_SECONDS=abc events
```

Output:

```
events: EVENTS_DELIVERY_TIMEOUT_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and `EVENTS_DEPTH_MAX` are each unset, or a positive whole number.

Postconditions:

- Nothing has changed. events opened no database, served nothing, delivered nothing, told systemd nothing, and sent telemetry nothing.

## The host gives events a delivery attempt limit that is not a number of attempts

events reads `EVENTS_DELIVERY_ATTEMPTS` before it serves, so a bad value is found at start rather than when a subscriber first answers with an error. A value that is not a positive whole number of attempts — `0`, `-1`, `2.5`, `10x`, or `abc` — is the caller's mistake, so it is a usage error and events serves nothing. The value is quoted back verbatim. events checks it after `DRAIN_SECONDS`, `EVENTS_DEPTH_MAX` and `EVENTS_DELIVERY_TIMEOUT_SECONDS` and before its other settings and its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ EVENTS_DELIVERY_ATTEMPTS=abc events
```

Output:

```
events: EVENTS_DELIVERY_ATTEMPTS is 'abc', not a positive whole number of attempts
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and the two settings checked before this one are each unset, or a positive whole number.

Postconditions:

- Nothing has changed. events opened no database, served nothing, delivered nothing, told systemd nothing, and sent telemetry nothing.

## The host gives events an in-flight limit that is not a number of deliveries

events reads `EVENTS_INFLIGHT_MAX` before it serves, so a bad value is found at start rather than at the first delivery. A value that is not a positive whole number of deliveries — `0`, `-1`, `2.5`, `4x`, or `abc` — is the caller's mistake, so it is a usage error and events serves nothing. The value is quoted back verbatim. events checks it after `DRAIN_SECONDS`, `EVENTS_DEPTH_MAX`, `EVENTS_DELIVERY_TIMEOUT_SECONDS` and `EVENTS_DELIVERY_ATTEMPTS` and before `EVENTS_RETENTION_DAYS`, `EVENTS_DECLARATIONS_SECONDS` and its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ EVENTS_INFLIGHT_MAX=abc events
```

Output:

```
events: EVENTS_INFLIGHT_MAX is 'abc', not a positive whole number of deliveries
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and the three settings checked before this one are each unset, or a positive whole number.

Postconditions:

- Nothing has changed. events opened no database, served nothing, delivered nothing, told systemd nothing, and sent telemetry nothing.

## The host gives events a retention window that is not a number of days

events reads `EVENTS_RETENTION_DAYS` before it serves, so a bad value is found at start rather than at the first sweep. A value that is not a positive whole number of days — `0`, `-1`, `2.5`, `2d`, or `abc` — is the caller's mistake, so it is a usage error and events serves nothing: a window it cannot read is not a window it can guess, and sweeping by a guess would lose events. The value is quoted back verbatim. events checks it after `DRAIN_SECONDS`, `EVENTS_DEPTH_MAX`, `EVENTS_DELIVERY_TIMEOUT_SECONDS`, `EVENTS_DELIVERY_ATTEMPTS` and `EVENTS_INFLIGHT_MAX` and before `EVENTS_DECLARATIONS_SECONDS` and its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ EVENTS_RETENTION_DAYS=abc events
```

Output:

```
events: EVENTS_RETENTION_DAYS is 'abc', not a positive whole number of days
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and the four settings checked before this one are each unset, or a positive whole number.

Postconditions:

- Nothing has changed. events opened no database, served nothing, told systemd nothing, and sent telemetry nothing; no event was swept.

## The host gives events a declarations refresh interval that is not a number of seconds

events reads `EVENTS_DECLARATIONS_SECONDS` before it serves, so a bad value is found at start rather than when it first asks its siblings what they emit and accept. A value that is not a positive whole number of seconds — `0`, `-1`, `2.5`, `60s`, or `abc` — is the caller's mistake, so it is a usage error and events serves nothing. The value is quoted back verbatim. events checks it last of its settings, and before it looks for its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ EVENTS_DECLARATIONS_SECONDS=abc events
```

Output:

```
events: EVENTS_DECLARATIONS_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` and the five settings checked before this one are each unset, or a positive whole number.

Postconditions:

- Nothing has changed. events opened no database, served nothing, asked no sibling for its declarations, told systemd nothing, and sent telemetry nothing.
