# Stories — stopping

How events stops, what becomes of the emits and deliveries in flight when it does, and how the next events picks up where it left off. `systemctl stop` and `systemctl restart` send `SIGTERM`; a developer's `Ctrl-C` sends `SIGINT`, which events treats the same way. events closes its own copy of the socket and nothing more: the socket belongs to systemd, which keeps it open, and events never removes `/run/ikigenba/events.sock`. It waits at most `DRAIN_SECONDS` (`S02`), which on a host is always less than the time the service unit allows before systemd kills it. When events is told to stop, with `SIGTERM` or `SIGINT`, it stops taking new connections and finishes the requests it has already accepted, a sibling's post to `/emit` among them, so an event that reached events is answered and, when accepted, stored before it exits (`S07`); connections made after that wait in the socket's queue for the next events. A delivery in flight at the signal, a `POST` to a subscriber's `/events` that has not been answered (`S11`), is waited for like a request, within the same `DRAIN_SECONDS`: one the subscriber answers in time counts as it would at any other moment, and one still unanswered at the drain deadline is abandoned, its connection closed without waiting longer. No delivery starts after the signal, not a first attempt and not a retry: an event emitted during the drain, and every event waiting for a subscriber, is delivered by the next events. An abandoned delivery does not count as one of the subscriber's attempts (`S11`), and the next events starts its deliveries, each event's again included, at `attempt` 1, so stopping and starting events never brings a subscriber nearer to being paused. An abandoned delivery is not trouble and nothing is lost by it: the subscriber's cursor never moved past the event, so the next events to start delivers the same event to that subscriber again, which is safe because a subscriber tells an event it has seen by its `id`. A delivery is not a request events accepted, so an abandoned one is not counted in the `stopped with <n> requests unfinished` line below. Once its requests and deliveries have finished or been abandoned, and each request has recorded its `request.finished`, events records `service.stopping` with `reason`, the name of the signal it received, `SIGTERM` or `SIGINT`. That is the last record of its trail: events sends everything it recorded before exiting, within the same drain deadline, and a record it has not sent when the deadline comes goes to stderr as an `undelivered event` line instead (`S02`). A `service.started` with no `service.stopping` before the next one is how the trail shows an events that died rather than stopped. A request still running at the drain deadline, a `search` over a large log say, is cut off: its connection is closed without the rest of its answer. Losing a request is trouble, so events says how many it lost and exits non-zero. The actor is the host, whether systemd or a developer at a terminal standing in for it. Unless a story says otherwise, `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds after the signal; the host's services file lists `repos`, `scripts`, and `telemetry`, enabled; repos declares that it emits `repo.pushed`, and scripts that it accepts every event (`S06`); scripts is a subscriber with status `ok` (`S10`); and telemetry takes every record.

## The host stops events

Nothing is in flight on the bus and every request finishes in time, so the stop is silent.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

events exits 0. Nothing is on stdout or stderr.

Preconditions:

- events is serving as process `<pid>`, on the socket it was passed.
- Every request events has accepted finishes within 4 seconds of the signal, leaving events at least a second of the drain to send its last records; no delivery is in flight, and no event is waiting for any subscriber.

Postconditions:

- Every request accepted before the signal received its full response.
- telemetry has received every record events made. The last is `service.stopping`, after the `request.finished` of every request accepted before the signal, with an empty request id and an empty user:

  ```
  {"time":"<time>","service":"events","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  With `SIGINT` the `reason` is `SIGINT`.
- `/run/ikigenba/events.sock` still exists, and connections made to it after events exited, a sibling's next emit among them, wait in the socket's queue for the next events to answer.

## The host stops events while a request outlasts the drain

A request still running at the deadline is cut off, and the drain has used the whole deadline, so events has no time left to send its last records to telemetry: each goes to stderr as an `undelivered event` line instead. The trail then ends with the cut-off request's `request.started` and no `request.finished`, which is how it shows a request that never finished.

Command:

```
$ kill -TERM <pid>
```

Output:

```
events: undelivered event: {"time":"<time>","service":"events","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
events: stopped with <n> requests unfinished
```

events exits 1, 5 seconds after the signal. The text is on stderr; stdout is empty. The output is one or more `events: undelivered event: ` lines and the `stopped with` line, whose position among them is not fixed; the `service.stopping` line shown is always among them. `<n>` is the number of requests still running at the deadline; when it is 1 the last line reads `events: stopped with 1 request unfinished`. Any other record events had made and not yet sent when the deadline came is written as an `undelivered event` line too, in the order the records were made, and a cut-off request's `request.finished` may be written the same way, never sent.

Preconditions:

- events is serving as process `<pid>`, on the socket it was passed.
- `<n>` of the requests events has accepted are still running 5 seconds after the signal, and no delivery is in flight.

Postconditions:

- Every request that finished within 5 seconds of the signal received its full response; the `<n>` that did not were cut off.
- telemetry has received no `service.stopping` from this events, and no `request.finished` for any of the `<n>` requests cut off; each of them has its `request.started` in the trail.
- `/run/ikigenba/events.sock` still exists, and connections made to it after events exited wait in the socket's queue for the next events to answer.

## The host stops events while a sibling's emit is in flight

repos' post reached events before the signal, so events finishes it as it finishes any request: the event is stored and repos is answered 204, and repos has nothing to retry. Nothing was lost, so the stop is silent.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

events exits 0. Nothing is on stdout or stderr.

Preconditions:

- events is serving as process `<pid>`, on the socket it was passed.
- repos' `POST /emit` of `evt_3c8e1a7f5b2d9604`, a `repo.pushed` repos has declared, was accepted before the signal and is still being read when the signal comes; it is answered within 3 seconds of the signal, leaving events time within the drain to send its last records. events holds no event with that id.
- No other request is running at the signal, and no delivery is in flight.

Postconditions:

- repos received `HTTP/1.1 204 No Content` for its post.
- Once an events is serving again, `search` (`S09`) finds `evt_3c8e1a7f5b2d9604` in the log, with a `seq` after every event the log held before it, and the trail holds events' `event.accepted` with `event=evt_3c8e1a7f5b2d9604` (`S14`), recorded before its `service.stopping` with `reason=SIGTERM`.
- This events did not deliver `evt_3c8e1a7f5b2d9604`: it was stored after the signal. The next events to start delivers it to scripts, with `attempt` 1.
- `/run/ikigenba/events.sock` still exists, and connections made to it after events exited, a sibling's next emit among them, wait in the socket's queue for the next events to answer.

## The host stops events while a delivery finishes within the drain

A delivery in flight at the signal is not cut short for it: events waits for scripts' answer as it waits for a request it accepted, and the answer counts as it would at any other moment. Once the answer is in, events records `service.stopping` and exits, without waiting out the rest of the deadline.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

events exits 0. Nothing is on stdout or stderr.

Preconditions:

- events is serving as process `<pid>`, on the socket it was passed.
- events is delivering `evt_3c8e1a7f5b2d9604` to scripts at the signal, scripts' cursor is the `seq` before it, and no other event is waiting for any subscriber.
- scripts answers that delivery ok 2 seconds after the signal, leaving events at least a second of the drain to send its last records.
- No request is running at the signal, and none arrives after it.

Postconditions:

- Once an events is serving again, `subscribers` (`S10`) answers scripts `ok`, its cursor the `seq` of `evt_3c8e1a7f5b2d9604`, and its lag 0; the next events does not deliver `evt_3c8e1a7f5b2d9604` to scripts again.
- The trail holds events' `event.delivered` for the delivery (`S14`), and after it events' last record, `service.stopping` with `reason=SIGTERM`.
- `/run/ikigenba/events.sock` still exists.

## The host stops events while a delivery outlasts the drain

events must exit before the service unit's stop timeout, so a delivery scripts has still not answered at the drain deadline is abandoned: events closes the connection and leaves scripts' cursor where it was. scripts may have done its work, or may still be doing it; either way the event is delivered again by the next events. The drain has used the whole deadline, so events has no time left to send its last records to telemetry: each goes to stderr as an `undelivered event` line instead (`S02`).

Command:

```
$ kill -TERM <pid>
```

Output:

```
events: undelivered event: {"time":"<time>","service":"events","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
```

events exits 0, 5 seconds after the signal. The text is on stderr; stdout is empty. Any other record events had made and not yet sent when the deadline came is written as an `undelivered event` line too, in the order the records were made, `service.stopping` last of them. stderr holds no other line: no `stopped with` line, since no request was cut off.

Preconditions:

- events is serving as process `<pid>`, on the socket it was passed.
- `EVENTS_DELIVERY_TIMEOUT_SECONDS` is 30, so a delivery may wait longer than the drain.
- events is delivering `evt_3c8e1a7f5b2d9604` to scripts at the signal, and scripts' cursor is the `seq` before it; scripts has not answered 5 seconds after the signal. No other event is waiting for any subscriber.
- No request is running at the signal, and none arrives after it.

Postconditions:

- scripts' connection for the delivery was closed without events waiting for its answer.
- scripts' cursor has not moved: once an events is serving again, `subscribers` (`S10`) answers scripts with its cursor the `seq` before `evt_3c8e1a7f5b2d9604`, until that events delivers it again (story below).
- The abandoned delivery did not count as an attempt. The trail holds no `event.delivered` for it, and no `service.stopping` from this events; the line above is where it is recorded.
- `/run/ikigenba/events.sock` still exists, and connections made to it after events exited wait in the socket's queue for the next events to answer.

## The host starts events after a delivery was abandoned

The next events finds scripts' cursor where the last one left it and delivers from there, so the event whose delivery was abandoned reaches scripts again: the same event, with the same `id` and the same `seq`. scripts, which may have handled it the first time, knows it by its `id`.

Command:

```
$ sudo systemctl start ikigenba-events.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `ikigenba-events.socket` is active and `ikigenba-events.service` is not running; the events that last ran abandoned its delivery of `evt_3c8e1a7f5b2d9604` to scripts at its drain deadline, as the story above tells.
- scripts answers every delivery ok.

Postconditions:

- `ikigenba-events.service` is `active`, and events is serving on `/run/ikigenba/events.sock`.
- scripts has received a `POST /events` whose body is `evt_3c8e1a7f5b2d9604` in its delivered form, with the same `id`, `seq`, and `received` as the delivery that was abandoned (`S11`), and with `attempt` 1.
- Once scripts has answered, `subscribers` (`S10`) answers scripts `ok`, its cursor the `seq` of `evt_3c8e1a7f5b2d9604`, and its lag 0, and the trail holds this events' `event.delivered` with `event=evt_3c8e1a7f5b2d9604` and `service=scripts` (`S14`).
