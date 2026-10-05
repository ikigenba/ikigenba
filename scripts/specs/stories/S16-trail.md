# Stories — trail

What scripts' trail holds, and what it never holds. scripts records what it does as events it sends to the platform's telemetry service, as sites does; how it finds telemetry, and what it does with an event telemetry cannot take, is told in `S02`. An operator, or an agent working for one, follows what happened from one thing they know — a request id, a user, a script's id, a run's id, or a time. The stories show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"scripts","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when scripts recorded the event, in UTC to the microsecond, as `2026-10-05T09:14:02.123456Z`; `service` is always `scripts`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`. Both are empty for a start or a stop, which no request caused. A run's events are the exception: a run is caused by the `run` call that asked for it, and it keeps that call's request id and user for as long as it lives, so its `run.started` and its `run.finished` both carry them, whichever request or moment ends it — a `cancel` made under another request id, a `delete` of its script, the script ending on its own, the time limit, the drain deadline, or the next start finding it still recorded `running`. A run's events are never empty in either. `attrs` holds the event's attributes, flat, their keys in alphabetical order. Attributes name what happened and the ids of what it touched, never data: no attribute carries a script's name, a repository's name, a ref, a run's input, a script's output, a file's name or its content, a caller's email, a request's query, a tool's arguments, or the text of a refusal. A script is named by its id under the key `script`, and a run by its id under `run`, so the trail of a script survives anything done to it. The one place a script's name, or the name of a file a run wrote, reaches the trail is a `path`, `/nightly-report/` or `/nightly-report/runs/run_3f9a1c2e8b7d4a60/out/report.csv` say, the URL path as it arrived, without its query, in `request.started` as on every app.

scripts records these events and no others:

- `service.started`, once scripts is serving, with `version`; and `service.stopping`, its last event, with `reason`, `SIGTERM` or `SIGINT` (`S02`);
- `request.started`, as each request arrives, with `method` and `path`; and `request.finished`, once its answer is complete, with `status`, `duration_us`, `request_bytes`, and `response_bytes`, shown as `<n>` and `<bytes>` unless a story fixes them (`S02`);
- `tool.called`, for each call of one of its nine tools answered with a result, with `tool`, `kind`, `outcome`, and `duration_us` (`S05`);
- `script.created`, `script.updated`, and `script.deleted`, one for each `create`, `update`, and `delete` that changes the catalog, each with `script` alone (`S06`, `S09`, `S10`);
- `run.started`, when a run's process starts, with `run`; `script`, its script's id; `sha`, the full commit it resolved; and `trigger`, `manual` (`S08`);
- `run.finished`, when a run reaches a final status, with `duration_us`, the whole microseconds from its start to its end, shown as `<n>` unless a story fixes it; `exit_code`, a number, only when its `status` is `exited`; `reason`, only when its `status` is `failed`: `repository_missing`, `commit_missing`, `too_large`, `git_failed`, `timed_out`, or `start_failed` (`S08`); `run`; `status`, `exited`, `killed`, `timed_out`, or `failed`; and `truncated`, a JSON boolean, `true` when either of its streams was cut at `OUTPUT_MAX_BYTES` (`S17`). It has no `script`: the run's id leads to it through the run's `run.started` or `result`.

