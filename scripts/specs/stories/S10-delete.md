# Stories — delete

`delete`, the tool that removes one of the caller's scripts and every run it has, for good. Its one argument is `name`, required, a string, the name of one of the caller's scripts. A run of the script still `running` is killed first, as `cancel` kills one (`S11`): its process group is killed whole and the run ends `killed`. A run of it still `queued` (`S08`) is taken out of the queue first, as `cancel` takes one (`S11`), so it never starts, and it ends `killed` too. Then the script's catalog record goes, with every one of its runs' records, whatever their status, every one of its subscriptions (`S26`), and everything scripts holds for it under `state/runs/<script id>/`, each run's folder with its `tree/`, `out/`, `input.json`, `stdout`, and `stderr`; nothing of the script is orphaned, and an event matching a pattern it was subscribed to, delivered after the call, starts nothing for it (`S27`). It answers `{"deleted":true,"id":"<script id>"}`. The script's repository is not scripts' and is untouched: its commits, and every other script that runs from it, are as they were, and a new script can be created from it at once; delete reads no repository and runs no git, so a script whose repository is gone is deleted the same. There is no undo; once the call has answered, the script's page and its runs' pages answer not-found (`S12`, `S13`, `S14`), `result` and `cancel` with any of its runs' ids are refused with `no run '<id>'` (`S11`), its name is free for any user's `create` (`S06`), and its id and its runs' ids name nothing. `delete` is of kind `destructive`.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: `S06`'s shared catalog, in which the caller `u_7f3a9c21` owns `nightly-report` (`scr_6d1f4a9b2e8c7035`, repository `rep_9c2e4b7a1d3f8e05`, seven runs, `run_8a2c6e1f9b3d5074` of them still `running` since `2026-10-05T09:31:40Z`), `sync-crm` (`scr_a2e7c4f9b1d03856`, its run `run_6b2d8f4a0c9e1735` still `running`), `rotate-keys` (`scr_5c9b1e3a7f2d4068`, repository `rep_7b3e9a0c5d1f2846`, its one run `run_1e9c3a7f5b0d2864` `failed`), and `backfill` (`scr_e8f2a6c0d4b19357`, repository `rep_0f6a2d9e8c4b7153`, gone from repos, never run), and `u_2b8e1d04` owns `digest` (`scr_3b7f9d1c5e0a2846`, its one run `run_0c4e8a2f6b1d9375`); no script is subscribed to any event unless a story says otherwise; every run's folder is at `state/runs/<script id>/<run id>/` but `run_72b0c8f5e3d1a946`'s, already gone; no run is queued unless a story says otherwise. A delete that removes a script records, for each of its runs it killed, running or queued, that run's `run.finished`, with `run`, `status` `killed`, `duration_us`, and `truncated`, under the run's own user and request id (`S02`); then `script.deleted`, with `script`, the id; both before its `tool.called`. The name is in no event. A refusal removes and kills nothing and records no `script.*` or `run.*` event. scripts writes nothing to stderr for any answer in this group.

## A model deletes a script

The ordinary case: a script with no run running. `rotate-keys` has one run, which failed to start; the run's record and its folder go with the script.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete","arguments":{"name":"rotate-keys"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"scr_5c9b1e3a7f2d4068"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- The catalog no longer holds `scr_5c9b1e3a7f2d4068` or its run `run_1e9c3a7f5b0d2864`. `list` (`S07`) answers `backfill`, `nightly-report`, and `sync-crm`; `show`, `update`, `delete`, `subscribe`, `unsubscribe`, `run`, and `runs` with `rotate-keys` are refused with `no script named 'rotate-keys'`; `result` and `cancel` with `run_1e9c3a7f5b0d2864` are refused with `no run 'run_1e9c3a7f5b0d2864'`.
- `state/runs/scr_5c9b1e3a7f2d4068/` no longer exists. The other scripts' directories under `state/runs/` are untouched, and their running runs run on.
- `../repos/state/repos/rep_7b3e9a0c5d1f2846.git` is untouched: scripts ran no git and wrote nothing there.
- `GET /rotate-keys/` and `GET /rotate-keys/runs/run_1e9c3a7f5b0d2864/` are answered 404 with scripts' not-found page, as for any name none of the caller's scripts has (`S12`, `S13`).
- The name `rotate-keys` is free: any user may create a script named `rotate-keys` (`S06`), which gets a new id and no runs.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"script.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"script":"scr_5c9b1e3a7f2d4068"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `rotate-keys` is in none of them. No `run.finished` is recorded: the run had ended long before.

