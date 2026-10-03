# Stories — rename

`rename`, the tool that gives one of the caller's repositories a new name. It takes two arguments, both required strings: `repo`, the repository, by id or by name as `show` reads it (`S07`), among the caller's repositories only; and `name`, the new name, held to the rule `create` holds a name to (`S06`), and not already the name of another of the caller's repositories. It answers what `show` answers for the repository under its new name: the same `id`, `head`, `size_bytes`, and `created`, and a `clone_url` ending `/<new name>.git`. A rename changes the catalog's name and the repository's `ikigenba.name` configuration, and nothing else: the id never changes, the directory `state/repos/<id>.git` is named by the id and is not moved, and the history is untouched, so a sibling that pinned the id or a sha keeps working. The old clone URL stops reaching the repository at once, and a clone whose remote names it must be pointed at the new one; that is why `rename` is of kind `destructive`. An unavailable repository (`S14`) can be renamed too: the catalog takes the new name, and its directory, which verification found broken, is not written to, so its `ikigenba.name` is left as it was. Renaming a repository to the name it already has is a success that changes nothing and records nothing but the call. A rename is not refused while git operations on the repository are running: one that began under the old URL runs to its end. Every rule the arguments break is reported in one answer, `repo` first, then `name` (`S05`).

The actor, the request shape, the result envelope, and the fixture are those of `S06`: the caller `u_7f3a9c21` owns `notes` (`rep_3f9a0c1d2e4b5a69`, `refs/heads/main` at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`, created `2026-09-30T10:15:00Z`) and `site` (`rep_8c21d4e0f7a3b915`), and `u_2b8e1d04` owns `journal` (`rep_d41c7a9e05b28f63`). A rename that changes a name records `repo.renamed`, with `repo`, the id, and `owner`, the caller's user id, before its `tool.called`; neither the old name nor the new is in any event. A refusal changes nothing. repos writes nothing to stderr for any answer in this group.

## A model renames a repository

The ordinary case. The answer is the repository under its new name, with the clone URL the model must now push to.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"notes","name":"notebook"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"rep_3f9a0c1d2e4b5a69","name":"notebook","default_branch":"main","head":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d","size_bytes":<notes-size>,"available":true,"created":"2026-09-30T10:15:00Z","clone_url":"https://repos.sbx.ikigenba.dev/notebook.git","credentials":"<credentials>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<notes-size>` is the directory's size, the same as before the call; `<credentials>` is the text of `S06`'s `A model creates a repository`.

Preconditions:

- The preamble's: the caller owns `notes` and no repository named `notebook`.

Postconditions:

- The caller's repository `rep_3f9a0c1d2e4b5a69` is named `notebook`; the caller has no repository named `notes`. `list` (`S07`) answers `notebook`, then `site`.
- Its directory is still `state/repos/rep_3f9a0c1d2e4b5a69.git`, holding the same refs, `refs/heads/main` still at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`. `git config --file state/repos/rep_3f9a0c1d2e4b5a69.git/config --get ikigenba.name` prints `notebook`; `ikigenba.id`, `ikigenba.owner`, and `ikigenba.created` are unchanged.
- `notes` is free: the caller may create a new, empty `notes` (`S06`), which gets a new id.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"repo.renamed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"owner":"u_7f3a9c21","repo":"rep_3f9a0c1d2e4b5a69"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"rename"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  Neither `notes` nor `notebook` is in them.

## A model renames a repository it names by id

`repo` may be the id, which is how a sibling that stored the id renames without knowing the current name. The answer and every postcondition are those of `A model renames a repository`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"rep_3f9a0c1d2e4b5a69","name":"notebook"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member and the `structuredContent` and text block of `A model renames a repository`.

Preconditions:

- The preamble's.

Postconditions:

- Those of `A model renames a repository`.

## A git client fetches from a repository's old clone URL after a rename

The old name names nothing once the rename is done, for the caller as for anyone, so a clone whose remote still points at it is answered as for a repository that does not exist (`S11`); the model learns the new URL from the `rename` answer, or from `show` by id. The new URL reaches the same repository and answers as `S11` fixes.

Request:

```
GET /notes.git/info/refs?service=git-upload-pack HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is the one line `repository not found`.

Preconditions:

- The preamble's, after `A model renames a repository`: `rep_3f9a0c1d2e4b5a69` is named `notebook`, and the caller has no repository named `notes`.

Postconditions:

- Nothing has changed. The same request to `/notebook.git/info/refs?service=git-upload-pack` is answered `200` with the advertisement of `rep_3f9a0c1d2e4b5a69`'s refs, `refs/heads/main` at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d` among them (`S11`).

## A model renames a repository to the name it already has

Nothing to change, and not a failure: a model retrying a rename that already happened gets the same answer it would have got the first time. No `repo.renamed` is recorded, since nothing was renamed.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"notes","name":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member and the `structuredContent` and text block `show` answers for `notes` (`S07`'s `A model shows a repository by name`).

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `notes` keeps its name, and its configuration was not rewritten.
- repos recorded no `repo.renamed`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"rename"}}
  ```

## A model renames a repository while a push to it is running

A rename moves nothing on disk, so it does not wait for git and is not refused: a push that began under the old URL, before the rename, runs to its end and updates the same repository, whose refs are then what that push made them. Only requests that begin after the rename must use the new URL.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"notes","name":"notebook"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member and the `structuredContent` and text block of `A model renames a repository`, but that `head` and `size_bytes` are whatever they are when the call runs, before or after the push lands.

Preconditions:

- The preamble's, and a push of the caller's to `/notes.git/git-receive-pack` (`S11`) is running when the call is made, moving `refs/heads/main` from `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d` to a new commit.

Postconditions:

- Those of `A model renames a repository`, except for the refs: the push completes, its client's git reports success, and `refs/heads/main` of `rep_3f9a0c1d2e4b5a69` is at the pushed commit. telemetry has received the push's `repo.pushed` with `repo` `rep_3f9a0c1d2e4b5a69`, the same id `repo.renamed` names.

## A model renames a repository to a name that is not allowed

The new name is held to the rule `create` holds a name to (`S06`), with the same line for every way of breaking it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"notes","name":"Notebook"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: must be 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `notes` keeps its name.
- repos recorded no `repo.renamed`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"rename"}}
  ```

