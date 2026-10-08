# Stories — catalog

`catalog`, the read tool that tells an agent what the bus carries, by event name. It is how an agent learns what it can `search` for (`S09`) and who would react to it. Its result is `{"events":[...]}`, one entry per string, each `{"event":"<string>","emits":[...],"accepts":[...],"count":<n>,"last_seen":"<time>"}`, sorted by the string's bytes, an order that puts `*` before every letter and digit, so `cron.*.fired` comes before `cron.hourly.fired`. The strings listed are every event name or pattern (`S06`) a service declares that it emits, and every name the retained log holds an event of; a string both declared and logged is one entry. A name no event has yet carried is listed with a count of 0, so the catalog is whole before anything has been emitted; and a name the log still holds but no service declares any more, exactly or by a pattern, is listed with an empty `emits`. An entry for a name: `emits` holds one `{"service":"<service>","attrs":["<attr>",...]}` for each service whose declaration has an `emits` element whose `event` is that name or a pattern matching it, with the attribute names that element declares, in the order it declares them; an element naming it exactly wins over a pattern, and otherwise the first matching element in the declaration's order gives them. `accepts` holds the name of each service whose declaration accepts that name exactly, a service that accepts `*` listed under every name; a pattern a service accepts lists it under no name. `count` is how many events of that name the retained log holds, and `last_seen` is the `received` of the newest of them (`S07`), RFC 3339 UTC to the microsecond; with none retained, `count` is 0 and `last_seen` is left out. An entry for a pattern: `emits` holds each service whose declaration has an `emits` element whose `event` is exactly that pattern, with that element's attribute names as for a name; `accepts` holds the name of each service whose declaration accepts exactly that pattern, or accepts `*`; and, since no event carries a pattern (`S07`), `count` is 0 and `last_seen` is left out. Only the retained log counts (`S13`): an event the sweep has removed counts nowhere. The order of the services within `emits` and `accepts` is not fixed by this group. Its arguments are `service`, optional, a service's name, and `event`, optional, a string. `service` keeps the entries that list that service under `emits` or `accepts`: the names and patterns it declares that it emits, the names one of the patterns it emits matches, and the names and patterns it accepts, a service that accepts `*` keeping every entry. `event` keeps the entry whose string is exactly it: a pattern given there keeps that pattern's entry, not the names it matches. Both are matched exactly as given, each entry kept is as it would be in the whole catalog, and an empty string is the same as leaving the argument out. A filter that matches nothing answers `{"events":[]}`, not an error. When events cannot reach its log, `catalog` refuses with `cannot reach the log; try again later`. `catalog` changes nothing, and it is of kind `read`.

The actor, the request shape, and the result envelope are those of `S09`: an agent reaches events through the MCP gateway's `call` with `service` `events` and `tool` `catalog`, under request id `a8f3c1e7b2d94605f1e8c3a7b9d2e4f6`, and a successful result carries its answer as `structuredContent` and as one text block holding that object encoded compactly; a response body below is laid out for reading, and its white space is not fixed. events is serving (`S02`) with every setting at its default, it is `2026-10-05T09:32:00Z`, and no event is accepted and no declaration changes while a story's calls are made. In this group the services file enables `repos`, `scripts`, and `sites`, a service the stories suppose, and every other service it lists declares nothing (`S06`); events holds their declarations, `repos`':

```
{"emits":[{"event":"repo.pushed","attrs":["repo","ref","old","new"]}],"accepts":[]}
```

`scripts`':

```
{"emits":[],"accepts":["*"]}
```

and `sites`':

```
{"emits":[{"event":"site.published","attrs":["repo","sha"]}],"accepts":["repo.pushed"]}
```

Unless a story says otherwise, events' log holds exactly `S09`'s eight events, `seq` 4175 to 4182.

## An agent reads the catalog before anything has been emitted

