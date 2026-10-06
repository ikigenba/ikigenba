# Stories — skip and resume

`skip` and `resume`, the two tools an agent unsticks a `paused` subscriber (`S10`, `S11`) with. Each takes one argument, `service`, required, a string, the subscriber's service name, matched as given. `skip` gives up the event the subscriber is stuck on: events records it as skipped, the trail gaining `event.skipped` with the attributes `event`, the event's id, and `service`, the subscriber's; moves the subscriber's `cursor` to that event's `seq`; makes it `ok`; and goes on delivering to it from the next event it accepts. The skipped event is never delivered to that subscriber again, and stays in the log for everyone else. `resume` keeps it: events clears the pause, makes the subscriber `ok`, and delivers the same event to it again from `attempt` 1, with all of `EVENTS_DELIVERY_ATTEMPTS` before it can be paused again (`S11`); it records nothing in the trail of its own. Each answers with the subscriber's entry as `subscribers` gives it (`S10`) at the moment of the answer, its `since` that moment. Each refuses, changing nothing, a service that is no subscriber, with `no subscriber '<service>'`, and a subscriber that is not `paused`, with `'<service>' is not paused`, quoting the name as sent; the first check is whether the service is a subscriber. `skip` gives up a delivery no tool can bring back, so it is of kind `destructive`; `resume` only makes delivery try again, so it is of kind `additive`; both run with the gateway's `mutate`, and their `tool.called` carries that kind (`S05`).