## A model deletes a script while one of its runs is running

A model need not cancel a script's runs before deleting it. The running run is killed as `cancel` kills it, its process group whole, so nothing the script started outlives it, and its end is recorded in the trail as `killed` before the script goes. Unlike a cancel, nothing of the run is kept: its record and its folder, with whatever it wrote so far, go with the script's other runs. A run of another script is not touched.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"scr_6d1f4a9b2e8c7035"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's: `nightly-report` has seven runs, `run_8a2c6e1f9b3d5074` still `running`, its process alive and 214 bytes on its stdout so far, and the six others ended; six of them have a folder under `state/runs/scr_6d1f4a9b2e8c7035/`, `run_72b0c8f5e3d1a946`'s being already gone.

Postconditions:

- `run_8a2c6e1f9b3d5074`'s process group was killed before the call answered: no process of it is left.
- The catalog no longer holds `scr_6d1f4a9b2e8c7035` or any of its seven runs. `runs` with `nightly-report` is refused with `no script named 'nightly-report'`, and `result` and `cancel` with `run_8a2c6e1f9b3d5074`, or any other of its runs' ids, with `no run '<id>'`.
- `state/runs/scr_6d1f4a9b2e8c7035/` no longer exists, the running run's folder with it.
- `sync-crm`'s run `run_6b2d8f4a0c9e1735` is still `running`, and `state/runs/scr_a2e7c4f9b1d03856/` is untouched.
- `../repos/state/repos/rep_9c2e4b7a1d3f8e05.git` is untouched.
- `GET /nightly-report/` and `GET /nightly-report/runs/run_8a2c6e1f9b3d5074/` are answered 404 with scripts' not-found page (`S12`, `S13`). The name `nightly-report` is free.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received three events, in this order. The first is the killed run's `run.finished`, under the user and request id the run carries, those of the `run` call that started it, `9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2` (`S11`); `<request-id>` is the request's id (`S02`), and `duration_us` is the run's from its start to the kill:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_8a2c6e1f9b3d5074","status":"killed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"script.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"script":"scr_6d1f4a9b2e8c7035"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  ```

  No `run.finished` is recorded for the six runs that had already ended.

## A model deletes a script while one of its runs is queued

A run still waiting for a slot goes with its script as a running one does. It is taken out of the queue, so it never starts, neither in the slot the delete frees by killing the script's running run nor later; its end is recorded in the trail as `killed`, as `cancel` records it, before the script goes; and its record and its folder go with the script's other runs. It never ran, so the trail has its `run.finished` and no `run.started`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"delete","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"scr_6d1f4a9b2e8c7035"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's, except that scripts runs with `RUN_MAX_ACTIVE` `2`: `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` are running and take both slots, and `nightly-report` has an eighth run, `run_c4a8e2f6b0d93157`, asked for by `u_7f3a9c21` in the request `5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5` at `2026-10-05T09:31:50Z`, answered `queued` (`S08`), and the only run queued. Its folder `state/runs/scr_6d1f4a9b2e8c7035/run_c4a8e2f6b0d93157/` holds its `input.json`, `tree/` and `out/`, empty.

Postconditions:

- `run_8a2c6e1f9b3d5074`'s process group was killed before the call answered: no process of it is left. `run_c4a8e2f6b0d93157` never started: no process of it ran, before the call or since.
- The catalog no longer holds `scr_6d1f4a9b2e8c7035` or any of its eight runs; nothing is queued. `result` and `cancel` with `run_c4a8e2f6b0d93157` are refused with `no run 'run_c4a8e2f6b0d93157'`.
- `state/runs/scr_6d1f4a9b2e8c7035/` no longer exists, the queued run's folder with it.
- `sync-crm`'s run `run_6b2d8f4a0c9e1735` is still `running`, and `state/runs/scr_a2e7c4f9b1d03856/` is untouched. No run was started.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received four events. The first two are the two killed runs' `run.finished`, each under the user and request id the run carries, those of the `run` call that made it, in an order that is not fixed; `duration_us` is each run's from its `started` to its being recorded `killed`, so the queued run's counts the time it waited. Then `script.deleted` and `tool.called`, in that order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_8a2c6e1f9b3d5074","status":"killed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_c4a8e2f6b0d93157","status":"killed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"script.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"script":"scr_6d1f4a9b2e8c7035"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  ```

  No `run.started` is recorded for `run_c4a8e2f6b0d93157`, and no `run.finished` for the six runs that had already ended.