A space where the bus is new: the services have declared what they emit and accept, and no event has reached events yet. The catalog already names every event the services will emit, so an agent can plan a search, or a subscription, before the first one fires. `scripts`, which accepts every event, is listed under both names.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[
  {"event":"repo.pushed","emits":[{"service":"repos","attrs":["repo","ref","old","new"]}],"accepts":["scripts","sites"],"count":0},
  {"event":"site.published","emits":[{"service":"sites","attrs":["repo","sha"]}],"accepts":["scripts"],"count":0}]}
```

and a `content` array of one text block whose text is that object encoded compactly. Neither entry has a `last_seen`.

Preconditions:

- The preamble's, except that events' log holds no event: nothing has been emitted to it since its database was made.

Postconditions:

- Nothing has changed; the log is still empty.

## An agent reads the catalog of the retained log

The log holds `S09`'s eight events: seven `repo.pushed` and one `site.published`. Each `last_seen` is when events received the newest of them: 4182 for `repo.pushed`, 4177 for `site.published`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[
  {"event":"repo.pushed","emits":[{"service":"repos","attrs":["repo","ref","old","new"]}],"accepts":["scripts","sites"],"count":7,"last_seen":"2026-10-05T09:31:58.209553Z"},
  {"event":"site.published","emits":[{"service":"sites","attrs":["repo","sha"]}],"accepts":["scripts"],"count":1,"last_seen":"2026-10-04T16:20:12.035410Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog after the sweep has removed events

Only what is retained counts. A day and a half later the sweep (`S13`) has removed the events received more than two days before, those of 2026-10-04: `repo.pushed` counts the four left, and `site.published`, still declared by `sites`, is listed with a count of 0 and no `last_seen`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[
  {"event":"repo.pushed","emits":[{"service":"repos","attrs":["repo","ref","old","new"]}],"accepts":["scripts","sites"],"count":4,"last_seen":"2026-10-05T09:31:58.209553Z"},
  {"event":"site.published","emits":[{"service":"sites","attrs":["repo","sha"]}],"accepts":["scripts"],"count":0}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, except that it is `2026-10-06T21:00:00Z`, and that events' log, which held `S09`'s eight events, now holds only `seq` 4179 to 4182: `EVENTS_RETENTION_DAYS` is unset, so the window is 2 days, and the sweep (`S13`) has removed `seq` 4175 to 4178, each received on 2026-10-04 before `16:20:13Z`, more than two days before. `scripts` and `sites`, the subscribers, are both `ok` with their cursors at 4182 (`S10`), so their places in the log held none of them. No event has been accepted since 4182.

Postconditions:

- Nothing has changed; the log is as it was. A `search` with no filter (`S09`) answers the four events, 4182 down to 4179, as the catalog's counts say.

## An agent reads the catalog of a name no service declares any more

A new release of `sites` no longer emits `site.published`, but the log still holds the one it emitted. The name stays in the catalog while an event of it is retained, with no service in `emits`, so the agent can still find what was emitted and know that nothing emits it now.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"event":"site.published"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[{"event":"site.published","emits":[],"accepts":["scripts"],"count":1,"last_seen":"2026-10-04T16:20:12.035410Z"}]}
```

and a `content` array of one text block whose text is exactly that line. Once the sweep has removed 4177 (`S13`), `site.published` is not listed at all, and the same call answers `{"events":[]}`.

Preconditions:

- The preamble's, except that `sites` now declares `{"emits":[],"accepts":["repo.pushed"]}`, and events has asked it since (`S06`).

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog of one service

