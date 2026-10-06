# Stories — delivery

How an accepted event reaches the services that accept it. events pushes: for every subscriber (`S10`) whose declaration accepts the event's name, it POSTs the event to `/events` on that service's socket, as the services file names it, carrying the event's members, `seq` and `received` included, and `attempt`, which is 1 for the first try of an event and one more for each try after it. Each subscriber gets its events in `seq` order and one at a time: events sends it no event until it has finished the one before, so a later event never overtakes an earlier one, whatever happens to either. Subscribers do not wait for one another, except that events has at most `EVENTS_INFLIGHT_MAX` deliveries in flight at once across all its subscribers, and a delivery beyond that waits for one to be answered. A subscriber answers ok, answers skip, or answers an error, which carries an error text. ok finishes the event: the subscriber's `cursor` moves to its `seq` and the trail records `event.delivered`. skip finishes it too, and the trail records `event.skipped` instead. Both carry the attributes `event`, the event's id, and `service`, the subscriber's. An error leaves the event unfinished, and so does a delivery that is not answered within `EVENTS_DELIVERY_TIMEOUT_SECONDS` seconds or that cannot reach the subscriber at all: each is a failed attempt. After a failed attempt events tries the same event again, waiting one second before the second attempt and twice as long before each attempt after that; when the answer has status 429 or 503 and a `Retry-After` header, the next attempt comes no sooner than `Retry-After` says. After `EVENTS_DELIVERY_ATTEMPTS` failed attempts, the subscriber is `paused` on that event, with that event and the last error text as its `reason` (`S10`), and the trail records `subscriber.paused` with the attributes `service`, `event`, the event's id, and `error`, that text; events tries it no more, and every later event for that subscriber waits behind it until an agent skips or resumes it (`S12`). A subscriber's `cursor` also moves past every event it does not accept (`S10`). Delivery is at least once: a subscriber may be handed the same event again, and tells copies apart by the event's `id`. There is no dead-letter queue: an event is finished, waiting, or the one a subscriber is paused on, and nothing else. An agent follows delivery through `subscribers` (`S10`) and the trail; a service follows it by what reaches its `/events`.

The actor is events delivering an event, and an agent that reads `subscribers` through the MCP gateway's `call` as `S10` does, with the request shape and result envelope `S10` fixes; a `since` is shown as a placeholder naming the moment it is. events is serving on `sbx.ikigenba.dev` (`S02`) with every setting at its default unless a story says otherwise; each call is made when its preconditions say. This group's log is its own, distinct from those of `S07`, `S09`, `S10`, `S12` and `S13`. The services file enables `repos`, `scripts`, and `sites`, a service the stories suppose: `repos` declares that it emits `repo.pushed`; `scripts` declares `"accepts":["*"]`; `sites` declares that it emits `site.published` and `"accepts":["repo.pushed"]`. Both subscribers are `ok` at `cursor` 6000, the head of the log before the push event, `scripts` since `<scripts-since>` and `sites` since `<sites-since>`, unless a story says otherwise. repos has just emitted the push event, `evt_73645825df95e7de`, which events accepted at `2026-10-05T09:31:58Z` as `seq` 6001:

```
{"id":"evt_73645825df95e7de","time":"2026-10-05T09:31:58.104050Z","service":"repos","event":"repo.pushed","request_id":"60559f24b61de19a9c1ca6cded29f255","user":"u_7f3a9c21","attrs":{"new":"b4a1e6f57427210cfb507607825bbd40739d8660","old":"27b51d6cb3caf4232f8d7b9b9ba03f4e3b4f47f4","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":6001,"received":"2026-10-05T09:31:58.108261Z"}
```

events writes nothing to stdout or stderr for any delivery in this group, whatever its answer.

## events delivers an event to every service that accepts it

