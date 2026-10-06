# Stories — update

`update`, the tool that changes the ref one of the caller's scripts runs from. It takes, in this order, `name`, a string, the name of one of the caller's scripts, and `ref`, a string, the ref every later run resolves; both are required, and nothing else about a script can be changed: its name, its repository, and its id are fixed at create (`S06`). `ref` is held to the rule `create` holds it to: a string git accepts as a ref name, and it is not resolved, so a ref that names nothing yet is accepted and a run of it fails as `S08` tells. A ref sent with the value the script already has is not a change. The checks run in this order — the script; the ref — and the first that fails is the whole answer, one line naming it. `update` changes what the next `run` without a `ref` resolves (`S08`), never a run already made: a run keeps the sha and ref it resolved, whether it has ended or is still running, and a running run is not signalled. It reads no repository and runs no git. A script's subscriptions (`S26`) are kept across an update, each with the time it was made, and a run an event starts after the call resolves the new ref (`S27`). It answers the script object `show` answers (`S07`), as it is after the call, its `subscriptions` included.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: `S06`'s shared catalog, in which the caller `u_7f3a9c21` owns `nightly-report` (`scr_6d1f4a9b2e8c7035`, repository `rep_9c2e4b7a1d3f8e05`, ref `main`, created `2026-09-18T16:40:00Z`, its newest run `run_8a2c6e1f9b3d5074` still `running` since `2026-10-05T09:31:40Z`), `sync-crm` (`scr_a2e7c4f9b1d03856`, repository `rep_41d8f0a6b2c97e13`, ref `main`, created `2026-09-25T10:00:00Z`, its newest run `run_6b2d8f4a0c9e1735` still `running` since `2026-10-05T09:31:00Z` at `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3`), `rotate-keys` (`scr_5c9b1e3a7f2d4068`, repository `rep_7b3e9a0c5d1f2846`, ref `release`, created `2026-09-28T08:00:00Z`, its one run `run_1e9c3a7f5b0d2864` `failed` with reason `commit_missing`, started `2026-10-04T22:00:00Z`, since that repository has no `release`), and `backfill`, and `u_2b8e1d04` owns `digest` (`scr_3b7f9d1c5e0a2846`, ref `main`); no script is subscribed to any event unless a story says otherwise. `update` is of kind `additive`. An update that changes the ref records one `script.updated`, with `script`, the id, before its `tool.called`; an update to the ref the script already has records nothing. Neither the name nor the ref is in any event. A refusal changes nothing and records no `script.*` event. scripts writes nothing to stderr for any answer in this group.

## A model changes the ref a script runs from

