# Stories — search

`search`, the read tool that lists the events of the retained log that match a filter, newest first, a page at a time. Newest means the highest `seq`: the log's order is the order events accepted the events, and `search` reads it backwards, whatever the events' own `time`s say, since a producer can emit an event a moment before another service's but have it reach events a moment after. Its arguments are all optional, and their schema is the input schema `S05` gives for `search`. The filters: `services`, an array of service names, keeps the events any of them emitted; `events`, an array of event names, keeps the events carrying any of them; `user`, a string, keeps the events whose `user` is exactly it; `request_id`, a string, keeps the events whose `request_id` is exactly it; `cause`, a string, keeps the events whose `cause` is exactly it; `attrs`, an object whose values are strings, numbers, or booleans, keeps the events whose attributes carry every one of its keys with exactly that value, a number matching a number and a string a string; `since`, an RFC 3339 time, keeps the events whose `time` is at or after it; and `until`, an RFC 3339 time, keeps the events whose `time` is before it. A time is compared as an instant, so that a `since` that is not before `until` matches nothing. An empty string given as `user`, `request_id`, or `cause` is a value like any other: it keeps the events whose member is empty. A filter left out is unbounded; the filters given are ANDed. The page: `limit`, a whole number from 1 to 500, 50 when left out, is the most events one answer holds; `cursor`, the value a previous answer gave, continues from where that answer stopped, with the same filters. Only the retained log is searched: an event the sweep has removed (`S13`) is found by no search.

The result is `{"records":[...]}`, one record per event, and, when more events match than the page holds, a second member, `"cursor":"<cursor>"`, an opaque string; passing it back as `cursor`, with the same filters, answers the next page, and the last page has no `cursor` member. A page carries `limit` records, or fewer only on the last page. A record is the event as events stored it: its eleven members `id`, `time`, `service`, `event`, `request_id`, `user`, `attrs`, `cause`, and `depth`, exactly as the producer emitted them (`S07`), and `seq` and `received`, which events gave it when it accepted it. The order of the members within a record, and of the keys within `attrs`, is not fixed; the records below are laid out in one order for reading. A refusal the tool makes is an `isError` result with one text block: `since is not an RFC 3339 time: '<value>'` and `until is not an RFC 3339 time: '<value>'` for a time the tool cannot read; `limit must be between 1 and 500, got <n>` for a limit outside the range; `cursor is not one search issued` for a cursor that no search answered; `cannot reach the log; try again later` when events cannot reach its log. An empty `cursor` is the same as leaving it out: the first page. A filter that matches nothing is an empty page, `{"records":[]}`, never an error. `search` changes nothing, and it is of kind `read`.

