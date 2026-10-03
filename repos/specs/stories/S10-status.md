# Stories — status

`status`, the tool that shows how hard repos is working, without a shell on the host. It takes no arguments and answers `{"read":{...},"write":{...},"repos":[...]}`. `read` and `write` are the host-wide pressure on git, one for each kind of operation (`S12`): `slots`, how many may run at once, `READ_SLOTS` and `WRITE_SLOTS` as repos read them at start (`S02`); `active`, how many are running now; and `queued`, how many are waiting for a slot. A fetch or clone is a read; a push and a maintenance run (`S13`) are writes. Those counts take in every user's operations, since they all share the slots, but name no repository and no user, so they tell the caller how busy the host is and nothing about anyone else's repositories. `repos` lists the caller's own repositories only, sorted by name ascending, each with the members, in this order, `id`, `name`, `size_bytes`, the size of its directory on disk in bytes; `limit_bytes`, `REPO_MAX_BYTES`, the size at or past which a push to it is refused (`S12`); `available` (`S07`); and `busy`, `true` while a git operation or maintenance on it is running, the same test `delete` applies (`S09`). An operation waiting for a slot or for its repository's lock is counted in `queued` and does not make its repository busy. A caller with no repositories gets `"repos":[]` beside the pressure. `status` is of kind `read`, changes nothing, and records no domain event.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `notes` (`rep_3f9a0c1d2e4b5a69`) and `site` (`rep_8c21d4e0f7a3b915`), and `u_2b8e1d04` owns `journal` (`rep_d41c7a9e05b28f63`); repos runs with no `[env]` setting in its environment unless a story says otherwise, so `READ_SLOTS` is 8, `WRITE_SLOTS` 2, and `REPO_MAX_BYTES` 1073741824. Every story is read-only: nothing changes but the trail, which gains the request's `request.started`, its `tool.called` with `tool` `status`, `kind` `read`, and the outcome `S05` fixes, and its `request.finished`. The tool call is not a git operation and takes no slot. repos writes nothing to stderr for any answer in this group.

## A model checks status while repos is idle

Nothing is running and nothing waiting: every count is 0 and no repository is busy.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: status

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"read":{"slots":8,"active":0,"queued":0},"write":{"slots":2,"active":0,"queued":0},"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":<notes-size>,"limit_bytes":1073741824,"available":true,"busy":false},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<site-size>,"limit_bytes":1073741824,"available":true,"busy":false}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly. `<notes-size>` and `<site-size>` are as `list` answers them (`S07`). `journal` is `u_2b8e1d04`'s and is not listed.

Preconditions:

- The preamble's: no git operation or maintenance is running or waiting.

Postconditions:

- Nothing has changed.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"status"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model checks status while repos is under load

The caller sees every slot taken and one clone waiting, so it knows to expect waits or `503`s (`S12`) and why; it sees which of its own repositories are busy, and so which it cannot delete yet (`S09`). Another user's clone fills a read slot and is counted, but nothing in the answer says whose it is or what it reads. The clone of `site` is waiting, not running, so `site` is busy only because of the push.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: status

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"status","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"read":{"slots":2,"active":2,"queued":1},"write":{"slots":1,"active":1,"queued":0},"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":<notes-size>,"limit_bytes":1073741824,"available":true,"busy":true},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<site-size>,"limit_bytes":1073741824,"available":true,"busy":true}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly. `<site-size>` is the size of `site`'s directory when the call runs, the push's objects included as far as they have been written.

Preconditions:

- The preamble's, except that repos was started with `READ_SLOTS=2` and `WRITE_SLOTS=1` in its environment.
- When the call is made, these git operations are running: a clone of `notes` by the caller and a clone of `journal` by `u_2b8e1d04`, which fill both read slots; and a push to `site` by the caller, which fills the write slot. A second clone of `site` by the caller is waiting for a read slot. No maintenance is running.

Postconditions:

- Nothing has changed. The operations go on as they were; the call took no slot and did not delay any of them.

## A model checks status while maintenance runs on one of its repositories

Maintenance (`S13`) is a write: it holds a write slot and its repository's lock for as long as it runs, so it shows in `write` and makes its repository busy, as a push would.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: status

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"status","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"read":{"slots":8,"active":0,"queued":0},"write":{"slots":2,"active":1,"queued":0},"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":<notes-size>,"limit_bytes":1073741824,"available":true,"busy":true},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<site-size>,"limit_bytes":1073741824,"available":true,"busy":false}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's, and scheduled maintenance of `notes` is running when the call is made; no git request is running or waiting.

Postconditions:

- Nothing has changed.

## A model checks status with an unavailable repository and its own limits

An unavailable repository (`S14`) is listed, marked unavailable, and never busy, since nothing runs on it. `limit_bytes` is whatever `REPO_MAX_BYTES` repos was started with, the same for every repository, and the slots are the started values.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: status

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"status","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"read":{"slots":4,"active":0,"queued":0},"write":{"slots":3,"active":0,"queued":0},"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":0,"limit_bytes":5000000,"available":false,"busy":false},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<site-size>,"limit_bytes":5000000,"available":true,"busy":false}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's, except that repos was started with `READ_SLOTS=4`, `WRITE_SLOTS=3`, and `REPO_MAX_BYTES=5000000` in its environment, and `state/repos/rep_3f9a0c1d2e4b5a69.git` was missing at that start, so `notes` is unavailable (`S14`) and its directory still missing. Nothing is running or waiting.

Postconditions:

- Nothing has changed.

## A model sends status an argument

`status` takes no arguments, so anything sent to it is an unknown field, refused as its arguments are read (`S05`). A model cannot ask for another user's repositories, or one repository's status, by naming it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: status

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"status","arguments":{"repo":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"read","outcome":"invalid_arguments","tool":"status"}}
  ```
