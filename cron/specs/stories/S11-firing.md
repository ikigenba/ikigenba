# Stories — firing

What happens when a trigger's slot comes: cron fires the trigger by emitting one event on the suite's event bus, the `events` app, which delivers it to every service that accepts it, scripts subscribed to it among them. A trigger's slots are the moments its `when` names, in UTC, to the minute (`S06`); its `next` is the next slot it will fire, and its `last_fired` the slot of its latest fire (`S07`). Only an active trigger fires; a paused one has no `next` and fires nothing (`S09`). cron fires each slot at most once, and fires a trigger only for a slot later than its `last_fired`. It never makes up slots: when it reaches a trigger's slot late, it still fires that slot, unless the slot after it is due too, in which case it fires only the latest slot due and the earlier ones never fire. When cron starts, every active trigger's `next` is the first slot after that moment, so the slots that passed while cron was not running are gone. A fire of the trigger with slug `<slug>` emits the event named `cron.<slug>.fired`, which events receives as one JSON object of exactly nine members:

```
{"id":"<event-id>","time":"<time>","service":"cron","event":"cron.<slug>.fired","request_id":"<request-id>","user":"<owner>","attrs":{"scheduled":"<slot>","trigger":"<trigger-id>","when":"<when>"},"cause":"","depth":0}
```

`<event-id>` is the event's own id, which cron gives it: `evt_` followed by 16 lowercase hexadecimal digits, a new one for every event. `<time>` is the moment cron emitted the event, UTC to the microsecond, as `2026-10-05T10:00:00.004218Z`. A fire is not a request, and no one asked for it: cron gives each fire a request id of its own, `<request-id>`, 32 lowercase hexadecimal digits, the shape of every request id, a new one for every fire, so the trail shows each fire as one request (`S12`); and the fire runs as the trigger's owner, so `<owner>` is the owner's user id, never their email, which is in no event. `cause` is empty and `depth` is 0, since no event led to a fire. `attrs` holds exactly three attributes, their keys in alphabetical order: `scheduled`, the slot the fire is for, RFC 3339 UTC to the second, as `2026-10-05T10:00:00Z`; `trigger`, the trigger's id; and `when`, its schedule, exactly as it was sent. A fire carries nothing else: no payload of an agent's, no slug beyond the one in the event's name. So a late fire shows as the gap between `time` and `scheduled`. cron declares this event to events as the pattern `cron.*.fired`, with the attribute names `scheduled`, `trigger`, and `when`. As it emits a fire, cron records it in its own trail under the same name, with the same request id, user, and attributes (`S12`), and sets the trigger's `last_fired` to the slot and its `next` to the first slot after the moment it fired; a fire records no `request.started`, `request.finished`, or `tool.called`, since no request or tool was part of it. Firing is cron's routine and writes nothing to stdout or stderr. The stories share `S06`'s triggers and users: the caller `u_7f3a9c21` (`mg@example.com`) owns `hourly` (`crn_3f9a1c7e5b2d8046`, `@hourly`, last fired `2026-10-05T09:00:00Z`, next `2026-10-05T10:00:00Z`) and the paused `weekly_digest` (`crn_5c7b9e2f4a6d1038`, `0 8 * * 1`, last fired `2026-09-28T08:00:00Z`); `u_2b8e1d04` (`ann@example.com`) owns `month_end` (`crn_1a4f8c6e9b3d7025`, `@monthly`, never fired, next `2026-11-01T00:00:00Z`) and `nightly_backup` (`crn_8d2e6b4a1f7c3095`, `30 2 * * *`, last fired `2026-10-05T02:30:00Z`, next `2026-10-06T02:30:00Z`). The actor is a trigger, whose slot comes while the host runs cron; a developer stands in for the host by running the binary with a socket passed in, as `S02` tells. Unless a story says otherwise, cron was started at `2026-10-05T09:32:00Z` with `IKIGENBA_SERVICES` naming the suite's services file (`S03`), whose `events` entry is the event bus, which takes every event as soon as it is sent, and whose `telemetry` entry takes every event; and the host runs on, unsuspended, through every slot a story names.

