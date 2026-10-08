# Stories — ingest

How an event gets onto the bus: a service emits it by posting it to events' socket at `POST /emit`, one event per request, and events stores it in its log. The path is reached over the socket only: on a host a service connects to `/run/ikigenba/events.sock` directly, and nginx answers the public `/emit` 404 (`S17`). It takes no identity headers: a service emitting is not a caller, and events neither looks for `X-User-Id`, `X-User-Email`, or `X-Request-Id` on it nor minds them. The body is the event as its producer emitted it, one JSON object of exactly nine members:

```
{"id":"<id>","time":"<time>","service":"<service>","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>},"cause":"<cause>","depth":<depth>}
```

`id` is the event's own id, which its producer gives it: `evt_` followed by sixteen lowercase hexadecimal digits. `time` is when the producer emitted it, UTC to the microsecond, as `2026-10-05T09:40:12.318442Z`: exactly six fractional digits and a `Z`. `service` is the producer's name and is never empty. `event` is the event's name: two or more lowercase words joined by single `.`s, each word a lowercase ASCII letter followed by lowercase letters and digits, with single `_` allowed between them, so that the whole matches `^[a-z][a-z0-9]*(_[a-z0-9]+)*(\.[a-z][a-z0-9]*(_[a-z0-9]+)*)+$`, `repo.pushed` or `cron.hourly.fired` say; a name never holds `*`. `request_id` and `user` are the request and the user that caused it, strings, each empty when there is none. `attrs` is an object, flat: every value is a string, a number, or `true` or `false`, and every key is lowercase, starting with a letter, with digits and single underscores allowed. `cause` is the id of the event whose handling led the producer to emit this one, and is empty when a person or an agent started the chain; `depth` is 0 when `cause` is empty, and otherwise the cause's depth plus one. events accepts an event only when its producer, the service its `service` names, has declared that it emits events of that name (`S06`): its declaration has an `emits` element whose `event` equals the name or, failing that, one whose `event` is a pattern that matches it; and only when its `depth` is at most `EVENTS_DEPTH_MAX`, 8 by default (`S02`). That is all it asks of an event beyond its shape: its attributes are not compared with the attribute names its producer declared, its `cause` is not looked up in the log, and its `depth` is not compared with the cause's, so an event whose cause the log does not hold, or no longer holds, is accepted all the same. An event it accepts it stores with two members more: `seq`, the event's place in the log, one more than the last event accepted before it, and `received`, the moment events accepted it, UTC to the microsecond in the same layout as `time`; `search` (`S09`) answers it so, and events delivers it to every service that accepts it (`S10`, `S11`). events keeps one copy of an event: an event whose `id` the log already holds is not stored again, and is answered 204 before events looks at its producer's declaration or its depth. events judges a post in this order, and the first check it fails is the answer: the method, the content type, the size of the body as it arrived, the shape of the body, the size of the event as events writes it out, whether the log already holds its `id`, and then the producer's declaration and the depth. Every answer has an empty body, and the status says everything a producer acts on: 204, the event is in the log; 4xx, the event itself is at fault and sending it again cannot succeed; 500, it can be sent again. A post to `/emit` adds no `request.started` or `request.finished` to events' trail, whatever its answer; a refused post stores nothing and records no `event.accepted` (`S14`), and events writes nothing to stderr for any post in this group: a refused or lost event is its producer's to report, as the producer's own stories tell.

events is serving on the host (`S02`) with every setting at its default, and holds `repos`', `scripts`', and `sites`' declarations as `S06`'s preamble gives them; its log holds `S09`'s eight events, `seq` 4175 to 4182. The requests below are made on the socket by a developer, as the `ikigenba` user, standing in for the service the event names.

## A service emits an event

`repos` has accepted a push to `main` of `rep_7b3e9a0c5d1f2846` and emits the `repo.pushed` it declared. events stores it, after the head of the log, and answers once it is stored.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: the log holds no event `evt_578b5c72dc4ee60c`, and its last event has `seq` 4182.

Postconditions:

- events' log holds the event, with `seq` 4183. A `search` (`S09`) with `{"limit":1}` answers it first, as the newest event of the log:

  ```
  {"records":[{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":4183,"received":"<received>"}],"cursor":"<cursor>"}
  ```

  where every member but `seq` and `received` is as posted, and `<received>` is the moment events accepted the event, in the layout of `time`.
- `catalog` (`S08`) counts eight `repo.pushed`, the newest of them this one.
- telemetry has received, from events, one `event.accepted` (`S14`):

  ```
  event.accepted cause= event=evt_578b5c72dc4ee60c
  ```

