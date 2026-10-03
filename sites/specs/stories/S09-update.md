# Stories — update

`update`, the tool that changes how one of the caller's sites is offered: who may see it, whether it is listed, and the ref it tracks. It takes `name`, required, a string, the name of one of the caller's sites, and, in this order, `visibility`, a string, `listed`, a boolean, and `ref`, a string, all optional, of which at least one must be sent. Each is held to the rule `create` holds it to (`S06`): `visibility` is `public` or `private`; `ref` is a string git accepts as a ref name, and is not resolved. A field sent with the value the site already has is not a change. An update is all or nothing: every field sent is checked before any is applied, and a refusal applies none. The checks run in this order — the site; that a field was sent; `visibility`; `ref`; that the apex site stays public (`S13`) — and the first that fails is the whole answer, one line naming it. `update` never changes the name, the slug, or the published commit: `listed` changes whether the landing page shows the site to others (`S03`), never its URL, and `ref` changes what the next `publish` without a ref resolves (`S08`), never what is being served. It answers the site object `show` answers (`S07`), as it is after the call.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `blog` (`sit_4e7a1c9b0d2f8635`, slug `blog`, repository `rep_8c21d4e0f7a3b915`, ref `main`, public, listed, published `2026-10-01T10:00:00Z` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, created `2026-10-01T09:30:00Z`) and `scratch` (`sit_2d6f8a0c4e1b3957`, slug `scratch-7c1e9a4f`, ref `preview`, public, unlisted, unpublished, created `2026-10-02T08:00:00Z`), and `u_2b8e1d04` owns `recipes`; no apex is set unless a story sets it. `update` is of kind `additive`. An update records one `site.updated` per field it changed, with `site`, the id, `field`, the field's name, and `value`, its new value as a string (`true` and `false` for `listed`), in the order of the input schema, before its `tool.called`; a field sent with the value it already had records nothing. The name is in no event. A refusal records no `site.*` event. sites writes nothing to stderr for any answer in this group.

## A model makes a site private

A site that should be seen only by the space's users is made private. The change takes effect with the next request: a guest is sent to sign in, and a signed-in user is served as before, with `Cache-Control: private, no-cache` (`S12`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","visibility":"private"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"private","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `blog` is public.

Postconditions:

- The catalog has `blog` private; its commit, `published`, and tree are as they were.
- A guest's `GET /blog/` is answered 302 to auth's sign-in (`S12`).
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.updated","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"field":"visibility","site":"sit_4e7a1c9b0d2f8635","value":"private"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model unlists a site

An unlisted site leaves the landing page of every user but its owner (`S03`). Its slug is not changed: the URL already given out keeps working, so unlisting hides a site from the page but not from anyone who has its address. Listing an unlisted site the same way keeps its suffixed slug: `scratch` listed is still at `/scratch-7c1e9a4f/`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","listed":false},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":false,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `blog` is listed.

Postconditions:

- The catalog has `blog` unlisted, slug still `blog`. `GET /blog/` is answered as before (`S11`).
- telemetry has received, between the request's `request.started` and its `tool.called`:

  ```
  {"time":"<time>","service":"sites","event":"site.updated","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"field":"listed","site":"sit_4e7a1c9b0d2f8635","value":"false"}}
  ```

## A model changes the ref a site tracks

A site that should follow another branch from now on is pointed at it. Nothing is published by the change: the site keeps serving the commit it has until the model publishes (`S08`), and the new ref is not resolved now, so a branch still to be pushed is accepted.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","ref":"preview"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"preview","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. `commit` and `published` are unchanged.

Preconditions:

- The preamble's: `blog` tracks `main`.

Postconditions:

- The catalog has `blog` tracking `preview`, still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`. No git ran, nothing under `cache/` changed, and `GET /blog/` is served from the same tree with the same `ETag`.
- The next `publish` of `blog` with no `ref` resolves `preview` (`S08`).
- telemetry has received `site.updated` with attributes `{"field":"ref","site":"sit_4e7a1c9b0d2f8635","value":"preview"}`, and no `site.published`.

## A model changes several things about a site at once

Several fields go in one call, and each that changes is recorded on its own, in the order of the input schema. A field sent with the value the site already has, `visibility` here, is no change and records nothing. Listing `scratch` keeps its slug.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"update","arguments":{"name":"scratch","visibility":"public","listed":true,"ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_2d6f8a0c4e1b3957","name":"scratch","slug":"scratch-7c1e9a4f","url":"https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"created":"2026-10-02T08:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. `scratch` is still unpublished, so there is no `commit` or `published`.

Preconditions:

- The preamble's: `scratch` is public, unlisted, and tracks `preview`.

Postconditions:

- The catalog has `scratch` public, listed, tracking `main`, slug `scratch-7c1e9a4f`, unpublished.
- telemetry has received the request's five events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.updated","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"field":"listed","site":"sit_2d6f8a0c4e1b3957","value":"true"}}
  {"time":"<time>","service":"sites","event":"site.updated","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"field":"ref","site":"sit_2d6f8a0c4e1b3957","value":"main"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model updates a site to what it already is

Every field sent already has the value sent, so there is nothing to change. That is not a refusal: a model retrying an update that already happened gets the answer it would have got the first time, and nothing is recorded but the call.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","visibility":"public","listed":true},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- sites recorded no `site.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  ```

## A model calls update with nothing to change

A call naming only the site asks for nothing. It is refused, naming the fields that could be sent, so the model is not left believing it changed something.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
update needs at least one of visibility, listed, ref
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- sites recorded no `site.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"update"}}
  ```

## A model updates a site with a visibility sites does not have

A site is `public` or `private`, nothing else, and the value is not folded. The refusal applies none of the call: `listed`, sent beside the bad visibility, is not changed either.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","visibility":"hidden","listed":false},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
visibility must be public or private
```

Preconditions:

- The preamble's: `blog` is public and listed.

Postconditions:

- Nothing has changed: `blog` is still public and still listed.
- sites recorded no `site.updated`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model updates a site with a ref git would not accept

A ref is held to the rule `create` holds it to: a string git accepts as a ref name. `..bad` is not one, and is refused quoting it as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","ref":"..bad"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid ref '..bad'
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `blog` still tracks `main`.
- sites recorded no `site.updated`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model makes the apex site private

The apex site is what the space's bare address sends every visitor to (`S13`), signed in or not, so it must stay public. Making it private is refused; the model clears or moves the apex first with `apex` and then makes the site private.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","visibility":"private"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
apex site must be public
```

Preconditions:

- The preamble's, except that the apex is `blog` (`S13`).

Postconditions:

- Nothing has changed: `blog` is public and still the apex.
- sites recorded no `site.updated`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model updates another user's site

Only a site's owner updates it. Another user's site does not exist for the caller, and gets exactly the answer a site that does not exist gets.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"update","arguments":{"name":"recipes","visibility":"private"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'recipes'
```

Preconditions:

- The preamble's: `recipes` is `u_2b8e1d04`'s, public.

Postconditions:

- Nothing has changed: `recipes` is still public.
- sites recorded no `site.updated`; the request's `tool.called` has `kind` `additive` and `outcome` `error`, as for a site that does not exist.

## A model updates a site it does not have

A name among none of the caller's sites is refused, quoting it as sent; the site is looked up before the fields are looked at, so the same answer comes whatever else the call sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"update","arguments":{"name":"wiki","listed":false},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'wiki'
```

Preconditions:

- The preamble's: no site is named `wiki`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"update"}}
  ```

## A model sends arguments update does not take

A field of the wrong JSON type, or one the tool does not have, is refused as the arguments are read, every offence in one answer: the tool's fields first, in the order of its input schema, then each unknown field in the order it was sent (`S05`). Here the model sent `listed` as a string and tried to rename the site, which `update` does not do.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","listed":"no","new_name":"journal"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
listed: expected boolean, got string
new_name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `blog` is still listed and still named `blog`.
- sites recorded no `site.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"update"}}
  ```