## A trigger fires on its slot

`hourly`'s next slot is `10:00`. When the clock reaches it, cron fires `hourly` once: the bus receives `cron.hourly.fired` and delivers it to every service that accepts it, a script subscribed to `cron.*.fired` or to `cron.hourly.fired` among them. The trigger then shows the fire as its `last_fired` and the following hour as its `next`. No other trigger has a slot at `10:00`, so nothing else fires.

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's: cron has been serving since `2026-10-05T09:32:00Z` over `S06`'s triggers, and `hourly`'s `next` is `2026-10-05T10:00:00Z`.
- The clock passes `2026-10-05T10:00:00Z`.

Postconditions:

- At `2026-10-05T10:00:00Z`, or a moment after, events has received exactly one event from cron, and no other:

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.hourly.fired","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T10:00:00Z","trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"},"cause":"","depth":0}
  ```

  `<event-id>` is `evt_` and 16 lowercase hexadecimal digits; `<time>` is the moment cron emitted it, at or just after `2026-10-05T10:00:00Z`; `<request-id>` is 32 lowercase hexadecimal digits, a new id cron made for this fire and used for nothing else.
- telemetry has received one event from cron for the fire, under the same request id and user, and no `request.started`, `request.finished`, or `tool.called` with it:

  ```
  {"time":"<time>","service":"cron","event":"cron.hourly.fired","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T10:00:00Z","trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"}}
  ```

  `mg@example.com` is in neither event.
- `show` of `hourly` (`S07`) answers:

  ```
  {"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"@hourly","owner":"mg@example.com","status":"active","created":"2026-09-20T08:00:00Z","last_fired":"2026-10-05T10:00:00Z","next":"2026-10-05T11:00:00Z"}
  ```

- Every other trigger is as it was. cron wrote nothing to stdout or stderr.

## A trigger fires late, before its next slot is due

cron can reach a slot late: the machine stalled, or cron was busy, while the slot came. A slot reached late is still fired, as long as the slot after it is not yet due, and the fire says which slot it is for: `scheduled` is the slot, `10:00`, and `time` the moment cron got to it, so whoever reads the event sees how late it was. The next slot is unchanged by the lateness.

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's: cron has been serving since `2026-10-05T09:32:00Z`, and `hourly`'s `next` is `2026-10-05T10:00:00Z`.
- The host was suspended from `2026-10-05T09:59:00Z` to `2026-10-05T10:20:00Z`, so cron ran nothing in that time and reaches the `10:00` slot at `2026-10-05T10:20:00Z`, before `hourly`'s next slot, `11:00`.

Postconditions:

- Shortly after `2026-10-05T10:20:00Z`, events has received exactly one `cron.hourly.fired` for the slot, the event of `A trigger fires on its slot` with `scheduled` still `2026-10-05T10:00:00Z` and a `time` after `2026-10-05T10:20:00Z`. telemetry has received the fire's `cron.hourly.fired` with the same attributes.
- `show` of `hourly` answers `last_fired` `2026-10-05T10:00:00Z` and `next` `2026-10-05T11:00:00Z`, and at `11:00` it fires as usual, with `scheduled` `2026-10-05T11:00:00Z`.
- cron wrote nothing to stdout or stderr.

## A trigger with several slots due at once fires only the latest

When cron gets to a trigger so late that more than one of its slots is due, it fires once, for the latest slot due, and the earlier slots never fire: cron does not catch up. A script woken by the trigger runs once, not once per missed hour.

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's: cron has been serving since `2026-10-05T09:32:00Z`, and `hourly`'s `next` is `2026-10-05T10:00:00Z`.
- The host was suspended from `2026-10-05T09:59:00Z` to `2026-10-05T12:10:00Z`, so when cron runs again `hourly`'s slots `10:00`, `11:00`, and `12:00` have all come, and `13:00` has not.

Postconditions:

- Shortly after `2026-10-05T12:10:00Z`, events has received exactly one `cron.hourly.fired`, with `scheduled` `2026-10-05T12:00:00Z`, its `trigger`, `when`, `user`, `cause`, and `depth` as in `A trigger fires on its slot`. No event from cron has `scheduled` `2026-10-05T10:00:00Z` or `2026-10-05T11:00:00Z`, then or ever.
- telemetry has received one `cron.hourly.fired` for it, with the same attributes, and no other.
- `show` of `hourly` answers `last_fired` `2026-10-05T12:00:00Z` and `next` `2026-10-05T13:00:00Z`.
- No other trigger had a slot in that time, and every other trigger is as it was. cron wrote nothing to stdout or stderr.

## A paused trigger lets its slot pass

A paused trigger has no `next`, and its slots pass with nothing fired; pausing stops it firing and nothing more (`S09`). When it is resumed, it fires from its next slot after the resume, and the slots that passed while it was paused are not made up (`S09`). (A paused trigger is not a paused subscriber of the events app: the trigger is a schedule that does not fire, the subscriber a service that is not being delivered to.)

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's: `weekly_digest` is paused, has no `next`, and last fired at `2026-09-28T08:00:00Z`.
- cron serves on, and the clock passes `2026-10-12T08:00:00Z`, a Monday at `08:00`, which `0 8 * * 1` names; `weekly_digest` stays paused throughout.

Postconditions:

- events has received no event from cron whose `trigger` is `crn_5c7b9e2f4a6d1038`, and telemetry no `cron.weekly_digest.fired`.
- `show` of `weekly_digest` answers as before:

  ```
  {"id":"crn_5c7b9e2f4a6d1038","slug":"weekly_digest","when":"0 8 * * 1","owner":"mg@example.com","status":"paused","created":"2026-09-01T12:00:00Z","last_fired":"2026-09-28T08:00:00Z"}
  ```

- cron wrote nothing to stdout or stderr.

## A trigger's slots pass while cron is down and are not made up

cron fires only while it runs. A trigger whose slots came while cron was stopped, or dead, does not fire them when cron starts again: starting is not a slot, and cron does not catch up. Each active trigger's `next` is worked out afresh from the moment cron starts, so `hourly` fires next at the first hour after the start, and its `last_fired` still shows the last slot it really fired.

Command:

```
$ sudo systemctl start ikigenba-cron.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed cron, and `ikigenba-cron.socket` is active (`S02`); `ikigenba-cron.service` is not running.
- The cron that last ran stopped at `2026-10-05T09:32:30Z`, with `/opt/cron/state/cron.db` holding `S06`'s triggers, and nothing has run cron since; `hourly`'s slots `10:00`, `11:00`, and `12:00` came while it was down.
- The host starts cron at `2026-10-05T12:10:00Z`.

Postconditions:

- `ikigenba-cron.service` is `active`, and cron is serving on `/run/ikigenba/cron.sock`.
- No trigger fired at the start: events has received no event from cron, and telemetry has received cron's `service.started` (`S02`) and no `cron.*` event.
- `show` of `hourly` answers `last_fired` `2026-10-05T09:00:00Z`, the last slot it fired, and `next` `2026-10-05T13:00:00Z`; at `13:00` it fires as usual. `nightly_backup`'s `next` is `2026-10-06T02:30:00Z` and `month_end`'s `2026-11-01T00:00:00Z`, as before, and `weekly_digest` is still paused, with no `next`.
- cron wrote nothing to stdout or stderr.

## A trigger restored with an older last_fired fires nothing it already fired

A database restored from the host's replica can be older than the triggers' last fires: here the replica was taken before `hourly` fired at `10:00`, so the restored `hourly` says it last fired at `09:00`. cron does not compare the record with what it fired before; it starts as on any start, working out each active trigger's `next` from the moment it starts. So a restore fires nothing again, and makes up nothing: the `10:00` fire, which events already holds, is not repeated, and `show` reports the replica's older `last_fired` until the trigger fires next. That is stated here, not prevented.

Command:

```
$ sudo systemctl start ikigenba-cron.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed cron, and `ikigenba-cron.socket` is active (`S02`); `ikigenba-cron.service` is not running.
- The cron that last ran fired `hourly` at `2026-10-05T10:00:00Z`, and events holds that `cron.hourly.fired`; then `/opt/cron/state/cron.db` was lost, and has been restored from the host's replica taken at `2026-10-05T09:58:00Z`, which holds `S06`'s triggers, `hourly` with `last_fired` `2026-10-05T09:00:00Z` among them.
- The host starts cron at `2026-10-05T10:20:00Z`.

Postconditions:

- No trigger fired at the start: events holds exactly one event from cron with `scheduled` `2026-10-05T10:00:00Z`, the one fired before the restore, and telemetry has received cron's `service.started` (`S02`) and no `cron.*` event.
- `show` of `hourly` answers `last_fired` `2026-10-05T09:00:00Z` and `next` `2026-10-05T11:00:00Z`.
- Nothing fires for `hourly` until `11:00`; then it fires as in `A trigger fires on its slot`, with `scheduled` `2026-10-05T11:00:00Z`, and `show` answers `last_fired` `2026-10-05T11:00:00Z` and `next` `2026-10-05T12:00:00Z`.
- cron wrote nothing to stdout or stderr.

## A trigger created just before a slot fires at that slot

A trigger's first `next` is the first slot strictly after the moment it was created (`S06`), however soon that is: one created two seconds before `09:45` fires at `09:45`, while one created at `09:45:00` exactly is due first at `10:00`. Its first fire is like any other, run as the user who created it.

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's, and at `2026-10-05T09:44:58Z` the caller `u_7f3a9c21` created `crm_sync` with `when` `*/15 * * * *` (`S06`); the answer gave it the id `<trigger-id>`, `crn_` and 16 lowercase hexadecimal digits, and `next` `2026-10-05T09:45:00Z`, and nothing else has changed it.
- The clock passes `2026-10-05T09:45:00Z`.

Postconditions:

- At `2026-10-05T09:45:00Z`, or a moment after, events has received exactly one event from cron for the slot:

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.crm_sync.fired","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T09:45:00Z","trigger":"<trigger-id>","when":"*/15 * * * *"},"cause":"","depth":0}
  ```

  `<request-id>` is the fire's own, not the request id of the call that created the trigger. telemetry has received the fire's `cron.crm_sync.fired`, with the same request id, user, and attributes.
- `show` of `crm_sync` answers `last_fired` `2026-10-05T09:45:00Z` and `next` `2026-10-05T10:00:00Z`.
- cron wrote nothing to stdout or stderr.

## Two triggers due on the same slot each fire once

Triggers fire independently. At midnight on the first of November, `hourly` and `month_end` are both due; each fires once, as its own owner, under a request id of its own, so each fire is its own request in the trail. Which of the two is emitted first is not fixed.

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's triggers, except that it is now `2026-10-31T23:59:30Z`, cron has been serving throughout, and `hourly` last fired at `2026-10-31T23:00:00Z`, so its `next` is `2026-11-01T00:00:00Z`; `month_end` has never fired, and its `next` is `2026-11-01T00:00:00Z`.
- The clock passes `2026-11-01T00:00:00Z`.

Postconditions:

- At `2026-11-01T00:00:00Z`, or a moment after, events has received exactly these two events from cron, in either order, each with its own `id` and its own `request_id`:

  ```
  {"id":"<event-id-1>","time":"<time>","service":"cron","event":"cron.hourly.fired","request_id":"<request-id-1>","user":"u_7f3a9c21","attrs":{"scheduled":"2026-11-01T00:00:00Z","trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"},"cause":"","depth":0}
  {"id":"<event-id-2>","time":"<time>","service":"cron","event":"cron.month_end.fired","request_id":"<request-id-2>","user":"u_2b8e1d04","attrs":{"scheduled":"2026-11-01T00:00:00Z","trigger":"crn_1a4f8c6e9b3d7025","when":"@monthly"},"cause":"","depth":0}
  ```

  `<request-id-1>` and `<request-id-2>` differ. telemetry has received one `cron.hourly.fired` and one `cron.month_end.fired`, each under its fire's request id and user, with the same attributes.
- `show` of `hourly` answers `last_fired` `2026-11-01T00:00:00Z` and `next` `2026-11-01T01:00:00Z`; `show` of `month_end` answers `last_fired` `2026-11-01T00:00:00Z` and `next` `2026-12-01T00:00:00Z`.
- cron wrote nothing to stdout or stderr.

## A trigger fires while the event bus cannot be reached

A fire does not wait for the bus. When events cannot take the event — it is not installed, is stopped, or cannot be reached — cron keeps it and tries again, sending the events it holds one at a time, in the order it emitted them, and an event's retry window, about five minutes, runs from its first delivery attempt. Were events to take events again within the window, the event would reach it then, late, and nothing else would differ. Here events stays down past the window, so cron drops the event, records `event.lost` in its trail, carrying the dropped event's `id` among its attributes, and writes one line to stderr, whose text beyond the `cron: ` prefix is not fixed. A dropped event is not lost from the trigger: the fire happened, the trail's `cron.hourly.fired` records it, and `last_fired` is the slot all the same. The next slot fires as usual.

Command:

```
$ cron
```

Output:

```
cron: <text>
```

Does not exit. stdout is empty; stderr holds the one line above, written when the event is dropped, about five minutes after the slot.

Preconditions:

- The preamble's: cron has been serving since `2026-10-05T09:32:00Z`, and `hourly`'s `next` is `2026-10-05T10:00:00Z`.
- events was stopped at `2026-10-05T09:50:00Z` and stays stopped until `2026-10-05T10:30:00Z`; telemetry takes every event throughout.

Postconditions:

- At `2026-10-05T10:00:00Z` telemetry received the fire's `cron.hourly.fired`, as in `A trigger fires on its slot`, under the fire's request id and `u_7f3a9c21`.
- About five minutes later, telemetry received an `event.lost` from cron whose attributes carry the `evt_` id cron gave the dropped `cron.hourly.fired`.
- Once events serves again, it holds no event from cron with `scheduled` `2026-10-05T10:00:00Z`, and nothing brings it back.
- `show` of `hourly` answers `last_fired` `2026-10-05T10:00:00Z` and `next` `2026-10-05T11:00:00Z`; at `11:00` it fires as usual, and events, serving again, receives that `cron.hourly.fired`.

## A deleted trigger never fires again

Deleting a trigger removes it (`S10`), and with it every slot it would have fired: nothing fires under its id again. The slug is free once more, but a trigger created with it later is a new trigger, with a new id, firing on its own schedule (`S06`).

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's, and at `2026-10-05T09:40:00Z` the caller `u_7f3a9c21` deleted `hourly` (`S10`), which emitted `cron.hourly.deleted`.
- The clock passes `2026-10-05T10:00:00Z` and every hour after it.

Postconditions:

- events has received no `cron.hourly.fired` after the deletion, and no event from cron whose `trigger` is `crn_3f9a1c7e5b2d8046` after its `cron.hourly.deleted`; telemetry likewise.
- `show` of `hourly` is refused with `no trigger named 'hourly'` (`S07`).
- cron wrote nothing to stdout or stderr.
