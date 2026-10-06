# Stories — runs, result and cancel

`runs`, `result` and `cancel`, the three tools through which the caller follows its runs after `run` has made them (`S08`). `runs` takes one argument, `name`, required, a string, the name of one of the caller's scripts, looked up among the caller's scripts only, and answers `{"runs":[...]}`, every run the catalog keeps for that script, newest first by `started`, ties broken by id, each entry the shape `S05` fixes — `id`, `sha`, `ref`, `trigger`, `event`, `status`, `exit_code`, `started`, `finished`, `truncated`, `reason` — and `{"runs":[]}` for a script that has never run; a `name` that names none of the caller's scripts is refused with exactly `no script named '<name>'`. `result` and `cancel` each take one argument, `run`, required, a string, a run's id, looked up among the runs of the caller's own scripts only; an id that names none of them, another user's run included, is refused with exactly `no run '<id>'`, quoting the value as sent, and that lookup comes before anything else. A run's `trigger` is `manual` for a run `run` started (`S08`) and `event` for one an event started (`S27`), and only the latter has `event`, the id of the event that started it, right after `trigger`, in every shape this group answers. `result` answers the run object `S05` fixes, then `stdout`, `stderr` and `files`, or `files_gone` when the run's folder is no longer there; it reads the catalog and the run's folder and nothing else. `runs` reads the catalog only. Neither runs git, writes anything or touches a process, and both are of kind `read`. A run is `queued` while it waits for a slot to run in (`S08`): `runs` lists it, `result` answers it as not yet finished, as it answers a running run, and `cancel` ends it. `cancel` ends a run that is still `running` or `queued`: a running run's process group it kills whole, as the drain deadline does (`S18`), and a queued run it takes out of the queue, so it never starts; either way it records the run `killed` with its `finished`, keeps everything the run wrote, and answers the run in the shape `runs` lists it; a run whose status is already final, whatever it is, is refused with exactly `run '<id>' has already ended`. `cancel` is of kind `destructive`. What makes a run's output truncated, and how long a run may take, is `S17`'s; a run ended by stopping is `S18`'s; how ended runs are pruned is `S19`'s; what a run sees while it runs is `S15`'s.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: `S06`'s shared catalog, with now `2026-10-05T09:32:00Z`. The caller `u_7f3a9c21` (`mg@example.com`) owns `nightly-report` (`scr_6d1f4a9b2e8c7035`), whose seven runs, newest first, are `run_8a2c6e1f9b3d5074`, `running` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` since `2026-10-05T09:31:40Z`; `run_3f9a1c2e8b7d4a60`, `exited` 0; `run_c71d0b5e4a2f9386`, `exited` 1; `run_5e8b3d7a1c0f6294`, `timed_out` with its output truncated; `run_19f6a4d2c8e3b705`, `killed`, from the tag `v1`; `run_d4a7e2c9f1b8630a`, `failed` with `commit_missing` on the ref `release`; and `run_72b0c8f5e3d1a946`, `exited` 0, whose folder is gone. The caller also owns `sync-crm`, whose run `run_6b2d8f4a0c9e1735` is `running`, `rotate-keys`, and `backfill` (`scr_e8f2a6c0d4b19357`), which has never run; `u_2b8e1d04` (`ann@example.com`) owns `digest`, whose one run is `run_0c4e8a2f6b1d9375`, `exited` 0. Every run's trigger is `manual`, and its user is its script's owner, unless a story says otherwise; no run is queued unless a story says otherwise. Each run but `run_72b0c8f5e3d1a946` has its folder at `state/runs/<script id>/<run id>/`. `runs` and `result` change nothing, and their trail is the request's `request.started`, its `tool.called` with `tool` `runs` or `result`, `kind` `read`, and the outcome `S05` fixes, and its `request.finished`; neither records a `run.*` event. A `cancel` that ends a run records that run's `run.finished`, under the run's own request id and user, before its `tool.called`; a refused `cancel` kills nothing and records no `run.*` event. scripts writes nothing to stderr for any answer in this group.

## A model lists a script's runs

The ordinary case: every run of `nightly-report` the catalog keeps, newest first, enough to tell which are still running, which ended and how, and which commit each ran. Each entry follows the rules `S05` fixes: `exit_code` only for an `exited` run, `finished` absent while the run is running or queued, `sha` absent where the ref never resolved, and `reason` only for a `failed` run. `run_72b0c8f5e3d1a946`'s folder is gone, but its record is kept and listed like any other; `runs` does not look at folders.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"runs":[{"id":"run_8a2c6e1f9b3d5074","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"manual","status":"running","started":"2026-10-05T09:31:40Z","truncated":false},{"id":"run_3f9a1c2e8b7d4a60","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"manual","status":"exited","exit_code":0,"started":"2026-10-05T09:14:02Z","finished":"2026-10-05T09:14:14Z","truncated":false},{"id":"run_c71d0b5e4a2f9386","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"manual","status":"exited","exit_code":1,"started":"2026-10-04T09:14:02Z","finished":"2026-10-04T09:14:11Z","truncated":false},{"id":"run_5e8b3d7a1c0f6294","sha":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37","ref":"main","trigger":"manual","status":"timed_out","started":"2026-10-03T09:14:02Z","finished":"2026-10-03T09:24:02Z","truncated":true},{"id":"run_19f6a4d2c8e3b705","sha":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37","ref":"v1","trigger":"manual","status":"killed","started":"2026-10-02T09:14:02Z","finished":"2026-10-02T09:17:43Z","truncated":false},{"id":"run_d4a7e2c9f1b8630a","ref":"release","trigger":"manual","status":"failed","started":"2026-10-01T09:14:02Z","finished":"2026-10-01T09:14:02Z","truncated":false,"reason":"commit_missing"},{"id":"run_72b0c8f5e3d1a946","sha":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60","ref":"main","trigger":"manual","status":"exited","exit_code":0,"started":"2026-09-30T09:14:02Z","finished":"2026-09-30T09:14:13Z","truncated":false}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly. `sync-crm`'s and `digest`'s runs are not in it.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No git ran, and no run folder was read.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"runs"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No script's or run's id or name is in them.

## A model lists a script's runs, one of them started by an event

A script subscribed to an event (`S26`) runs whenever the events app delivers it (`S27`), so its runs mix those a model asked for and those events started. `runs` lists them together, newest first as always, and tells them apart: a run an event started has `trigger` `event` and `event`, the id of the event, which a run `run` started has not. Here `nightly-report` is subscribed to `repo.pushed`, and a push event started its newest run, which has already exited.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 18 whose `result` has no `isError` member, a `structuredContent` of `{"runs":[...]}` whose first entry is

```
{"id":"run_e2b6d0a4c8f17359","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"event","event":"evt_3a7d9c1e5b2f8064","status":"exited","exit_code":0,"started":"2026-10-05T09:31:52Z","finished":"2026-10-05T09:31:57Z","truncated":false}
```

followed by the seven entries of `A model lists a script's runs`, as that story shows them, each with `trigger` `manual` and no `event`; and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's, and `nightly-report` is subscribed to `repo.pushed`. The events app delivered the event `evt_3a7d9c1e5b2f8064`, a `repo.pushed`, at `2026-10-05T09:31:52Z`, which started the run `run_e2b6d0a4c8f17359` of `nightly-report` as `u_7f3a9c21`, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`; it exited 0 at `2026-10-05T09:31:57Z`.

Postconditions:

- Nothing has changed. No git ran, and no run folder was read. The request's `tool.called` has `tool` `runs`, `kind` `read`, and `outcome` `ok`.

## A model lists a script's runs while one waits for a slot

A run that waits for a slot is listed with the rest, newest first by `started`, the time of the `run` call that made it, with `status` `queued`; like a running run's, its entry has no `finished` and no `exit_code`. It has its `sha`, since its ref resolved before it was queued. Here `nightly-report`'s newest run is queued behind the two running runs.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 19 whose `result` has no `isError` member, a `structuredContent` of `{"runs":[...]}` whose first entry is

```
{"id":"run_c4a8e2f6b0d93157","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"manual","status":"queued","started":"2026-10-05T09:31:50Z","truncated":false}
```

followed by the seven entries of `A model lists a script's runs`, as that story shows them; and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's, except that scripts runs with `RUN_MAX_ACTIVE` `2`, so `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735`, both running, take both slots, and `run_c4a8e2f6b0d93157` of `nightly-report`, asked for by `u_7f3a9c21` in the request `5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5` at `2026-10-05T09:31:50Z`, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, was answered `queued` (`S08`) and is the only run queued. Its folder `state/runs/scr_6d1f4a9b2e8c7035/run_c4a8e2f6b0d93157/` holds its `input.json`, `tree/` and `out/`, empty, and no `stdout` or `stderr`; no process of it has run.

Postconditions:

- Nothing has changed: `run_c4a8e2f6b0d93157` is still queued, and listing it neither started it nor moved it in the queue. No git ran, and no run folder was read. The request's `tool.called` has `tool` `runs`, `kind` `read`, and `outcome` `ok`.

## A model lists the runs of a script that has never run

A script nobody has run has no runs, which is an empty list, not an error. `backfill`'s repository is gone from repos, but `runs` reads the catalog only and never looks for it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"runs","arguments":{"name":"backfill"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"runs":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's: `backfill` has never run, and there is no `rep_0f6a2d9e8c4b7153.git` under `../repos/state/repos`.

Postconditions:

- Nothing has changed. No git ran.

## A model lists the runs of another user's script

`runs` is owner-only, and another user's script does not exist for the caller: asking for its runs gets exactly the answer a script that does not exist gets, and says nothing of whether it has run.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"runs","arguments":{"name":"digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s, and its run `run_0c4e8a2f6b1d9375` has exited.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `runs`, `kind` `read`, and `outcome` `error`, as for a script that does not exist.

## A model lists the runs of a script it does not have

A name among none of the caller's scripts is refused, quoting it as sent. The same answer comes for a name that is not a valid name, since it can name nothing, and for a script's id, which is not its name: `scr_6d1f4a9b2e8c7035` gets `no script named 'scr_6d1f4a9b2e8c7035'`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"runs","arguments":{"name":"weekly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'weekly-report'
```

Preconditions:

- The preamble's: no script is named `weekly-report`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"runs"}}
  ```

## A model calls runs without saying which script

`name` is required. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"runs","arguments":{"script":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: missing required field
script: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `runs`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.

## A model reads a finished run's result

The ordinary case, and how a model learns everything one run did: which commit it ran, as whom and for which request, how it ended, what it printed, and what it left under its `out/`. `run_3f9a1c2e8b7d4a60` ran `nightly-report` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` for twelve seconds and exited 0, printing 1229 bytes to standard output and nothing to standard error, and wrote three files under `out/`: `charts/sales.svg`, 12034 bytes, `report.html`, 48211 bytes, and `report.csv`, 7904 bytes. `stdout` and `stderr` are the whole of what the run's `stdout` and `stderr` files hold, and `files` lists every regular file under `out/`, at any depth, as its path relative to `out/` with `/` between its parts and its size in bytes, sorted by path. The run's input is not in the result; the run's page shows it (`S13`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_3f9a1c2e8b7d4a60"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_3f9a1c2e8b7d4a60","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","user":"u_7f3a9c21","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","trigger":"manual","status":"exited","exit_code":0,"started":"2026-10-05T09:14:02Z","finished":"2026-10-05T09:14:14Z","stdout_bytes":1229,"stderr_bytes":0,"truncated":false,"stdout":"<stdout>","stderr":"","files":[{"path":"charts/sales.svg","size":12034},{"path":"report.csv","size":7904},{"path":"report.html","size":48211}]}
```

where `<stdout>` stands for the whole 1229 bytes of the run's `stdout` file as one JSON string, beginning `reading events since 2026-10-04 from telemetry\n` and ending `done in 11.6s\n`; and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's: `state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60/` holds a `stdout` of 1229 bytes, an empty `stderr`, and under `out/` the files `charts/sales.svg`, `report.html` and `report.csv` and nothing else.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received the request's `request.started`, then `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}` as the attributes of its `tool.called`, then its `request.finished` with `status` 200. No run's id and no part of its output is in them.

## A model reads the result of a run still running

`run` never waits for the script (`S08`), so a model follows a run by calling `result` until its status is final. While the run is `running`, `result` answers what exists at that moment: the run's record, without `finished` and without `exit_code`, the output written so far, and the files under `out/` so far. Each call reads afresh, so a later call may show more. `run_8a2c6e1f9b3d5074` has been running for twenty seconds, has printed the 214 bytes shown below to standard output, nothing to standard error, and has written nothing under `out/` yet.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_8a2c6e1f9b3d5074"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_8a2c6e1f9b3d5074","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","user":"u_7f3a9c21","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","trigger":"manual","status":"running","started":"2026-10-05T09:31:40Z","stdout_bytes":214,"stderr_bytes":0,"truncated":false,"stdout":"reading events since 2026-10-04 from telemetry\n  sales:    318 events\n  support:  131 events\n  billing:  64 events\nrendering report\nfetching chart data for sales\nfetching chart data for support\ndrawing sales chart\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's: `run_8a2c6e1f9b3d5074` was asked for by `u_7f3a9c21` in the request `9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2` and its process is still alive; its folder's `stdout` holds the 214 bytes shown, its `stderr` is empty, and its `out/` is empty.

Postconditions:

- Nothing has changed: the run is still running, and `result` neither waited for it nor touched its process.

## A model reads the result of a queued run

A queued run has not started, so `result` answers it as not finished, as it answers a running run: its record with `status` `queued`, without `finished` and without `exit_code`. Its script has written nothing, so `stdout_bytes` and `stderr_bytes` are 0, `stdout` and `stderr` are `""`, and `files` is `[]`; a later call answers it `running` once a slot frees and its process starts (`S08`), and then its output so far.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_c4a8e2f6b0d93157"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 20 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_c4a8e2f6b0d93157","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","user":"u_7f3a9c21","request_id":"5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5","trigger":"manual","status":"queued","started":"2026-10-05T09:31:50Z","stdout_bytes":0,"stderr_bytes":0,"truncated":false,"stdout":"","stderr":"","files":[]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's, except that scripts runs with `RUN_MAX_ACTIVE` `2`, so `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735`, both running, take both slots, and `run_c4a8e2f6b0d93157` of `nightly-report`, asked for by `u_7f3a9c21` in the request `5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5` at `2026-10-05T09:31:50Z`, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, was answered `queued` (`S08`) and is the only run queued. Its folder `state/runs/scr_6d1f4a9b2e8c7035/run_c4a8e2f6b0d93157/` holds its `input.json`, `tree/` and `out/`, empty, and no `stdout` or `stderr`; no process of it has run.

Postconditions:

- Nothing has changed: the run is still queued, and `result` neither started it nor moved it in the queue.

## A model reads the result of a run that could not start

A run that could not start is still a run (`S08`), and `result` answers it like any other: `status` `failed`, its `reason`, and `finished`, the moment the failure was recorded. `run_d4a7e2c9f1b8630a` was asked to run `nightly-report` from `release`, which names no commit in the repository, so its ref never resolved: there is no `sha`, no tree was unpacked, and no process ran. Its folder is there, holding the run's `input.json` and nothing the script wrote, so `stdout` and `stderr` are `""` and `files` is `[]`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_d4a7e2c9f1b8630a"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_d4a7e2c9f1b8630a","script":"scr_6d1f4a9b2e8c7035","ref":"release","user":"u_7f3a9c21","request_id":"c2f5a8d1e4b7093a6d9c2f5e8b1a4d70","trigger":"manual","status":"failed","started":"2026-10-01T09:14:02Z","finished":"2026-10-01T09:14:02Z","stdout_bytes":0,"stderr_bytes":0,"truncated":false,"reason":"commit_missing","stdout":"","stderr":"","files":[]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's: `run_d4a7e2c9f1b8630a` was asked for by `u_7f3a9c21` in the request `c2f5a8d1e4b7093a6d9c2f5e8b1a4d70` and recorded `failed` with `commit_missing`; `rep_9c2e4b7a1d3f8e05` has no `release`. Its folder `state/runs/scr_6d1f4a9b2e8c7035/run_d4a7e2c9f1b8630a/` holds its `input.json`, and no `tree/`.

Postconditions:

- Nothing has changed. No git ran: `result` does not try the ref again.

## A model reads the result of a run whose files are gone

A run's record outlives its folder. `run_72b0c8f5e3d1a946`'s folder is no longer under `state/runs/`, so its output and files cannot be read, but the run happened and its record says how it ended. `result` answers the record as it is, without `stdout`, `stderr` and `files`, and with `files_gone`, `true`, last; a missing folder is a fact about the run, not an error. `stdout_bytes` and `stderr_bytes` are what the record kept when the run ended.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_72b0c8f5e3d1a946"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_72b0c8f5e3d1a946","script":"scr_6d1f4a9b2e8c7035","sha":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60","ref":"main","user":"u_7f3a9c21","request_id":"5d8a1e4b7c0f3926a5d8e1b4c7f0a396","trigger":"manual","status":"exited","exit_code":0,"started":"2026-09-30T09:14:02Z","finished":"2026-09-30T09:14:13Z","stdout_bytes":1204,"stderr_bytes":0,"truncated":false,"files_gone":true}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's: the catalog records `run_72b0c8f5e3d1a946`, asked for by `u_7f3a9c21` in the request `5d8a1e4b7c0f3926a5d8e1b4c7f0a396`, as `exited` 0 with 1204 bytes of standard output and none of standard error kept; `state/runs/scr_6d1f4a9b2e8c7035/run_72b0c8f5e3d1a946/` does not exist.

Postconditions:

- Nothing has changed. The record is kept as it was, and no folder was made for it.
- The request's `tool.called` has `tool` `result`, `kind` `read`, and `outcome` `ok`.

## A model reads the result of another user's run

A run belongs to its script's owner, and another user's run does not exist for the caller: asking for it by its id gets exactly the answer an id that names no run gets, though the caller holds the id.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_0c4e8a2f6b1d9375"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no run 'run_0c4e8a2f6b1d9375'
```

Preconditions:

- The preamble's: `run_0c4e8a2f6b1d9375` is a run of `digest`, `u_2b8e1d04`'s, and its folder is there.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `result`, `kind` `read`, and `outcome` `error`, as for a run that does not exist.

## A model reads the result of a run it does not have

An id that names none of the caller's runs is refused, quoting it as sent. The same answer comes for a value that is not a run's id at all, since it can name nothing: a script's id, `scr_6d1f4a9b2e8c7035`, gets `no run 'scr_6d1f4a9b2e8c7035'`, and a script's name, `nightly-report`, gets `no run 'nightly-report'`. A run pruned from the catalog (`S19`) or deleted with its script (`S10`) is gone and is answered the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_9f0e1d2c3b4a5968"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no run 'run_9f0e1d2c3b4a5968'
```

Preconditions:

- The preamble's: no run has the id `run_9f0e1d2c3b4a5968`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"result"}}
  ```

## A model calls result without saying which run

`run` is required and is a string. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`); a `run` that is not a string is refused the same way, as `run: expected string, got number` for a number.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"result","arguments":{"id":"run_3f9a1c2e8b7d4a60"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
run: missing required field
id: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `result`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.

## A model cancels a running run

A model that started a run it no longer wants, or one that is taking too long, ends it now rather than waiting for `SCRIPT_SECONDS` (`S17`). `cancel` kills the run's process group whole, the script and every process it started, as the drain deadline does (`S18`); records the run `killed`, with `finished` the moment it was recorded; and only then answers, with the run as `runs` lists it. Nothing the run wrote is removed: its `stdout`, `stderr`, `input.json`, `tree/` and `out/` stay in its folder as they were at the kill, and `result` reads them as for any ended run. The run's `run.finished` carries the run's own request id and user, those of the `run` call that started it, not the `cancel`'s. `run_8a2c6e1f9b3d5074` has run for twenty seconds when the call arrives.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: cancel

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"cancel","arguments":{"run":"run_8a2c6e1f9b3d5074"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_8a2c6e1f9b3d5074","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"manual","status":"killed","started":"2026-10-05T09:31:40Z","finished":"2026-10-05T09:32:00Z","truncated":false}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's: `run_8a2c6e1f9b3d5074` was asked for by `u_7f3a9c21` in the request `9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2`, and its process is alive; it is `2026-10-05T09:32:00Z`.

Postconditions:

- The run's process and every process in its group are gone. The catalog records `run_8a2c6e1f9b3d5074` as `killed`, finished `2026-10-05T09:32:00Z`, with no exit code.
- `state/runs/scr_6d1f4a9b2e8c7035/run_8a2c6e1f9b3d5074/` is still there, holding what the run wrote up to the kill. `result` (above) now answers `status` `killed` with `finished` and the output kept; `runs` lists the run `killed`; and a second `cancel` of it is refused with `run 'run_8a2c6e1f9b3d5074' has already ended` (below).
- `sync-crm`'s `run_6b2d8f4a0c9e1735` is still running: `cancel` kills one run's process group and no other.
- telemetry has received the request's four events, in this order, where `<request-id>` is the `cancel` request's id (`S02`) and `<duration>` is the run's whole microseconds from its start to its being recorded `killed`, about 20000000:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"duration_us":<duration>,"run":"run_8a2c6e1f9b3d5074","status":"killed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"cancel"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  There is no event of `cancel`'s own; `run.finished` with `status` `killed` is how the trail shows it.

## A model cancels a queued run

A model that no longer wants a run still waiting for a slot ends it the same way it ends a running one. There is no process to kill: `cancel` takes the run out of the queue, so it never starts, not when the next slot frees nor later; records it `killed`, with `finished` the moment it was recorded and no exit code; and answers with the run as `runs` lists it. Its folder is kept as it was when it was queued, `input.json`, `tree/` and `out/`, empty, with no `stdout` or `stderr`, since its script never ran. Its `run.finished` carries the run's own request id and user, and the trail holds no `run.started` for it, ever. The runs running are not touched, and no slot frees for it: the runs queued after it, if any, move up but start only when a running run ends.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: cancel

{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"cancel","arguments":{"run":"run_c4a8e2f6b0d93157"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 21 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_c4a8e2f6b0d93157","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"manual","status":"killed","started":"2026-10-05T09:31:50Z","finished":"2026-10-05T09:32:00Z","truncated":false}
```

and a `content` array of one text block whose text is exactly that object encoded compactly.

Preconditions:

- The preamble's, except that scripts runs with `RUN_MAX_ACTIVE` `2`, so `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735`, both running, take both slots, and `run_c4a8e2f6b0d93157` of `nightly-report`, asked for by `u_7f3a9c21` in the request `5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5` at `2026-10-05T09:31:50Z`, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, was answered `queued` (`S08`) and is the only run queued. Its folder `state/runs/scr_6d1f4a9b2e8c7035/run_c4a8e2f6b0d93157/` holds its `input.json`, `tree/` and `out/`, empty, and no `stdout` or `stderr`; no process of it has run. It is `2026-10-05T09:32:00Z`.

Postconditions:

- `run_c4a8e2f6b0d93157` is no longer queued and will never start: no process of it has run. The catalog records it as `killed`, finished `2026-10-05T09:32:00Z`, with no exit code.
- `state/runs/scr_6d1f4a9b2e8c7035/run_c4a8e2f6b0d93157/` is still there, as it was: `input.json`, `tree/`, and `out/`, empty, and no `stdout` or `stderr`. `result` now answers it `killed`, with `finished`, `stdout_bytes` and `stderr_bytes` 0, `stdout` and `stderr` `""`, and `files` `[]`; `runs` lists it `killed`; and a second `cancel` of it is refused with `run 'run_c4a8e2f6b0d93157' has already ended`.
- `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` are still running, untouched.
- telemetry has received the request's four events, in this order, where `<request-id>` is the `cancel` request's id (`S02`) and `<duration>` is the run's whole microseconds from its `started` to its being recorded `killed`, about 10000000:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5","user":"u_7f3a9c21","attrs":{"duration_us":<duration>,"run":"run_c4a8e2f6b0d93157","status":"killed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"cancel"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No `run.started` was recorded for the run, before the call or since.

## A model cancels a run that has already ended

`cancel` ends only a run that is still running or queued. A run whose status is final, `exited`, `killed`, `timed_out` or `failed`, has nothing left to kill, and is refused, quoting the id as sent: the model learns the run ended on its own terms, and reads how with `result`. `run_3f9a1c2e8b7d4a60` exited 0 at `2026-10-05T09:14:14Z`. A run that could not start, `run_d4a7e2c9f1b8630a` say, is refused the same way, as is a run that ends on its own while the `cancel` is on its way, which keeps the status it ended with.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: cancel

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"cancel","arguments":{"run":"run_3f9a1c2e8b7d4a60"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
run 'run_3f9a1c2e8b7d4a60' has already ended
```

Preconditions:

- The preamble's: `run_3f9a1c2e8b7d4a60` is `exited` 0.

Postconditions:

- Nothing has changed: the run is `exited` 0, finished `2026-10-05T09:14:14Z`, as it was, and nothing was killed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"cancel"}}
  ```

  No `run.finished` was recorded.

## A model cancels another user's run

Another user's run does not exist for the caller, so `cancel` cannot end it and does not say whether it could: the run is looked up among the caller's runs first, and finding none is the whole answer. `run_0c4e8a2f6b1d9375` has exited, but the caller is told it has no such run, not that the run has ended; a running run of another user's would get the same answer and keep running.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: cancel

{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"cancel","arguments":{"run":"run_0c4e8a2f6b1d9375"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 15 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no run 'run_0c4e8a2f6b1d9375'
```

Preconditions:

- The preamble's: `run_0c4e8a2f6b1d9375` is a run of `digest`, `u_2b8e1d04`'s, and is `exited` 0.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `cancel`, `kind` `destructive`, and `outcome` `error`, as for a run that does not exist. No `run.finished` was recorded.

## A model cancels a run it does not have

An id that names none of the caller's runs is refused, quoting it as sent, and nothing is killed. A value that is not a run's id, a script's id or name say, gets the same answer, since it can name nothing.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: cancel

{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"cancel","arguments":{"run":"run_9f0e1d2c3b4a5968"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 16 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no run 'run_9f0e1d2c3b4a5968'
```

Preconditions:

- The preamble's: no run has the id `run_9f0e1d2c3b4a5968`.

Postconditions:

- Nothing has changed: `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` are still running. The request's `tool.called` has `tool` `cancel`, `kind` `destructive`, and `outcome` `error`.

## A model calls cancel without saying which run

`run` is required and is a string. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`), and nothing is killed. A model that names the run's script instead of the run learns that `cancel` takes a run.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: cancel

{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"cancel","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 17 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
run: missing required field
name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `run_8a2c6e1f9b3d5074` is still running. The request's `tool.called` has `tool` `cancel`, `kind` `destructive`, `outcome` `invalid_arguments`, and `duration_us` 0.
