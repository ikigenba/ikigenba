# Stories — retention

How long the log keeps an event. events keeps an event for `EVENTS_RETENTION_DAYS` days, a positive whole number read from its environment, and 2 when that is unset or empty; a value that is not a positive whole number is refused at start (`S02`). On a host the value comes from the manifest's `[env]` table (`S02`), which opsctl writes into `/etc/opt/ikigenba/events/env`; a developer sets it in the environment they run the binary with. A sweep deletes every event received more than `EVENTS_RETENTION_DAYS` days before the moment of the sweep: an event's age is measured from its `received`, the moment events accepted it, never from the `time` its producer gave it, so an event that reached events late, accepted all the same (`S07`), is kept for the whole window from its arrival. And a sweep never deletes an event a subscriber that is `ok` or `paused` (`S10`) has not finished: nothing after the lowest `cursor` among those subscribers is deleted, however old, so a subscriber that is behind or stuck still has every event it is owed. A `gone` subscriber holds nothing in place. events sweeps once when it starts, before it reports ready, and then once every hour for as long as it runs; nothing else deletes an event. A swept event is gone from every tool at once: `search` (`S09`) no longer answers it, and `catalog` (`S08`) no longer counts it in its event's `count`. Sweeping writes nothing to stdout or stderr: it is routine, not trouble. The actor is the host, or a developer at a terminal standing in for it, and an agent that reads the log afterwards through the MCP gateway's `call`, as `S08` and `S09` make the calls.

The fixture every story here shares, unless a story says otherwise, is this group's own, distinct from the log `S08` and `S09` share: the services file enables `repos`, which declares that it emits `repo.pushed`, `scripts`, which declares `"accepts":["*"]`, and `sites`, a service the stories suppose, which declares `"accepts":["repo.pushed"]`. `state/events.db` holds 65 `repo.pushed` events from `repos` and no other event: 40, `seq` 4101 to 4140, emitted and received on `2026-10-01`, and 25, `seq` 4141 to 4165, emitted and received on or after `2026-10-03T10:30:00Z`. `scripts` and `sites` are both `ok` at `cursor` 4165, the head. `repos` emits nothing more before the tool calls a story names.

## The host starts events and old events are swept

The log holds events from before the window and every subscriber has finished them; events' first act on starting is to sweep them, so an events that was stopped for a while never serves a stale log.

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
- `EVENTS_RETENTION_DAYS` is unset, so the window is 2 days.
- The preamble's fixture. events starts at `2026-10-05T09:00:00Z`, so the window reaches back to `2026-10-03T09:00:00Z`.

Postconditions:

- events is serving on the socket it was passed, and the sweep ran before it reported ready.
- The 40 events of `2026-10-01` are gone and the 25 later ones remain: `search` with `services` `["repos"]` and `until` `"2026-10-03T00:00:00Z"` answers no event, `search` with `services` `["repos"]` answers the 25 from `seq` 4165 down to 4141, and `catalog` answers `repo.pushed` with `count` 25.
- `scripts` and `sites` are as they were, `ok` at `cursor` 4165.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## The host gives events a longer window

events reads `EVENTS_RETENTION_DAYS` from its environment (`S02`). The manifest's `[env]` declares its default, `2` (`S02`), which the host writes into `/etc/opt/ikigenba/events/env` on every activate; an operator who wants a longer log changes the value there and restarts events, the change lasting until the next activate rewrites the file, and a developer sets it on the command line. The sweep at start and every hourly sweep then use that window.

Command:

```
$ EVENTS_RETENTION_DAYS=7 events
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- `bin/events` exists and is on the `PATH` as `events`.
- `LISTEN_PID` is events' process id and `LISTEN_FDS` is `1`: one listening socket is passed in, as file descriptor 3.
- The preamble's fixture. events starts at `2026-10-05T09:00:00Z`, so the window reaches back to `2026-09-28T09:00:00Z`.
- The tool calls below are made before `2026-10-05T10:00:00Z`, within the hour of the start.

Postconditions:

- events is serving on the socket it was passed.
- No event was deleted: `search` with `services` `["repos"]` and `until` `"2026-10-03T00:00:00Z"` answers the 40 events of `2026-10-01`, and `catalog` answers `repo.pushed` with `count` 65.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## The sweep keeps an event that reached events late

An event's age is counted from when events received it. `repos` was cut off from events for days and, once it could reach it again, emitted an event whose `time` is long before the window; events accepted it (`S07`), and the sweep at start keeps it, since it was received inside the window.

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
- `EVENTS_RETENTION_DAYS` is unset, so the window is 2 days.
- The preamble's fixture, and besides it the log holds `seq` 4166, `evt_5b2e8d4a1c7f3069`, a `repo.pushed` event from `repos` with request id `6f0a4c8e2b5d4197a3c7e1f5b9d2a6c0`, whose `time` is `2026-09-28T16:02:11.204530Z` and whose `received` is `2026-10-05T08:40:03.118204Z`. `scripts` and `sites` are both `ok` at `cursor` 4166, the head.
- events starts at `2026-10-05T09:00:00Z`, and the tool calls below are made before `2026-10-05T10:00:00Z`.

Postconditions:

- events is serving on the socket it was passed.
- `evt_5b2e8d4a1c7f3069` is kept: `search` with `request_id` `"6f0a4c8e2b5d4197a3c7e1f5b9d2a6c0"` answers it. The 40 events of `2026-10-01` are gone, as in `The host starts events and old events are swept`, and `catalog` answers `repo.pushed` with `count` 26.
- `evt_5b2e8d4a1c7f3069` is swept only once it was received more than 2 days before a sweep, by the first hourly sweep after `2026-10-07T08:40:03Z`.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## An event ages out while events runs

An event that was inside the window when events started crosses it later. The hourly sweep removes it, so within an hour of its aging out it is gone from every tool, without a restart and without a line on stderr. The agent here searches for it after that hour.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 1a7c3e5b9d0f4286a0e4c8b2f6d9a3e7
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"request_id":"3e9b5d1f7a2c4068e2b6d0f4a8c1e5b9"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member and a `structuredContent` that answers no event (`S09`).

Preconditions:

- events has been serving since `2026-10-05T09:00:00Z` with `EVENTS_RETENTION_DAYS` unset, so the window is 2 days.
- The log held one event with request id `3e9b5d1f7a2c4068e2b6d0f4a8c1e5b9`, `seq` 4141, emitted and received at `2026-10-03T10:30:00Z`: inside the window at start, and outside it from `2026-10-05T10:30:00Z`. `scripts` and `sites` had both finished it.
- It is after `2026-10-05T11:30:00Z`: at least one hourly sweep has run since the event aged out.

Postconditions:

- Nothing has changed but the log, from which the sweep, not the search, removed the event: `catalog`'s `count` for `repo.pushed` no longer includes it.
- events has written nothing to stdout or stderr since it started.

## A subscriber that is behind keeps its events from the sweep

`sites` is `ok` but far behind: it answers slowly, and events it has not yet been handed were received before the window. The sweep deletes old events only up to its `cursor`, so it is still handed every event it is owed, however long it takes. Events before its `cursor`, which every subscriber has finished, are swept as usual.

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
- `EVENTS_RETENTION_DAYS` is unset, so the window is 2 days.
- The preamble's fixture, but `sites` is `ok` at `cursor` 4120, and takes about 4 seconds to answer each delivery, ok. events starts at `2026-10-05T09:00:00Z`.
- The tool calls below are made before `2026-10-05T10:00:00Z`, within the hour of the start.

Postconditions:

- events is serving on the socket it was passed.
- The 20 events from `seq` 4101 to 4120 are gone, and the 20 from 4121 to 4140 remain though they were received before the window: `search` with `services` `["repos"]` and `until` `"2026-10-03T00:00:00Z"` answers those 20, from `seq` 4140 down to 4121, and `catalog` answers `repo.pushed` with `count` 45.
- `sites` is `ok`, and the events after `seq` 4120 have been or are being delivered to it in order, `seq` 4121 first.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## A paused subscriber keeps its events from the sweep

`sites` has been `paused` since before the window began, stuck on an old event. The sweep deletes old events only up to its `cursor`: the event it is stuck on and every event after it stay, however old, so that skipping or resuming it (`S12`) still finds them. Events before its `cursor`, which every subscriber has finished, are swept as usual.

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
- `EVENTS_RETENTION_DAYS` is unset, so the window is 2 days.
- The preamble's fixture, but `sites` is `paused` at `cursor` 4120, on `seq` 4121, an event of `2026-10-01`. events starts at `2026-10-05T09:00:00Z`.

Postconditions:

- events is serving on the socket it was passed.
- The 20 events from `seq` 4101 to 4120 are gone, and the 20 from 4121 to 4140 remain though they were received before the window: `search` with `services` `["repos"]` and `until` `"2026-10-03T00:00:00Z"` answers those 20, from `seq` 4140 down to 4121, and `catalog` answers `repo.pushed` with `count` 45.
- `sites` is still `paused` at `cursor` 4120 on `seq` 4121; its `lag` is 45.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.

## A gone subscriber does not keep its events from the sweep

`sites` is `gone`, its service disabled, with its `cursor` far behind. A `gone` subscriber is owed nothing while it is gone, so the sweep deletes old events past its `cursor` all the same; should it return, it starts again from the head (`S10`).

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
- `EVENTS_RETENTION_DAYS` is unset, so the window is 2 days.
- The preamble's fixture, but `sites`' entry in the services file has `"enabled": false` and its subscriber is `gone` at `cursor` 4120. events starts at `2026-10-05T09:00:00Z`.

Postconditions:

- events is serving on the socket it was passed.
- All 40 events of `2026-10-01` are gone, those after `sites`' `cursor` included: `search` with `services` `["repos"]` and `until` `"2026-10-03T00:00:00Z"` answers no event, and `catalog` answers `repo.pushed` with `count` 25.
- `sites` is still `gone` at `cursor` 4120.
- Nothing was written to stdout or stderr.
- It keeps running until it is signalled.