The actor is an agent working through an MCP client, reaching events through the MCP gateway's `call` with `service` `events`, `tool` `search`, and the tool's own arguments as `args`. Each request is the HTTP request the client sends to a running mcp on `sbx.ikigenba.dev`, on revision `2026-07-28`, with `Mcp-Name: call` and the caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`, here `a8f3c1e7b2d94605f1e8c3a7b9d2e4f6`, an id of the call's own; the gateway forwards it to events on its socket and relays events' result verbatim (`S05`). A successful result carries its answer as `structuredContent` and as one text block holding that object encoded compactly; a refusal is status 200 with `isError` `true`, no `structuredContent`, and one text block (`S05`). A response body below is laid out for reading: its white space is not fixed. events is serving (`S02`) with every setting at its default, it is `2026-10-05T09:32:00Z`, and no event is accepted while a story's calls are made unless the story says so.

The log fixture is the same in this group and in `S08`, and a group that names an event of this log by its `seq` means the event shown here: events' retained log holds exactly these eight events, `seq` 4175 to 4182, every earlier one having been swept (`S13`). They are a push by `u_1e9b4d07` to `main` and a new tag of `rep_41d8f0a6b2c97e13`, one request emitting one `repo.pushed` per ref (4175, 4176); a `site.published` that `sites`, a service the stories suppose, emitted while it handled 4175, so caused by it and one deeper (4177); a push by `u_2b8e1d04` to `rep_d41c7a9e05b28f63`, which repos emitted before 4177 was emitted but which reached events after it (4178); pushes to `main` by `u_7f3a9c21` to `rep_9c2e4b7a1d3f8e05` (4179) and `rep_41d8f0a6b2c97e13` (4180); a push by `u_1e9b4d07` to `rep_9c2e4b7a1d3f8e05` (4181); and the head of the log, a push by `u_7f3a9c21` to `rep_7b3e9a0c5d1f2846`, `evt_8c3f1a6e2d9b4075` (4182). Each is shown as `search` answers it, in the order events accepted them:

```
{"id":"evt_3a7c1e9b5d2f4086","time":"2026-10-04T16:20:11.402316Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"a4c123b1612dd272d1371c17149d439536b3216f","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4175,"received":"2026-10-04T16:20:11.407022Z"}
{"id":"evt_2d7a9c4e1f6b3058","time":"2026-10-04T16:20:11.402401Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"0000000000000000000000000000000000000000","ref":"refs/tags/v1.2.0","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4176,"received":"2026-10-04T16:20:11.409876Z"}
{"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"}
{"id":"evt_6e9a2c5f0b3d7148","time":"2026-10-04T16:20:11.951240Z","service":"repos","event":"repo.pushed","request_id":"b3f7d1a9c5e24680f2b6d0a8c4e1f397","user":"u_2b8e1d04","attrs":{"new":"e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92","old":"c0a4e8f2b6d1937e5a9c3f7b1d0e2a4c6f8b9d10","ref":"refs/heads/main","repo":"rep_d41c7a9e05b28f63"},"cause":"","depth":0,"seq":4178,"received":"2026-10-04T16:20:12.040117Z"}
{"id":"evt_c4a9e2f6b1d83075","time":"2026-10-05T08:47:03.118204Z","service":"repos","event":"repo.pushed","request_id":"176813e02ea68ef786e4d3cea27d2693","user":"u_7f3a9c21","attrs":{"new":"dba41ecccc3fc1626e53a13043b026c48bbf33fe","old":"b0eb53f16947ccf25ec84d8dbc74254770f58904","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":4179,"received":"2026-10-05T08:47:03.122871Z"}
{"id":"evt_e1c5a8f2d6b94037","time":"2026-10-05T09:12:40.551930Z","service":"repos","event":"repo.pushed","request_id":"4b484e73cf575dcad6ba2b0aee0ca923","user":"u_7f3a9c21","attrs":{"new":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3","old":"daeeb975729fae923d5a4fd12aabfe228f219e9c","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4180,"received":"2026-10-05T09:12:40.556218Z"}
{"id":"evt_2f6b9d1e4a7c0583","time":"2026-10-05T09:20:15.730468Z","service":"repos","event":"repo.pushed","request_id":"9e5c1a7d3f0b4862c8a2e6f0d4b9c173","user":"u_1e9b4d07","attrs":{"new":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","old":"dba41ecccc3fc1626e53a13043b026c48bbf33fe","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":4181,"received":"2026-10-05T09:20:15.734902Z"}
{"id":"evt_8c3f1a6e2d9b4075","time":"2026-10-05T09:31:58.204117Z","service":"repos","event":"repo.pushed","request_id":"6c2e9a4f1b7d3058e2a6c9f4b1d7e305","user":"u_7f3a9c21","attrs":{"new":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","old":"7d5b3f1e9c0a2846b8d4f6e1a3c5b7d9e0f2a4c6","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":4182,"received":"2026-10-05T09:31:58.209553Z"}
```

## An agent searches the whole log

No filter: the whole retained log, newest first. 4178 comes before 4177 in the answer though its `time` is earlier, because events accepted it later and the newest comes first.

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

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[
  {"id":"evt_8c3f1a6e2d9b4075","time":"2026-10-05T09:31:58.204117Z","service":"repos","event":"repo.pushed","request_id":"6c2e9a4f1b7d3058e2a6c9f4b1d7e305","user":"u_7f3a9c21","attrs":{"new":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","old":"7d5b3f1e9c0a2846b8d4f6e1a3c5b7d9e0f2a4c6","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":4182,"received":"2026-10-05T09:31:58.209553Z"},
  {"id":"evt_2f6b9d1e4a7c0583","time":"2026-10-05T09:20:15.730468Z","service":"repos","event":"repo.pushed","request_id":"9e5c1a7d3f0b4862c8a2e6f0d4b9c173","user":"u_1e9b4d07","attrs":{"new":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","old":"dba41ecccc3fc1626e53a13043b026c48bbf33fe","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":4181,"received":"2026-10-05T09:20:15.734902Z"},
  {"id":"evt_e1c5a8f2d6b94037","time":"2026-10-05T09:12:40.551930Z","service":"repos","event":"repo.pushed","request_id":"4b484e73cf575dcad6ba2b0aee0ca923","user":"u_7f3a9c21","attrs":{"new":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3","old":"daeeb975729fae923d5a4fd12aabfe228f219e9c","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4180,"received":"2026-10-05T09:12:40.556218Z"},
  {"id":"evt_c4a9e2f6b1d83075","time":"2026-10-05T08:47:03.118204Z","service":"repos","event":"repo.pushed","request_id":"176813e02ea68ef786e4d3cea27d2693","user":"u_7f3a9c21","attrs":{"new":"dba41ecccc3fc1626e53a13043b026c48bbf33fe","old":"b0eb53f16947ccf25ec84d8dbc74254770f58904","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":4179,"received":"2026-10-05T08:47:03.122871Z"},
  {"id":"evt_6e9a2c5f0b3d7148","time":"2026-10-04T16:20:11.951240Z","service":"repos","event":"repo.pushed","request_id":"b3f7d1a9c5e24680f2b6d0a8c4e1f397","user":"u_2b8e1d04","attrs":{"new":"e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92","old":"c0a4e8f2b6d1937e5a9c3f7b1d0e2a4c6f8b9d10","ref":"refs/heads/main","repo":"rep_d41c7a9e05b28f63"},"cause":"","depth":0,"seq":4178,"received":"2026-10-04T16:20:12.040117Z"},
  {"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"},
  {"id":"evt_2d7a9c4e1f6b3058","time":"2026-10-04T16:20:11.402401Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"0000000000000000000000000000000000000000","ref":"refs/tags/v1.2.0","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4176,"received":"2026-10-04T16:20:11.409876Z"},
  {"id":"evt_3a7c1e9b5d2f4086","time":"2026-10-04T16:20:11.402316Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"a4c123b1612dd272d1371c17149d439536b3216f","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4175,"received":"2026-10-04T16:20:11.407022Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. There is no `cursor`: eight events match and the page holds fifty.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent searches by services and events

`services` and `events` each take several names and match any of them; together they keep the events of any named service carrying any named event. Of everything `repos` and `sites` emitted, only `sites`' one `site.published` carries the one name asked for.

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

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"services":["repos","sites"],"events":["site.published"]}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[{"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"}]}
```

and a `content` array of one text block whose text is exactly that line. With `events` left out, the answer is the whole log, as in `An agent searches the whole log`; with `{"services":["sites"]}` alone, it is this one record.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent searches by user

Everything one user's requests put on the bus, across repositories and days, newest first: the three refs `u_1e9b4d07` pushed. 4177 is not among them, though it follows from that user's push: `sites` emitted it with no user.

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

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"user":"u_1e9b4d07"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[
  {"id":"evt_2f6b9d1e4a7c0583","time":"2026-10-05T09:20:15.730468Z","service":"repos","event":"repo.pushed","request_id":"9e5c1a7d3f0b4862c8a2e6f0d4b9c173","user":"u_1e9b4d07","attrs":{"new":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","old":"dba41ecccc3fc1626e53a13043b026c48bbf33fe","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":4181,"received":"2026-10-05T09:20:15.734902Z"},
  {"id":"evt_2d7a9c4e1f6b3058","time":"2026-10-04T16:20:11.402401Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"0000000000000000000000000000000000000000","ref":"refs/tags/v1.2.0","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4176,"received":"2026-10-04T16:20:11.409876Z"},
  {"id":"evt_3a7c1e9b5d2f4086","time":"2026-10-04T16:20:11.402316Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"a4c123b1612dd272d1371c17149d439536b3216f","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4175,"received":"2026-10-04T16:20:11.407022Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent searches by request id

The agent knows the request id of a push from the trail and asks what it put on the bus: one event per ref the push changed.

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

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"request_id":"4d6608697a8d41bed440e50454f31af3"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[
  {"id":"evt_2d7a9c4e1f6b3058","time":"2026-10-04T16:20:11.402401Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"0000000000000000000000000000000000000000","ref":"refs/tags/v1.2.0","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4176,"received":"2026-10-04T16:20:11.409876Z"},
  {"id":"evt_3a7c1e9b5d2f4086","time":"2026-10-04T16:20:11.402316Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"a4c123b1612dd272d1371c17149d439536b3216f","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4175,"received":"2026-10-04T16:20:11.407022Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent follows a chain by its cause

`cause` finds what an event led to: the events emitted while a service handled it, each naming it as its cause. Asked for the events `evt_3a7c1e9b5d2f4086` caused, the answer is the `site.published` `sites` emitted while handling it. Each event found can be asked about in turn, one link of the chain at a time. `cause` `""` keeps the events that start a chain (`An agent searches for events with no user`).

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

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"cause":"evt_3a7c1e9b5d2f4086"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[{"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"}]}
```

and a `content` array of one text block whose text is exactly that line. With `{"cause":"evt_5d1f8b3e7c0a2649"}` the answer is `{"records":[]}`: nothing has followed from 4177.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent searches by an attribute's value

`attrs` finds the events whose attributes hold the given keys with the given values, whatever service emitted them: everything that names the repository `rep_41d8f0a6b2c97e13`, `sites`' `site.published` among the pushes. A string is matched as a string, exactly.

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

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"attrs":{"repo":"rep_41d8f0a6b2c97e13"}}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[
  {"id":"evt_e1c5a8f2d6b94037","time":"2026-10-05T09:12:40.551930Z","service":"repos","event":"repo.pushed","request_id":"4b484e73cf575dcad6ba2b0aee0ca923","user":"u_7f3a9c21","attrs":{"new":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3","old":"daeeb975729fae923d5a4fd12aabfe228f219e9c","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4180,"received":"2026-10-05T09:12:40.556218Z"},
  {"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"},
  {"id":"evt_2d7a9c4e1f6b3058","time":"2026-10-04T16:20:11.402401Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"0000000000000000000000000000000000000000","ref":"refs/tags/v1.2.0","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4176,"received":"2026-10-04T16:20:11.409876Z"},
  {"id":"evt_3a7c1e9b5d2f4086","time":"2026-10-04T16:20:11.402316Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"a4c123b1612dd272d1371c17149d439536b3216f","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4175,"received":"2026-10-04T16:20:11.407022Z"}]}
```

and a `content` array of one text block whose text is that object encoded compactly. With `{"repo":"rep_41d8f0a6b2c97e13","ref":"refs/tags/v1.2.0"}` the answer is 4176 alone, the one event carrying both values.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent pages through a search to its end

`limit` sets the page; the cursor carries the search on. Three pages of three cover the log: the third holds the two left and no cursor, which is how the agent knows it has everything.

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

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"limit":3}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

then, with the `<cursor>` the first answer gave,

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a8f3c1e7b2d94605f1e8c3a7b9d2e4f6
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"limit":3,"cursor":"<cursor>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

and once more with the `<cursor>` the second answer gave, as `id` 9.

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, three times. The first body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member and a `structuredContent` of

```
{"records":[
  {"id":"evt_8c3f1a6e2d9b4075","time":"2026-10-05T09:31:58.204117Z","service":"repos","event":"repo.pushed","request_id":"6c2e9a4f1b7d3058e2a6c9f4b1d7e305","user":"u_7f3a9c21","attrs":{"new":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","old":"7d5b3f1e9c0a2846b8d4f6e1a3c5b7d9e0f2a4c6","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":4182,"received":"2026-10-05T09:31:58.209553Z"},
  {"id":"evt_2f6b9d1e4a7c0583","time":"2026-10-05T09:20:15.730468Z","service":"repos","event":"repo.pushed","request_id":"9e5c1a7d3f0b4862c8a2e6f0d4b9c173","user":"u_1e9b4d07","attrs":{"new":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","old":"dba41ecccc3fc1626e53a13043b026c48bbf33fe","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":4181,"received":"2026-10-05T09:20:15.734902Z"},
  {"id":"evt_e1c5a8f2d6b94037","time":"2026-10-05T09:12:40.551930Z","service":"repos","event":"repo.pushed","request_id":"4b484e73cf575dcad6ba2b0aee0ca923","user":"u_7f3a9c21","attrs":{"new":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3","old":"daeeb975729fae923d5a4fd12aabfe228f219e9c","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4180,"received":"2026-10-05T09:12:40.556218Z"}]}
```

with, besides `records`, a member `"cursor":"<cursor>"`, an opaque string. The second, `id` 8, holds

```
{"records":[
  {"id":"evt_c4a9e2f6b1d83075","time":"2026-10-05T08:47:03.118204Z","service":"repos","event":"repo.pushed","request_id":"176813e02ea68ef786e4d3cea27d2693","user":"u_7f3a9c21","attrs":{"new":"dba41ecccc3fc1626e53a13043b026c48bbf33fe","old":"b0eb53f16947ccf25ec84d8dbc74254770f58904","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0,"seq":4179,"received":"2026-10-05T08:47:03.122871Z"},
  {"id":"evt_6e9a2c5f0b3d7148","time":"2026-10-04T16:20:11.951240Z","service":"repos","event":"repo.pushed","request_id":"b3f7d1a9c5e24680f2b6d0a8c4e1f397","user":"u_2b8e1d04","attrs":{"new":"e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92","old":"c0a4e8f2b6d1937e5a9c3f7b1d0e2a4c6f8b9d10","ref":"refs/heads/main","repo":"rep_d41c7a9e05b28f63"},"cause":"","depth":0,"seq":4178,"received":"2026-10-04T16:20:12.040117Z"},
  {"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"}]}
```

with another `cursor`. The third, `id` 9, has no `cursor` and a `structuredContent` of

```
{"records":[
  {"id":"evt_2d7a9c4e1f6b3058","time":"2026-10-04T16:20:11.402401Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"0000000000000000000000000000000000000000","ref":"refs/tags/v1.2.0","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4176,"received":"2026-10-04T16:20:11.409876Z"},
  {"id":"evt_3a7c1e9b5d2f4086","time":"2026-10-04T16:20:11.402316Z","service":"repos","event":"repo.pushed","request_id":"4d6608697a8d41bed440e50454f31af3","user":"u_1e9b4d07","attrs":{"new":"daeeb975729fae923d5a4fd12aabfe228f219e9c","old":"a4c123b1612dd272d1371c17149d439536b3216f","ref":"refs/heads/main","repo":"rep_41d8f0a6b2c97e13"},"cause":"","depth":0,"seq":4175,"received":"2026-10-04T16:20:11.407022Z"}]}
```

Each `content` array is one text block whose text is its object encoded compactly. No event is on two pages and none is skipped: the three pages together are the log, newest first, as `An agent searches the whole log` answers it.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent searches a time range

`since` and `until` bound each event's own `time`, when its producer emitted it, not when events received it: `since` is inclusive and `until` exclusive. From `16:20:12` on 2026-10-04 to the next minute, the one event emitted in that span is `sites`' `site.published`. 4178 reached events after `16:20:12`, but `repos` emitted it before, so it is not in the range.

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

{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"since":"2026-10-04T16:20:12Z","until":"2026-10-04T16:21:00Z"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 16 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[{"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"}]}
```

and a `content` array of one text block whose text is exactly that line. With `since` `2026-10-04T16:20:11.9Z` instead, the answer also holds 4178, first, since its `seq` is the higher:

```
{"records":[
  {"id":"evt_6e9a2c5f0b3d7148","time":"2026-10-04T16:20:11.951240Z","service":"repos","event":"repo.pushed","request_id":"b3f7d1a9c5e24680f2b6d0a8c4e1f397","user":"u_2b8e1d04","attrs":{"new":"e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92","old":"c0a4e8f2b6d1937e5a9c3f7b1d0e2a4c6f8b9d10","ref":"refs/heads/main","repo":"rep_d41c7a9e05b28f63"},"cause":"","depth":0,"seq":4178,"received":"2026-10-04T16:20:12.040117Z"},
  {"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"}]}
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent searches for events with no user

An empty `user` is a value like any other, and matches the events whose `user` is empty: those no user caused. Here that is `sites`' `site.published`, which it emitted while handling a delivery, with no user and no request. An empty `request_id` matches the events whose `request_id` is empty the same way, here 4177 alone too, and an empty `cause` the events no event caused, those that start a chain: here the seven `repo.pushed`, 4182 down to 4175 without 4177.

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

{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"user":""}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 17 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[{"id":"evt_5d1f8b3e7c0a2649","time":"2026-10-04T16:20:12.031877Z","service":"sites","event":"site.published","request_id":"","user":"","attrs":{"repo":"rep_41d8f0a6b2c97e13","sha":"daeeb975729fae923d5a4fd12aabfe228f219e9c"},"cause":"evt_3a7c1e9b5d2f4086","depth":1,"seq":4177,"received":"2026-10-04T16:20:12.035410Z"}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent's filters match nothing

A well-formed filter that no event satisfies is an empty page; the agent learns the log holds nothing of the kind, and can widen the filter or ask `catalog` (`S08`) what there is. `repos` emits no `site.published`.

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

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"services":["repos"],"events":["site.published"]}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent gives a range that ends before it starts

`since` not before `until` is a range holding no instant. It is not an error: it matches nothing.

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

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"since":"2026-10-05T09:00:00Z","until":"2026-10-05T08:00:00Z"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[]}
```

and a `content` array of one text block whose text is exactly that line. `since` equal to `until` answers the same.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent asks for a page outside the range

`limit` is 1 to 500; 0 and 501 are refused alike, each with the number given.

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

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"limit":0}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
limit must be between 1 and 500, got 0
```

With `"limit":501` the text is `limit must be between 1 and 500, got 501`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent gives a time the tool cannot read

`since` and `until` are RFC 3339 or nothing; a date alone, a bare word, or a local time with no offset is refused, naming the argument and quoting the value.

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

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"since":"yesterday"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
since is not an RFC 3339 time: 'yesterday'
```

With `{"until":"2026-10-05 09:00"}` the text is `until is not an RFC 3339 time: '2026-10-05 09:00'`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent passes a cursor no search issued

A cursor is only what a `search` answer gave; a made-up one, or one damaged in transit, is refused rather than guessed at.

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

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"cursor":"page2"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cursor is not one search issued
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.

## An agent searches while events cannot reach its log

events answers from its log or not at all: a search it cannot run is refused, never answered as an empty page, which would read as "nothing happened".

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

{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"user":"u_7f3a9c21"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 18 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cannot reach the log; try again later
```

Preconditions:

- The preamble's, but events' database can no longer be read: its storage has begun failing since events opened it.

Postconditions:

- Nothing has changed. events is still serving, and once its database can be read again the same call answers the events of `u_7f3a9c21`.

## An agent passes search an argument it does not have

Arguments that do not fit the tool's input schema are refused as every tool of the suite refuses them (`S05`); events searches nothing.

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

{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"bogus":"x"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 15 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
bogus: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed; the log is as it was.
