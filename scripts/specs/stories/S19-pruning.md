# Stories — pruning

How scripts keeps its runs from growing without end. Two settings, read once when scripts starts (`S02`), say what is kept: `RUN_KEEP_DAYS`, 15 when unset or empty, and `RUN_KEEP_COUNT`, 10 when unset or empty. A run is past keeping when it is older than `RUN_KEEP_DAYS` — it started more than that many days of 24 hours before the moment scripts prunes — and is not among its script's newest `RUN_KEEP_COUNT` runs, newest in the order `runs` answers them, by `started`, ties by id (`S11`). The newest `RUN_KEEP_COUNT` runs of each script are kept whatever their age, and every run within `RUN_KEEP_DAYS` is kept however many there are: the count only ever keeps a run, never removes one. Every run counts alike, whatever its status, a run still running and one that failed to start included. scripts prunes at start, after it has marked the runs left `running` as `killed` (`S18`) and before it is ready (`S02`), and again each time a run ends — on its own, at `SCRIPT_SECONDS`, on `cancel`, by its script's `delete` (`S10`), as a run that could not start, or at a drain deadline — and each time over the whole catalog, every script of every owner, not only the script whose run ended. There is no timer: a run that becomes past keeping while no run ends stays until the next run ends or the next start. Pruning a run removes it whole and at once: its record and its folder `state/runs/<script id>/<run id>/`, the read-only `tree/` within it included, together. A run whose folder is already gone loses its record all the same. Afterwards the run is gone everywhere: `runs` leaves it out, `result` and `cancel` answer `no run '<id>'` (`S11`), and its page is not found (`S13`). Pruning is housekeeping, not something anyone did: it records no event and writes nothing to stderr; it never touches a run that is kept, and it never touches a script. The actors are a model calling a tool through an MCP client, with requests as `S05` shows them, the host, and an operator. scripts runs on a host in `/opt/scripts` over `S06`'s shared catalog, with `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` unset unless a story sets them, and telemetry takes every event. Where a story says so, the catalog also holds four older runs of the caller `u_7f3a9c21`'s `nightly-report` (`scr_6d1f4a9b2e8c7035`), the September runs, each of trigger `manual` and user `u_7f3a9c21`, `exited` with `exit_code` `0`, from `main` at `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60`, with its folder under `state/runs/scr_6d1f4a9b2e8c7035/`, and each older than 15 days at `2026-10-05T09:32:00Z`:

| run | started | finished |
|---|---|---|
| `run_a9d3f7b1c5e02846` | `2026-09-20T09:14:02Z` | `2026-09-20T09:14:12Z` |
| `run_6e0a4c8f2d1b7593` | `2026-09-19T17:30:00Z` | `2026-09-19T17:30:10Z` |
| `run_b3f7c1e5a9d20864` | `2026-09-19T09:14:02Z` | `2026-09-19T09:14:13Z` |
| `run_4d8e2a6c0f3b9175` | `2026-09-18T17:00:00Z` | `2026-09-18T17:00:12Z` |

With them `nightly-report` has eleven runs. Until its run `run_8a2c6e1f9b3d5074` started, at `2026-10-05T09:31:40Z`, it had ten, all kept by `RUN_KEEP_COUNT`; that run made `run_4d8e2a6c0f3b9175` the eleventh, outside the newest ten and older than 15 days, so past keeping, and no run has ended since.

## The host starts scripts with runs past keeping

Runs become past keeping while no run ends, and a scripts that died rather than stopped never pruned after the runs it was running, so a start prunes before scripts is ready, and a run that is past keeping is never seen by the first request the new scripts answers. The start first settles the runs the dead scripts left `running` (`S18`); a run so marked `killed` is a run like any other here, and still counts among its script's newest.

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed scripts, and `ikigenba-scripts.socket` is active, as in `S02`'s `The host starts scripts`; `ikigenba-scripts.service` is not running, and `/opt/scripts/etc/env` leaves `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` unset.
- `/opt/scripts/state/scripts.db` holds `S06`'s shared catalog and the September runs. The scripts that last ran died without stopping — it was killed with `SIGKILL` — at `2026-10-05T09:31:50Z`, while `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` were running, so both are still recorded `running`, and no process of either is running.
- The host starts scripts at `2026-10-05T09:32:00Z`.

Postconditions:

