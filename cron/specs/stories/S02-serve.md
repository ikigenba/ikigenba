# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: cron never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names cron's own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-cron.socket`, which holds the Unix socket `/run/ikigenba/cron.sock`, beside `ikigenba-cron.service`, which runs `/opt/cron/bin/cron` with no arguments as the `ikigenba` user, with `/opt/cron` as its working directory and `/opt/cron/etc/env` as its environment file; nginx proxies cron's public name, `cron.<space>`, to `http://unix:/run/ikigenba/cron.sock:`. The service is `Type=notify`: cron tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, cron drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty. On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's `etc/env` and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

cron has no settings of its own: its manifest has no `[env]` (`S01`), and `DRAIN_SECONDS` is the only value it checks in its environment. Its environment also carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/var/lib/ikigenba/services.json`, which opsctl sets in the environment the host gives cron. The file lists the platform's services: it feeds the launcher in the banner of cron's pages (`S03`), it holds the description cron's MCP endpoint gives its clients as instructions (`S05`), its entry named `telemetry` is where cron sends its trail, and its entry named `events` is where cron emits to the event bus (both below). cron reads the variable once, when it starts, and reads the file it names afresh whenever it needs it, so a rewritten file shows without a restart. cron never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, cron starts and serves all the same, treats the file as listing no services, and says nothing about the file itself.

cron's environment may also carry `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`, from which it builds `<display>`, the display string `cron --version` prints under the same environment (`S01`). cron reads them once, when it starts, and shows that string as its version wherever it shows one: in its `service.started` event (below), its pages' footer and its about screen (`S03`), and its MCP `serverInfo` (`S05`). With neither set the string is empty, and cron starts and serves all the same.

cron checks `DRAIN_SECONDS` first, then looks for its socket, and only then opens its SQLite database, the triggers, at `state/cron.db`, relative to its working directory, creating `state/` and the database on its first start and bringing the database up to date by applying, in order, every migration it carries that the database has not had (`S01`). Then it works out, for every active trigger, the next slot its schedule calls for after now; a slot that passed while no cron was running is gone, and the start fires nothing for it (`S11`). Then it is ready, and from then on it fires each trigger as `S11` tells. So a start refused as a usage error has touched nothing, not even the database. A database cron cannot open is a start it refuses, with one line on stderr, `cron: cannot open database state/cron.db: <reason>`, and exit status 1. A database that records a migration it does not carry is one a newer cron has upgraded; cron applies nothing to it, warns on stderr that it is ahead, and goes on with its start as usual (below). cron is the database's only writer, and the host replicates it as the manifest declares (`S01`).

cron keeps a trail: it records what it does as events it sends to the platform's telemetry service, where an operator, or an agent working for one, follows what happened from one thing they know — a request id, a user, a trigger's id, or a time. cron finds telemetry in the services file `IKIGENBA_SERVICES` names, as the entry named `telemetry`, and sends each event to that entry's socket; it looks the entry up afresh for every event, so a telemetry installed after cron started is found without a restart. What telemetry does with an event is told in telemetry's own stories. The stories here show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"cron","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when cron recorded the event, in UTC to the microsecond, as `2026-10-05T09:32:00.123456Z`; `service` is always `cron`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`, each empty for an event no request caused — a start or a stop — except that a fire, which no request causes, carries a request id cron mints for it and the trigger's owner's user id (`S11`); and `attrs` holds the event's attributes, flat, their keys in alphabetical order. Each event has a fixed set of attribute keys, and they hold ids, times, and a trigger's schedule, never anything else: no event carries a caller's or owner's email, a credential, a request's query, or a tool's arguments beyond the schedule a trigger's own events name as `when`. A trigger is named in an attribute by its id, under the key `trigger`, and its slug reaches the trail as the middle word of its events' names, `cron.hourly.fired` say. cron sends its events one at a time, in the order it recorded them, and an answer never waits for its events to be sent. Telemetry takes an event by answering `204`. When it cannot be reached — the services file is unset, unreadable, or has no `telemetry` entry, or nothing answers on its socket — or it answers anything other than `204` or a `4xx`, cron tries the event a few times over a fraction of a second. When it answers `4xx`, it has refused the event itself, and sending it again cannot help, so cron does not retry it. Either way cron then writes the event to stderr as one line, `cron: undelivered event: <event>`, where `<event>` is the JSON telemetry would have received, and carries on serving, so nothing in the trail is lost without trace. A developer whose environment names no services file therefore sees every event on stderr. A developer stands in for telemetry with a services file whose `telemetry` entry names a socket that a listener of their own holds and that takes every event it is sent; a story that says telemetry takes every event means that, or, on a host, the telemetry service itself.