## A model deletes a script that has never run

`backfill` has never run, so there is no run to kill and nothing under `state/runs/` to remove, and its repository is gone from repos. Neither matters: delete reads no repository, and removes the record all the same.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete","arguments":{"name":"backfill"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"scr_e8f2a6c0d4b19357"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's: `backfill` has no runs and there is no `state/runs/scr_e8f2a6c0d4b19357/`; there is no `../repos/state/repos/rep_0f6a2d9e8c4b7153.git`.

Postconditions:

- The catalog no longer holds `scr_e8f2a6c0d4b19357`. Nothing under `state/runs/` changed, no git ran, and no repository was read.
- The name `backfill` is free.
- telemetry has received `script.deleted` with attributes `{"script":"scr_e8f2a6c0d4b19357"}` before the request's `tool.called`, whose `outcome` is `ok`, and no `run.*` event.

## A model deletes a script subscribed to an event

A script's subscriptions are its own and go with it: nothing of them is left to start a run of a script that is gone, and a new script that later takes the name starts with none. Removing them records nothing of its own; the call's trail is that of any delete.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete","arguments":{"name":"rotate-keys"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"scr_5c9b1e3a7f2d4068"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's, and `rotate-keys` is subscribed to `repo.pushed`, created `2026-10-04T10:15:00Z` (`S26`). No other script is subscribed to it.

Postconditions:

- The catalog no longer holds `scr_5c9b1e3a7f2d4068`, its run `run_1e9c3a7f5b0d2864`, or its subscription to `repo.pushed`; `state/runs/scr_5c9b1e3a7f2d4068/` no longer exists.
- An event named `repo.pushed` delivered to scripts after the call starts no run at all, since no script is subscribed to it any longer (`S27`).
- A script created later with the name `rotate-keys` answers `"subscriptions":[]` (`S06`); it inherits nothing of the deleted one.
- `subscribe` and `unsubscribe` with `rotate-keys` are refused with `no script named 'rotate-keys'` (`S26`).
- telemetry has received `script.deleted` with attributes `{"script":"scr_5c9b1e3a7f2d4068"}` before the request's `tool.called`, whose `kind` is `destructive` and `outcome` `ok`, and no `run.*` event; the event name is in no event.

## A model deletes another user's script

Only a script's owner deletes it. Another user's script does not exist for the caller, and gets exactly the answer a script that does not exist gets; it and its runs are untouched.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"delete","arguments":{"name":"digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s.

Postconditions:

- Nothing was removed: `digest` is in the catalog with its run `run_0c4e8a2f6b1d9375`, whose folder `state/runs/scr_3b7f9d1c5e0a2846/run_0c4e8a2f6b1d9375/` is as it was.
- scripts recorded no `script.deleted`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"delete"}}
  ```

## A model deletes a script it does not have

A name among none of the caller's scripts is refused, quoting it as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"delete","arguments":{"name":"cleanup"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'cleanup'
```

Preconditions:

- The preamble's: no script is named `cleanup`.

Postconditions:

- Nothing was removed and no run was killed.
- scripts recorded no `script.deleted`; the request's `tool.called` has `kind` `destructive` and `outcome` `error`.

## A model deletes a script that is already gone

A second delete of the same script finds nothing, and is refused as for any script the caller does not have: delete is not quietly repeated, so a model learns the first call did what it asked rather than that something else did.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"delete","arguments":{"name":"rotate-keys"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'rotate-keys'
```

Preconditions:

- The preamble's, after `A model deletes a script`.

Postconditions:

- Nothing has changed. scripts recorded no second `script.deleted`; the request's `tool.called` has `outcome` `error`.

## A model calls delete without saying which script

`name` is required and is the only argument. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`). Here the model named the script by its id under a key the tool does not have.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"delete","arguments":{"id":"scr_5c9b1e3a7f2d4068"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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
id: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing was removed: `rotate-keys` is in the catalog with its run and its folder.
- scripts recorded no `script.deleted`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"destructive","outcome":"invalid_arguments","tool":"delete"}}
  ```
