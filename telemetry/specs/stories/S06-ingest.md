# Stories — ingest

How an event gets into the trail: a sibling service posts it to telemetry's socket at `POST /ingest`, one event per request, and telemetry stores it. The path is the one route that takes no identity headers: a sibling is not a caller, so nothing it posts names `X-User-Id`, `X-User-Email`, or `X-Request-Id`, and telemetry neither looks for them nor minds them. The body is the event as the sibling's own stories show it (dummy's `S2-serve.md`), one JSON object whose members are, in this order, `time`, `service`, `event`, `request_id`, `user`, and `attrs`:

```
{"time":"<time>","service":"<service>","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`time` is when the sibling recorded the event, UTC to the microsecond, as `2026-10-02T14:03:07.123456Z`: exactly six fractional digits and a `Z`. `service` is the sibling's name and is never empty. `event` is a lowercase dotted name, `request.started` say. `request_id` and `user` are strings, each empty when the event has none. `attrs` is an object, flat: every value is a string, a number, or `true` or `false`, and every key is lowercase, starting with a letter, with digits and single underscores allowed. A stored record is the event exactly as posted, and every tool answers it in that same shape (`S09-search.md`, `S11-trace.md`). Records with the same time keep the order telemetry received them. On a host the sibling connects to `/run/ikigenba/telemetry.sock` directly, never through nginx, and nginx answers the public `/ingest` 404 (`S14-on-a-space.md`); the requests below are made on the socket by a developer, as the `ikigenba` user, standing in for a sibling. An event posted to `/ingest` adds exactly one record, the sibling's, and none of telemetry's own: telemetry records its own requests (`S12-own-events.md`) for every path but this one. A refusal stores nothing, and its body is not fixed. telemetry writes nothing to stderr for anything in this group: a refused or lost event is the sender's to report, as its own stories tell, and a sender that gets a 500 tries again before writing the event to its stderr as undelivered.

## A sibling service posts an event

dummy has answered a request and records its `request.finished`. The post carries the event and nothing else, and telemetry stores it as sent. Nothing waits on it but the sender's queue.

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":1842,"status":200}}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- telemetry is serving on the socket it was passed, with a database it can write.
- The trail holds no record whose request id is `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`.
- The trail holds no record from `dummy` later than `2026-10-02T14:03:07.123456Z`, and dummy posts no other record before the tool calls below.

Postconditions:

- The trail holds one more record, the event exactly as posted. A `trace` of `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` (`S11-trace.md`) answers `{"records":[<record>]}` where `<record>` is the posted object, member for member, every member's value equal to the one posted, written as `S09-search.md` writes a record, compactly with `attrs` sorted by key; a `search` with `services` `["dummy"]` (`S09-search.md`) answers it first, as the newest record from dummy; `count` (`S10-count.md`) counts it; and `catalog` (`S08-catalog.md`) lists `dummy` with the event `request.finished` carrying the attribute keys `duration_us` and `status`.
- No record of telemetry's own was added: the trail holds no `request.started` or `request.finished` for this post.
- telemetry wrote nothing to stderr.

## A sibling posts an event with no request and no user

An event no request caused, a `service.started` say, has an empty `request_id` and an empty `user`. Both are stored empty, as sent, and the record is found by its service and event rather than by a request.

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"c604e32"}}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- telemetry is serving on the socket it was passed, with a database it can write.
- The trail holds no `service.started` from `dummy` later than `2026-10-02T14:03:07.123456Z`, and dummy posts no other before the search below.

Postconditions:

- The trail holds one more record, the event exactly as posted, with `"request_id":""` and `"user":""`. A `search` with `services` `["dummy"]` and `events` `["service.started"]` answers it first.
- telemetry wrote nothing to stderr.

## A caller sends the ingest path a method it does not take

`/ingest` takes only `POST`. Any other method is answered 405 naming the one method the path takes, and nothing is read.

Request:

```
GET /ingest HTTP/1.1
```

```
PUT /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"c604e32"}}
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: POST
```

Status 405. Exactly one `Allow` header, whose value is `POST`. The body is not fixed.

Preconditions:

- telemetry is serving on the socket it was passed.

Postconditions:

- Nothing has changed. No record was stored, whatever the body held.
- telemetry wrote nothing to stderr.

## A sibling posts an event that is not declared as JSON

The body must be declared `application/json`. A post with no `Content-Type`, or one naming another media type, is refused unread, however good its body. A parameter on the type, `application/json; charset=utf-8`, is still JSON and is taken.

Request:

```
POST /ingest HTTP/1.1
Content-Type: text/plain

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"c604e32"}}
```

```
POST /ingest HTTP/1.1

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"c604e32"}}
```

Response:

```
HTTP/1.1 415 Unsupported Media Type
```

Status 415. The body is not fixed.

Preconditions:

- telemetry is serving on the socket it was passed.

Postconditions:

- Nothing has changed. No record was stored.
- telemetry wrote nothing to stderr.

## A sibling posts a body that is too large

An event is metadata, never data, so one is small. A body longer than 65536 bytes is refused without being parsed, whatever it holds.

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

<a body of 65537 bytes or more>
```