The ordinary case: the push event is accepted, both subscribers accept `repo.pushed`, and each is handed it once and answers ok. Their cursors move to the event, and the agent sees both current.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e1a8b7c9bd0ec8afdf3576fea8cff6c6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6001,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6001,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's. `scripts` and `sites` each answered the delivery of `evt_73645825df95e7de` ok, and this call comes after both answers.

Postconditions:

- `scripts` and `sites` each received one POST to `/events` on its own socket, carrying the push event's members exactly as the preamble shows them, with `attempt` 1. Neither received it a second time.
- telemetry has received from events `event.delivered` with attributes `{"event":"evt_73645825df95e7de","service":"scripts"}` and `event.delivered` with attributes `{"event":"evt_73645825df95e7de","service":"sites"}`.
- The call changed nothing.

## events delivers a subscriber's events in order, one at a time

Three events are accepted in quick succession. Each subscriber is handed all three, in the order of their `seq`, and is never handed the next before it has answered the one before; a slow answer holds back the events behind it, not the other subscriber's deliveries.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 0d4734fd6dc4ca7da5868f9c382933e8
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6003,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6003,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, and after the push event events accepted two more `repo.pushed` events from `repos`, `evt_90749ac37a3c4556` as `seq` 6002 and `evt_5c422f66fc318294` as `seq` 6003, before `scripts` had answered the delivery of `seq` 6001. `scripts` took two seconds to answer each delivery, ok; `sites` answered each at once, ok. This call comes after every answer.

Postconditions:

- `scripts` received `seq` 6001, then 6002, then 6003, each with `attempt` 1, and received each only after it had answered the one before: at no moment did it hold two of them unanswered.
- `sites` received the same three in the same order, without waiting on `scripts`' answers.
- telemetry has received from events six `event.delivered`, one for each delivery.

## events delivers an event only to the services that accept it

`sites` emits `site.published`, which `scripts` accepts, since it accepts every name; `sites` accepts only `repo.pushed`, so it is not handed its own event or any other it does not accept, and its `cursor` moves past it all the same (`S10`). The event:

```
{"id":"evt_89ddd04f8e2d9068","time":"2026-10-05T09:32:04.650261Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_7b3e9a0c5d1f2846","sha":"b4a1e6f57427210cfb507607825bbd40739d8660"},"cause":"evt_73645825df95e7de","depth":1,"seq":6002,"received":"2026-10-05T09:32:04.654472Z"}
```

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 5374c3263bfe4af8ad49f9d581b3efef
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6002,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6002,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, both subscribers having answered the push event ok. Then events accepted `evt_89ddd04f8e2d9068`, the `site.published` event above, as `seq` 6002, and `scripts` answered its delivery ok. This call comes after that answer.

Postconditions:

- `scripts` received `evt_89ddd04f8e2d9068` once, with `attempt` 1.
- `sites` received no POST to `/events` for `evt_89ddd04f8e2d9068`.
- telemetry has received from events `event.delivered` with attributes `{"event":"evt_89ddd04f8e2d9068","service":"scripts"}`, and no record of a delivery of it to `sites`.

## A subscriber answers skip and events moves on

A subscriber that has nothing to do with an event answers skip. events counts the event as finished for it: its `cursor` moves to the event and its `lag` drops, the trail records `event.skipped` rather than `event.delivered`, and the event is not tried again. Its next event is delivered as usual. Here `sites` answers skip to the push event.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 06adabca27bbdeddc26696f0d05c21fd
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6002,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6002,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's. `sites` answered skip to the delivery of the push event, and `scripts` answered it ok. events then accepted `evt_90749ac37a3c4556`, a `repo.pushed` event from `repos`, as `seq` 6002, and both subscribers answered its delivery ok. This call comes after those answers.

Postconditions:

- `sites` received the push event once, with `attempt` 1, and then `evt_90749ac37a3c4556`, with `attempt` 1.
- telemetry has received from events `event.skipped` with attributes `{"event":"evt_73645825df95e7de","service":"sites"}`, and no `event.delivered` for that delivery; and `event.delivered` with attributes `{"event":"evt_73645825df95e7de","service":"scripts"}`, `event.delivered` with attributes `{"event":"evt_90749ac37a3c4556","service":"scripts"}`, and `event.delivered` with attributes `{"event":"evt_90749ac37a3c4556","service":"sites"}`.
- The push event is still in the log: `search` (`S09`) answers it as before.

