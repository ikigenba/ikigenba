# Stories — trail

What events' own trail holds: the record of what the broker did, which it sends to the platform's telemetry service as every app does, apart from the log it keeps of the events services emit to it. The two are different things: an event on the bus is a fact other services act on, and lives in events' log, where `search` (`S09`) finds it; a trail record says what events did with it, and lives in telemetry, where telemetry's own tools find it. events finds telemetry in the services file `IKIGENBA_SERVICES` names, as the entry named `telemetry`, and sends each record to that entry's socket; one telemetry cannot take is written to stderr as one line, `events: undelivered event: <event>`, where `<event>` is the JSON telemetry would have received, and events carries on (`S02`). The stories show each record as the JSON object telemetry receives, `{"time":"<time>","service":"events","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}`, or, where a story fixes only a record's name and attributes, as the name followed by each attribute as `<key>=<value>`. `<time>` is when events recorded it, in UTC to the microsecond; `service` is always `events`; `attrs` is flat. Attributes name what happened and the ids of what it touched, never data: no record of events' carries a bus event's attributes, a tool's arguments, a caller's email, or a request's query. A bus event is named in a record by its id, under the key `event`, and a subscriber by its service's name, under the key `service`; the one piece of text a record carries is the error a subscriber gave when it was paused. events records:

- `service.started`, once events is serving, with `version`; and `service.stopping`, its last record, with `reason`, `SIGTERM` or `SIGINT` (`S02`, `S15`);
- `request.started`, as each request arrives, but for a post to `/emit`, with `method` and `path`; and `request.finished`, once its answer is complete, again but for a post to `/emit`, with `status`, `duration_us`, `request_bytes`, and `response_bytes`, shown as `<n>` and `<bytes>` (`S03`);
- `tool.called`, for each call of one of its five tools answered with a result, with `tool`, `kind`, `outcome`, and `duration_us` (`S05`);
- `sibling.called`, for each call events makes to a service's socket, asking for its declarations (`S06`) or delivering to it (`S11`), with `target`, the service's name; `method` and `path`, `GET` and `/declarations` or `POST` and `/events`; `status`, the status of the service's answer, 0 when none came; and `duration_us`;
- `event.accepted`, once for each event events stores in its log (`S07`), with `event`, the stored event's id, and `cause`, the stored event's cause, empty when it has none; a second copy of an event it already holds stores nothing and records no second `event.accepted`;
- `event.delivered`, once for each delivery a subscriber answers ok (`S11`), with `event`, the delivered event's id, and `service`, the subscriber's;
- `event.skipped`, once for each event a subscriber passes over, whether the subscriber answered the delivery skip (`S11`) or an agent skipped the event with `skip` (`S12`); with `event`, the skipped event's id, and `service`, the subscriber's;
- `subscriber.paused`, once each time a subscriber becomes `paused` (`S11`), with `service`, the subscriber's; `event`, the id of the event it is stuck on; and `error`, the error text its last attempt answered.

A post to `/emit` adds to the trail at most the `event.accepted` of the event it stores and a `sibling.called` for any declarations ask it causes (`S06`); a refused or duplicate emit adds no `event.accepted`, and no post adds request records. A `resume` (`S12`) records only its request and its `tool.called`. This group does not fix the request id and user that `sibling.called`, `event.accepted`, `event.delivered`, `subscriber.paused`, and an `event.skipped` a subscriber's answer caused carry. A request's records carry its request id and its caller and come in this order: `request.started`, then any `event.*` record the request caused, then its `tool.called` if it called a tool, then `request.finished`. The broker never records `event.lost`: an event that never reached events was never in its log, and the producer that gave up on it records the loss in its own trail. Unless a story says otherwise, events serves on the host with the suite's services file, whose enabled services are `repos`, `scripts`, and `telemetry`, and telemetry takes every record; repos declares that it emits `repo.pushed` with the attributes `repo`, `ref`, `old`, and `new`, and scripts that it accepts every event (`S06`); and scripts is a subscriber with status `ok` and lag 0 (`S10`). The emits below are made on events' socket by a developer, as the `ikigenba` user, standing in for repos, or for sites where a story says so (`S07`).

## A developer standing in for repos emits an event and finds the broker's records of it

A developer, standing in for repos, emits a push. events stores it, records `event.accepted`, delivers it to scripts, and, once scripts answers ok, records `event.delivered`. The broker's part is found in the trail by the event's id.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_3c8e1a7f5b2d9604","time":"2026-10-06T09:14:02.123456Z","service":"repos","event":"repo.pushed","request_id":"8e2a5c7f0d3b6194e8a2c5f7d0b3e6a1","user":"u_7f3a9c21","attrs":{"new":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37","old":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: events holds no event `evt_3c8e1a7f5b2d9604`, and scripts is `ok` with lag 0.
- scripts answers ok to every delivery it is sent.

Postconditions:

- events' log holds the event with the next `seq` (`S07`), and scripts' cursor has moved to that `seq` (`S10`).
- telemetry has received, from events, one `event.accepted`:

  ```
  event.accepted cause= event=evt_3c8e1a7f5b2d9604
  ```

  and, after it, events' `sibling.called` with `target=scripts`, `method=POST`, and `path=/events` for the delivery, and, once scripts has answered ok:

  ```
  event.delivered event=evt_3c8e1a7f5b2d9604 service=scripts
  ```

