# Stories — delete

`delete`, the tool that takes one of the caller's sites off the web for good. Its one argument is `name`, required, a string, the name of one of the caller's sites. It removes the site's catalog record and everything sites holds for it under `cache/sites/<site id>/`, and answers `{"deleted":true,"id":"<site id>"}`. The site's repository is not sites' and is untouched: its commits, and every other site that publishes from it, are as they were, and a new site can be created from it at once. There is no undo; once the call has answered, the site's URL answers not-found (`S11`), its name and slug are free for any user's `create` (`S06`), and its id names nothing. When the site was the apex (`S13`), the apex is cleared with it, so the space's bare address does not send visitors to a site that is gone. `delete` is of kind `destructive`.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `blog` (`sit_4e7a1c9b0d2f8635`, repository `rep_8c21d4e0f7a3b915`, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, its tree at `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`), `handbook`, and `scratch` (`sit_2d6f8a0c4e1b3957`, on the same repository as `blog`), and `u_2b8e1d04` owns `recipes`; no apex is set unless a story sets it. A delete that removes a site records `site.deleted`, with `site`, the id, and, when the site was the apex, then `site.apex` with `site` `""`, both before its `tool.called`; the name is in no event. A refusal removes nothing and records no `site.*` event. sites writes nothing to stderr for any answer in this group.

## A model deletes a site

The ordinary case.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"sit_4e7a1c9b0d2f8635"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- The catalog no longer holds `sit_4e7a1c9b0d2f8635`. `list` (`S07`) answers `handbook` and `scratch`; `show`, `publish`, `update`, and `delete` with `blog` are refused with `no site named 'blog'`.
- `cache/sites/sit_4e7a1c9b0d2f8635/` no longer exists. `handbook`'s and `scratch`'s directories under `cache/sites/`, if any, are untouched.
- `../repos/state/repos/rep_8c21d4e0f7a3b915.git` is untouched: sites ran no git and wrote nothing there. `scratch`, on the same repository, is as it was.
- `GET /blog/` is answered 404 with sites' not-found page, as for any slug no site has: no visitor cookie is set and no `site.viewed` is recorded (`S11`).
- The name `blog` is free: any user may create a site named `blog` (`S06`), which gets a new id.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `blog` is in none of them.

## A model deletes the apex site

The apex is a setting that names a site; with the site gone it would name nothing, so the delete clears it. The space's bare address then answers not-found, as with no apex set, until a model sets another (`S13`). Clearing the apex is recorded as `apex` records it, after the delete.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"sit_4e7a1c9b0d2f8635"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's, except that the apex is `blog` (`S13`).

Postconditions:

- Those of `A model deletes a site`, and the apex is unset: `apex` with no arguments answers `{"apex":null}`, and `GET /` on the apex host, `Host: ikigenba.dev`, is answered 404 with sites' not-found page (`S13`).
- telemetry has received the request's five events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"site.apex","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"site":""}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model deletes another user's site

Only a site's owner deletes it. Another user's site does not exist for the caller, and gets exactly the answer a site that does not exist gets; it is untouched and goes on being served.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete","arguments":{"name":"recipes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'recipes'
```

Preconditions:

- The preamble's: `recipes` is `u_2b8e1d04`'s.

Postconditions:

- Nothing was removed: `recipes` is in the catalog, its tree under `cache/sites/sit_6b1d3f5a7c9e0284/` is as it was, and `GET /recipes/` is served as before.
- sites recorded no `site.deleted`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"delete"}}
  ```

## A model deletes a site it does not have

A name among none of the caller's sites is refused, quoting it as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete","arguments":{"name":"wiki"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'wiki'
```

Preconditions:

- The preamble's: no site is named `wiki`.

Postconditions:

- Nothing was removed.
- sites recorded no `site.deleted`; the request's `tool.called` has `kind` `destructive` and `outcome` `error`.

## A model deletes a site that is already gone

A second delete of the same site finds nothing, and is refused as for any site the caller does not have: delete is not quietly repeated, so a model learns the first call did what it asked rather than that something else did.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"delete","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'blog'
```

Preconditions:

- The preamble's, after `A model deletes a site`.

Postconditions:

- Nothing has changed. sites recorded no second `site.deleted`; the request's `tool.called` has `outcome` `error`.

## A model calls delete without saying which site

`name` is required and is the only argument. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`). Here the model named the site by its id under a key the tool does not have.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"delete","arguments":{"id":"sit_4e7a1c9b0d2f8635"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: missing required field
id: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing was removed: `blog` is in the catalog with its tree.
- sites recorded no `site.deleted`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"destructive","outcome":"invalid_arguments","tool":"delete"}}
  ```