`rotate-keys` runs from `release`, a branch its repository does not have, so its one run failed to start. The model points it at `main`. Nothing runs by the change, and the new ref is not resolved now; the next `run` of `rotate-keys` without a `ref` resolves `main`. The run already made is a record of what happened and is not rewritten: it still says `release`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"update","arguments":{"name":"rotate-keys","ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_5c9b1e3a7f2d4068","name":"rotate-keys","repo":"rep_7b3e9a0c5d1f2846","ref":"main","created":"2026-09-28T08:00:00Z","subscriptions":[],"last_run":{"id":"run_1e9c3a7f5b0d2864","status":"failed","started":"2026-10-04T22:00:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly. `id`, `name`, `repo`, `created`, `subscriptions`, and `last_run` are unchanged.

Preconditions:

- The preamble's: `rotate-keys` runs from `release`.

Postconditions:

- The catalog has `rotate-keys` running from `main`. Its run `run_1e9c3a7f5b0d2864` is as it was, `failed` with reason `commit_missing` and ref `release`, and its folder `state/runs/scr_5c9b1e3a7f2d4068/run_1e9c3a7f5b0d2864/` is untouched.
- No git ran, no repository was read, no run was made, and no script ran.
- The next `run` of `rotate-keys` with no `ref` resolves `main` (`S08`).
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"script.updated","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"script":"scr_5c9b1e3a7f2d4068"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `rotate-keys` and the ref `main` are in none of them.

## A model changes the ref of a script while a run of it is running

A run resolved its ref and unpacked its commit before `run` answered (`S08`), so a change of the script's ref has nothing to say to it. The running run goes on undisturbed with the sha and ref it resolved, and ends as it would have; only runs made after the call use the new ref. The model need not wait for a run to end, or cancel it, before changing the ref.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"update","arguments":{"name":"sync-crm","ref":"next"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_a2e7c4f9b1d03856","name":"sync-crm","repo":"rep_41d8f0a6b2c97e13","ref":"next","created":"2026-09-25T10:00:00Z","subscriptions":[],"last_run":{"id":"run_6b2d8f4a0c9e1735","status":"running","started":"2026-10-05T09:31:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly. `next` names no branch of `rep_41d8f0a6b2c97e13` yet; it is accepted all the same, since it is not resolved until a run.

Preconditions:

- The preamble's: `sync-crm` runs from `main`, and its run `run_6b2d8f4a0c9e1735` is `running`, resolved from `main` at `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3`.

Postconditions:

- The catalog has `sync-crm` running from `next`.
- `run_6b2d8f4a0c9e1735` is still `running`, with sha `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3` and ref `main`: its process was not signalled, its `tree/` is as it was unpacked, and `runs` (`S11`) lists it with ref `main`. When it ends it is recorded as any run is (`S11`, `S15`).
- No git ran and no repository was read.
- The next `run` of `sync-crm` with no `ref` resolves `next` (`S08`).
- telemetry has received `script.updated` with attributes `{"script":"scr_a2e7c4f9b1d03856"}`, and no `run.*` event.

## A model updates a script to the ref it already has

The ref sent is the one the script already runs from, so there is nothing to change. That is not a refusal: a model retrying an update that already happened gets the answer it would have got the first time, and nothing is recorded but the call.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"update","arguments":{"name":"nightly-report","ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_6d1f4a9b2e8c7035","name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"main","created":"2026-09-18T16:40:00Z","subscriptions":[],"last_run":{"id":"run_8a2c6e1f9b3d5074","status":"running","started":"2026-10-05T09:31:40Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `nightly-report` runs from `main`.

Postconditions:

- Nothing has changed. `run_8a2c6e1f9b3d5074` runs on.
- scripts recorded no `script.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  ```

## A model changes the ref of a script subscribed to an event

A subscription names a script, not a ref, so changing the ref neither removes nor remakes it: `rotate-keys` stays subscribed to `repo.pushed`, with the time the subscription was made, and the next run an event of that name starts resolves the new ref, as the next `run` without a `ref` does. The model need not unsubscribe and subscribe again.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"update","arguments":{"name":"rotate-keys","ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_5c9b1e3a7f2d4068","name":"rotate-keys","repo":"rep_7b3e9a0c5d1f2846","ref":"main","created":"2026-09-28T08:00:00Z","subscriptions":[{"event":"repo.pushed","created":"2026-10-04T10:15:00Z"}],"last_run":{"id":"run_1e9c3a7f5b0d2864","status":"failed","started":"2026-10-04T22:00:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `rotate-keys` runs from `release`, and it is subscribed to `repo.pushed`, created `2026-10-04T10:15:00Z` (`S26`).

Postconditions:

- The catalog has `rotate-keys` running from `main`, still subscribed to `repo.pushed`, created `2026-10-04T10:15:00Z`.
- An event named `repo.pushed` delivered to scripts after the call starts a run of `rotate-keys` that resolves `main` (`S27`).
- No git ran, no repository was read, no run was made, and no script ran.
- telemetry has received `script.updated` with attributes `{"script":"scr_5c9b1e3a7f2d4068"}` before the request's `tool.called`, whose `outcome` is `ok`; the event name is in no event.

## A model updates a script with a ref git would not accept

A ref is held to the rule `create` holds it to: a string git accepts as a ref name. `..bad` is not one, and is refused quoting it as sent. A ref git would accept but that names nothing is not refused; a run of it fails (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"update","arguments":{"name":"nightly-report","ref":"..bad"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid ref '..bad'
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `nightly-report` still runs from `main`.
- scripts recorded no `script.updated`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model calls update without a ref

`update` exists to change the ref, so `ref` is required: a call naming only the script asks for nothing and is refused as its arguments are read, before the script is looked up (`S05`), so the model is not left believing it changed something.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"update","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
ref: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- scripts recorded no `script.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"update"}}
  ```

## A model updates another user's script

Only a script's owner updates it. Another user's script does not exist for the caller, and gets exactly the answer a script that does not exist gets.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"update","arguments":{"name":"digest","ref":"v2"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s and runs from `main`.

Postconditions:

- Nothing has changed: `digest` still runs from `main`.
- scripts recorded no `script.updated`; the request's `tool.called` has `kind` `additive` and `outcome` `error`, as for a script that does not exist.

## A model updates a script it does not have

A name among none of the caller's scripts is refused, quoting it as sent; the script is looked up before the ref is looked at, so the same answer comes whatever ref the call sent, `..bad` included.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"update","arguments":{"name":"cleanup","ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'cleanup'
```

Preconditions:

- The preamble's: no script is named `cleanup`.

Postconditions:

- Nothing has changed.
- scripts recorded no `script.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"update"}}
  ```

## A model sends arguments update does not take

A field of the wrong JSON type, or one the tool does not have, is refused as the arguments are read, every offence in one answer: the tool's fields first, in the order of its input schema, then each unknown field in the order it was sent (`S05`). Here the model sent `ref` as a number, tried to move the script to another repository, and tried to rename it, none of which `update` does: a script's repository and name are fixed at create, and a script on another repository is another script.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"update","arguments":{"name":"nightly-report","ref":2,"repo":"rep_41d8f0a6b2c97e13","new_name":"daily-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
ref: expected string, got number
repo: unknown field
new_name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `nightly-report` is still named `nightly-report`, on `rep_9c2e4b7a1d3f8e05`, running from `main`.
- scripts recorded no `script.updated`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"update"}}
  ```