- No record of events' carries `rep_9c2e4b7a1d3f8e05`, the two commits, or the ref.
- events wrote nothing to stderr.

## A developer standing in for sites emits a caused event and finds its cause in the trail

sites, handling the delivery of repos' push, emits `site.published` naming the push as its cause, one deeper than the push. An event a service emits while it handles a delivery names the delivered event as its cause, and `event.accepted` carries that cause, so an operator can later walk a chain through the trail from one id to the next without reading the log.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_9a4d2f6c1e8b7035","time":"2026-10-06T09:14:03.654321Z","service":"sites","event":"site.published","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"repo":"rep_9c2e4b7a1d3f8e05","sha":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37"},"cause":"evt_3c8e1a7f5b2d9604","depth":1}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- events holds `evt_3c8e1a7f5b2d9604` at depth 0, as the story above left it, and no event `evt_9a4d2f6c1e8b7035`.
- sites is enabled in the services file and declares that it emits `site.published` with the attributes `repo` and `sha` and accepts `repo.pushed` (`S06`).
- `EVENTS_DEPTH_MAX` is 8, its default.

Postconditions:

- events' log holds `evt_9a4d2f6c1e8b7035` with the next `seq`, its `cause` `evt_3c8e1a7f5b2d9604` and its `depth` 1, as emitted.
- telemetry has received, from events, one `event.accepted`:

  ```
  event.accepted cause=evt_3c8e1a7f5b2d9604 event=evt_9a4d2f6c1e8b7035
  ```

- events wrote nothing to stderr.

## A developer standing in for repos emits an event a subscriber skips

A subscriber that has nothing to do with an event answers skip. events counts it as done, moves the subscriber's cursor past it, and records `event.skipped` naming the event and the subscriber, so the trail tells an event a subscriber handled from one it passed over.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_5e2b8d1a4c7f9036","time":"2026-10-06T09:20:11.000001Z","service":"repos","event":"repo.pushed","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"new":"d29f5b3a8e0c4176f3b2a5d8e1c4f7b0a3d6e9f2","old":"c18e4a2f7d9b3065e2a1f4c7d0b3e6a9f2c5d8e1","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: events holds no event `evt_5e2b8d1a4c7f9036`, and scripts is `ok` with lag 0.
- scripts answers skip to the delivery of `evt_5e2b8d1a4c7f9036`.

Postconditions:

- events' log holds the event, and scripts' cursor has moved to its `seq`, with lag 0 (`S10`).
- telemetry has received, from events, `event.accepted` with `event=evt_5e2b8d1a4c7f9036` and `cause` empty; `sibling.called` with `target=scripts`, `method=POST`, and `path=/events` for the delivery; and, once scripts has answered, no `event.delivered` but:

  ```
  event.skipped event=evt_5e2b8d1a4c7f9036 service=scripts
  ```

- events wrote nothing to stderr.

## A developer standing in for repos emits an event a subscriber fails on

A subscriber that answers an error is tried again until `EVENTS_DELIVERY_ATTEMPTS` attempts have failed, and is then paused (`S11`). The pause is the one moment the trail carries text: `subscriber.paused` holds the error the last attempt answered, beside the subscriber and the event, so an operator finds why a subscriber stopped without asking events. Here `EVENTS_DELIVERY_ATTEMPTS` is 1, so the first failure pauses.

Request:

```
POST /emit HTTP/1.1
Content-Type: application/json

{"id":"evt_6f3c9a2e5b8d1047","time":"2026-10-06T09:25:40.000002Z","service":"repos","event":"repo.pushed","request_id":"5c1e8a3f6b9d2047c5e8a1f3b6d9c2e4","user":"u_7f3a9c21","attrs":{"new":"e3a0c6b4f9d1528704c3b6e9f2a5d8c1b4e7f0a3","old":"d29f5b3a8e0c4176f3b2a5d8e1c4f7b0a3d6e9f2","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0}
```

Response:

```
HTTP/1.1 204 No Content
```

Status 204. The body is empty.

Preconditions:

- The preamble's: events holds no event `evt_6f3c9a2e5b8d1047`, and scripts is `ok` with lag 0.
- `EVENTS_DELIVERY_ATTEMPTS` is 1.
- scripts answers the delivery of `evt_6f3c9a2e5b8d1047` with an error whose text is `commit not found`.

Postconditions:

- scripts is `paused` at `evt_6f3c9a2e5b8d1047` (`S10`), and its cursor has not moved.
- telemetry has received, from events, `event.accepted` with `event=evt_6f3c9a2e5b8d1047` and `cause` empty; `sibling.called` with `target=scripts`, `method=POST`, and `path=/events` for the attempt; and then:

  ```
  subscriber.paused error=commit not found event=evt_6f3c9a2e5b8d1047 service=scripts
  ```

  and no `event.delivered` or `event.skipped` for the event.
- events wrote nothing to stderr.