- events wrote nothing to stderr.

## cron emits an event a pattern it declares matches

`cron` declares the names it emits as five patterns, `cron.*.fired` among them (`S06`), and here its trigger `hourly` fires and it emits `cron.hourly.fired`. No `emits` element of its declaration equals the name, but the pattern `cron.*.fired` matches it, so the name is declared, and events stores the event without asking `cron` again.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_f93076e37ecfe1df","time":"2026-10-05T10:00:00.004218Z","service":"cron","event":"cron.hourly.fired","request_id":"5c1e8a3f7b2d9064a6e0c4f8b2d7a193","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T10:00:00Z","trigger":"crn_3a8f2d6c9e1b4705","when":"0 * * * *"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's, and the services file also enables `cron`, on `/run/ikigenba/cron.sock`, and events holds its declaration, `{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]}],"accepts":[]}` (`S06`). The log holds no event `evt_f93076e37ecfe1df`, and its last event has `seq` 4182.

Postconditions:

- events did not ask `cron` for its declaration.
- events' log holds the event, with `seq` 4183. A `search` (`S09`) with `{"limit":1}` answers it first, as the newest event of the log:

  ```
  {"records":[{"id":"evt_f93076e37ecfe1df","time":"2026-10-05T10:00:00.004218Z","service":"cron","event":"cron.hourly.fired","request_id":"5c1e8a3f7b2d9064a6e0c4f8b2d7a193","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T10:00:00Z","trigger":"crn_3a8f2d6c9e1b4705","when":"0 * * * *"},"cause":"","depth":0,"seq":4183,"received":"<received>"}],"cursor":"<cursor>"}
  ```

  where every member but `seq` and `received` is as posted, and `<received>` is the moment events accepted the event, in the layout of `time`.
- `catalog` (`S08`) counts one `cron.hourly.fired`, emitted by `cron` with the attribute names `trigger`, `when`, and `scheduled`, which the pattern `cron.*.fired` declares.
- telemetry has received, from events, one `event.accepted` (`S14`):

  ```
  event.accepted cause= event=evt_f93076e37ecfe1df
  ```

- events wrote nothing to stderr.

## A service emits an event another event caused

`sites`, handling the delivery of `evt_8c3f1a6e2d9b4075`, publishes the site that push changed, and emits `site.published` naming the push as its cause, one deeper than the push. events stores the event with the `cause` and `depth` it was emitted with, and `search` follows them (`S09`).

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_aea63367c29a80b2","time":"2026-10-05T09:40:20.104733Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_7b3e9a0c5d1f2846","sha":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4"},"cause":"evt_8c3f1a6e2d9b4075","depth":1}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: the log holds no event `evt_aea63367c29a80b2`, and its last event has `seq` 4182.

Postconditions:

- events' log holds the event, with `seq` 4183, `cause` `evt_8c3f1a6e2d9b4075`, and `depth` 1. A `search` (`S09`) with `{"cause":"evt_8c3f1a6e2d9b4075"}` answers it alone.
- telemetry has received, from events, one `event.accepted` (`S14`):

  ```
  event.accepted cause=evt_8c3f1a6e2d9b4075 event=evt_aea63367c29a80b2
  ```

- events wrote nothing to stderr.

## A service emits an event a second time

A producer that did not hear events' answer, its connection cut say, sends the same event again, with the same `id`. events already holds it and keeps the one copy, and answers 204 all the same, so the producer counts it delivered and moves on.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's, but `A service emits an event` has run: the log holds `evt_578b5c72dc4ee60c` with `seq` 4183, and its last event has `seq` 4183.

Postconditions:

- Nothing has changed. The log holds `evt_578b5c72dc4ee60c` once, with `seq` 4183, and its last event is still 4183: a `search` (`S09`) with `{"request_id":"b4ea410fb9102f29a422eb14ab2f2d0f"}` answers one record. No service is delivered the event a second time on account of the post.
- events recorded no second `event.accepted` (`S14`).
- events wrote nothing to stderr.

## A service emits again an event it no longer declares

The duplicate check comes first. `sites` has been deployed without `site.published`, and its emitter sends again the `site.published` the log already holds, 4177, which it never heard events take. events finds the `id` in its log and answers 204 without judging the copy by the declaration it now holds, so the producer counts it delivered rather than lost. A copy of an event the log holds whose `depth` is now over a lowered `EVENTS_DEPTH_MAX` is answered 204 the same way.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's, except that `sites` now declares `{"emits":[],"accepts":["repo.pushed"]}`, and events holds that declaration (`S06`). The log holds `evt_5d1f8b3e7c0a2649` as `seq` 4177.

