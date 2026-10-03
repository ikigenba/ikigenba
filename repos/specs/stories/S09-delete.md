# Stories — delete

`delete`, the tool that removes one of the caller's repositories for good. Its one argument is `repo`, required, a string: the repository by id or by name as `show` reads it (`S07`), among the caller's repositories only. It removes the repository's catalog entry and its directory, `state/repos/<id>.git`, with every ref and object in it, and answers `{"id":"<id>","name":"<name>"}`, the id and name the repository had, so the model can tell a sibling holding the id that it is gone. There is no undo and no trash; once the call has answered, the name is free and the id names nothing, in any tool and on git (`S11`). A repository is deleted only while nothing is running on it: while a git operation (a fetch or a push, `S11`) or maintenance (`S13`) on it is in flight, `delete` is refused with an `isError` result whose one text block is exactly `repository '<name>' is busy; try again once its git operations finish`, its `outcome` `error`, and nothing is removed; the model calls again once the operation has finished. An operation still waiting for a slot is not in flight and does not make a repository busy (`S12`). An unavailable repository (`S14`) is deleted like any other, which is how a caller clears one away: its entry is removed and whatever is at its directory's path, if anything, with it. `delete` is of kind `destructive`.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `notes` (`rep_3f9a0c1d2e4b5a69`, `refs/heads/main` at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`) and `site` (`rep_8c21d4e0f7a3b915`), and `u_2b8e1d04` owns `journal` (`rep_d41c7a9e05b28f63`). A delete that removes a repository records `repo.deleted`, with `repo`, the id, and `owner`, the caller's user id, before its `tool.called`; the name is in no event. A refusal changes nothing. repos writes nothing to stderr for any answer in this group.

## A model deletes a repository

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

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete","arguments":{"repo":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"rep_3f9a0c1d2e4b5a69","name":"notes"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's: the caller owns `notes`, and nothing is running on it.

Postconditions:

- The catalog no longer holds `rep_3f9a0c1d2e4b5a69`. `list` (`S07`) answers `site` alone; `show` with `notes` or with `rep_3f9a0c1d2e4b5a69` is refused with `repo: no repository '<repo>'`.
- `state/repos/rep_3f9a0c1d2e4b5a69.git` no longer exists. `site`'s and `journal`'s directories are untouched.
- A git request to `/notes.git/...` is answered `404` with `repository not found` (`S11`).
- The name is free: the caller may create a new `notes` (`S06`), which is empty and gets a new id, never `rep_3f9a0c1d2e4b5a69` while that id is still catalogued or its directory still exists; ids are random, so a deleted id coming back is vanishingly unlikely rather than ruled out.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"repo.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"owner":"u_7f3a9c21","repo":"rep_3f9a0c1d2e4b5a69"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `notes` is in none of them.

## A model deletes a repository it names by id

`repo` may be the id. The answer and every postcondition are those of `A model deletes a repository`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete","arguments":{"repo":"rep_3f9a0c1d2e4b5a69"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"rep_3f9a0c1d2e4b5a69","name":"notes"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- Those of `A model deletes a repository`.

## A model deletes a repository while a clone of it is running

Removing a repository's directory under a running git would cut the clone off midway, so `delete` refuses instead, naming the repository, and the model tries again once the clone has finished. A push, or maintenance (`S13`), running on the repository is refused the same way. The refusal is not a rule offence: the arguments were right, and the same call succeeds later.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete","arguments":{"repo":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
repository 'notes' is busy; try again once its git operations finish
```

The name is the repository's own, however `repo` named it: the same call with `rep_3f9a0c1d2e4b5a69` gets the same text.

Preconditions:

- The preamble's, and a clone of `notes` by the caller (`POST /notes.git/git-upload-pack`, `S11`) is running when the call is made.

Postconditions:

- Nothing was removed: `notes` is in the catalog, its directory and refs are as they were, and the clone runs to its end and succeeds.
- repos recorded no `repo.deleted`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"delete"}}
  ```

- Once the clone has finished, the same call succeeds as in `A model deletes a repository`.

## A model deletes a repository that is unavailable

A repository startup verification could not open (`S14`) cannot be fetched or pushed, but it can be deleted, so a broken repository need not stay in the caller's listing for ever. Its catalog entry is removed, and so is whatever is at its directory's path; when nothing is there, there is nothing more to remove. The answer is the same as for an available repository.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete","arguments":{"repo":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"rep_3f9a0c1d2e4b5a69","name":"notes"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's, except that `state/repos/rep_3f9a0c1d2e4b5a69.git` held a directory that is not a valid bare repository when repos last started, so startup verification marked `notes` unavailable (`S14`).

Postconditions:

- The catalog no longer holds `rep_3f9a0c1d2e4b5a69`, and `state/repos/rep_3f9a0c1d2e4b5a69.git` no longer exists. On the next start, verification has nothing of it to check and records no `repo.unavailable` for it.
- telemetry has received `repo.deleted` with `repo` `rep_3f9a0c1d2e4b5a69` and `owner` `u_7f3a9c21`, and a `tool.called` with `outcome` `ok`, as in `A model deletes a repository`.

## A model deletes a repository it does not have

A `repo` that names none of the caller's repositories is refused, quoting the value as sent, as a rule offence (`S05`); another user's repository, by name or by id, is refused exactly the same way, and is untouched. Here the model names `u_2b8e1d04`'s `journal` by name.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"delete","arguments":{"repo":"journal"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: no repository 'journal'
```

Preconditions:

- The preamble's: `journal` is `u_2b8e1d04`'s, and the caller owns no repository of that name.

Postconditions:

- Nothing was removed: `u_2b8e1d04`'s `journal` is in the catalog, and `state/repos/rep_d41c7a9e05b28f63.git` is untouched.
- repos recorded no `repo.deleted`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"delete"}}
  ```

## A model deletes a repository that is already gone

A second delete of the same repository finds nothing, and is refused as for any repository the caller does not have: delete is not quietly repeated, so a model learns the first call did what it asked rather than that something else did.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"delete","arguments":{"repo":"rep_3f9a0c1d2e4b5a69"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: no repository 'rep_3f9a0c1d2e4b5a69'
```

Preconditions:

- The preamble's, after `A model deletes a repository`.

Postconditions:

- Nothing has changed. repos recorded no second `repo.deleted`; the request's `tool.called` has `outcome` `error`.

## A model calls delete without saying which repository

`repo` is required and is the only argument. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"delete","arguments":{"name":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: missing required field
name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing was removed: `notes` is in the catalog with its directory.
- repos recorded no `repo.deleted`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"destructive","outcome":"invalid_arguments","tool":"delete"}}
  ```