A request's events carry its request id and its caller and come in this order: `request.started`, then its `script.*` and `run.*` events, then its `tool.called` if it called a tool, then `request.finished`. Every run records exactly one `run.finished`. A run that starts records `run.started` in its `run` call's request, before the call's `tool.called`, and its `run.finished` when it ends, always after its `run.started`; where that falls among the `run` call's own later events is not fixed, since a script can end before its call is answered. A run that could not start records only its `run.finished`, with `status` `failed`, in its `run` call's request, before the call's `tool.called` (`S08`). A `cancel` records the run's `run.finished`, with `status` `killed`, in its own request, before its `tool.called`, under the run's request id and user, not the cancel's (`S11`). A `delete` of a script with a run still `running` records that run's `run.finished`, with `status` `killed`, the same way, in its own request before its `script.deleted` (`S10`). A run ended at the drain deadline records its `run.finished` as scripts stops (`S18`), and a run the next start finds still recorded `running` records its `run.finished`, `killed`, before that start's `service.started` (`S18`). A tool call that is refused records no `script.*` or `run.*` event. A request that touches no script — a page, a download of a run's file, `/_appkit/`, a tool that only reads — records only its `request.started`, its `tool.called` if it called a tool, and its `request.finished`. scripts never records a run's removal: a run pruned (`S19`) or deleted with its script (`S10`) leaves no event for being removed, beyond the `run.finished` a run still `running` records when its script's `delete` kills it. Nor does it record what a running script does: a call a script makes to a sibling is recorded by that sibling, under the run's request id and user (`S15`), and scripts records nothing for it. Unless a story says otherwise, scripts runs on the host with the suite's services file (`S05`), whose `scripts` entry has the `url` `https://scripts.sbx.ikigenba.dev`; telemetry takes every event; the time is `2026-10-05T09:32:00Z`; and the catalog holds `S06`'s shared catalog: the caller `u_7f3a9c21` (`mg@example.com`) owns `nightly-report`, `scr_6d1f4a9b2e8c7035`, over `rep_9c2e4b7a1d3f8e05` at `main`, which is `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, with the tag `v1` at `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`, and whose newest run `run_8a2c6e1f9b3d5074` has been `running` since `2026-10-05T09:31:40Z`; `sync-crm`, `scr_a2e7c4f9b1d03856`, with its run `run_6b2d8f4a0c9e1735` `running`; `rotate-keys`, `scr_5c9b1e3a7f2d4068`, over `rep_7b3e9a0c5d1f2846` at `release`, a branch that repository does not have, with one run, `run_1e9c3a7f5b0d2864`, `failed` with reason `commit_missing`; and `backfill`, never run; `u_2b8e1d04` (`ann@example.com`) owns `digest`, `scr_3b7f9d1c5e0a2846`. Requests come through nginx or the gateway as `S05` shows them, with `X-Request-Id` `7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9` unless a story fixes another.

## An operator follows a script's creation

`create` (`S06`) records `script.created` with the new script's id and nothing else. The name the model chose, the repository, and the ref are in no event; the script's id is how the rest of its trail is found.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly","repo":"rep_41d8f0a6b2c97e13"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"crm-weekly","repo":"rep_41d8f0a6b2c97e13","ref":"main","created":"<created>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the id scripts gave the script, `scr_` and 16 lowercase hexadecimal digits, and `<created>` the time of the call.

Preconditions:

- The preamble's: no script is named `crm-weekly`, and `rep_41d8f0a6b2c97e13` is the caller's repository.

Postconditions:

- The catalog holds `crm-weekly`, never run, as `S06` tells. No run was made and no script ran.
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"script.created","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"script":"<id>"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `crm-weekly`, `rep_41d8f0a6b2c97e13`, `main`, and `mg@example.com` are in none of them.

## An operator follows a change to a script's ref