Postconditions:

- Nothing has changed. The log holds `evt_5d1f8b3e7c0a2649` once, as 4177, and still ends at 4182. events did not ask `sites` for its declaration, and recorded no `event.accepted` (`S14`).
- events wrote nothing to stderr.

## A service emits an event with attributes it did not declare

A declaration's attribute names tell agents what an event carries (`S08`); events does not hold an event to them. `repos` declares `repo`, `ref`, `old`, and `new` for `repo.pushed`, and here emits one that also carries `forced`, a boolean, and is accepted as emitted.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_6a2e9c4f1b8d3075","time":"2026-10-05T09:42:05.771309Z","service":"repos","event":"repo.pushed","request_id":"3d8b1f5e9a2c4706b1e5d9a3f7c2e608","user":"u_7f3a9c21","attrs":{"forced":true,"new":"656cf2d133187c8df95247f2866028de71159b42","old":"eee65f53e9421ce50211670eae679f02e8d28a79","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: the log ends at `seq` 4182 and holds no event `evt_6a2e9c4f1b8d3075`.

Postconditions:

- events' log holds the event, with `seq` 4183 and its five attributes as emitted: a `search` (`S09`) with `{"attrs":{"forced":true}}` answers it alone.
- `catalog` (`S08`) still lists `repo.pushed` with the attribute names `repos` declares, `repo`, `ref`, `old`, and `new`.
- telemetry has received, from events, one `event.accepted` (`S14`) with `event` `evt_6a2e9c4f1b8d3075` and `cause` empty.
- events wrote nothing to stderr.

## A service emits an event whose cause events does not hold

events does not look an event's cause up: a cause swept from the log, or one events never held, is still the producer's account of the chain, and the event is accepted for its shape and its depth. Here `sites` names as its cause an event the log does not hold.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_4c7f2a9e5d1b0863","time":"2026-10-05T09:42:40.218554Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_9c2e4b7a1d3f8e05","sha":"dba41ecccc3fc1626e53a13043b026c48bbf33fe"},"cause":"evt_9b3d7f1a5c2e8406","depth":3}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: the log ends at `seq` 4182, and holds neither `evt_4c7f2a9e5d1b0863` nor `evt_9b3d7f1a5c2e8406`.

Postconditions:

- events' log holds the event, with `seq` 4183, `cause` `evt_9b3d7f1a5c2e8406`, and `depth` 3, as emitted.
- telemetry has received, from events, one `event.accepted` (`S14`):

  ```
  event.accepted cause=evt_9b3d7f1a5c2e8406 event=evt_4c7f2a9e5d1b0863
  ```

- events wrote nothing to stderr.

## A service emits an event older than the retention window

A producer's clock, or a long outage, can hand events an event whose `time` is already older than `EVENTS_RETENTION_DAYS`. It is accepted all the same, since refusing it would lose it. Its age, for the sweep, counts from when events received it (`S13`), so it is delivered like any other and swept with the events received when it was, once no `ok` or `paused` subscriber still needs it.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_3e8a1c5f9b2d7046","time":"2026-09-30T08:15:00.000000Z","service":"repos","event":"repo.pushed","request_id":"7a1d5c9e3f2b4860d4a8c2e6f0b9d317","user":"u_7f3a9c21","attrs":{"new":"c3cd6089065c3146e80a9c222670bbe4f4c54977","old":"023c39c200661fccd268a29a0d347301ef56e64d","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: `EVENTS_RETENTION_DAYS` is unset, so the window is 2 days, it is `2026-10-05T09:43:00Z`, and the event's `time` is five days before. The log ends at `seq` 4182.

Postconditions:

- events' log holds the event, with `seq` 4183 and a `received` of the moment it was accepted, on 2026-10-05. A `search` (`S09`) with `{"limit":1}` answers it first, as the newest event of the log, though its `time` is older than any other's.
- The sweep does not remove it before it removes the events received on 2026-10-05 (`S13`).
- telemetry has received, from events, one `event.accepted` (`S14`) with `event` `evt_3e8a1c5f9b2d7046` and `cause` empty.
- events wrote nothing to stderr.

## A service emits an event it has not declared

