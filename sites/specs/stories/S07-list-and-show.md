# Stories — list and show

`list` and `show`, the two tools that tell the caller what sites it has. Both are of kind `read`, change nothing, run no git, and see only the caller's own sites: another user's is not in a listing, and asking for it by name gets the answer a site that does not exist gets. `list` takes no arguments and answers `{"sites":[...]}`, one entry per site the caller owns, sorted by name ascending, each with the members, in this order, `id`, `name`, `slug`, `url`, `visibility`, `listed`, and `commit`, the sha the site is published at, absent while it is unpublished; a caller with none gets `{"sites":[]}`. `show` takes one argument, `name`, required, a string, the site's name, looked up among the caller's sites only; a site's id or slug is not its name. It answers the site object `create` answers (`S06`), members in the same order — `id`, `name`, `slug`, `url`, `repo`, `ref`, `visibility`, `listed`, `commit`, `created`, `published` — where `commit` is the published sha and `published` the time of the last publish, RFC 3339 UTC to the second, both absent while the site is unpublished. `url` is built for the request as `S06` fixes. A `name` that names none of the caller's sites is refused with exactly `no site named '<name>'`, quoting the value as sent.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `blog` (`sit_4e7a1c9b0d2f8635`, created `2026-10-01T09:30:00Z`, published `2026-10-01T10:00:00Z` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`), `handbook` (`sit_9a3c5e7b1d0f2468`, private, created `2026-10-01T11:00:00Z`, published `2026-10-01T11:05:00Z` at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`), and `scratch` (`sit_2d6f8a0c4e1b3957`, slug `scratch-7c1e9a4f`, ref `preview`, unlisted, created `2026-10-02T08:00:00Z`, unpublished), and `u_2b8e1d04` owns `recipes` (`sit_6b1d3f5a7c9e0284`). Every story is read-only: nothing changes but the trail, which gains the request's `request.started`, its `tool.called` with `tool` `list` or `show`, `kind` `read`, and the outcome `S05` fixes, and its `request.finished`; neither tool records a `site.*` event. sites writes nothing to stderr for any answer in this group.

## A model lists its sites

The ordinary case: every site the caller owns, by name, each with its URL and enough to tell whether it is published and who can see it. `scratch` is unpublished, so its entry has no `commit`; it is unlisted, and its URL carries its slug. `recipes` is `u_2b8e1d04`'s and is not listed. A call may leave `arguments` out or send `{}`; both are the same call.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"sites":[{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"},{"id":"sit_9a3c5e7b1d0f2468","name":"handbook","slug":"handbook","url":"https://sites.sbx.ikigenba.dev/handbook/","visibility":"private","listed":true,"commit":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d"},{"id":"sit_2d6f8a0c4e1b3957","name":"scratch","slug":"scratch-7c1e9a4f","url":"https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/","visibility":"public","listed":false}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No site's id, name, or slug is in them.

## A model lists its sites when it has none

A caller that owns no site gets an empty list, not an error, even while other users own sites.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_5c0e7a92
X-User-Email: bo@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"sites":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's; the caller is `u_5c0e7a92` (`bo@example.com`), who owns no site. The catalog's four sites are all other users'.

Postconditions:

- Nothing has changed.

## A model sends list an argument

`list` takes no arguments, so anything sent to it is an unknown field, refused as its arguments are read (`S05`). A model that believes it filtered the listing must learn that it did not.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `list`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.

## A model shows a site by name

The ordinary case, and how a model learns everything about one site: where it is served, which repository and ref it publishes from, what it is published at, and when.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"show","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. `commit` is the sha `blog` was last published at, whatever `main` points at now: `show` reads the catalog, not the repository.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's `request.started`, then `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"show"}` as the attributes of its `tool.called`, then its `request.finished` with `status` 200. Neither the id nor the name of `blog` is in them.

## A model shows a site that has not been published

A site nobody has published has no `commit` and no `published`; it has everything else, its URL included, which answers not-found until a publish (`S08`, `S11`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"show","arguments":{"name":"scratch"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_2d6f8a0c4e1b3957","name":"scratch","slug":"scratch-7c1e9a4f","url":"https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/","repo":"rep_8c21d4e0f7a3b915","ref":"preview","visibility":"public","listed":false,"created":"2026-10-02T08:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `scratch` is unpublished.

Postconditions:

- Nothing has changed.

## A model shows another user's site

`show` is owner-only, and another user's site does not exist for the caller: asking for it gets exactly the answer a site that does not exist gets. That `recipes` is taken the caller can learn only from `create` (`S06`), and its pages only by opening them (`S11`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"show","arguments":{"name":"recipes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'recipes'
```

Preconditions:

- The preamble's: `recipes` is `u_2b8e1d04`'s.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `show`, `kind` `read`, and `outcome` `error`, as for a site that does not exist.

## A model shows a site it does not have

A name among none of the caller's sites is refused, quoting it as sent. The same answer comes for a name that is not a valid name, since it can name nothing, and for a site's id or slug, which are not names: `scratch-7c1e9a4f` gets `no site named 'scratch-7c1e9a4f'`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"show","arguments":{"name":"wiki"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'wiki'
```

Preconditions:

- The preamble's: no site is named `wiki`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"show"}}
  ```

## A model calls show without saying which site

`name` is required. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"show","arguments":{"site":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: missing required field
site: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `show`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.