`service` keeps the names that service emits or accepts. `repos` emits `repo.pushed` and accepts nothing, so its catalog is that one entry, as it is in the whole catalog, with who else accepts it. For `scripts`, which accepts every event, the answer is the whole catalog.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"service":"repos"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[{"event":"repo.pushed","emits":[{"service":"repos","attrs":["repo","ref","old","new"]}],"accepts":["scripts","sites"],"count":7,"last_seen":"2026-10-05T09:31:58.209553Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. With `{"service":"sites"}` the answer is both entries, since `sites` accepts one name and emits the other.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog of one event

`event` keeps that name's entry alone.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"event":"repo.pushed"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member and a `structuredContent` equal to the one `An agent reads the catalog of one service` answers for `repos`: the one entry for `repo.pushed`. The `content` array is one text block whose text is that object encoded compactly. With `{"service":"repos","event":"site.published"}` the answer is `{"events":[]}`: both filters apply, and `repos` neither emits nor accepts `site.published`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent gives catalog an empty name

An empty `service` or `event` is the same as leaving it out: the whole catalog.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"service":"","event":""}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member and a `structuredContent` equal to the one `An agent reads the catalog of the retained log` answers, both entries. The `content` array is one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog of a service that declares nothing

A name no declaration and no retained event carries, a service never deployed or one that emits and accepts nothing, is an empty catalog, not an error: the agent learns there is nothing to search for. An `event` nothing declares or carries answers the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"service":"widgets"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's: no service named `widgets` declares anything, and the log holds no event of it.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog of a service that declares a pattern

`cron` emits an event each time one of its triggers is created, paused, resumed, deleted, or fires, the event's name holding the trigger's slug, and declares them all with five patterns, `cron.*.created`, `cron.*.deleted`, `cron.*.fired`, `cron.*.paused`, and `cron.*.resumed`. Each pattern has its own entry, listing who declares and who accepts that pattern; it carries no event, so its count is 0. Each name a pattern matches that the log holds an event of has an entry too, emitted by `cron` with the attributes that pattern declares: here `cron.hourly.fired`, which `cron.*.fired` matches. `scripts`, which accepts every event, is listed under all six. The patterns sort first, `*` before any letter.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"service":"cron"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[
  {"event":"cron.*.created","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.deleted","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.fired","emits":[{"service":"cron","attrs":["trigger","when","scheduled"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.paused","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.resumed","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.hourly.fired","emits":[{"service":"cron","attrs":["trigger","when","scheduled"]}],"accepts":["scripts"],"count":2,"last_seen":"2026-10-05T11:00:00.006341Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. The whole catalog holds these six entries before `repo.pushed` and `site.published`, as `An agent reads the catalog of the retained log` answers them.

Preconditions:

- The preamble's, except that it is `2026-10-05T11:02:00Z`, and that the services file also enables `cron`, a service the stories suppose, whose declaration events holds is:

  ```
  {"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]}],"accepts":[]}
  ```

- events' log also holds, after `S09`'s eight events, two `cron.hourly.fired` events `cron` emitted, `seq` 4183 and 4184:

  ```
  {"id":"evt_9f4b2d7e1c6a3058","time":"2026-10-05T10:00:00.003418Z","service":"cron","event":"cron.hourly.fired","request_id":"1f7b3d9e5a2c8046e0b4d8a2f6c1e735","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T10:00:00Z","trigger":"crn_3a8f2d6c9e1b4705","when":"0 * * * *"},"cause":"","depth":0,"seq":4183,"received":"2026-10-05T10:00:00.007952Z"}
  {"id":"evt_b7e3a1c9d5f20846","time":"2026-10-05T11:00:00.002976Z","service":"cron","event":"cron.hourly.fired","request_id":"7d3a9f1c5e8b2064b9e3f7a1d5c0b842","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T11:00:00Z","trigger":"crn_3a8f2d6c9e1b4705","when":"0 * * * *"},"cause":"","depth":0,"seq":4184,"received":"2026-10-05T11:00:00.006341Z"}
  ```

  No event has been accepted since 4184.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog of a pattern

`event` matches the entry's string exactly, so a pattern given there keeps the pattern's own entry and not the names it matches.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"event":"cron.*.fired"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[{"event":"cron.*.fired","emits":[{"service":"cron","attrs":["trigger","when","scheduled"]}],"accepts":["scripts"],"count":0}]}
```

and a `content` array of one text block whose text is exactly that line. With `{"event":"cron.hourly.fired"}` the answer is that name's entry alone, as `An agent reads the catalog of a service that declares a pattern` answers it.

Preconditions:

- The preamble's, except as in `An agent reads the catalog of a service that declares a pattern`: it is `2026-10-05T11:02:00Z`, the services file also enables `cron`, whose declaration events holds is `{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]}],"accepts":[]}`, and events' log also holds `seq` 4183 and 4184, the two `cron.hourly.fired` events, after `S09`'s eight.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog of a name declared both exactly and by a pattern

`cron` declares `cron.hourly.fired` with an extra attribute, after its five patterns, `cron.*.fired` among them, which also matches it. The element naming it exactly wins, wherever it stands, so the name's entry carries its attributes; each pattern's entry keeps the pattern's own.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"service":"cron"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[
  {"event":"cron.*.created","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.deleted","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.fired","emits":[{"service":"cron","attrs":["trigger","when","scheduled"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.paused","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.*.resumed","emits":[{"service":"cron","attrs":["trigger","when"]}],"accepts":["scripts"],"count":0},
  {"event":"cron.hourly.fired","emits":[{"service":"cron","attrs":["trigger","when","scheduled","late"]}],"accepts":["scripts"],"count":2,"last_seen":"2026-10-05T11:00:00.006341Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `cron` is listed once under `cron.hourly.fired`.

Preconditions:

- The preamble's, except as in `An agent reads the catalog of a service that declares a pattern`: it is `2026-10-05T11:02:00Z`, the services file also enables `cron`, whose declaration events holds is `{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]},{"event":"cron.hourly.fired","attrs":["trigger","when","scheduled","late"]}],"accepts":[]}`, and events' log also holds `seq` 4183 and 4184, the two `cron.hourly.fired` events, after `S09`'s eight.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog of a service that accepts a pattern

`sites` accepts `cron.*.fired` as well as `repo.pushed`. It is listed under the pattern's entry, which it accepts exactly, but not under `cron.hourly.fired`: a pattern a service accepts lists it under no name. So its catalog holds that pattern's entry, and the two it emits or accepts by name, but not `cron.hourly.fired`, nor `cron`'s other four patterns, which it does not accept.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"service":"sites"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has no `isError` member, a `structuredContent` of

```
{"events":[
  {"event":"cron.*.fired","emits":[{"service":"cron","attrs":["trigger","when","scheduled"]}],"accepts":["scripts","sites"],"count":0},
  {"event":"repo.pushed","emits":[{"service":"repos","attrs":["repo","ref","old","new"]}],"accepts":["scripts","sites"],"count":7,"last_seen":"2026-10-05T09:31:58.209553Z"},
  {"event":"site.published","emits":[{"service":"sites","attrs":["repo","sha"]}],"accepts":["scripts"],"count":1,"last_seen":"2026-10-04T16:20:12.035410Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. In the whole catalog `cron.hourly.fired` is as `An agent reads the catalog of a service that declares a pattern` answers it, `scripts` alone under its `accepts`.

Preconditions:

- The preamble's, except as in `An agent reads the catalog of a service that declares a pattern`: it is `2026-10-05T11:02:00Z`, the services file also enables `cron`, whose declaration events holds is `{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]}],"accepts":[]}`, and events' log also holds `seq` 4183 and 4184, the two `cron.hourly.fired` events, after `S09`'s eight.
- `sites` declares `{"emits":[{"event":"site.published","attrs":["repo","sha"]}],"accepts":["repo.pushed","cron.*.fired"]}`, and events has asked it since (`S06`).

Postconditions:

- Nothing has changed; the log is as it was.

## An agent reads the catalog while events cannot reach its log

The counts are the log's, so a catalog drawn without the log would be wrong; events refuses rather than answer a part of it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cannot reach the log; try again later
```

Preconditions:

- The preamble's, but events' database can no longer be read: its storage has begun failing since events opened it.

Postconditions:

- Nothing has changed. events is still serving, and once its database can be read again the same call answers as in `An agent reads the catalog of the retained log`.

## An agent passes catalog an argument it does not have

Arguments that do not fit the tool's input schema are refused as every tool of the suite refuses them (`S05`); events answers nothing about the bus.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{"bogus":"x"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
bogus: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.