Response:

```
HTTP/1.1 413 Request Entity Too Large
```

Status 413. The body is not fixed.

Preconditions:

- telemetry is serving on the socket it was passed.
- The request body is longer than 65536 bytes. A body of exactly 65536 bytes is not too large and is judged on its content.

Postconditions:

- Nothing has changed. No record was stored.
- telemetry wrote nothing to stderr.

## A sibling posts a body that is not JSON

A body that is not one well-formed JSON text is refused. That covers a truncated object, text that is not UTF-8, two JSON texts in one body, and an empty body.

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy"
```

Response:

```
HTTP/1.1 400 Bad Request
```

Status 400. The body is not fixed.

Preconditions:

- telemetry is serving on the socket it was passed.

Postconditions:

- Nothing has changed. No record was stored.
- telemetry wrote nothing to stderr.

## A sibling posts JSON that is not an event

A well-formed JSON text that is not exactly an event is refused the same way. The body below leaves `user` out; each of these is refused identically:

- a value that is not an object, `[]` or `"request.finished"` say;
- an object missing one of the six members, or holding one twice, or holding any other member, `"level":"info"` say;
- a `time` that is not a string, or not in the shape above: `2026-10-02T14:03:07Z` (no fraction), `2026-10-02T14:03:07.123Z` (three digits), `2026-10-02T14:03:07.123456+00:00` (an offset), `2026-10-02 14:03:07.123456Z` (a space);
- a `service` that is not a string, or is `""`;
- an `event` that is not a string, or not a name: the shape is two lowercase parts joined by one dot, each starting with a letter, so `started`, `Request.Finished`, `request..finished`, and `request.finished.` are refused;
- a `request_id` or `user` that is not a string;
- an `attrs` that is not an object, or holds a nested value, `"widget":{"id":"wgt_1"}` or `"tags":["a"]`, a `null`, a key that is not lowercase-with-underscores, `Widget-Id` or `_id` say, or the same key twice.

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","attrs":{"duration_us":1842,"status":200}}
```

Response:

```
HTTP/1.1 400 Bad Request
```

Status 400. The body is not fixed.

Preconditions:

- telemetry is serving on the socket it was passed.

Postconditions:

- Nothing has changed. No record was stored.
- telemetry wrote nothing to stderr.

## A sibling posts an event telemetry cannot store

The post is good, but the store cannot take it: the database can no longer be written to. telemetry answers 500 so the sender tries again, and keeps serving; it writes nothing to stderr, since the event is the sender's and the sender reports what it could not deliver (dummy's `S2-serve.md`).

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":1842,"status":200}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
```

Status 500. The body is not fixed.

Preconditions:

- telemetry is serving on the socket it was passed.
- telemetry's database cannot be written to: its storage has begun refusing writes since telemetry opened it, the filesystem holding `state/telemetry.db` full, say.

Postconditions:

- No record was stored: a `trace` of `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` answers `{"records":[]}`.
- telemetry is still serving; the next post after the database can be written again is stored.
- telemetry wrote nothing to stderr. The sender, after its own retries, writes the event to its stderr as `dummy: undelivered event: <event>`.

## A sibling posts an event older than the retention window

A sibling's clock, or a long queue, can hand telemetry an event already older than the window. It is taken all the same, since refusing it would lose it, and the next sweep removes it like any other record of its age (`S07-retention.md`).

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-09-01T09:00:00.000000Z","service":"dummy","event":"request.finished","request_id":"8c4d1e2f3a5b4c6d9e0f1a2b3c4d5e6f","user":"u_7f3a9c21","attrs":{"duration_us":1842,"status":200}}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- telemetry is serving on the socket it was passed, with a database it can write.
- `RETENTION_DAYS` is unset, so the window is 15 days, and today is `2026-10-02`: the event's time is 31 days ago.

Postconditions:

- The trail holds the record until the next sweep: a `trace` of `8c4d1e2f3a5b4c6d9e0f1a2b3c4d5e6f` answers it. After the next sweep, within the hour, the same trace answers `{"records":[]}`, and no tool answers the record.
- telemetry wrote nothing to stderr.

## A sibling's post adds no record of telemetry's own

telemetry records every request it serves as `request.started` and `request.finished` (`S12-own-events.md`), except a post to `/ingest`: the trail would otherwise hold two records of telemetry's own for every one record of anyone else's. The sibling's event is the only record a post adds, and a refused post adds none.

Request:

```
POST /ingest HTTP/1.1
Content-Type: application/json

{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":1842,"status":200}}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- telemetry is serving on the socket it was passed, with a database it can write.

Postconditions:

- A `search` with `services` `["telemetry"]` and `attrs` `{"path":"/ingest"}` answers `{"records":[]}`, now and after any number of posts.
- The sibling's record was stored, as in `A sibling service posts an event`.