## A model renames a repository to the name of another of its repositories

Two of the caller's repositories cannot share a name, so a rename onto a taken name is refused, never read as a request to replace or merge the repository that holds it. A name another user holds is free to the caller (`S06`): renaming `notes` to `journal` succeeds.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"notes","name":"site"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: 'site' is already one of your repositories
```

Preconditions:

- The preamble's: the caller owns `notes` and `site`.

Postconditions:

- Nothing has changed: `notes` and `site` keep their names, ids, and refs.
- repos recorded no `repo.renamed`; the request's `tool.called` has `outcome` `error`.

## A model renames a repository it does not have

A `repo` that names none of the caller's repositories is refused, quoting the value as sent; another user's repository, by name or by id, is refused exactly the same way (`S07`). Here the model names `u_2b8e1d04`'s `journal` by its id.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"rep_d41c7a9e05b28f63","name":"diary"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: no repository 'rep_d41c7a9e05b28f63'
```

Preconditions:

- The preamble's: `rep_d41c7a9e05b28f63` is `u_2b8e1d04`'s `journal`.

Postconditions:

- Nothing has changed: `u_2b8e1d04`'s repository is still named `journal`, and its configuration is untouched.
- repos recorded no `repo.renamed`; the request's `tool.called` has `outcome` `error`.

## A model breaks both of rename's rules at once

Every rule is checked and every broken one reported, `repo` before `name`, so a model learns both from one answer. Whether the new name is taken is judged among the caller's repositories whatever `repo` says, so a model that misnames the repository and picks a taken name learns both.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"drafts","name":"site"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: no repository 'drafts'
name: 'site' is already one of your repositories
```

Preconditions:

- The preamble's: the caller owns `site` and no repository named `drafts`.

Postconditions:

- Nothing has changed. The request's `tool.called` has `outcome` `error`.

## A model calls rename without a new name

Both fields are required. A call that leaves one out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer: the tool's fields in the order `repo`, `name`, then each unknown field in the order it was sent (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"rename","arguments":{"repo":"notes","new_name":"notebook"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: missing required field
new_name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `notes` keeps its name.
- repos recorded no `repo.renamed`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"destructive","outcome":"invalid_arguments","tool":"rename"}}
  ```
