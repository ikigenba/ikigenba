# Stories — list and show

`list` and `show`, the two tools that tell the caller what repositories it has. Both are of kind `read`, change nothing, and see only the caller's own repositories: another user's is not in a listing, and asking for it by name or by id is the same as asking for one that does not exist. `list` takes no arguments and answers `{"repos":[...]}`, one entry per repository the caller owns, sorted by name ascending, each with the members, in this order, `id`, `name`, `size_bytes`, `head`, and `available`; a caller with none gets `{"repos":[]}`. `show` takes one argument, `repo`, required, a string: the repository's id or its name. A value that begins `rep_` is read as an id and anything else as a name, and either is looked up among the caller's repositories only. It answers the members `create` answers (`S06`), in the same order: `id`, `name`, `default_branch`, `head`, `size_bytes`, `available`, `created`, `clone_url`, and `credentials`, with `clone_url` and `credentials` built for the request it answers as `S06` fixes. `head` is the 40-digit hexadecimal sha `refs/heads/main` points at, and is absent when the repository has no commit on it; `size_bytes` is the size of the repository's directory on disk in bytes as it is when the call runs; `available` is `false` for a repository startup verification found missing or broken (`S14`), whose `head` is then absent, since it cannot be read, and whose `size_bytes` is what its directory holds, 0 when there is none. A `repo` that names none of the caller's repositories is refused with `repo: no repository '<repo>'`, quoting the value as sent, as a rule offence (`S05`).

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `notes` (`rep_3f9a0c1d2e4b5a69`, `refs/heads/main` at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`, created `2026-09-30T10:15:00Z`) and `site` (`rep_8c21d4e0f7a3b915`, no commits, created `2026-10-01T09:00:00Z`), and `u_2b8e1d04` owns `journal` (`rep_d41c7a9e05b28f63`). Every story is read-only: nothing changes but the trail, which gains the request's `request.started`, its `tool.called` with `tool` `list` or `show`, `kind` `read`, and the outcome `S05` fixes, and its `request.finished`; neither tool records a domain event. repos writes nothing to stderr for any answer in this group.

## A model lists its repositories

The ordinary case: every repository the caller owns, by name, each with enough to tell whether it has been pushed to and how big it is. `journal` is `u_2b8e1d04`'s and is not listed. A call may leave `arguments` out or send `{}`; both are the same call.

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
{"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":<notes-size>,"head":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d","available":true},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<site-size>,"available":true}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly. `<notes-size>` and `<site-size>` are the sizes of `state/repos/rep_3f9a0c1d2e4b5a69.git` and `state/repos/rep_8c21d4e0f7a3b915.git` on disk, in bytes, each more than 0.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No repository's id or name is in them.

## A model lists its repositories when it has none

A caller that owns nothing gets an empty list, not an error, even while other users own repositories.

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
{"repos":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's; the caller is `u_5c0e7a92`, who owns no repository. The preamble's three repositories are all other users'.

Postconditions:

- Nothing has changed.

## A model lists its repositories when one is unavailable

A repository startup verification could not open stays in the catalog and in the listing, marked unavailable, so the model learns it exists and is broken rather than finding it gone. Its `head` cannot be read and is absent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"repos":[{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","size_bytes":0,"available":false},{"id":"rep_8c21d4e0f7a3b915","name":"site","size_bytes":<site-size>,"available":true}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's, except that `state/repos/rep_3f9a0c1d2e4b5a69.git` was missing when repos last started, so startup verification marked `notes` unavailable (`S14`), and it is still missing.

Postconditions:

- Nothing has changed. `notes` is still in the catalog and still unavailable.

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

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list","arguments":{"name":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `list`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.

## A model shows a repository by name

The ordinary case, and how a model that knows only the name learns the clone URL, the head it would fetch, and how to give git its token. `notes` has commits, so `head` is the sha `refs/heads/main` points at.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"show","arguments":{"repo":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","default_branch":"main","head":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d","size_bytes":<notes-size>,"available":true,"created":"2026-09-30T10:15:00Z","clone_url":"https://repos.sbx.ikigenba.dev/notes.git","credentials":"<credentials>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<notes-size>` is as in `A model lists its repositories`; `<credentials>` is the text of `S06`'s `A model creates a repository`, whose command's scope is `https://*.sbx.ikigenba.dev`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's `request.started`, then `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"show"}` as the attributes of its `tool.called`, then its `request.finished` with `status` 200. Neither the id nor the name of `notes` is in them.

## A model shows a repository by id

An id reaches the same repository its name does, and keeps reaching it after a rename (`S08`), so a sibling that stored the id need not track names. The answer is the one of `A model shows a repository by name`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"show","arguments":{"repo":"rep_3f9a0c1d2e4b5a69"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member and a `structuredContent` that is exactly the one of `A model shows a repository by name`, member for member, with its text block.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.

## A model shows a repository that has no commits

A repository nobody has pushed to has no `refs/heads/main`, so its result has no `head`; it is still available and still has a clone URL to push the first commit to.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"show","arguments":{"repo":"site"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"rep_8c21d4e0f7a3b915","name":"site","default_branch":"main","size_bytes":<site-size>,"available":true,"created":"2026-10-01T09:00:00Z","clone_url":"https://repos.sbx.ikigenba.dev/site.git","credentials":"<credentials>"}
```

and a `content` array of one text block whose text is that object encoded compactly; `<credentials>` is as in `A model shows a repository by name`.

Preconditions:

- The preamble's: `site` has no commits.

Postconditions:

- Nothing has changed.

## A model shows a repository that is unavailable

An unavailable repository is still the caller's and still shown, so the model can tell it is broken rather than gone, and can delete it (`S09`). Its `head` is absent; its clone URL is given, though git requests to it are answered `503` until it verifies again (`S11`, `S14`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"show","arguments":{"repo":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"rep_3f9a0c1d2e4b5a69","name":"notes","default_branch":"main","size_bytes":0,"available":false,"created":"2026-09-30T10:15:00Z","clone_url":"https://repos.sbx.ikigenba.dev/notes.git","credentials":"<credentials>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, except that `notes` is unavailable and its directory missing, as in `A model lists its repositories when one is unavailable`.

Postconditions:

- Nothing has changed. `show` does not try to verify the repository again; only a restart does (`S14`).

## A model shows a repository it does not have

A name or id among none of the caller's repositories is refused, quoting the value as sent. The same answer comes for an id that is well formed but was never minted, for one that is not well formed at all, such as `rep_zz`, and for a name that is not a valid name, since it can name nothing.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"show","arguments":{"repo":"drafts"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: no repository 'drafts'
```

Preconditions:

- The preamble's: the caller owns no repository named `drafts`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"show"}}
  ```

## A model shows another user's repository

Phase one is owner-only, and another user's repository does not exist for the caller: asking for it by name or by id gets exactly the answer a repository that does not exist gets, so the caller cannot even learn that the name or id is in use. Both forms below are refused the same way, each quoting the value it sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"show","arguments":{"repo":"journal"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"show","arguments":{"repo":"rep_d41c7a9e05b28f63"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly, for the first form:

```
invalid arguments:
repo: no repository 'journal'
```

and for the second:

```
invalid arguments:
repo: no repository 'rep_d41c7a9e05b28f63'
```

Preconditions:

- The preamble's: `journal`, `rep_d41c7a9e05b28f63`, is `u_2b8e1d04`'s, and the caller owns no repository named `journal`.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `show` and `outcome` `error`, as for a repository that does not exist.

## A model calls show without saying which repository

`repo` is required. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"show","arguments":{"id":"rep_3f9a0c1d2e4b5a69"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: missing required field
id: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `show`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.