- `ikigenba-scripts.service` is `active`, and scripts is serving on `/run/ikigenba/scripts.sock`.
- `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` are recorded `killed` (`S18`).
- `run_4d8e2a6c0f3b9175` is gone: its record, and its folder `/opt/scripts/state/runs/scr_6d1f4a9b2e8c7035/run_4d8e2a6c0f3b9175/`, `tree/` and all. `result` of it by `u_7f3a9c21` answers `no run 'run_4d8e2a6c0f3b9175'` (`S11`).
- Every other run and its folder is as it was: `nightly-report` keeps its newest ten, `run_8a2c6e1f9b3d5074` among them, three of them the September runs older than 15 days, and `sync-crm`, `rotate-keys` and `digest` keep their one run each.
- telemetry has received the two `run.finished` of the marked runs and then `service.started`, as `S18` tells, and no other event: nothing records the pruning. scripts has written nothing to the journal.

## A run ends and the runs past keeping are removed

scripts prunes when a run ends, not on a clock: `run_4d8e2a6c0f3b9175` has been past keeping since `run_8a2c6e1f9b3d5074` started, and is still there, and `result` still answers it, until a run ends. The run that ends need not be one of the same script's, nor the same user's: every run ending prunes the whole catalog. Here `sync-crm`'s run ends on its own, and `nightly-report`'s oldest run goes, while `nightly-report`'s own newest run is still running and untouched. The model that asks for the removed run afterwards is told it has no such run.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_4d8e2a6c0f3b9175"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no run 'run_4d8e2a6c0f3b9175'
```

Preconditions:

- scripts is serving, started with `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` unset, over `S06`'s shared catalog and the September runs. `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` are running, and no run has ended since `run_8a2c6e1f9b3d5074` started.
- `run_6b2d8f4a0c9e1735`'s script exits 0 on its own at `2026-10-05T09:33:00Z`; this call reaches scripts after that.

Postconditions:

- `run_6b2d8f4a0c9e1735` is recorded `exited` with `exit_code` `0` (`S15`), and telemetry has received its `run.finished`; that is the only event its ending recorded.
- `run_4d8e2a6c0f3b9175` is gone: its record, and its folder `state/runs/scr_6d1f4a9b2e8c7035/run_4d8e2a6c0f3b9175/`, `tree/` and all; `runs` of `nightly-report` leaves it out (`S11`).
- `run_8a2c6e1f9b3d5074` is still running, and every other run and its folder is as it was.
- scripts wrote nothing to stderr. Pruning recorded no event; telemetry has received this call's `request.started`, its `tool.called` with `kind` `read`, `outcome` `error` and `tool` `result`, and its `request.finished` with `status` `200`, under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and user `u_7f3a9c21`.

## A model finds a script's newest runs kept whatever their age

Age alone never removes a script's newest runs: the newest `RUN_KEEP_COUNT` of each script are kept however old they are, so a script that runs rarely keeps a record of its last runs, and a script's newest run is never pruned. Three of `nightly-report`'s newest ten are the September runs, older than 15 days, and all three are kept through a prune. The next run of `nightly-report` will push the oldest of them out of the newest ten, and the first run to end after that removes it.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` whose `runs` holds ten entries, newest first (`S11`): `run_8a2c6e1f9b3d5074`, `run_3f9a1c2e8b7d4a60`, `run_c71d0b5e4a2f9386`, `run_5e8b3d7a1c0f6294`, `run_19f6a4d2c8e3b705`, `run_d4a7e2c9f1b8630a`, `run_72b0c8f5e3d1a946`, and last the three September runs still kept:

```
{"id":"run_a9d3f7b1c5e02846","sha":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60","ref":"main","trigger":"manual","status":"exited","exit_code":0,"started":"2026-09-20T09:14:02Z","finished":"2026-09-20T09:14:12Z","truncated":false}
{"id":"run_6e0a4c8f2d1b7593","sha":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60","ref":"main","trigger":"manual","status":"exited","exit_code":0,"started":"2026-09-19T17:30:00Z","finished":"2026-09-19T17:30:10Z","truncated":false}
{"id":"run_b3f7c1e5a9d20864","sha":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60","ref":"main","trigger":"manual","status":"exited","exit_code":0,"started":"2026-09-19T09:14:02Z","finished":"2026-09-19T09:14:13Z","truncated":false}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- scripts is serving, started with `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` unset, over `S06`'s shared catalog and the September runs, as `A run ends and the runs past keeping are removed` left it: `run_6b2d8f4a0c9e1735` ended at `2026-10-05T09:33:00Z`, scripts pruned then, and `run_4d8e2a6c0f3b9175` is gone. `run_8a2c6e1f9b3d5074` is still running.

Postconditions:

- Nothing has changed. The three September runs and their folders under `state/runs/scr_6d1f4a9b2e8c7035/` are still there, and `result` of each answers it (`S11`).

## A model finds every run within the keeping age kept, however many there are

`RUN_KEEP_COUNT` keeps a script's newest runs past `RUN_KEEP_DAYS`; it is not a cap. A script that runs often keeps every run of the last `RUN_KEEP_DAYS` days, however many more than `RUN_KEEP_COUNT` that is. Here only two runs of each script are kept past the age, and `nightly-report` keeps all seven of its runs, since every one started within the last 15 days.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member and a `structuredContent` whose `runs` holds all seven of `nightly-report`'s runs in `S06`'s shared catalog, newest first (`S11`), from `run_8a2c6e1f9b3d5074` to `run_72b0c8f5e3d1a946`.

Preconditions:

- scripts is serving over `S06`'s shared catalog, without the September runs, started with `RUN_KEEP_COUNT` set to `2` and `RUN_KEEP_DAYS` unset.
- `run_6b2d8f4a0c9e1735`'s script exits 0 on its own at `2026-10-05T09:33:00Z`, and scripts prunes then; this call reaches scripts after that.

Postconditions:

- Nothing was pruned: every run of every script, and every run folder under `state/runs/`, is as it was before `run_6b2d8f4a0c9e1735` ended.

## A run ends while a run past keeping has already lost its folder

A run's record survives its folder going missing, and is shown as a run whose files are gone (`S11`, `S13`, `S20`). Pruning such a run is no different and no failure: its record goes, there being no folder to remove, and nothing is said about the folder that was not there.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"result","arguments":{"run":"run_4d8e2a6c0f3b9175"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no run 'run_4d8e2a6c0f3b9175'
```