`update` (`S09`) records `script.updated` with the script's id alone: the trail says the script changed, not to what. The ref it had and the ref it has now are read from the script itself, with `show` (`S07`).

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"update","arguments":{"name":"rotate-keys","ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"scr_5c9b1e3a7f2d4068","name":"rotate-keys","repo":"rep_7b3e9a0c5d1f2846","ref":"main","created":"2026-09-28T08:00:00Z","last_run":{"id":"run_1e9c3a7f5b0d2864","status":"failed","started":"2026-10-04T22:00:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `rotate-keys` runs from `release`.

Postconditions:

- `rotate-keys` runs from `main` (`S09`); its run `run_1e9c3a7f5b0d2864` is as it was.
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"script.updated","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"script":"scr_5c9b1e3a7f2d4068"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  ```

  `rotate-keys`, `release`, and `main` are in neither. An update to the ref the script already has changes nothing and records no `script.updated` (`S09`).

## An operator follows a script's deletion

`delete` (`S10`) records `script.deleted` with the script's id. The script's earlier events, and its runs' `run.started`, still name that id, so its whole trail, from `script.created` to `script.deleted`, can be followed after it is gone. A run that has ended records nothing more when it is deleted with its script: `rotate-keys`' one run ended long before, with its `run.finished`, and removing it is not an event. A run still `running` is killed first and records its `run.finished`, `killed`, under its own request id and user, before the `script.deleted` (`S10`); here none is running.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete","arguments":{"name":"rotate-keys"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"scr_5c9b1e3a7f2d4068"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- `S06`'s shared catalog: the caller owns `rotate-keys`, whose one run, `run_1e9c3a7f5b0d2864`, has ended, and whose run folder is `state/runs/scr_5c9b1e3a7f2d4068/run_1e9c3a7f5b0d2864/`.

Postconditions:

- The catalog holds no `rotate-keys` and no run `run_1e9c3a7f5b0d2864`, and its folder is gone, as `S10` tells.
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order, and no `run.*` event:

  ```
  {"time":"<time>","service":"scripts","event":"script.deleted","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"script":"scr_5c9b1e3a7f2d4068"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  ```

  `rotate-keys` is in neither.

## An operator follows a run from its start to its end

`run` (`S08`) records `run.started` once the script's process has started, with the run's id, its script's id, the commit it resolved, and how it was started. The run then goes on without the request, and when the script ends on its own scripts records `run.finished`, with the status, the exit code, how long it ran, and whether its output was cut. That event comes after the request is over, but it carries the `run` call's request id and user all the same, so following `7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9` finds the whole run, and the calls its script made to siblings under that id (`S15`). The input the model gave is in no event; it is in the run's `input.json` (`S15`).

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","input":{"since":"2026-10-04"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the id scripts gave the run, `run_` and 16 lowercase hexadecimal digits.

Preconditions:

- The preamble's: `nightly-report` runs from `main`, which is `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`.
- The script at that commit exits 0 within `SCRIPT_SECONDS`, having written less than `OUTPUT_MAX_BYTES` to each of its streams.

Postconditions:

- The run `<id>` is recorded, and once the script has ended it is `exited` with `exit_code` 0 (`S08`, `S11`).
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"run"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- When the script has ended, telemetry has received, after the run's `run.started`, one more event, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":false}}
  ```

  `<n>` is the time from the run's `started` to its `finished`. Where it falls among the request's `tool.called` and `request.finished` is not fixed: a script that ends before its call is answered can be recorded before either. A script that exits non-zero records the same event with its exit code as `exit_code`, and one that dies of a signal scripts did not send records it with `exit_code` 128 plus the signal's number, 137 for `SIGKILL` (`S15`).
- `nightly-report`, `main`, `since`, `2026-10-04`, the script's output, and `mg@example.com` are in no event.

## An operator follows a run that could not start

A run that could not start is still a run, and it leaves a trace: its only event is `run.finished`, with `status` `failed` and the reason, recorded in the `run` call's request before its `tool.called`. There is no `run.started`, since no process started, and the call's `tool.called` has `outcome` `ok`, since the call did what it was asked: it made a run (`S05`, `S08`). The event carries no `sha`, so an operator learns whether the ref resolved from `result` (`S11`), not the trail.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"run","arguments":{"name":"rotate-keys"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","reason":"commit_missing"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `rotate-keys` runs from `release`, and `rep_7b3e9a0c5d1f2846` has no ref `release`.

Postconditions:

- The run `<id>` is recorded `failed` with reason `commit_missing`, and no script ran (`S08`).
- telemetry has received the request's four events, in this order, and no `run.started`:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"reason":"commit_missing","run":"<id>","status":"failed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"run"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `<n>` in `run.finished` is the time from the run's `started` to its `finished`. Every other reason (`S08`) records the same events with its own `reason`. `release` is in none of them, and scripts wrote nothing to stderr: a run that could not start is a fact of the trail, not trouble (`S02`).

## An operator follows a run that was cancelled

`cancel` (`S11`) records no event of its own beyond its `tool.called`: the run it ends records `run.finished` with `status` `killed`. That event is recorded inside the cancel's request, between its `request.started` and its `tool.called`, but it carries the run's own request id and user, the `run` call's, not the cancel's. The run's trail is whole under one request id, and the cancel's request is tied to it by the run's id in the event. Only the run's owner can cancel it (`S11`), so its user is the cancel's caller too; the request ids differ.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: cancel

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"cancel","arguments":{"run":"run_8a2c6e1f9b3d5074"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"run_8a2c6e1f9b3d5074","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"manual","status":"killed","started":"2026-10-05T09:31:40Z","finished":"<finished>","truncated":false}
```

and a `content` array of one text block whose text is that object encoded compactly. `<finished>` is the time of the cancel.

Preconditions:

- The preamble's: `run_8a2c6e1f9b3d5074`, of `nightly-report`, has been `running` since `2026-10-05T09:31:40Z`. It was started by a `run` call of `u_7f3a9c21`'s whose request id was `9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2`, and its `run.started` carries that id. Its script has written less than `OUTPUT_MAX_BYTES` to each stream.

Postconditions:

- `run_8a2c6e1f9b3d5074` is `killed`, its process group killed whole (`S11`).
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_8a2c6e1f9b3d5074","status":"killed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"cancel"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `<n>` in `run.finished` is the time from `2026-10-05T09:31:40Z` to `<finished>`. The `run.finished` has no `exit_code`: scripts killed the process.
- No other `run.finished` is ever recorded for `run_8a2c6e1f9b3d5074`.

## An operator follows a run that timed out with its output truncated

A run that outlives `SCRIPT_SECONDS` is killed by scripts on its own (`S17`), at a moment no request is part of, and its `run.finished` carries the `run` call's request id and user, never empty ones as a start or a stop has. Its `truncated` says the output was cut; how much was kept is read from `result` (`S11`), not the trail.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","ref":"v1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: the tag `v1` in `rep_9c2e4b7a1d3f8e05` names `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`.
- `SCRIPT_SECONDS` is 600 and `OUTPUT_MAX_BYTES` 1048576, their defaults.
- The script at that commit writes more than 1048576 bytes to its standard output and is still running 600 seconds after it started.

Postconditions:

- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"run"}}
  ```

  `v1` is in neither: the commit is, the ref is not.
- About 600 seconds after the run started, scripts has killed its process group, the run is `timed_out` (`S17`), and telemetry has received, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"<id>","status":"timed_out","truncated":true}}
  ```

  `<n>` is about 600000000. There is no `exit_code`. A run that is truncated and ends any other way carries `truncated` `true` the same.