cron records these events and no others (`S12`):

- `service.started`, once cron is serving and has told systemd it is ready, with `version`, `<display>`, the string `cron --version` prints under the environment cron was started with (`S01`);
- `service.stopping`, when cron is told to stop, once every request it accepted has finished, or at the drain deadline for any still running, with `reason`, the name of the signal that stopped it, `SIGTERM` or `SIGINT`; it is the last event cron records, except the `event.lost` of a bus event dropped when a stop's drain ends (`S13`) and the `request.finished` of a request cut off at the drain deadline, which follow it;
- `request.started`, as each request arrives, with `method` and `path`, the request's URL path without its query;
- `request.finished`, once that request's answer is complete, with `status`, the status of cron's answer; `duration_us`, how long cron took to answer, in whole microseconds; `request_bytes`, how many bytes of the request's body cron read; and `response_bytes`, how many bytes of the response's body cron wrote; the three vary from request to request, and a story's event JSON shows `duration_us` as `<n>` and the two byte counts as `<bytes>` unless it fixes them;
- `tool.called`, for each call of one of its seven tools that is answered with a result, with `tool`, `kind`, `outcome`, and `duration_us` (`S05`);
- `cron.<slug>.created`, `cron.<slug>.paused`, `cron.<slug>.resumed`, and `cron.<slug>.deleted`, when `create`, `pause`, `resume`, or `delete` changes the trigger `<slug>`, each with `trigger` and `when` (`S06`, `S09`, `S10`); `update` records none (`S08`);
- `cron.<slug>.fired`, each time the trigger `<slug>` fires, with `scheduled`, `trigger`, and `when` (`S11`);
- `event.lost`, when cron drops an event it emitted to the event bus because the bus did not take it within its retry window (below), carrying the lost event's `id` among its attributes.

The events of a request carry its request id and its caller, and come in this order: `request.started`, then the request's trigger event, if it made one, then `tool.called` for a tool call, then `request.finished`. A fire is no request: its trail is its one `cron.<slug>.fired` (`S11`).

cron also emits events to the suite's event bus, the `events` app, which delivers them to the services that accept them; it is how a trigger reaches whatever an agent has subscribed to it. Every trigger event cron records in the trail it also emits to the bus, and no other: `cron.<slug>.created`, `cron.<slug>.paused`, `cron.<slug>.resumed`, `cron.<slug>.deleted`, and `cron.<slug>.fired`, each with the same name, the same attributes, and the same request id and user as the record in the trail. The trail's record is unchanged; the bus event is in addition to it. A tool call that changes nothing, or is refused, emits nothing to the bus. An agent sees a bus event through the gateway and the events app's own tools, with `id`, the event's own id, beginning `evt_`, which cron gives it; `service` `cron`; `event`, its name; `attrs` holding its attributes; `request_id` and `user`, as in the trail; and `cause` and `depth`. A tool call sets the `cause` and `depth` of the event it emits with two headers on its request to `/mcp`: when that request carries both `X-Event-Cause`, an event id, `evt_` followed by 16 lowercase hexadecimal digits, and `X-Event-Depth`, a non-negative whole decimal number, the event has that id as its `cause` and that number plus one as its `depth`. When either header is absent or malformed, cron ignores the pair as a whole: `cause` is empty, `depth` is `0`, and the call is otherwise unaffected. A fire's event has an empty `cause` and `depth` `0`. Neither header changes the trail's record. What else the events app shows of an event is its own. No answer waits for the bus, and no fire waits for it either. When the events app cannot take an event — it is not installed, is stopped, or cannot be reached — cron keeps the event and delivers it once the events app takes events again. It sends queued events one at a time, in order, and an event's retry window, about five minutes, runs from its first delivery attempt, so an event behind another waits its turn and is kept longer; when the events app returns, the event at the head goes through and the rest follow at once. An event the events app has not taken by the end of its window is dropped, and cron records `event.lost` in the trail, carrying the dropped event's `id` among its attributes; a fire whose event is dropped still counts as the trigger's latest fire (`S11`). When cron stops, it goes on delivering the bus events it holds within the drain, and drops any the events app has not taken when the drain ends the same way, with its `event.lost` (`S13`). Either way cron serves exactly as it does otherwise, and a user or the gateway sees no difference.