## An operator follows an agent's skip of a stuck event

An agent that unsticks a paused subscriber with `skip` (`S12`) leaves the same `event.skipped` a subscriber's own skip does, inside the agent's request, so the trail says who passed the event over and when.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 0a9d6c3f8e1b4725a0d3c6f9e2b5a8d1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: skip

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"skip","arguments":{"service":"scripts"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` is events' answer to `skip` (`S12`), with no `isError` member.

Preconditions:

- scripts is `paused` at `evt_6f3c9a2e5b8d1047`, as the story above left it.

Postconditions:

- scripts is no longer `paused`, and its cursor has moved to the `seq` of `evt_6f3c9a2e5b8d1047` (`S12`).
- telemetry has received, from events, under request id `0a9d6c3f8e1b4725a0d3c6f9e2b5a8d1` and user `u_7f3a9c21`, in this order:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"0a9d6c3f8e1b4725a0d3c6f9e2b5a8d1","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"event.skipped","request_id":"0a9d6c3f8e1b4725a0d3c6f9e2b5a8d1","user":"u_7f3a9c21","attrs":{"event":"evt_6f3c9a2e5b8d1047","service":"scripts"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"0a9d6c3f8e1b4725a0d3c6f9e2b5a8d1","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"skip"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"0a9d6c3f8e1b4725a0d3c6f9e2b5a8d1","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `commit not found` is in none of them.

## An operator finds events asking for declarations in the trail

events asks every enabled service for its declarations before it is ready, and again every `EVENTS_DECLARATIONS_SECONDS` (`S06`), and each ask is a call to a sibling, recorded like any other. A service that answers nothing, or has no such path, is recorded with the status it answered, or 0.

Command:

```
$ sudo systemctl start ikigenba-events.service
```

Output:

```
```

Exits 0, once events has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- The preamble's services file, whose enabled services are `repos`, `scripts`, and `telemetry`.
- `ikigenba-events.socket` is active, and `ikigenba-events.service` is not running.

Postconditions:

- telemetry has received, from events, before its `service.started`, one `sibling.called` with `method=GET` and `path=/declarations` for each enabled service, with `target=repos`, `target=scripts`, and `target=telemetry`, each with the `status` of that service's answer; the order of the three is not fixed. A minute later, `EVENTS_DECLARATIONS_SECONDS` at its default, it has received the three again.
- events wrote nothing to stderr.

## An operator follows an agent's call to events' tools

A read tool changes nothing, so its call records only the request and the call.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 2e7c4a9f1b3d6058c2e5a8f1b4d7c0e3
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: search

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"services":["repos"],"events":["repo.pushed"]},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `search` (`S09`), with no `isError` member.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail.
- telemetry has received, from events, the request's three records, in this order:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"2e7c4a9f1b3d6058c2e5a8f1b4d7c0e3","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"2e7c4a9f1b3d6058c2e5a8f1b4d7c0e3","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"search"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"2e7c4a9f1b3d6058c2e5a8f1b4d7c0e3","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `repos`, `repo.pushed`, and `mg@example.com` are in none of them.

## An operator follows a user's visit to events' landing page

Request:

```
GET / HTTP/1.1
Host: events.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8f2c6e0b3d7159a2c4e6f8b0d1e3a5
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`).

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail.
- telemetry has received, from events, under request id `4a8f2c6e0b3d7159a2c4e6f8b0d1e3a5` and user `u_7f3a9c21`, in this order:

  ```
  request.started method=GET path=/
  request.finished status=200
  ```

## An operator finds no record of the broker's for an event it never took

An event the producer could not get to events is not in events' log, so events has nothing to record for it: no `event.accepted`, no delivery, and no `event.lost`, which is the producer's record of its own loss. The operator who finds repos' `event.lost` in the trail, naming `evt_7b1e4c9a2d5f8063`, looks for that push among events' log and does not find it.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 6d0b3f8a1c5e9274d6a0c3f5b8e1d4a7
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: search

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{"services":["repos"],"events":["repo.pushed"],"user":"u_7f3a9c21"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` is events' answer to `search` (`S09`), with no `isError` member, listing every retained `repo.pushed` from repos under `u_7f3a9c21`; no event it lists has the id `evt_7b1e4c9a2d5f8063`.

Preconditions:

- events was stopped, and nothing answered on its socket, from before repos emitted `evt_7b1e4c9a2d5f8063`, a `repo.pushed` under `u_7f3a9c21`, until after repos gave up on it.
- repos recorded `event.lost` in the trail, carrying `evt_7b1e4c9a2d5f8063` among its attributes.
- events is serving again, and repos has not emitted `evt_7b1e4c9a2d5f8063` since.

Postconditions:

- Nothing has changed but the trail, which gains the call's `request.started`, `tool.called` with `tool=search` and `outcome=ok`, and `request.finished`, as in the story above.
- No subscriber was delivered `evt_7b1e4c9a2d5f8063`.
- The trail holds no record from events that names `evt_7b1e4c9a2d5f8063`: no `event.accepted`, no `event.delivered`, no `event.skipped`, and no `event.lost`. events records `event.lost` for no event, ever.
