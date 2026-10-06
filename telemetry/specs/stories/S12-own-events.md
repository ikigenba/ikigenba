# Stories — own events

telemetry keeps a trail of its own, on the same terms as every app of the platform (dummy's `S2-serve.md`): `service.started` with its version when it reports ready, `service.stopping` with the reason when it is signalled, `request.started` and `request.finished` for every request it serves, and `tool.called` for every tool call it runs at `/mcp`, each with `service` `telemetry` and in the shape every sibling's events have (`S06-ingest.md`). It differs from a sibling in one way only: it records its own events straight into its store, never through its socket. It needs no services file and no `telemetry` entry to do so, a missing or empty `IKIGENBA_SERVICES` changes nothing about its trail, and `/ingest` never sees an event of telemetry's own. The one path it does not record is `/ingest` itself (`S06-ingest.md`). Its own records are kept and swept like any other (`S07-retention.md`) and are answered by every tool like any other, so an agent sees telemetry in the trail beside the services it serves: a trace of a gateway call to telemetry shows the gateway's forward and telemetry's execution together, and a search for `telemetry` shows when it started. When telemetry cannot store one of its own events, it writes the event to stderr as one line, `telemetry: undelivered event: <event>`, where `<event>` is the JSON it would have stored, and carries on serving; that is the only line its own trail ever puts on stderr. The requests below are made to a running telemetry on its socket with the identity headers a gate or the gateway sets, as dummy's `S9-mcp.md` makes them, and carry the headers and `_meta` every request on the `2026-07-28` revision carries (`S05-mcp.md`); the members every result carries on that revision are not repeated.

## An agent finds telemetry's own start in the trail

The first record telemetry makes at each start is its `service.started`, with no request id and no user and the version `telemetry --version` prints (`S01-bootstrap.md`). A new version in a start record is how a deploy of telemetry shows in the trail, as it does for every other service.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: search

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"services":["telemetry"],"events":["service.started"],"limit":1},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[{"time":"<time>","service":"telemetry","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}]}
```

and a `content` array of one text block whose text is exactly that line. `<time>` is when this telemetry reported ready, and `v<semver>` is the version its `telemetry --version` prints.

Preconditions:

- telemetry is serving on the socket it was passed, started once since its database was created.

Postconditions:

- Nothing has changed in the trail but telemetry's own records of this request, below.

## A request to telemetry is in its own trail

Every request telemetry serves, a page or a tool call, leaves `request.started` and `request.finished` under the request's id and the caller's `X-User-Id`, with a `tool.called` between them for a tool call. They are stored off the request's path, in order, shortly after they happen, without holding up the answer, so a later `trace` answers them; whether a tool call finds the records of the request that runs it is not fixed (`S05-mcp.md`). Here a developer stands in for the gateway and sends the id by hand.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: count

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"count","arguments":{"services":["dummy"]},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` is `count`'s answer (`S10-count.md`): no `isError` member and a `structuredContent` of `{"total":<n>}`.

Preconditions:

- telemetry is serving on the socket it was passed, with a database it can write.
- The trail holds no record whose request id is `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`.

Postconditions:

- The trail holds three records of telemetry's own for the request, in this order, and a `trace` of `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` (`S11-trace.md`) answers exactly them, oldest first:

  ```
  {"time":"<time>","service":"telemetry","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"telemetry","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"count"}}
  {"time":"<time>","service":"telemetry","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The tool's arguments and its answer are in none of them, and neither is the caller's email.
- telemetry wrote nothing to stderr.

## An agent traces a gateway call through to telemetry

A developer on the host calls a telemetry tool through the MCP gateway, at the gateway's socket, standing in for an agent on a space (`S14-on-a-space.md`). The gateway records its own events and posts them to `/ingest`; telemetry records its own straight into its store; both use the id and the user the developer sent and the gateway forwarded. A later trace of that id shows the whole exchange: the gateway's request, its two hops to telemetry (a `tools/list` to learn the tool's kind, then the `tools/call`), telemetry's execution of the tool, and the gateway's answer. What the trace fixes is each service's own order: mcp's `request.started` first and its `request.finished` last, with its two `sibling.called` and then its `tool.called` between them in that order; telemetry's five records in the order `request.started`, `request.finished`, `request.started`, `tool.called`, `request.finished`, all of them after mcp's `request.started` and before its `tool.called`, the first hop's before the second hop's. The relative order of a hop's telemetry `request.finished` and mcp's `sibling.called` for that hop is not fixed: each service stamps its own time.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d1e4b9c2f0a4c3e8b6d5a2f1e0c9b48
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: trace

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"trace","arguments":{"request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member and a `structuredContent` of `{"records":[...]}` whose `records` are exactly these ten, oldest first, in an order the prose above fixes; the one shown is one the trail can hold:

```
{"time":"<time>","service":"mcp","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
{"time":"<time>","service":"telemetry","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
{"time":"<time>","service":"telemetry","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
{"time":"<time>","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"method":"POST","path":"/mcp","status":200,"target":"telemetry"}}
{"time":"<time>","service":"telemetry","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
{"time":"<time>","service":"telemetry","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"search"}}
{"time":"<time>","service":"telemetry","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
{"time":"<time>","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"method":"POST","path":"/mcp","status":200,"target":"telemetry"}}
{"time":"<time>","service":"mcp","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"call"}}
{"time":"<time>","service":"mcp","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
```

The `content` is one text block holding that same object encoded compactly. The first and second `telemetry` pairs are the gateway's `tools/list` and `tools/call` hops; only the second ran a tool. The third and fourth records may be the other way round, and so may the seventh and eighth.

Preconditions:

- telemetry is serving on the socket it was passed, with a database it can write.
- Earlier, a developer on the host, as the `ikigenba` user, sent the gateway's socket one `call` of telemetry's `search` tool (mcp's `S08`), with `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, `X-User-Id: u_7f3a9c21`, and `X-User-Email: mg@example.com` by hand: no nginx stood in front of the gateway, so no `/check` was made and auth recorded nothing under that id. Every event the gateway recorded for it reached `/ingest`.
- No other service recorded an event under that id.

Postconditions:

- Nothing has changed in the trail but telemetry's own three records of this request, under `7d1e4b9c2f0a4c3e8b6d5a2f1e0c9b48`, as in `A request to telemetry is in its own trail`, with `tool` `trace`.
- telemetry wrote nothing to stderr.

## The host stops telemetry and its stopping is its last record

When signalled, telemetry finishes the requests it has accepted, records `service.stopping` with the signal's name, `SIGTERM` or `SIGINT`, and exits (`S02-serve.md`). The record goes straight into its store, inside the same drain deadline, and is telemetry's last record until the next `service.started`; a stop whose deadline cuts the storing short writes it to stderr instead (`S02-serve.md`).

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- telemetry is serving as process `<pid>`, on the socket it was passed, with a database it can write.
- Every request telemetry has accepted finishes within the drain deadline, and every event of telemetry's own still queued is stored within it, so no event of its trail goes to stderr (`S02-serve.md`).
- telemetry next starts on the same `state/telemetry.db`, and the `search` below is made, within `RETENTION_DAYS` days of the stop, so no sweep has removed the `service.stopping`.

Postconditions:

- telemetry's last record before its next `service.started` is its `service.stopping`, after the `request.finished` of every request accepted before the signal. Once telemetry serves again, a `search` with `services` `["telemetry"]`, `until` the `time` of that next `service.started`, and `limit` `1` answers

  ```
  {"records":[{"time":"<time>","service":"telemetry","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}]}
  ```

  The `until` keeps out the new start and everything telemetry records after it, the search's own records included. With `SIGINT` the `reason` is `SIGINT`.

## telemetry cannot store one of its own events

The store cannot take a record, so telemetry's own events for a request have nowhere to go. The request itself is still answered: a page does not need the store. Each event telemetry could not store goes to stderr as an `undelivered event` line, in the order the events were recorded, so nothing in the trail is lost without trace. telemetry keeps serving, and once the store takes records again its events go there without a restart; a line already written to stderr is not stored later.

Request:

```
GET / HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03-landing.md`).

Preconditions:

- telemetry is serving on the socket it was passed.
- telemetry's database cannot be written to: its storage has begun refusing writes since telemetry opened it, the filesystem holding `state/telemetry.db` full, say.

Postconditions:

- The trail holds no record of the request.
- telemetry wrote two lines to stderr, in this order:

  ```
  telemetry: undelivered event: {"time":"<time>","service":"telemetry","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  telemetry: undelivered event: {"time":"<time>","service":"telemetry","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  Nothing is on stdout.
- telemetry is still serving.