The events app learns what cron emits from cron itself, at `/declarations` on cron's socket (`The events app asks cron what it declares`). cron declares each of its five events by a pattern, its middle word `*`, since the slugs are users' to choose: `cron.*.created`, `cron.*.paused`, `cron.*.resumed`, and `cron.*.deleted`, each with the attribute names `trigger` and `when`, and `cron.*.fired`, with `trigger`, `when`, and `scheduled`. cron accepts no event, so the events app delivers it none; what cron answers at `/events` is the events app's contract, and no story here fixes it.

stderr holds only trouble: a condition cron cannot go on from — the start-up refusals below and the requests lost to a drain cut short — an event cron could not deliver to telemetry, a bus event cron dropped, and a database ahead of the binary, which cron warns of once as it starts and serves all the same (below). A failure cron handles is not trouble: a tool call refused, a path answered `404`, a request answered 500, like a request answered any other way, is recorded in the trail or the tool's result and earns no line on stderr. So while telemetry and the event bus take every event and its database is not ahead of it, a running cron writes nothing to stdout or stderr, and under systemd the journal holds only trouble. cron writes one line to stderr for each bus event it drops; its text beyond the `cron: ` prefix is not fixed, and a bus event the events app takes late earns none. Every line cron writes to stderr begins `cron: `.

These are the terms every app of the platform serves on. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. cron's manifest declares `guests = false` (`S01`), so on a host with an authenticator nginx passes cron no request from a visitor with no credential: it sends one to sign in at a page (`S03`) and challenges one at `/mcp` (`S05`). The mcp gateway calls cron at its socket (`S05`); the events app reaches cron at its socket too, on `/events` and `/declarations`, which take no identity, and which cron's nginx fragment keeps from browsers (`S14`). A request that reaches cron with no `X-Request-Id`, or an empty one, as a developer's request does, is given an id of the same shape by cron, a new one for each such request, so every event about a request names it. cron calls no sibling while serving a request: the events it sends to telemetry and to the event bus are sent apart from the request, and no answer waits for them.

## The host starts cron

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/cron.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before cron starts and while it is stopped; cron's part is to serve what arrives on it. `systemctl start` returns once cron has opened its database, worked out each active trigger's next slot, and reported that it is ready. At that moment cron records `service.started`, with `<display>` as its `version`: a new value there is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-cron.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed cron: `/opt/cron/bin/cron` exists, and `ikigenba-cron.socket` and `ikigenba-cron.service` are published.
- `/opt/cron/etc/env` sets `DRAIN_SECONDS` to a valid value, and `IKIGENBA_SERVICES` to the host's services file.
- `ikigenba-cron.socket` is active, so `/run/ikigenba/cron.sock` exists and accepts connections.
- `/opt/cron/state/cron.db` exists, from an earlier start, records no migration this cron does not carry, and holds the space's triggers (`S06`): `hourly`, `month_end`, `nightly_backup`, and `weekly_digest`, the last of them paused.
- `ikigenba-cron.service` is not running, and it is `2026-10-05T09:32:00Z`.
- The host's services file lists the telemetry service, which takes every event, and the events app, which takes every event.

Postconditions:

- `ikigenba-cron.service` is `active`, and cron is serving on `/run/ikigenba/cron.sock`: a connection there, and every connection queued before cron started, is answered by cron.
- cron listens on no other socket and no port.
- `/opt/cron/state/cron.db` is the database it opened, now up to date, and every trigger it held is still there, with the same id, slug, schedule, owner, status, and last fire. Each active trigger's next slot is the first its schedule calls for after the start: `show` answers `next` `2026-10-05T10:00:00Z` for `hourly`, `2026-11-01T00:00:00Z` for `month_end`, and `2026-10-06T02:30:00Z` for `nightly_backup`, and no `next` for `weekly_digest`, which is paused (`S07`).
- The start fired no trigger and emitted nothing to the event bus.
- telemetry has received one event from cron, with no request id and no user, whose `version` is `<display>`, the string `cron --version` prints under the environment the host gives cron (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`:

  ```
  {"time":"<time>","service":"cron","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- cron has written nothing to the journal.
- It keeps running until it is signalled.

## The host starts cron for the first time

Nothing of cron's state exists yet. cron creates the `state/` directory if it is absent, then creates `state/cron.db` and applies every migration it carries, and serves with no trigger. The same start succeeds when `state/` already exists and only the database is absent. Neither a fresh deployment nor any other first start needs a directory created beforehand. The paths are relative to cron's working directory.

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`.
- `LISTEN_PID` is cron's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `NOTIFY_SOCKET` is unset, so cron reports readiness to nobody.
- `DRAIN_SECONDS` is unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- `state/cron.db` does not exist.
- Either `state/` is absent and cron can create it in its working directory, or `state/` is an existing directory in which cron can create the database.

Postconditions:

- `state/` exists, created by cron if it was absent.
- `state/cron.db` now exists, created by this start, and is up to date: `cron db status` prints `0001 applied <time>`, `<time>` being the moment this start applied it (`S01`). It holds no trigger: `list` answers `{"triggers":[]}` for every caller (`S07`).
- cron is serving on the socket it was passed, and on no other.
- telemetry has received exactly one event from cron, its `service.started` with `version` `<display>`, under an empty request id and an empty user.
- It keeps running until it is signalled.

## The host starts cron with no services file

A developer running cron at a terminal, or a host whose services file is not there, gives cron no list of services. It serves all the same, and every request and tool call is answered as it would be with the file, and every trigger fires as it would (`S11`). What the file would have given is gone: the launcher in its pages' banner (`S03`), the instructions its MCP endpoint gives (`S05`), the trail's destination, and the bus's. With no `telemetry` entry there is nowhere to send the trail, so every event cron records goes to stderr as an `undelivered event` line (above), starting with `service.started`. With no `events` entry the events app cannot be reached, so each bus event cron emits is kept and, at the end of its retry window, dropped with its `event.lost` (above). A services file that exists but has no entry named `telemetry`, or none named `events`, is the same for the trail, or for the bus.

Command:

```
$ cron
```

Output:

```
cron: undelivered event: {"time":"<time>","service":"cron","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
```

Does not exit. The line is on stderr, written once cron is serving; stdout is empty. Every event cron records from then on is written the same way, one line each.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`.
- `LISTEN_PID` is cron's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or valid.
- `IKIGENBA_SERVICES` is unset, or names a file that does not exist.
- `state/cron.db` exists, from an earlier start, or can be created as in `The host starts cron for the first time`.

Postconditions:

- cron is serving on the socket it was passed, and answers every request as it would with a services file: its pages have no launcher, and its MCP endpoint gives no instructions.
- Every event cron records is on stderr as an `undelivered event` line; none was sent anywhere.

## The host starts cron where its state directory cannot be created

A regular file named `state` occupies the path where cron needs its state directory. cron has taken its socket, but it reports the database setup failure and exits before it serves or reports ready. The failure is not the caller's usage, so it exits 1.

Command:

```
$ cron
```

Output:

```
cron: cannot open database state/cron.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying directory-creation failure.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`.
- `LISTEN_PID` is cron's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or valid.
- `state` is an existing regular file in cron's working directory.

Postconditions:

- The existing `state` file is unchanged.
- No database was created. cron served nothing, fired nothing, told systemd nothing, sent telemetry nothing, and emitted nothing to the bus.

## The host starts cron with a database it cannot open

`state/cron.db` exists but cron cannot open it — the file is not writable by the service user, or its contents are not a valid SQLite database. cron writes a diagnostic naming the database problem and exits before it serves or reports ready. A missing file is not this error: an absent `state/cron.db` is created, empty, as on a first start. cron does not rebuild its triggers from anywhere: the triggers a lost database held come back only from its replica. Under systemd the start fails, and `systemctl start` reports it; the socket stays up, and the gateway's calls wait in its queue for a cron that can answer them. No trigger fires while no cron is serving.

Command:

```
$ cron
```

Output:

```
cron: cannot open database state/cron.db: <reason>
```

Exits 1. The line is on stderr; stdout is empty. `<reason>` is the underlying open failure.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`.
- `LISTEN_PID` is cron's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or valid.
- `state/cron.db` exists but cannot be opened.

Postconditions:

- Nothing has changed. `state/cron.db` is as it was. cron served nothing, fired nothing, told systemd nothing, sent telemetry nothing, and emitted nothing to the bus.

## The host starts cron with a database a newer cron has upgraded

A deploy rolled back to an older binary leaves it over a database a newer cron has upgraded: the database records a migration this cron does not carry. Data never rolls back, so older code must run on newer data, and this cron serves the database as it stands. It applies nothing, not even a migration it carries that the database lacks, and writes one line to stderr naming the lowest version it does not carry, zero-padded to four digits as `cron db status` prints it; on a host that line goes to the journal. Then it goes on with its start as any start does — working out each active trigger's next slot — serves, tells systemd it is ready, and fires its triggers (`S11`). The warning is a line on stderr only, not an event in the trail. `cron db status` lists every version the database records, the unknown ones as `unknown` (`S01`).

Command:

```
$ cron
```

Output:

```
cron: unknown migration version 0002: database is ahead of this binary
```

Does not exit. The line is on stderr, written before cron serves; stdout is empty.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`, carrying only migration `0001`.
- `LISTEN_PID` is cron's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, or valid.
- `IKIGENBA_SERVICES` names a services file whose `telemetry` entry names a socket a listener holds that takes every event.
- `state/cron.db` exists and records versions `0001` and `0002` as applied, and holds no trigger.

Postconditions:

- The database still records `0001` and `0002`, and no other version; this start applied nothing.
- cron is serving on the socket it was passed, over that `state/cron.db`: `list` answers `{"triggers":[]}` for every caller (`S07`).
- telemetry has received exactly one event from cron, its `service.started` with `version` `<display>`, under an empty request id and an empty user, and no event about the warning.
- The line above is the only one cron has written to stderr.
- It keeps running until it is signalled.

## The host starts cron where telemetry cannot be reached

The trail is not a reason to stop serving. When cron cannot deliver its events to telemetry — telemetry is not installed yet, is stopped, or the services file names no `telemetry` entry — cron starts, serves, and fires exactly as it does otherwise, and its events go to the journal as `undelivered event` lines (above). No answer and no fire waits on telemetry, so the gateway and the event bus see no difference. cron keeps looking for telemetry with every event, so once telemetry takes events again, cron's next events go to it without a restart; an event already written to the journal is not sent again.

Command:

```
$ sudo systemctl start ikigenba-cron.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed cron, and `ikigenba-cron.socket` is active, as in `The host starts cron`.
- `ikigenba-cron.service` is not running.
- The host's services file lists no `telemetry` entry, or nothing accepts connections on the socket that entry names.

Postconditions:

- `ikigenba-cron.service` is `active`, and cron is serving on `/run/ikigenba/cron.sock`, as in `The host starts cron`.
- The journal holds one line from cron, written after it reported that it was ready:

  ```
  cron: undelivered event: {"time":"<time>","service":"cron","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- Every event cron records while telemetry cannot be reached — every `cron.<slug>.fired` among them — is written to the journal the same way, one line each, every request is answered as it would be with telemetry taking events, and every bus event is emitted to the events app as it would be.

## The host starts cron without a socket

Run bare, with no socket passed in, cron has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run cron correctly.

Command:

```
$ cron
```

Output:

```
cron: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not cron's process id.
- `DRAIN_SECONDS` is unset, or valid.

Postconditions:

- Nothing has changed. cron opened no database, listened on nothing, fired nothing, told systemd nothing, sent telemetry nothing, and emitted nothing to the bus; an absent `state/cron.db` is still absent.

## The host passes cron more than one socket

cron serves on exactly one socket. A unit that passes it several is misconfigured, and cron will not guess which one it was meant to serve on.

Command:

```
$ cron
```

Output:

```
cron: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`.
- `LISTEN_PID` is cron's process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` is unset, or valid.

Postconditions:

- Nothing has changed. cron opened no database, served on neither socket, fired nothing, told systemd nothing, sent telemetry nothing, and emitted nothing to the bus.

## The host gives cron a drain deadline that is not a number of seconds

cron reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment cron is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and cron serves nothing. The value is quoted back verbatim. cron sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce. It checks `DRAIN_SECONDS` before it looks for its socket, so it is reported whether or not a socket was passed in.

Command:

```
$ DRAIN_SECONDS=abc cron
```

Output:

```
cron: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/cron` exists and is on the `PATH` as `cron`.
- `LISTEN_PID` is cron's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. cron opened no database, served nothing, fired nothing, told systemd nothing, sent telemetry nothing, and emitted nothing to the bus.

## The events app asks cron what it declares

The events app learns what cron emits by asking it, on cron's socket, when the events app starts and every so often after (events' `S06-declarations.md`). cron answers with its declaration: the five patterns its events' names follow, sorted by pattern, each with the attribute names that event carries, in the order cron declares them, and no event it accepts. The answer is the same whatever triggers exist, since a pattern covers every slug, and it takes no identity: the events app sends none, and cron answers it all the same.

Request:

```
GET /declarations HTTP/1.1
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is cron's declaration:

```
{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]}],"accepts":[]}
```

Preconditions:

- cron is serving, over the space's triggers (`S06`), or over none.
- The request is made on cron's socket, as the events app makes it, with no `X-User-Id` and no `X-User-Email`.

Postconditions:

- Nothing has changed. No trigger fired and nothing was emitted to the bus.
- The events app, holding this declaration, accepts `cron.hourly.fired`, and any other name of the five shapes, from cron, and lists cron under each pattern in its catalog (events' `S06-declarations.md`, `S08-catalog.md`).

## The host stops cron

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which cron treats the same way. cron stops taking new connections, finishes the requests it has already accepted, and exits. cron closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and cron never removes `/run/ikigenba/cron.sock`. It waits at most `DRAIN_SECONDS` for requests to finish, which on a host is always less than the time the service unit allows before systemd kills it. Once every request it accepted has finished, and each has recorded its `request.finished`, or at the drain deadline for any still running, cron records `service.stopping` with the reason it is stopping, the name of the signal it received, `SIGTERM` or `SIGINT`. That is the last event of its trail, except the `event.lost` of a bus event dropped when the drain ends (`S13`) and the `request.finished` of a request cut off at the drain deadline (`The host stops cron while a request outlasts the drain`), which follow it: cron sends everything it recorded before exiting, within the same drain deadline. Sending takes time, so a stop is silent when the requests leave cron at least a second of the drain for it; a request that finishes later still gets its whole response, but an event cron has not sent when the deadline comes goes to stderr as an `undelivered event` line instead. A `service.started` with no `service.stopping` before the next one is how the trail shows a cron that died rather than stopped. A slot that falls due once stopping has begun does not fire (`S13`). What becomes of bus events cron still holds at the signal is `S13`'s too; in this story the events app has taken every bus event and no slot falls due.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

cron exits 0. Nothing is on stdout or stderr.

Preconditions:

- cron is serving as process `<pid>`, on the socket it was passed, over the space's triggers (`S06`).
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `u_7f3a9c21`'s `create` of `crm_sync` with `when` `*/15 * * * *` has arrived at the signal and has not yet been answered.
- That call, and every other request cron has accepted, finishes at least a second before the drain deadline, within 4 seconds of the signal, so cron has that second left to send what it recorded.
- No trigger's next slot falls between the signal and cron's exit.
- telemetry takes every event as soon as it is sent, and so does the events app.

Postconditions:

- Every request accepted before the signal received its full response. The create succeeded: its result is `crm_sync`'s trigger (`S06`), and `show` of `crm_sync` answers it once a cron is serving again (`S07`).
- The events app has taken the create's `cron.crm_sync.created` (`S06`).
- telemetry has received every event cron recorded: the create's `cron.crm_sync.created`, its `tool.called`, and its `request.finished` with `status` `200`; and last, after the `request.finished` of every request accepted before the signal:

  ```
  {"time":"<time>","service":"cron","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  With `SIGINT` the `reason` is `SIGINT`.
- `/run/ikigenba/cron.sock` still exists, and connections made to it after cron exited wait in the socket's queue for the next cron to answer.

## The host stops cron while a request outlasts the drain

cron waits for accepted requests only until its drain deadline, `DRAIN_SECONDS` after the signal, so that it always exits before the service unit's stop timeout and is never killed mid-write by systemd. A request still running at the deadline — the landing page sent to a client that is still reading it, say — is cut off: cron closes its connection without the rest of the response. Losing a request is trouble, so cron says how many it lost and exits non-zero.

The drain has used the whole deadline, so cron has no time left to send `service.stopping`, or any other event not yet sent, to telemetry: each goes to stderr as an `undelivered event` line instead. telemetry never receives a `request.finished` for a request cut off: its `request.started` with no finish is how the trail shows it was cut off.

Command:

```
$ kill -TERM <pid>
```

Output:

```
cron: undelivered event: {"time":"<time>","service":"cron","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
cron: stopped with <n> requests unfinished
```

cron exits 1, 5 seconds after the signal. The text is on stderr; stdout is empty. `<n>` is the number of requests still running at the deadline. When `<n>` is 1 the line reads `cron: stopped with 1 request unfinished`. The `service.stopping` line above is always written. Any other event cron had recorded and not yet delivered to telemetry when the deadline came is written as an `undelivered event` line too; those lines are in the order the events were recorded, `service.stopping` last of them. The position of the `stopped with` line among them is not fixed. stderr may also hold, for a cut-off request, an `undelivered event` line carrying its `request.finished`, recorded after the deadline and before cron exited; whether it does is not fixed, and when it does, that line comes after the `service.stopping` line. stderr holds no other line.

Preconditions:

- cron is serving as process `<pid>`, on the socket it was passed, over the space's triggers (`S06`).
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `u_7f3a9c21`'s `GET /` is being answered at the signal, and its client is still reading the page 5 seconds after it.
- `<n>` of the requests cron has accepted, that one among them, are still running 5 seconds after the signal.
- No trigger's next slot falls between the signal and cron's exit, and cron holds no bus event the events app has not taken.
- telemetry takes every event.

Postconditions:

- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off, and their clients received no more of their responses.
- The triggers are as they were.
- Each of the `<n>` cut-off requests has its `request.started` recorded (delivered to telemetry, written to stderr as undelivered, or both, when it was being delivered as the deadline came). telemetry has received no `request.finished` for any of them, and no `service.stopping`.
- `/run/ikigenba/cron.sock` still exists, and connections made to it after cron exited wait in the socket's queue for the next cron to answer.

## The host restarts cron during a deploy

A deploy replaces cron's binary and restarts `ikigenba-cron.service` alone; `ikigenba-cron.socket` stays up throughout. Between the old cron exiting and the new one being ready, connections wait in the socket's queue instead of being refused, so a user or the gateway never sees cron missing. That holds because cron finishes what it accepted before it exits and leaves the socket where systemd put it. The triggers outlive the deploy: the new cron opens the same `state/cron.db`, bringing it up to date first when the new binary carries a migration the database has not had, works out each active trigger's next slot from the moment it starts, and fires every trigger the old one kept. A slot that falls due once the old cron has begun stopping, or while no cron is running, is gone (`S11`, `S13`); in this story none does.

Command:

```
$ sudo systemctl restart ikigenba-cron.service
```

Output:

```
```

Exits 0, once the new cron has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- cron is serving on `/run/ikigenba/cron.sock` under `ikigenba-cron.service`, over the space's triggers (`S06`).
- Users are loading cron's pages, and the gateway is calling cron's tools, throughout the restart, and every request the old cron accepted finishes at least a second before its drain deadline.
- No trigger's next slot falls between the signal to the old cron and the new cron's start.
- The host's services file lists the telemetry service, which takes every event, and the events app, which takes every event.

Postconditions:

- Every request sent was answered, by the old cron or the new one; none was refused and none was cut off.
- A new cron process is serving on `/run/ikigenba/cron.sock`, over the same `state/cron.db`, now up to date; every trigger is as the old cron left it, with the same id, slug, schedule, owner, status, last fire, and next slot.
- telemetry has received the old cron's `service.stopping`, with `reason` `SIGTERM`, and after it the new cron's `service.started`, whose `version` is the `<display>` of the new cron's environment. Every request the old cron answered is recorded before its `service.stopping`, and every request the new one answered after its `service.started`.