`repos` declares that it emits `repo.pushed` and nothing else, and here emits `repo.deleted`. The declaration events holds does not list the name, so events asks `repos` again first (`S06`); `repos` still does not declare it, and the event is refused. No service could be relying on an event its producer never declared.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_1ead8f062d15a4f4","time":"2026-10-05T09:41:03.552190Z","service":"repos","event":"repo.deleted","request_id":"0cc022238daceee2092f073ff80b9465","user":"u_7f3a9c21","attrs":{"repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 422 Unprocessable Entity
```

Status 422. The body is empty.

Preconditions:

- The preamble's, and `repos` answers `GET /declarations` with the declaration `S06`'s preamble gives, which does not list `repo.deleted`.

Postconditions:

- Between the post and the answer, events asked `repos` for its declaration, once.
- Nothing was stored: the log still ends at `seq` 4182, and a `search` (`S09`) with `{"events":["repo.deleted"]}` answers `{"records":[]}`. events recorded no `event.accepted`.
- events wrote nothing to stderr.

## cron emits a name its pattern does not match

`cron` declares the names it emits as its five patterns and nothing else, and here emits `cron.hourly.daily.fired`, one word longer than `cron.*.fired`: a `*` word matches exactly one word, so that pattern does not match it, and none of the other four does. The declaration events holds does not declare the name, so events asks `cron` again first (`S06`); `cron` still declares only its five patterns, and the event is refused. `cron.fired`, one word shorter, is refused the same way.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_2ebe3bd7cfdbd28c","time":"2026-10-05T10:00:00.003127Z","service":"cron","event":"cron.hourly.daily.fired","request_id":"8a4d2f6c0e9b3157d3f7a1c5e9b2d460","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T10:00:00Z","trigger":"crn_3a8f2d6c9e1b4705","when":"0 * * * *"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 422 Unprocessable Entity
```

Status 422. The body is empty.

Preconditions:

- The preamble's, and the services file also enables `cron`, on `/run/ikigenba/cron.sock`; events holds its declaration, and `cron` answers `GET /declarations` with the same, `{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]}],"accepts":[]}` (`S06`).

Postconditions:

- Between the post and the answer, events asked `cron` for its declaration, once.
- Nothing was stored: the log still ends at `seq` 4182, and a `search` (`S09`) with `{"events":["cron.hourly.daily.fired"]}` answers `{"records":[]}`. events recorded no `event.accepted`.
- events wrote nothing to stderr.

## A service emits an event deeper than a chain may go

Events that cause events can loop: a service that reacts to its own events, or two that react to each other's, would fill the log for ever. `EVENTS_DEPTH_MAX` bounds every chain: an event whose `depth` is greater is refused, and the chain stops there. Here `depth` 9 is one past the default 8.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_ef27e9589c6948f6","time":"2026-10-05T09:41:30.006215Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_7b3e9a0c5d1f2846","sha":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4"},"cause":"evt_d30c01c3f252edc8","depth":9}
```

Response:

```
HTTP/1.1 422 Unprocessable Entity
```

Status 422. The body is empty.

Preconditions:

- The preamble's, except that events' log holds, after `S09`'s eight events, `evt_d30c01c3f252edc8`, a `site.published` from `sites` at `depth` 8, with `seq` 4183.
- `EVENTS_DEPTH_MAX` is unset, so the deepest a chain may go is 8.

Postconditions:

- Nothing was stored: the log still ends at `seq` 4183, and a `search` (`S09`) with `{"cause":"evt_d30c01c3f252edc8"}` answers `{"records":[]}`. events recorded no `event.accepted`.
- events wrote nothing to stderr.

## A caller sends the emit path a method it does not take

`/emit` takes only `POST`. Any other method is answered 405 naming the one method the path takes, and nothing is read.

Request:

```
GET /emit HTTP/1.1
```

```
PUT /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: POST
```

Status 405. Exactly one `Allow` header, whose value is `POST`. The body is empty.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No event was stored, whatever the body held.
- events wrote nothing to stderr.

## A service emits an event that is not declared as JSON

The body must be declared `application/json`. A post with no `Content-Type`, or one naming another media type, is refused unread, however good its body. A parameter on the type, `application/json; charset=utf-8`, is still JSON and is taken.

Request:

```
POST /emit HTTP/1.1
Content-Type: text/plain

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

```
POST /emit HTTP/1.1

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 415 Unsupported Media Type
```

Status 415. The body is empty.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No event was stored.
- events wrote nothing to stderr.

## A service emits a body that is too large

An event is metadata, never data, so one is small. A body longer than 65536 bytes is refused without being parsed, whatever it holds.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