Preconditions:

- As in `A run ends and the runs past keeping are removed`, except that the folder `state/runs/scr_6d1f4a9b2e8c7035/run_4d8e2a6c0f3b9175/` is gone, removed by hand, so until the prune `result` of `run_4d8e2a6c0f3b9175` answered it with `files_gone` `true` (`S11`).
- `run_6b2d8f4a0c9e1735`'s script exits 0 on its own at `2026-10-05T09:33:00Z`; this call reaches scripts after that.

Postconditions:

- `run_4d8e2a6c0f3b9175`'s record is gone; `runs` of `nightly-report` leaves it out (`S11`). Nothing was created where its folder was.
- Every other run and its folder is as it was.
- scripts wrote nothing to stderr, and is still serving. Pruning recorded no event.

## An operator lowers the keeping age and count and restarts scripts

An operator who wants fewer runs kept sets the two settings in `/opt/scripts/etc/env`. scripts reads them only when it starts, so the edit alone changes nothing: the running scripts keeps pruning by the values it started with. The restart applies them, and the new scripts prunes by them before it is ready. A restart is a stop: had any run been running, it would have been killed (`S18`). The new values keep each script's newest two runs and every run of the last three days; older runs beyond those go, the one whose folder was already gone among them.

Command:

```
$ sudo systemctl restart ikigenba-scripts.service
```

Output:

```
```

Exits 0, once the new scripts has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- scripts is serving under `ikigenba-scripts.service` over `S06`'s shared catalog, without the September runs, started with `RUN_KEEP_DAYS` and `RUN_KEEP_COUNT` unset. No run is running: `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` have since exited 0 on their own, and the prunes after them removed nothing.
- The operator has since set `RUN_KEEP_DAYS=3` and `RUN_KEEP_COUNT=2` in `/opt/scripts/etc/env`; every run and run folder is still as it was.
- The host restarts scripts at `2026-10-05T09:40:00Z`.

Postconditions:

- A new scripts is serving, holding `RUN_KEEP_DAYS` 3 and `RUN_KEEP_COUNT` 2.
- `nightly-report`'s `run_19f6a4d2c8e3b705` (started `2026-10-02T09:14:02Z`), `run_d4a7e2c9f1b8630a` (started `2026-10-01T09:14:02Z`) and `run_72b0c8f5e3d1a946` (started `2026-09-30T09:14:02Z`) are gone: their records, and the folders `state/runs/scr_6d1f4a9b2e8c7035/run_19f6a4d2c8e3b705/` and `state/runs/scr_6d1f4a9b2e8c7035/run_d4a7e2c9f1b8630a/`; `run_72b0c8f5e3d1a946` had none. `result` of each answers `no run '<id>'` (`S11`).
- `runs` of `nightly-report` answers four runs: `run_8a2c6e1f9b3d5074` and `run_3f9a1c2e8b7d4a60`, its newest two, and `run_c71d0b5e4a2f9386` and `run_5e8b3d7a1c0f6294`, started within the last three days; their folders are as they were.
- `sync-crm`, `rotate-keys` and `digest` keep their one run each, and their folders.
- telemetry has received the old scripts' `service.stopping` and the new scripts' `service.started` (`S02`), and no event for the pruning. scripts has written nothing to the journal.
