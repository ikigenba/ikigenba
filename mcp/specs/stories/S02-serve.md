# Stories — serve

The bare binary serves, and it serves only on a listening socket it inherits: mcp never opens one of its own, and there is no port or address it falls back to. It takes the socket the way systemd socket activation passes it — `LISTEN_PID` names mcp's own process, `LISTEN_FDS` is `1`, and the socket is file descriptor 3 — and it removes the `LISTEN_*` variables from its environment once it has taken it. On a host, opsctl publishes `ikigenba-mcp.socket`, which holds the Unix socket `/run/ikigenba/mcp.sock`, beside `ikigenba-mcp.service`, which runs `/opt/mcp/bin/mcp` with no arguments as the `ikigenba` user, with `/opt/mcp` as its working directory and `/opt/mcp/etc/env` as its environment file; nginx proxies to `http://unix:/run/ikigenba/mcp.sock:`. The service is `Type=notify`: mcp tells systemd it is ready, by sending `READY=1` to `$NOTIFY_SOCKET`, once it is serving. When stopped, mcp drains for at most `DRAIN_SECONDS`, a positive whole number of seconds read from its environment, and 5 when that is unset or empty. On a host, opsctl owns this value and the service unit's stop timeout: both are space-wide settings in opsctl's configuration, opsctl writes the drain into every app's `etc/env` and the stop timeout (10 seconds by default, always longer than the drain) into every service unit, and an app's manifest never sets either. mcp's environment also carries `IKIGENBA_SERVICES`, the path of the host's services file, normally `/var/lib/ikigenba/services.json`, which opsctl sets in the environment the host gives mcp. The file lists the platform's services: it feeds the launcher in the connect page's banner (`S03`), and it is where the gateway finds the suite's MCP services and the socket each one serves on (`S06`). mcp reads the variable once, when it starts, and reads the file it names afresh on every request, so a rewritten file shows on the next request without a restart. mcp never fails to start over it: unset, empty, or naming a file that is missing, unreadable, or malformed, mcp starts and serves all the same, treats the file as listing no services, and says nothing about it. Such a file names no telemetry service either, so while it lasts every event of mcp's trail is undeliverable and reaches stderr as told below. mcp's stderr holds only trouble, and trouble is exactly two things: a condition mcp cannot continue from, and an event of its trail (below) that it could not deliver to the telemetry service. Everything else mcp does it records in its trail and writes nothing about, a request it answers with a 5xx included: a handled failure is a fact of the trail, recorded with its status, not a line in the journal. So under systemd the journal holds only trouble, and a healthy mcp whose trail is being delivered writes nothing at all. Every line mcp writes to stderr begins `mcp: `. The actor in these stories is the host, whether that is systemd or a developer at a terminal standing in for it.

mcp's environment may also carry `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`, from which it builds `<display>`, the display string `mcp --version` prints under the same environment (`S01`). mcp reads them once, when it starts, and shows that string as its version wherever it shows one: in its `service.started` event (below), the connect page's footer (`S03`), its MCP `serverInfo` (`S05`), and the `clientInfo` it names itself with to a backend (`S08`). With neither set the string is empty, and mcp starts and serves all the same.

These are the terms every app of the platform serves on, the same as dummy's. The socket is the app's only way in. Every app runs as the one `ikigenba` user, so any app can reach any sibling's socket, and nginx reaches them all; nothing else on the host can. The suite is a closed system that only we deploy services into, and an app trusts the suite: it trusts the headers nginx sets — `X-User-Id` and `X-User-Email`, the caller auth authenticated, and `X-Request-Id`, 32 lowercase hexadecimal characters nginx sets on every request and overwrites whatever a client sent — and it trusts a sibling that calls it to have forwarded them. An app that calls a sibling while serving a request calls it directly at its socket, not through nginx, and copies `X-User-Id`, `X-User-Email`, and `X-Request-Id` from the request it is serving onto the call, so the sibling cannot tell the call from one nginx made. Every request an app serves has a request id: a request that arrives with no `X-Request-Id`, or an empty one — a developer's request with no nginx in front, say — is given one in nginx's shape, 32 lowercase hexadecimal characters the app makes up, before anything else in the app sees the request, and from then on that id is the request's `X-Request-Id` in the app's trail and on every call the app makes for it.