<a body of 65537 bytes or more>
```

Response:

```
HTTP/1.1 413 Request Entity Too Large
```

Status 413. The body is empty.

Preconditions:

- The preamble's.
- The request body is longer than 65536 bytes. A body of exactly 65536 bytes is not too large as it arrived, and is judged as the rest of this group tells, its size as events writes it out included.

Postconditions:

- Nothing has changed. No event was stored.
- events wrote nothing to stderr.

## A service emits an event that is too large once written out

events writes out every event it keeps and delivers in one form, JSON in which each `<`, `>`, and `&` is written as a six-character escape, `\u003c` say. A body that spells those characters raw can arrive under the limit and grow past it in that form. events measures both, the body as it arrived and the event as it writes it out, and refuses an event whose written-out form is longer than 65536 bytes, so that everything it accepts it can also deliver.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

<a body of 65536 bytes or fewer, otherwise a well-formed event, one of whose attribute values holds 11000 raw `<` characters>
```

Response:

```
HTTP/1.1 413 Request Entity Too Large
```

Status 413. The body is empty.

Preconditions:

- The preamble's.
- The body is no longer than 65536 bytes, but the event it holds, written out with each `<` as `\u003c`, is longer than 65536 bytes.

Postconditions:

- Nothing has changed. No event was stored, and events asked no service for its declaration.
- events wrote nothing to stderr.

## A service emits a body that is not JSON

A body that is not one well-formed JSON text is refused. That covers a truncated object, text that is not UTF-8, two JSON texts in one body, and an empty body.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos"
```

Response:

```
HTTP/1.1 400 Bad Request
```

Status 400. The body is empty.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No event was stored.
- events wrote nothing to stderr.

## A service emits JSON that is not an event

A well-formed JSON text that is not exactly an event, as the preamble shapes one, is refused the same way, before events looks at any declaration or depth. The body below gives `depth` 1 with an empty `cause`; each of these is refused identically:

- a value that is not an object, `[]` or `"repo.pushed"` say;
- an object missing one of the nine members, or holding one twice, or holding any other member: `"level":"info"` say, or `seq` or `received`, which events gives an event and a producer never does;
- an `id` that is not a string, or not `evt_` and sixteen lowercase hexadecimal digits: `evt_578B5C72DC4EE60C`, `evt_578b5c72`, `578b5c72dc4ee60c`, or `""`;
- a `time` that is not a string, or not in the shape the preamble gives: `2026-10-05T09:40:12Z` (no fraction), `2026-10-05T09:40:12.318Z` (three digits), `2026-10-05T09:40:12.318442+00:00` (an offset), `2026-10-05 09:40:12.318442Z` (a space);
- a `service` that is not a string, or is `""`;
- an `event` that is not a string, or not a name: `pushed`, `Repo.Pushed`, `repo..pushed`, `repo.pushed.`, `repo__x.pushed`, `2fa.enabled`, `*`, `cron.*.fired`, and `cron.h*.fired` are refused;
- a `request_id` or `user` that is not a string;
- an `attrs` that is not an object, or holds a nested value, `"ref":{"name":"main"}` or `"refs":["main"]`, a `null`, a key that is not lowercase-with-underscores, `Repo`, `_id`, or `old-sha` say, or the same key twice;
- a `cause` that is not a string, or is neither empty nor an event id in the shape of `id`, `push-1` say;
- a `depth` that is not a whole number, `1.5` or `"0"` say, or that disagrees with `cause`: anything but 0 with an empty `cause`, or less than 1 with a `cause`.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":1}
```

Response:

```
HTTP/1.1 400 Bad Request
```

Status 400. The body is empty.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No event was stored, and events asked no service for its declaration.
- events wrote nothing to stderr.

## A service emits an event events cannot store

The post is good and the event declared, but the store cannot take it: the database can no longer be written to. events answers 500 so the producer sends it again, and keeps serving; it writes nothing to stderr, since the event is the producer's, and the producer reports an event it could not deliver.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_578b5c72dc4ee60c","time":"2026-10-05T09:40:12.318442Z","service":"repos","event":"repo.pushed","request_id":"b4ea410fb9102f29a422eb14ab2f2d0f","user":"u_7f3a9c21","attrs":{"new":"eee65f53e9421ce50211670eae679f02e8d28a79","old":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 500 Internal Server Error
```

Status 500. The body is empty.

Preconditions:

- The preamble's.
- events' database cannot be written to: its storage has begun refusing writes since events opened it, the filesystem holding `state/events.db` full, say.

Postconditions:

- No event was stored, and no `seq` was used: the log still ends at `seq` 4182. events recorded no `event.accepted`.
- events is still serving; the same event posted again once the database can be written is stored, with `seq` 4183, as in `A service emits an event`.
- events wrote nothing to stderr.
