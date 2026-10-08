# Stories — stopping

What becomes of cron's events for the bus, and of its triggers' slots, when cron stops. This group extends the stop `S02` tells, in which the host stops cron with `SIGTERM` (`systemctl stop`, `systemctl restart`) or `SIGINT` (a developer's `Ctrl-C`), and cron stops taking new connections, finishes the requests it has already accepted within its drain, at most `DRAIN_SECONDS` after the signal, records `service.stopping` once every request it accepted has finished, or at the drain deadline for any still running, and sends what it recorded to telemetry within the same deadline. cron also holds the events it has emitted for the event bus, the `events` app, that the bus has not yet taken (`S11`): a fire's `cron.<slug>.fired`, or a tool call's `cron.<slug>.created`, `paused`, `resumed`, or `deleted`. When cron stops it goes on delivering those it holds, one at a time, in the order it emitted them, within the drain, and any the bus has not taken when the drain ends is dropped, as one held past its retry window is (`S11`): cron records `event.lost` for it, carrying the dropped event's `id` among its attributes, and writes one line for it to stderr, whose text beyond the `cron: ` prefix is not fixed. Such an `event.lost` follows `service.stopping`, as does the `request.finished` of a request cut off at the drain deadline (`S02`); no other event does. A dropped bus event is not a request: it is not counted in `S02`'s `stopped with` line, and it never makes cron exit non-zero. A stop changes no trigger: each keeps its schedule, its status, and its `last_fired`, and a fire's `last_fired` is the slot whether or not its event reached the bus. From the signal on, cron fires nothing: a slot that comes after cron has been told to stop, while it drains, does not fire, and its trigger's `last_fired` stays as it was. A slot that comes while no cron is running is gone too, since the next cron to start works out each trigger's `next` from the moment it starts and makes up nothing (`S11`). So a deploy, which restarts cron, loses any slot that falls between the signal to the old cron and the new one starting. The actor is the host, whether systemd or a developer at a terminal standing in for it. cron runs over `S06`'s triggers, in which the caller `u_7f3a9c21` owns `hourly` (`crn_3f9a1c7e5b2d8046`, `@hourly`, last fired `2026-10-05T09:00:00Z`, next `2026-10-05T10:00:00Z`); `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds after the signal; telemetry takes every event as soon as it is sent; and no request is running at the signal and none arrives after it, unless a story says otherwise.

## The host stops cron with nothing pending

With no request running and no event held for the bus, cron has nothing to wait for: it records `service.stopping`, sends it, and exits at once, without waiting out the deadline. The triggers are as they were, and the next cron to start fires them from its own start on.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

cron exits 0, well within the drain deadline. Nothing is on stdout or stderr.

Preconditions:

- cron is serving as process `<pid>`, on the socket it was passed, over `S06`'s triggers, and it is `2026-10-05T09:32:00Z`.
- The bus has taken every event cron emitted, so cron holds none.

Postconditions:

- Every trigger is as it was: once a cron is serving again, `show` of `hourly` answers `last_fired` `2026-10-05T09:00:00Z` (`S07`).
- telemetry has received, last of cron's events:

  ```
  {"time":"<time>","service":"cron","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  and no `event.lost`. With `SIGINT` the `reason` is `SIGINT`.
- `/run/ikigenba/cron.sock` still exists, and connections made to it after cron exited wait in the socket's queue for the next cron to answer.

## The host stops cron while a fire's event is still unsent and the bus takes it within the drain

A trigger fired, but the bus could not take its event at once — events was itself restarting — and cron still holds it when it is told to stop. cron does not drop it at the signal: it keeps trying within the drain, and when events takes events again the fire's event goes through. Nothing was lost, so the stop is silent and exits 0, once the event is delivered and the trail sent.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

cron exits 0, within 4 seconds of the signal. Nothing is on stdout or stderr.

Preconditions:

- cron is serving as process `<pid>`, on the socket it was passed, over `S06`'s triggers.
- `hourly` fired at `2026-10-05T10:00:00Z` (`S11`); events could not be reached from `2026-10-05T09:59:50Z`, so cron holds the fire's `cron.hourly.fired`, and no other event for the bus.
- cron receives `SIGTERM` at `2026-10-05T10:00:01Z`, and events takes events again at `2026-10-05T10:00:03Z`, leaving cron at least a second of the drain to send what it recorded.

Postconditions:

- events holds the fire's `cron.hourly.fired` once, with `scheduled` `2026-10-05T10:00:00Z`, the `time` cron emitted it at, the fire's request id, and `user` `u_7f3a9c21`, as `S11` tells.
- telemetry has received the fire's `cron.hourly.fired`, then, last, cron's `service.stopping` with `reason` `SIGTERM`, and no `event.lost`.
- Once a cron is serving again, `show` of `hourly` answers `last_fired` `2026-10-05T10:00:00Z`.

## The host stops cron while a fire's event outlasts the drain

When the bus is still unreachable at the drain deadline, cron cannot wait for it any longer: it must exit before the service unit's stop timeout. It records `service.stopping` once every request it accepted has finished, or at the drain deadline for any still running, as always, then drops the event it holds, records `event.lost` for it, and writes the drop to stderr. The drain has used the whole deadline, so cron has no time left to send `service.stopping`, `event.lost`, or any other event not yet sent to telemetry: each goes to stderr as an `undelivered event` line instead (`S02`). The fire still happened: `last_fired` is the slot, and the trail's `cron.hourly.fired`, sent when the trigger fired, records it. No request was cut off, so cron exits 0.

Command:

```
$ kill -TERM <pid>
```

Output:

```
cron: undelivered event: {"time":"<time>","service":"cron","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
cron: <text>
cron: undelivered event: <event-lost>
```

cron exits 0, 5 seconds after the signal. The text is on stderr; stdout is empty. The lines come in this order: `service.stopping` first, then the drop of the fire's event, its text beyond `cron: ` not fixed, then `<event-lost>`, the `event.lost` cron recorded for it, as telemetry would have received it, carrying among its attributes the `evt_` id cron gave the dropped `cron.hourly.fired`. Any other event cron had recorded and not yet delivered to telemetry when the deadline came is written as an `undelivered event` line too, before the `service.stopping` line, in the order the events were recorded. stderr holds no `stopped with` line, since no request was cut off.

Preconditions:

- cron is serving as process `<pid>`, on the socket it was passed, over `S06`'s triggers.
- `hourly` fired at `2026-10-05T10:00:00Z` (`S11`), and telemetry received its `cron.hourly.fired` then; events could not be reached from `2026-10-05T09:59:50Z`, so cron holds the fire's `cron.hourly.fired` for the bus, and no other event for it.
- cron receives `SIGTERM` at `2026-10-05T10:00:01Z`, and events cannot be reached until well after cron has exited.

Postconditions:

- When events serves again, it holds no event from cron with `scheduled` `2026-10-05T10:00:00Z`, and nothing brings it back: the next cron does not emit it again.
- telemetry has received no `event.lost` and no `service.stopping` from this cron; the lines above are where they are recorded.
- Once a cron is serving again, `show` of `hourly` answers `last_fired` `2026-10-05T10:00:00Z`.
- `/run/ikigenba/cron.sock` still exists, and connections made to it after cron exited wait in the socket's queue for the next cron to answer.

## The host stops cron as a slot falls due

cron has been told to stop and is draining, waiting for a request it accepted before the signal, when `hourly`'s slot comes. A cron that is going away fires nothing more: the slot does not fire, now or later, since the next cron works out `hourly`'s `next` from its own start and makes up nothing (`S11`). `hourly` keeps the `last_fired` it had. The request finishes within the drain and nothing was lost, so the stop is silent and exits 0.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

cron exits 0, within 4 seconds of the signal. Nothing is on stdout or stderr.

Preconditions:

- cron is serving as process `<pid>`, on the socket it was passed, over `S06`'s triggers; `hourly`'s `next` is `2026-10-05T10:00:00Z`, and the bus has taken every event cron emitted, so cron holds none.
- cron receives `SIGTERM` at `2026-10-05T09:59:58Z`, while `u_7f3a9c21`'s `GET /` (`S03`), accepted before the signal, is being answered; that request finishes at `2026-10-05T10:00:02Z`, leaving cron at least a second of the drain to send what it recorded, and no other request is running.
- The next cron starts at `2026-10-05T10:00:30Z`.

Postconditions:

- No cron fired `hourly`'s slot `10:00`: events holds no event from cron with `scheduled` `2026-10-05T10:00:00Z`, and telemetry has received no `cron.hourly.fired` for it.
- telemetry has received the request's `request.finished` with `status` 200, then, last, cron's `service.stopping` with `reason` `SIGTERM`, and no `event.lost`.
- `show` of `hourly`, asked of the next cron, answers `last_fired` `2026-10-05T09:00:00Z` and `next` `2026-10-05T11:00:00Z`; at `11:00` it fires as usual (`S11`).
- `/run/ikigenba/cron.sock` still exists, and connections made to it after cron exited wait in the socket's queue for the next cron to answer.

## The host restarts cron across a slot during a deploy

A deploy restarts `ikigenba-cron.service` (`S02`), and a slot that comes between the old cron exiting and the new one starting has no cron to fire it. The new cron makes nothing up: it works out each trigger's `next` from the moment it starts, so the slot is gone, and a script waiting on the trigger misses that one fire. This is accepted, not trouble: both crons exit and start cleanly, and the restart succeeds. An operator who cares about a slot deploys away from it.

Command:

```
$ sudo systemctl restart ikigenba-cron.service
```

Output:

```
```

Exits 0, once the new cron has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- cron is serving on `/run/ikigenba/cron.sock` under `ikigenba-cron.service`, over `S06`'s triggers; `/opt/cron/bin/cron` has been replaced by a new release.
- The old cron receives `SIGTERM` at `2026-10-05T09:59:58Z`, holds no event for the bus, has no request running, and exits before `2026-10-05T10:00:00Z`.
- The new cron starts at `2026-10-05T10:00:01Z`, after `hourly`'s slot `10:00`.
- The host's services file lists the telemetry service, which takes every event, and the event bus, which takes every event.

Postconditions:

- A new cron process is serving on `/run/ikigenba/cron.sock`, over the same `state/cron.db`.
- No cron fired `hourly`'s slot `10:00`: events holds no event from cron with `scheduled` `2026-10-05T10:00:00Z`, and the trail holds no `cron.hourly.fired` for it.
- `show` of `hourly`, asked of the new cron, answers `last_fired` `2026-10-05T09:00:00Z` and `next` `2026-10-05T11:00:00Z`; at `11:00` it fires as usual (`S11`).
- telemetry has received the old cron's `service.stopping`, with `reason` `SIGTERM`, and after it the new cron's `service.started`, whose `version` is the `<display>` of the new cron's environment, and no `cron.*` event between them.