The actor is an agent working through an MCP client, which calls `skip` or `resume` through the MCP gateway's `mutate`, with `service` `events`, `tool` the tool's name, and the tool's own arguments as `args`, or reads `subscribers` through the gateway's `call` as `S10` does. Each request is the HTTP request the client sends to a running mcp on `sbx.ikigenba.dev`, on revision `2026-07-28`, with the caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`; the gateway forwards it to events on its socket, with the same request id and user, and relays events' result verbatim (`S05`). A `since` is shown as a placeholder naming the moment it is. events is serving on `sbx.ikigenba.dev` (`S02`) with every setting at its default; each call is made when its preconditions say. This group's log is its own, distinct from those of `S07`, `S09`, `S10`, `S11` and `S13`. The services file enables `repos`, which declares that it emits `repo.pushed`; `scripts`, which declares `"accepts":["*"]`; and `sites`, a service the stories suppose, which declares `"accepts":["repo.pushed"]`. The head of the log is `seq` 7003. `scripts` is `ok` at `cursor` 7003. `sites` is `paused` at `cursor` 7000 on `seq` 7001, the push event `evt_787e603fdb6e3c7c`, a `repo.pushed` event from `repos`, having answered all ten attempts at it with the error `publish failed: commit not found`:

```
{"id":"evt_787e603fdb6e3c7c","time":"2026-10-05T09:20:15.802911Z","service":"repos","event":"repo.pushed","request_id":"a73efda008d0a0e404e39ddcfb5eb38a","user":"u_7f3a9c21","attrs":{"new":"9ff2d619e8604be0c1b7f9702662e0d6cdbba0bd","old":"db9798fd815406f307ad8500bfcfeba54d6bde1f","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":7001,"received":"2026-10-05T09:20:15.807122Z"}
```

`seq` 7002 and 7003, `evt_4578db8fe2141eb4` and `evt_2bb86f0ee5675bd9`, are `repo.pushed` events from `repos` waiting behind it. events writes nothing to stdout or stderr for anything in this group.

## An agent skips the event a paused subscriber is stuck on

The agent decides the push event cannot be delivered to `sites`, its commit gone, say, and that `sites` should not wait on it any longer. `skip` moves `sites` past it and makes it `ok`; the answer shows it one event further on, with the two events behind it still to come, and events goes on to deliver them.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7956063977ff589e28ac8f96604fa00b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"skip","args":{"service":"sites"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"service":"sites","status":"ok","cursor":7001,"lag":2,"since":"<since>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<since>` is the moment of the call.

Preconditions:

- The preamble's. `sites` answers every delivery after the call ok.

Postconditions:

- `sites` is `ok`. events delivered `evt_4578db8fe2141eb4` and then `evt_2bb86f0ee5675bd9` to `sites`, each with `attempt` 1, and once both are answered `subscribers` answers `sites` with `cursor` 7003 and `lag` 0. `sites` was not handed `evt_787e603fdb6e3c7c` again.
- `evt_787e603fdb6e3c7c` is still in the log: `search` (`S09`) answers it as before. `scripts` is untouched.
- telemetry has received from events the request's four records, in this order:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"7956063977ff589e28ac8f96604fa00b","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"event.skipped","request_id":"7956063977ff589e28ac8f96604fa00b","user":"u_7f3a9c21","attrs":{"event":"evt_787e603fdb6e3c7c","service":"sites"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"7956063977ff589e28ac8f96604fa00b","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"skip"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"7956063977ff589e28ac8f96604fa00b","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- The deliveries that follow record their own `event.delivered` (`S11`).

## An agent resumes a paused subscriber

The agent has fixed what made `sites` fail, and wants the push event delivered after all. `resume` makes `sites` `ok` where it stands, and events delivers the push event to it again as a first attempt, then the events waiting behind it, in order.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 73903c0dec73c7aaa1ebdc00c053b32d
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"resume","args":{"service":"sites"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"service":"sites","status":"ok","cursor":7000,"lag":3,"since":"<since>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<since>` is the moment of the call.

Preconditions:

- The preamble's. `sites` answers every delivery after the call ok.

Postconditions:

- `sites` received `evt_787e603fdb6e3c7c` with `attempt` 1, then `evt_4578db8fe2141eb4` and `evt_2bb86f0ee5675bd9`, each with `attempt` 1, in that order, and once all three are answered `subscribers` answers `sites` with `cursor` 7003 and `lag` 0.
- telemetry has received from events the request's three records, in this order:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"73903c0dec73c7aaa1ebdc00c053b32d","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"73903c0dec73c7aaa1ebdc00c053b32d","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"resume"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"73903c0dec73c7aaa1ebdc00c053b32d","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- No `event.skipped` was recorded for `evt_787e603fdb6e3c7c`. The deliveries that follow record their own `event.delivered` (`S11`).

## A resumed subscriber that fails again is paused again

`resume` starts the count of attempts afresh; it does not make the event deliverable. When `sites` still cannot take the push event, it fails `EVENTS_DELIVERY_ATTEMPTS` attempts again and is `paused` on the same event, and the events behind it go on waiting. The agent sees it through `subscribers`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: d958b2ccdd98e1c6dff689e7de6217f6
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
{"subscribers":[{"service":"scripts","status":"ok","cursor":7003,"lag":0,"since":"<scripts-since>"},{"service":"sites","status":"paused","cursor":7000,"lag":3,"since":"<paused>","reason":{"event":"evt_787e603fdb6e3c7c","name":"repo.pushed","seq":7001,"error":"publish failed: commit not found"}}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<scripts-since>` is when `scripts` last became `ok`, and `<paused>` the moment `sites`' tenth attempt since the `resume` failed.

Preconditions:

- The preamble's. The agent then called `resume` for `sites`, as in `An agent resumes a paused subscriber`. `sites` answered every attempt after that with the error `publish failed: commit not found`, and this call comes after its tenth.

Postconditions:

- Since the `resume`, `sites` received `evt_787e603fdb6e3c7c` ten times, with `attempt` 1 to 10, and no later event.
- telemetry has received from events a new `subscriber.paused` with attributes `{"error":"publish failed: commit not found","event":"evt_787e603fdb6e3c7c","service":"sites"}`, and no `event.delivered` and no `event.skipped` for `sites` since the `resume`.

## An agent skips a subscriber that is not paused

`scripts` is `ok`: it is stuck on nothing, so there is nothing to skip. The call is refused, quoting the name as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 6bc9d6712f3c7460558c9146104245fa
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"skip","args":{"service":"scripts"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
'scripts' is not paused
```

Preconditions:

- The preamble's: `scripts` is `ok` at `cursor` 7003.

Postconditions:

- Nothing has changed: `scripts` is `ok` at `cursor` 7003, and no delivery was made or skipped.
- events recorded no `event.skipped`; the request's `tool.called` has `kind` `destructive` and `outcome` `error`.

## An agent resumes a subscriber that is not paused

`scripts` is `ok`: it is stuck on nothing, so there is nothing to retry. The call is refused, quoting the name as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 1610a841af302ee62d0c1b48830f8d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"resume","args":{"service":"scripts"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
'scripts' is not paused
```

Preconditions:

- The preamble's: `scripts` is `ok` at `cursor` 7003.

Postconditions:

- Nothing has changed: `scripts` is `ok` at `cursor` 7003, and no delivery was made or skipped.
- events recorded no `event.skipped`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## An agent skips a service that is no subscriber

`repos` emits events but accepts none, so it is no subscriber and has no place in the log to move. The call is refused, quoting the name as sent; a service events has never heard of is refused the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 9505797008125825180c5c9844a9e58c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"skip","args":{"service":"repos"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no subscriber 'repos'
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: no subscriber's `cursor` or status moved, and no delivery was made or skipped.
- events recorded no `event.skipped`; the request's `tool.called` has `kind` `destructive` and `outcome` `error`.

## An agent resumes a service that is no subscriber

`repos` emits events but accepts none, so it is no subscriber and has no place in the log to move. The call is refused, quoting the name as sent; a service events has never heard of is refused the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 5f9808a9c20cd8bf9e0ed482b910d021
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"resume","args":{"service":"repos"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no subscriber 'repos'
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: no subscriber's `cursor` or status moved, and no delivery was made or skipped.
- events recorded no `event.skipped`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## An agent calls skip without saying which subscriber

`service` is required. A call that leaves it out is refused as its arguments are read, as every tool of the suite refuses it (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: d6ade324a16f3344fa36588330fb4d03
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"skip","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
service: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `sites` is still `paused` on `evt_787e603fdb6e3c7c`.
- telemetry has received from events the request's three records, in this order:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"d6ade324a16f3344fa36588330fb4d03","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"d6ade324a16f3344fa36588330fb4d03","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"destructive","outcome":"invalid_arguments","tool":"skip"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"d6ade324a16f3344fa36588330fb4d03","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## An agent calls resume without saying which subscriber

`service` is required. A call that leaves it out is refused as its arguments are read, as every tool of the suite refuses it (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: f2d2b18b7d51f077e4840f26d767efbc
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"events","tool":"resume","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
service: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `sites` is still `paused` on `evt_787e603fdb6e3c7c`.
- telemetry has received from events the request's three records, in this order:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"f2d2b18b7d51f077e4840f26d767efbc","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"f2d2b18b7d51f077e4840f26d767efbc","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"resume"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"f2d2b18b7d51f077e4840f26d767efbc","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```
