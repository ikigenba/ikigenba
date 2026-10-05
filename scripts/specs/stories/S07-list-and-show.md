# Stories — list and show

`list` and `show`, the two tools that tell the caller what scripts it has. Both are of kind `read`, change nothing, run no git, read no repository, and see only the caller's own scripts: another user's is not in a listing, and asking for it by name gets the answer a script that does not exist gets. `list` takes no arguments and answers `{"scripts":[...]}`, one entry per script the caller owns, sorted by name ascending, each with the members, in this order, `id`, `name`, `repo`, the repository's id as the catalog keeps it, `ref`, and `last_run`, the script's newest run, absent when it has never run; a caller with none gets `{"scripts":[]}`. `show` takes one argument, `name`, required, a string, the script's name, looked up among the caller's scripts only; a script's id is not its name. It answers the script object `create` answers (`S06`), members in the same order — `id`, `name`, `repo`, `ref`, `created`, `last_run` — under the same rule for `last_run`. `last_run` is the run of the script started last, whatever its status, with `id`; `status`; `exit_code`, present only when `status` is `exited`; and `started`, in that order (`S05`). A `name` that names none of the caller's scripts is refused with exactly `no script named '<name>'`, quoting the value as sent.

The actor, the request shape, the result envelope, and the fixture are those of `S06`, whose shared catalog holds, it being `2026-10-05T09:32:00Z`: the caller `u_7f3a9c21`'s `backfill` (`scr_e8f2a6c0d4b19357`, repository `rep_0f6a2d9e8c4b7153`, which repos has deleted, ref `main`, created `2026-10-02T12:00:00Z`, never run), `nightly-report` (`scr_6d1f4a9b2e8c7035`, repository `rep_9c2e4b7a1d3f8e05`, ref `main`, created `2026-09-18T16:40:00Z`, newest run `run_8a2c6e1f9b3d5074`, `running` since `2026-10-05T09:31:40Z`), `rotate-keys` (`scr_5c9b1e3a7f2d4068`, repository `rep_7b3e9a0c5d1f2846`, ref `release`, newest run `run_1e9c3a7f5b0d2864`, `failed`, started `2026-10-04T22:00:00Z`), and `sync-crm` (`scr_a2e7c4f9b1d03856`, repository `rep_41d8f0a6b2c97e13`, ref `main`, newest run `run_6b2d8f4a0c9e1735`, `running` since `2026-10-05T09:31:00Z`), and `u_2b8e1d04`'s `digest` (`scr_3b7f9d1c5e0a2846`). Every story is read-only: nothing changes but the trail, which gains the request's `request.started`, its `tool.called` with `tool` `list` or `show`, `kind` `read`, and the outcome `S05` fixes, and its `request.finished`; neither tool records a `script.*` or `run.*` event, and neither touches a run, so a run still running runs on. scripts writes nothing to stderr for any answer in this group.

## A model lists its scripts

The ordinary case: every script the caller owns, by name, each with its repository, its ref, and how its newest run stands. `backfill` has never run, so its entry has no `last_run`, and its `repo` is given though that repository is gone from repos: `list` reads the catalog, not the repositories. `nightly-report`'s and `sync-crm`'s newest runs are still `running`, so they have no `exit_code`; `rotate-keys`'s newest run `failed`, and has none either. `digest` is `u_2b8e1d04`'s and is not listed. A call may leave `arguments` out or send `{}`; both are the same call.

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
{"scripts":[{"id":"scr_e8f2a6c0d4b19357","name":"backfill","repo":"rep_0f6a2d9e8c4b7153","ref":"main"},{"id":"scr_6d1f4a9b2e8c7035","name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"main","last_run":{"id":"run_8a2c6e1f9b3d5074","status":"running","started":"2026-10-05T09:31:40Z"}},{"id":"scr_5c9b1e3a7f2d4068","name":"rotate-keys","repo":"rep_7b3e9a0c5d1f2846","ref":"release","last_run":{"id":"run_1e9c3a7f5b0d2864","status":"failed","started":"2026-10-04T22:00:00Z"}},{"id":"scr_a2e7c4f9b1d03856","name":"sync-crm","repo":"rep_41d8f0a6b2c97e13","ref":"main","last_run":{"id":"run_6b2d8f4a0c9e1735","status":"running","started":"2026-10-05T09:31:00Z"}}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No git ran, and `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` run on.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No script's id or name, and no run's id, is in them.

## A model lists its scripts when it has none

A caller that owns no script gets an empty list, not an error, even while other users own scripts.

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
{"scripts":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's; the caller is `u_5c0e7a92` (`bo@example.com`), who owns no script. The catalog's five scripts are all other users'.

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

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

## A model shows a script by name

The ordinary case, and how a model learns everything the catalog holds about one script: which repository and ref it runs from, when it was created, and its newest run. `last_run` is that run as the catalog records it at the moment of the call: `run_8a2c6e1f9b3d5074` is still `running`, so it has no `exit_code`. Had it exited, as `run_3f9a1c2e8b7d4a60` before it did, `last_run` would carry the code between `status` and `started`, `{"id":"run_3f9a1c2e8b7d4a60","status":"exited","exit_code":0,"started":"2026-10-05T09:14:02Z"}`. A model follows the run with `result` and sees every run with `runs` (`S11`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"show","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_6d1f4a9b2e8c7035","name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"main","created":"2026-09-18T16:40:00Z","last_run":{"id":"run_8a2c6e1f9b3d5074","status":"running","started":"2026-10-05T09:31:40Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly. `ref` is the ref as the script keeps it, `main`, whatever commit `main` names now: `show` reads the catalog, not the repository.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No git ran, and `run_8a2c6e1f9b3d5074` runs on.
- telemetry has received the request's `request.started`, then `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"show"}` as the attributes of its `tool.called`, then its `request.finished` with `status` 200. Neither the id nor the name of `nightly-report` is in them.

## A model shows a script that has never run

A script nobody has run has no `last_run`; it has everything else. `backfill`'s repository, `rep_0f6a2d9e8c4b7153`, is gone from repos, and `show` neither notices nor says so, since it reads no repository: the script is answered as the catalog keeps it, and a run of it would be recorded `failed` with reason `repository_missing` (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"show","arguments":{"name":"backfill"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_e8f2a6c0d4b19357","name":"backfill","repo":"rep_0f6a2d9e8c4b7153","ref":"main","created":"2026-10-02T12:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `backfill` has never run, and there is no `../repos/state/repos/rep_0f6a2d9e8c4b7153.git`.

Postconditions:

- Nothing has changed. No run was made and no repository was read.

## A model shows another user's script

`show` is owner-only, and another user's script does not exist for the caller: asking for it gets exactly the answer a script that does not exist gets. That `digest` is taken the caller can learn only from `create` (`S06`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"show","arguments":{"name":"digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `show`, `kind` `read`, and `outcome` `error`, as for a script that does not exist.

## A model shows a script it does not have

A name among none of the caller's scripts is refused, quoting it as sent. The same answer comes for a name that is not a valid name, since it can name nothing, and for a script's id, which is not its name: `scr_6d1f4a9b2e8c7035` gets `no script named 'scr_6d1f4a9b2e8c7035'`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"show","arguments":{"name":"cleanup"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'cleanup'
```

Preconditions:

- The preamble's: no script is named `cleanup`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"show"}}
  ```

## A model calls show without saying which script

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

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"show","arguments":{"script":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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
script: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `show`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.