mcp is an app that calls its siblings: calling them is what the gateway is for. While serving a request to `/mcp` it calls a backend directly at the socket that backend's entry in the services file names (on a host, `/run/ikigenba/<name>.sock`), never through nginx, and copies `X-User-Id`, `X-User-Email`, and `X-Request-Id` from the request onto the call; an `X-User-Email` the request lacked is not sent. One request to the gateway may wait up to 50 seconds on its backends (`S08`). mcp records each request it makes to a backend in its trail, once the backend's status and headers arrive or the request fails, whatever its outcome (`S08`), and writes nothing to stderr about it.

mcp records what it does as a trail of events, the platform's telemetry, and sends each event to the telemetry service: the entry named `telemetry` in the services file, at the socket that entry names, as `POST /ingest`, never through nginx. mcp looks that entry up afresh for every event, so a telemetry service installed, moved, or restarted while mcp runs gets mcp's next event without mcp restarting. An event is one record: the time, in UTC to the microsecond; the service, always `mcp`; the event's name; the request id, the `X-Request-Id` of the request the event belongs to; the user, that request's `X-User-Id`, empty when it had none; and its attributes, flat names with string, number, or boolean values. The request id and the user are empty for an event that belongs to no request. Attributes carry what happened and the names of the things it happened to, never what a request or an answer held: no tool arguments, no results, no error text, no query string. Sending the trail never holds up a request: mcp answers as fast, and the same, with the telemetry service down as with it up. mcp records these events and no others; it has no events of its own beyond the ones every app of the platform records:

- `service.started`, once mcp is serving and has told systemd it is ready, with `version`, `<display>`, the string `mcp --version` prints under the environment mcp was started with (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`;
- `service.stopping`, when mcp stops, with `reason`: `SIGTERM` or `SIGINT`, the signal that stopped it, recorded once it has finished the requests it accepted, or `failed`, when its socket failed under it (`The host's socket fails while mcp serves`); it is the last event mcp sends;
- `request.started`, as each request arrives, with `method` and `path`, the request's URL path without its query;
- `request.finished`, once that request's answer is complete, with `status`, the HTTP status mcp answered with, `duration_us`, how long mcp took to answer, in whole microseconds, `request_bytes`, how many bytes of the request's body mcp read, and `response_bytes`, how many bytes of body its answer carried;
- `sibling.called`, for each request mcp makes to a backend, once the backend's status and headers arrive or the request fails (`S08`), with `target`, the service's name, `method`, `path`, `status`, the HTTP status the backend answered with or `0` when no answer came, and `duration_us`;
- `tool.called`, for each call of one of the gateway's four tools that is answered with a result (`S05`), with `tool`, `kind`, `outcome`, and `duration_us`.

A story shows the events a request added to the trail as a block, one event to a line, in the order mcp recorded them: the event's name, then each attribute as `<key>=<value>`, with `duration_us`, `request_bytes`, and `response_bytes` left out because they vary; the request id and the user every line of the block carries are stated beside it. A story's `Nothing has changed.` speaks of everything but the trail, which every request adds to. Unless a story says otherwise, the telemetry service is serving and stores every event mcp sends it, so no event reaches stderr. The services files these stories show leave out one entry, `telemetry`, whose `socket` is `/run/ikigenba/telemetry.sock`, where a stand-in that stores every event serves. It is not an MCP service (`"mcp": false`) and has no icon, so it appears in no page, list, or answer quoted here. On a host the real telemetry service is an MCP service the gateway reaches like any other (`S11`). A services file a story shows as missing, unreadable, or malformed holds no such entry, and then mcp's events reach stderr as below.

An event mcp cannot deliver is not lost without trace: mcp writes it to stderr as one line, `mcp: undelivered event: ` followed by the event exactly as it would have been sent, a JSON object whose members are, in this order, `time`, `service`, `event`, `request_id`, `user`, and `attrs`, with `time` in the form `2026-10-02T14:03:07.123456Z`. An event is undeliverable when the telemetry service has not stored it after three tries within a fraction of a second (no `telemetry` entry, nothing listening on its socket, or an answer that is not a success); at once, without another try, when the telemetry service refuses it as malformed; and at once, without being sent, when so many events are already waiting to be sent that mcp holds no more. mcp never sends an event again once it has written it to stderr.

## The host starts mcp

The socket keeps out every process that is not part of the suite or nginx, which a port on loopback would not: any process on the host can connect to a loopback port, and only the `ikigenba` user and nginx can connect to `/run/ikigenba/mcp.sock`. systemd owns the socket, so it exists, and accepts connections into its queue, before mcp starts and while it is stopped; mcp's part is to serve what arrives on it. `systemctl start` returns once mcp has reported that it is ready.

Command:

```
$ sudo systemctl start ikigenba-mcp.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed mcp: `/opt/mcp/bin/mcp` exists, and `ikigenba-mcp.socket` and `ikigenba-mcp.service` are published.
- `ikigenba-mcp.socket` is active, so `/run/ikigenba/mcp.sock` exists and accepts connections.
- `ikigenba-mcp.service` is not running.

Postconditions:

- `ikigenba-mcp.service` is `active`, and mcp is serving on `/run/ikigenba/mcp.sock`: a connection there, and every connection queued before mcp started, is answered by mcp.
- mcp listens on no other socket and no port.
- mcp has written nothing to the journal.
- The trail holds one event from this start, the first this mcp records, before the `request.started` of any request it answers, with an empty request id and an empty user, whose `version` is `<display>`, the string `mcp --version` prints under the environment the host gives mcp (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`:

  ```
  service.started version=<display>
  ```

- It keeps running until it is signalled.

## The host starts mcp while the telemetry service is down

The trail is not mcp's to serve: mcp serves the same whether or not the telemetry service is there, and readiness never waits on it. An event it cannot deliver is the one kind of handled trouble that reaches the journal, so the operator can see what the trail is missing and nothing is lost without trace.

Command:

```
$ sudo systemctl start ikigenba-mcp.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- mcp is installed, `ikigenba-mcp.socket` is active, and `ikigenba-mcp.service` is not running, as in `The host starts mcp`.
- The services file's `telemetry` entry names `/run/ikigenba/telemetry.sock`, and nothing accepts connections there.

Postconditions:

- mcp is serving on `/run/ikigenba/mcp.sock`, as in `The host starts mcp`.
- The journal holds one line from mcp, where `<time>` is when mcp recorded the event and `<display>` the string `mcp --version` prints under the environment the host gives mcp (`S01`):

  ```
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

- While the telemetry service stays down, every request mcp serves is answered exactly as it would be with the telemetry service up, and each event mcp records for it adds one such line to the journal. A services file with no `telemetry` entry is the same.
- Once the telemetry service is serving on the socket its entry names, the next event mcp records reaches it, with no restart of mcp. No event already written to the journal is sent.

## The host stops mcp

`systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which mcp treats the same way. mcp stops taking new connections, finishes the requests it has already accepted, and exits. It closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and mcp never removes `/run/ikigenba/mcp.sock`. It waits at most `DRAIN_SECONDS` for requests to finish, which on a host is always less than the time the service unit allows before systemd kills it.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

mcp exits 0. Nothing is on stdout or stderr.

Preconditions:

- mcp is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- Every request mcp has accepted finishes within 5 seconds of the signal.

Postconditions:

- Every request accepted before the signal received its full response.
- `/run/ikigenba/mcp.sock` still exists, and connections made to it after mcp exited wait in the socket's queue for the next mcp to answer.
- The trail holds, after the `request.finished` of every request accepted before the signal, one last event from mcp, with an empty request id and an empty user; after a `SIGINT` its reason is `SIGINT`:

  ```
  service.stopping reason=SIGTERM
  ```

  mcp delivered every event it had recorded before it exited.

## The host stops mcp while a request outlasts the drain

mcp waits for accepted requests only until its drain deadline, `DRAIN_SECONDS` after the signal, so that it always exits before the service unit's stop timeout and is never killed mid-write by systemd. A request still running at the deadline is cut off: its connection is closed without the rest of its response. The request still running may be one waiting on a backend; it is cut off and counted like any other. Losing a request is trouble, so mcp says how many it lost and exits non-zero. The drain deadline bounds the trail too: sending what is left of the trail happens inside the same `DRAIN_SECONDS`, so once the requests have used it all, nothing is left to send in, and every event not yet delivered goes to stderr instead, `service.stopping` among them. The trail then ends with the cut-off requests' `request.started` and no `request.finished` for them, which is how it shows a request that never finished.

Command:

```
$ kill -TERM <pid>
```

Output:

```
mcp: undelivered event: {"time":"<time>","service":"mcp","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
mcp: stopped with <n> requests unfinished
```

mcp exits 1, 5 seconds after the signal. The lines are on stderr; stdout is empty. The output is one or more `mcp: undelivered event: ` lines and then the `stopped with` line, always last; the `service.stopping` line shown is always among them, but other lines may come before or after it. `<n>` is the number of requests still running at the deadline. When `<n>` is 1 the last line reads `mcp: stopped with 1 request unfinished`. `<time>` is when mcp recorded the event. The undelivered lines are every event that was still waiting to be sent at the deadline, `service.stopping` included, in the order mcp recorded them, followed by any event mcp records after the deadline, such as a cut-off request's `request.finished`, which is written the same way, never sent, before the last line or not at all.

Preconditions:

- mcp is serving as process `<pid>`, on the socket it was passed.
- `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds.
- `<n>` of the requests mcp has accepted are still running 5 seconds after the signal.

Postconditions:

- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off.
- The trail holds no `service.stopping` from this mcp, and no `request.finished` for any of the `<n>` requests cut off.
- `/run/ikigenba/mcp.sock` still exists, and connections made to it after mcp exited wait in the socket's queue for the next mcp to answer.

## The host's socket fails while mcp serves

mcp serves on the one socket it was passed and cannot open another, so a socket whose accepting fails with an error the system reports as permanent, not temporary, is a condition mcp cannot continue from, and serving ends on it: mcp says why on stderr and exits non-zero, and systemd's restart policy decides what happens next. A temporary error, such as running out of file descriptors, is retried, and mcp keeps serving. Once serving has ended, mcp stops as a signal stops it: it accepts nothing more and lets the requests in progress finish within `DRAIN_SECONDS` of the failure, and a request still running at that deadline is cut off, as in `The host stops mcp while a request outlasts the drain`. No `stopped with` line is written, though: the failure line is mcp's only line of its own. Before it exits, mcp records why it stopped, so the trail shows a stop rather than a crash, and it gives the trail at most `DRAIN_SECONDS` from the failure to be delivered; what is still undelivered then goes to stderr. A `SIGTERM` or `SIGINT` that arrives once the socket has failed changes nothing: the reason stays `failed`, the `mcp: <error>` line stays last, and mcp still exits 1. A developer here stands in for systemd, running mcp with one socket passed in.

Command:

```
$ mcp
```

Output:

```
mcp: <error>
```

mcp exits 1. The lines are on stderr; stdout is empty. `<error>` is the reason the socket refused to accept, as the system reports it. Before that line, stderr holds one `mcp: undelivered event: ` line for each event up to and including `service.stopping` that mcp could not deliver to the telemetry service within 5 seconds of the failure, in the order mcp recorded them, and none when it delivered them all. Any event mcp records after `service.stopping` is written the same way, never sent, before the last line or not at all. The `mcp: <error>` line is always last.

Preconditions:

- `LISTEN_PID` is mcp's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- `DRAIN_SECONDS` is unset, so the deadline is 5 seconds.
- mcp has started serving and reported that it is ready; then accepting on the socket fails with an error the system reports as permanent, not temporary.

Postconditions:

- mcp accepted nothing after the failure and has exited. Every request in progress at the failure that finished within 5 seconds of it received its full response; any still running then was cut off and has no `request.finished` in the trail.
- The trail holds, as the last event from this mcp, after its `service.started` and the `request.finished` of every request that finished, with an empty request id and an empty user:

  ```
  service.stopping reason=failed
  ```

  unless the telemetry service could not take it within 5 seconds of the failure, in which case it is on stderr as an undelivered event line instead.

## The host restarts mcp during a deploy

A deploy replaces mcp's binary and restarts `ikigenba-mcp.service` alone; `ikigenba-mcp.socket` stays up throughout. Between the old mcp exiting and the new one being ready, connections wait in the socket's queue instead of being refused, so a client never sees mcp missing. That holds because mcp finishes what it accepted before it exits and leaves the socket where systemd put it.

Command:

```
$ sudo systemctl restart ikigenba-mcp.service
```

Output:

```
```

Exits 0, once the new mcp has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- mcp is serving on `/run/ikigenba/mcp.sock` under `ikigenba-mcp.service`.
- A client is sending requests to `/run/ikigenba/mcp.sock` throughout the restart.

Postconditions:

- Every request the client sent was answered, by the old mcp or the new one; none was refused and none was cut off.
- A new mcp process is serving on `/run/ikigenba/mcp.sock`.
- The trail holds the old mcp's `service.stopping reason=SIGTERM`, after the `request.finished` of every request the old mcp answered, then the new mcp's `service.started version=<display>`, whose `<display>` is that of the new mcp's environment; every request the new mcp answered is recorded after it. A new value there is how the trail shows a deploy.

## The host starts mcp without a socket

Run bare, with no socket passed in, mcp has nothing to serve on, and it does not open one of its own: there is no port or address it falls back to. That is the caller's mistake, so it is a usage error. `LISTEN_FDS` unset, or `LISTEN_PID` naming some other process, counts as no socket passed in. The detail says how to run mcp correctly.

Command:

```
$ mcp
```

Output:

```
mcp: no socket was passed in

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/mcp` exists and is on the `PATH` as `mcp`.
- `LISTEN_FDS` is unset in the environment, or `LISTEN_PID` is not mcp's process id.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. mcp listened on nothing, told systemd nothing, and sent nothing to the telemetry service: a start refused as a usage error records no event.

## The host passes mcp more than one socket

mcp serves on exactly one socket. A unit that passes it several is misconfigured, and mcp will not guess which one it was meant to serve on.

Command:

```
$ mcp
```

Output:

```
mcp: 2 sockets were passed in, expected 1

run it under systemd, with a listening socket passed in
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/mcp` exists and is on the `PATH` as `mcp`.
- `LISTEN_PID` is mcp's process id and `LISTEN_FDS` is `2`: two listening sockets are passed in, as file descriptors 3 and 4.
- `DRAIN_SECONDS` is unset, or a positive whole number.

Postconditions:

- Nothing has changed. mcp served on neither socket, told systemd nothing, and sent nothing to the telemetry service.

## The host gives mcp a drain deadline that is not a number of seconds

mcp reads `DRAIN_SECONDS` before it serves, so a bad value is found at start rather than at the moment mcp is asked to stop. A value that is not a positive whole number — `0`, `-1`, `2.5`, `5s`, or `abc` — is the caller's mistake, so it is a usage error and mcp serves nothing. The value is quoted back verbatim. mcp sets no upper limit: keeping the drain inside the service unit's stop timeout is opsctl's to enforce.

Command:

```
$ DRAIN_SECONDS=abc mcp
```

Output:

```
mcp: DRAIN_SECONDS is 'abc', not a positive whole number of seconds
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/mcp` exists and is on the `PATH` as `mcp`.
- `LISTEN_PID` is mcp's process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.

Postconditions:

- Nothing has changed. mcp served nothing, told systemd nothing, and sent nothing to the telemetry service.