- scripts wrote nothing to stderr.

## An operator finds no event for a refused run

A `run` that is refused made no run, so the trail says only that the call was made and refused: its `tool.called` with `outcome` `error`, and no `run.*` event. Here the model names a script it does not own; the name it sent and the text of the refusal are not recorded. A `run` refused while scripts is stopping (`S18`) records the same.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"run","arguments":{"name":"digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s, not the caller's.

Postconditions:

- No run was made, nothing was added under `state/runs/`, no git ran, and no script ran (`S08`).
- telemetry has received the request's three events, in this order, and no `run.*` event:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"run"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `digest` and `scr_3b7f9d1c5e0a2846` are in none of them.

## An operator finds no event for a script's calls to a sibling

A running script reaches the suite as a sibling service does, on the sibling's socket, sending the run's user and request id as `X-User-Id` and `X-Request-Id` (`S15`). scripts is not part of that call and records nothing for it: the sibling records it in its own trail, under the run's request id and user, as every app records a request. So the run's events and the sibling's are found together under one request id, and scripts' share of them is only `run.started` and `run.finished`, however many calls the script makes.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"run","arguments":{"name":"sync-crm"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- `S06`'s shared catalog: `sync-crm` runs from `main` of `rep_41d8f0a6b2c97e13`, which is `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3`.
- The script at that commit calls repos' `list` tool three times, each through the socket the services file names for `repos`, with the headers `S15` tells, and then exits 0.
- repos is serving and its entry is in the services file.

Postconditions:

- telemetry has received from scripts, for this run, exactly `run.started`, in the request as `An operator follows a run from its start to its end` shows it, and `run.finished`, with `status` `exited` and `exit_code` 0, both under request id `7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9` and user `u_7f3a9c21`. scripts recorded nothing for the three calls, and no event between `run.started` and `run.finished` names this run.
- repos has recorded each of the three calls in its own trail, with `request_id` `7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9` and `user` `u_7f3a9c21`, as its stories tell.

## An operator follows a user's visit to a run's page

A page records only the request's own pair: there is no event for viewing a script or a run, and no `run.*` event, though the page shows a run. Reading the repository's name for the page records nothing either. Of this request's events, only the `request.started`'s `path` holds the script's name and the run's id. A download of one of the run's files (`S14`) records the same pair and nothing else.

Request:

```
GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/?tab=output HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a7e3c1f9b5d2084e6c8a0f2d4b6e9c13
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the run's page (`S13`), an HTML page whose `h1` reads `run_3f9a1c2e8b7d4a60`.

Preconditions:

- `S06`'s shared catalog: `run_3f9a1c2e8b7d4a60`, of `nightly-report`, `exited` 0; it was asked for in the request `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, a different request from this one.

Postconditions:

- Nothing has changed but the trail. telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"a7e3c1f9b5d2084e6c8a0f2d4b6e9c13","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/nightly-report/runs/run_3f9a1c2e8b7d4a60/"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"a7e3c1f9b5d2084e6c8a0f2d4b6e9c13","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `tab=output` and `mg@example.com` are in neither.