## events holds deliveries back when as many as it allows are in flight

events has at most `EVENTS_INFLIGHT_MAX` deliveries unanswered at once, across all its subscribers; a delivery beyond that waits until one is answered, however many subscribers are waiting. Here the host runs events with `EVENTS_INFLIGHT_MAX=1`, so the two subscribers of the push event are handed it one after the other, though neither depends on the other. Which goes first is not fixed.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 6016ea0cd551f8bff99bbfa1f1dad50a
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6001,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6001,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, but events was started with `EVENTS_INFLIGHT_MAX=1` in its environment.
- `scripts` takes two seconds to answer a delivery, ok; `sites` answers at once, ok. This call comes after both have answered the push event.

Postconditions:

- `scripts` and `sites` each received the push event once, with `attempt` 1, and at no moment did both hold it unanswered: whichever received it second received it only after the first had answered.
- telemetry has received from events two `event.delivered`, one for each delivery.

## A subscriber answers an error and events tries again

A delivery answered with an error is not lost: events keeps the event and tries it again, waiting one second, then two, then four, so a subscriber that is briefly unable to take it gets it once it can. Here `scripts` is stopping and answers an error to every delivery until a new scripts is serving. The subscriber stays `ok` throughout; it is behind, not stuck.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: df4911b22684772538d981cbf4bfe706
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6001,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6001,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's. `scripts` was told to stop just before the push event was accepted, and answered an error to its first three deliveries of it. A new scripts was serving before the fourth, and answered it ok. This call comes after that answer.

Postconditions:

- `scripts` received the push event four times, with `attempt` 1, 2, 3, and 4. Attempt 2 came no sooner than one second after attempt 1, attempt 3 no sooner than two seconds after attempt 2, and attempt 4 no sooner than four seconds after attempt 3.
- `scripts` was never `paused`: three failed attempts are fewer than `EVENTS_DELIVERY_ATTEMPTS`, 10. The trail holds no `subscriber.paused`.
- `sites` received the push event once, and was not held back by `scripts`' failures.
- telemetry has received from events one `event.delivered` with attributes `{"event":"evt_73645825df95e7de","service":"scripts"}`, for the fourth attempt, and none for the three that failed.

## A subscriber does not answer in time

A delivery not answered within `EVENTS_DELIVERY_TIMEOUT_SECONDS` seconds is a failed attempt, as an error is: events stops waiting and tries the same event again a second later. The subscriber may have done its work all the same, so it may see the event twice, and tells the copies apart by its `id`. Here `sites` is slow to answer the first delivery and answers the second at once.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 49c1e15b27697c2d569ccaf0fa374136
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6001,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6001,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's. `sites` had not answered the first delivery of the push event 5 seconds after it was sent; it answered the second ok at once. This call comes after that answer.

Postconditions:

- `sites` received the push event twice, with `attempt` 1 and 2, the second no sooner than one second after events stopped waiting for the first, 5 seconds after sending it.
- telemetry has received from events one `event.delivered` with attributes `{"event":"evt_73645825df95e7de","service":"sites"}`, for the second attempt.
- `scripts` received the push event once, without waiting on `sites`.

## A subscriber cannot be reached

A delivery that cannot reach the subscriber, because nothing is listening on its socket, is a failed attempt too, and is tried again on the same schedule. Here `sites` is down for its first two attempts and serving again by its third.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3dd93a3673bcb8e3214b6f73fc9c8ec0
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6001,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6001,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's. Nothing was listening on `sites`' socket when the first and second deliveries of the push event were made, so neither connected; `sites` was serving again before the third, and answered it ok. This call comes after that answer.

Postconditions:

- `sites` received the push event once, with `attempt` 3: the first two attempts never reached it. Attempt 2 was made no sooner than one second after attempt 1, and attempt 3 no sooner than two seconds after attempt 2.
- telemetry has received from events one `event.delivered` with attributes `{"event":"evt_73645825df95e7de","service":"sites"}`, for the third attempt.
- `scripts` received the push event once, without waiting on `sites`.

## A subscriber fails every attempt and is paused

A subscriber that fails the same event `EVENTS_DELIVERY_ATTEMPTS` times is `paused` on it rather than tried for ever. Here the host runs events with `EVENTS_DELIVERY_ATTEMPTS=3`, and `sites` answers every attempt at the push event with the error `publish failed: commit not found`. The agent sees it stuck, on which event, and with what error.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 71a7778fb2ad310ff58699bf5db36e27
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6001,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"paused","cursor":6000,"lag":1,"since":"<paused>","reason":{"event":"evt_73645825df95e7de","name":"repo.pushed","seq":6001,"error":"publish failed: commit not found"}}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<paused>` is the moment `sites` was paused, when its third attempt failed.

Preconditions:

- The preamble's, but events was started with `EVENTS_DELIVERY_ATTEMPTS=3` in its environment.
- `sites` answered each delivery of the push event with the error `publish failed: commit not found`. This call comes at least ten seconds after the third attempt was answered.

Postconditions:

- `sites` received the push event three times, with `attempt` 1, 2, and 3, and no fourth time: events tries a paused subscriber no more.
- telemetry has received from events `subscriber.paused` with attributes `{"error":"publish failed: commit not found","event":"evt_73645825df95e7de","service":"sites"}`, and no `event.delivered` for `sites`.
- The call changed nothing.

## Later events wait behind a paused subscriber

While `sites` is `paused` on one event, no later event is delivered to it: delivering past the stuck event would break the order it relies on. They wait, and its `lag` grows with each; `scripts` is not held back.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: bc63e9e7d65445def536b0843ec54550
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6003,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"paused","cursor":6000,"lag":3,"since":"<paused>","reason":{"event":"evt_73645825df95e7de","name":"repo.pushed","seq":6001,"error":"publish failed: commit not found"}}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<paused>` is the moment `sites` was paused.

Preconditions:

- The preamble's, but events was started with `EVENTS_DELIVERY_ATTEMPTS=3` in its environment, and `sites` answered all three attempts at the push event with the error `publish failed: commit not found` and is `paused` on it.
- events then accepted `evt_90749ac37a3c4556` and `evt_5c422f66fc318294`, two more `repo.pushed` events from `repos`, as `seq` 6002 and 6003, and `scripts` answered each delivery ok. This call comes after those answers.

Postconditions:

- `sites` received neither `evt_90749ac37a3c4556` nor `evt_5c422f66fc318294`; both wait for it, and stay in the log.
- `scripts` received both, in order, after the push event.
- The call changed nothing.

## A subscriber asks events to wait before trying again

A subscriber that is busy can say how long to wait: to an answer of status 429 or 503 with a `Retry-After` header, events waits at least that long before the next attempt, not the one second it would otherwise wait. Here `sites` is busy and answers the push event's first delivery with status 503 and `Retry-After: 30`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 25da8397a6a9b4b9bf44ad261f6f0215
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"subscribers":[{"service":"scripts","status":"ok","cursor":6001,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"ok","cursor":6001,"lag":0,"since":"<sites-since>"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's. `sites` answered the first delivery of the push event with status 503 and the header `Retry-After: 30`, and answered the second ok. This call comes after the second answer.

Postconditions:

- `sites` received the push event twice, with `attempt` 1 and 2, the second no sooner than 30 seconds after it answered the first.
- `scripts` received the push event once, without waiting on `sites`.
- telemetry has received from events one `event.delivered` with attributes `{"event":"evt_73645825df95e7de","service":"sites"}`, for the second attempt.
