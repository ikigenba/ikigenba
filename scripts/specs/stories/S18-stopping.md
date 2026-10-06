# Stories — stopping

What becomes of runs when scripts stops, and of the runs a scripts that died left behind. This group extends the stop `S02` tells (`The host stops scripts`, `The host stops scripts while a request outlasts the drain`, `The host restarts scripts during a deploy`), whose stories have no run running. When scripts is told to stop, with `SIGTERM` or `SIGINT`, the `run` tool refuses every run it is asked for from then on, with the one line `scripts is stopping; try again later` (`S05`): a refused call makes no run and no run folder, runs no git and no script, and records no `run.*` event, and its `tool.called` has `outcome` `error`. An event the events app delivers from then on is refused the same way, starting no run for any script subscribed to it, so the events app delivers it again later (`S27`). The script is looked up first: a `name` that names none of the caller's scripts is answered `no script named '<name>'`, as at any other time (`S08`). The stopping refusal comes before the two `run` makes when runs are unavailable and when the run queue is full (`S08`): a call that would meet either while scripts is stopping is answered `scripts is stopping; try again later`. Every other tool, `result`, `runs` and `cancel` among them, answers during the drain as it always does. A `run` call scripts accepted before the signal is a request like any other: one whose ref is resolved and whose tree is unpacked within the drain while fewer than `RUN_MAX_ACTIVE` runs are running is answered `running` as usual (`S08`), and its script runs like the rest; one whose tree is unpacked within the drain while every place is taken is not queued, since no queued run starts once scripts is stopping: its run is recorded `failed`, with reason `queue_abandoned`, `finished` at that moment, and the call is answered `{"id":"<id>","status":"failed","sha":"<sha>","reason":"queue_abandoned"}`, as a run that could not start is (`S08`), its `run.finished`, `failed` with reason `queue_abandoned`, recorded in the call's request before its `tool.called`, under the call's request id and user, and no `run.started`; one whose git is still running at the drain deadline is cut off with the requests `S02` cuts off. Runs whose scripts are running at the signal go on: scripts does not signal them, and it waits for them as it waits for the requests it accepted, at most `DRAIN_SECONDS`. A run that ends within the drain is recorded as it ends, `exited` with its exit code (`S15`). At the deadline scripts kills the process group of every run still running, whole, the script and every process it started, and records each run `killed`, `finished` at the deadline, as `cancel` does (`S11`); the output each had written by then is kept in its folder, and the folder is kept. A run killed at the deadline is not a request: it is not counted in `S02`'s `stopped with <n> requests unfinished`, and it never makes scripts exit non-zero. A run still waiting in the queue at the signal (`S08`) never starts, not even when a running run ends within the drain and frees its place: at the signal scripts records each queued run `failed`, with reason `queue_abandoned`, `finished` at the signal, and records its `run.finished`. Its folder is kept as it was, with its `input.json`, its `tree/` and an empty `out/`, and no `stdout` or `stderr`, since its script never ran. A run abandoned so is not a request either: it is not counted in the `stopped with` line, it writes nothing to stderr of its own, and it never makes scripts exit non-zero. Each run's `run.finished` carries the request id and user of the run itself, the request id its `run` call carried, or for a run an event started the id of the delivery that started it (`S27`), and its `user` (`S16`), never the stop's, which has none; `run_8a2c6e1f9b3d5074`'s is `9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2`, and a story shows `run_6b2d8f4a0c9e1735`'s as `<request-id-2>`, the `request_id` `result` answers for that run. A scripts that dies rather than stops — killed with `SIGKILL`, crashed, or on a host that lost power — records nothing for the runs it was running; the next scripts to start finds them still recorded `running` and marks each `killed` before it is ready (`S02`). It records nothing for the runs that were waiting in the queue either; the next scripts finds them still recorded `queued` and finishes each `failed`, with reason `queue_abandoned`, in the same step, as a stop would have. So a deploy, which restarts scripts, kills every run in flight and abandons every run waiting. The actors are a model calling `run` through an MCP client, with requests as `S05` shows them, and the host, whether systemd or a developer at a terminal standing in for it. scripts runs over `S06`'s shared catalog, in which the caller `u_7f3a9c21` owns `nightly-report` (`scr_6d1f4a9b2e8c7035`) and `sync-crm` (`scr_a2e7c4f9b1d03856`), whose runs `run_8a2c6e1f9b3d5074` (since `2026-10-05T09:31:40Z`) and `run_6b2d8f4a0c9e1735` (since `2026-10-05T09:31:00Z`) are running, and `u_2b8e1d04` owns `digest`; `DRAIN_SECONDS` is unset, so the drain deadline is 5 seconds after the signal; `RUN_MAX_ACTIVE` and `RUN_MAX_QUEUED` are unset, so at most 2 runs run at once, as the two running runs do, and at most 10 wait for a place; and telemetry takes every event as soon as it is sent, unless a story says otherwise.

## A model asks for a run while scripts is stopping

scripts has been told to stop and will start no new run: a script started now would only be killed at the deadline. The call reaches scripts after the signal, on a connection scripts had already accepted, and is answered at once with the drain refusal, which tells the model to try again once a scripts is serving again. The answer is complete, so the request is finished, not cut off, and is not counted when scripts exits. Arguments refused as they are read against the input schema are refused as they always are, before the script is looked up.

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
Mcp-Name: run

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
scripts is stopping; try again later
```

The same call with a `ref` or an `input` is refused the same way.

Preconditions:

- The preamble's: `nightly-report` is the caller's, and its run `run_8a2c6e1f9b3d5074` is running.
- scripts has received `SIGTERM` less than 4 seconds before this call reached it, and is draining: it is waiting for `run_8a2c6e1f9b3d5074`, still running.

Postconditions:

- No run was made: `runs` of `nightly-report` answers the runs it had before the call, and nothing was added under `state/runs/scr_6d1f4a9b2e8c7035/`. No git ran and no script was started.
- `run_8a2c6e1f9b3d5074` is untouched by the call and goes on running.
- scripts wrote nothing to stderr for the call. telemetry has received the request's three events:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"run"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No `run.started` and no `run.finished` was recorded for it.

## A model asks for a run of a script it does not have while scripts is stopping

The script is looked up before the drain is: a name that names none of the caller's scripts is answered as it is at any other time, so the drain refusal never tells a caller that a script it does not own exists. `digest` is another user's.

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
Mcp-Name: run

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run","arguments":{"name":"digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

A name no user has, `ghost` say, is answered `no script named 'ghost'` the same way.

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s, not the caller's.
- scripts has received `SIGTERM` less than 4 seconds before this call reached it, and is draining, waiting for `run_8a2c6e1f9b3d5074`.

Postconditions:

- Nothing has changed: no run was made, nothing was added under `state/runs/`, no git ran and no script was started.
- scripts wrote nothing to stderr for the call. telemetry has received the request's `request.started`, its `tool.called` with `kind` `additive`, `outcome` `error` and `tool` `run`, and its `request.finished` with `status` `200`, under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and user `u_7f3a9c21`, and no `run.*` event.

## The host stops scripts while a script finishes within the drain

A script that is running when scripts is stopped is not cut short for it: scripts lets it run on and waits for it, as it waits for a request it accepted. A run that ends within the drain is recorded exactly as it would have been had nobody stopped scripts, and once the last run and the last request have ended scripts records `service.stopping` and exits, without waiting out the rest of the deadline. Nothing was lost, so the stop is silent and exits 0.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

scripts exits 0, once both runs have ended and their events are sent, within 4 seconds of the signal. Nothing is on stdout or stderr.

Preconditions:

- scripts is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog.
- `run_8a2c6e1f9b3d5074`'s script and `run_6b2d8f4a0c9e1735`'s are running at the signal, and each exits 0 on its own, `run_6b2d8f4a0c9e1735`'s 2 seconds after the signal and `run_8a2c6e1f9b3d5074`'s 3 seconds after it.
- No request is running at the signal, and none arrives after it.

Postconditions:

- Both runs are recorded as they ended: once a scripts is serving again, `result` of `run_8a2c6e1f9b3d5074` answers `status` `exited`, `exit_code` `0`, and `finished` 3 seconds after the signal, with all the output its script wrote, and `result` of `run_6b2d8f4a0c9e1735` the same with `finished` 2 seconds after the signal (`S11`). Their folders are kept.
- telemetry has received each run's `run.finished`, as it ended, under the request id and user of that run; `run_8a2c6e1f9b3d5074`'s is:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"run_8a2c6e1f9b3d5074","status":"exited","truncated":false}}
  ```

  and `run_6b2d8f4a0c9e1735`'s is the same with its own run id and request id, `<request-id-2>`. Then, last, scripts' `service.stopping` with `reason` `SIGTERM`.
- No process scripts started is still running.
- `/run/ikigenba/scripts.sock` still exists, and connections made to it after scripts exited wait in the socket's queue for the next scripts to answer.

## The host stops scripts while a script outlasts the drain

A script that is still running at the drain deadline is not waited for any longer: scripts must exit before the service unit's stop timeout, so at the deadline it kills the run's process group whole — the script and every process the script started — and records the run `killed`. A killed run is the stop working as designed, not trouble: it is not a request, so it is not counted in `S02`'s `stopped with` line, and scripts exits 0 when no request was cut off. Its record says what happened: `killed`, with no exit code, finished at the deadline, and the output it had written by then. The drain has used the whole deadline, so scripts has no time left to send the runs' `run.finished` or its own `service.stopping` to telemetry: each goes to stderr as an `undelivered event` line instead (`S02`).

Command:

```
$ kill -TERM <pid>
```

Output:

```
scripts: undelivered event: {"time":"<time>","service":"scripts","event":"run.finished","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_8a2c6e1f9b3d5074","status":"killed","truncated":false}}
scripts: undelivered event: {"time":"<time>","service":"scripts","event":"run.finished","request_id":"<request-id-2>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_6b2d8f4a0c9e1735","status":"killed","truncated":false}}
scripts: undelivered event: {"time":"<time>","service":"scripts","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
```

scripts exits 0, 5 seconds after the signal. The text is on stderr; stdout is empty. `<request-id-2>` is the `request_id` of `run_6b2d8f4a0c9e1735`. The order of the two `run.finished` lines is not fixed; `service.stopping` comes after both. Any other event scripts had recorded and not yet delivered when the deadline came is written as an `undelivered event` line too, in the order the events were recorded, `service.stopping` last of them. stderr holds no other line: no `stopped with` line, since no request was cut off.

Preconditions:

- scripts is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog.
- `run_8a2c6e1f9b3d5074`'s script and `run_6b2d8f4a0c9e1735`'s are running at the signal, and both are still running 5 seconds after it; `run_8a2c6e1f9b3d5074`'s has started a child process of its own, which is running too. Neither has written more than `OUTPUT_MAX_BYTES` to either stream.
- No request is running at the signal, and none arrives after it.

Postconditions:

- No process of either run, the child included, is still running, and no process scripts started is.
- Both runs are recorded `killed`: once a scripts is serving again, `result` of `run_8a2c6e1f9b3d5074` answers `status` `killed`, no `exit_code`, `finished` 5 seconds after the signal, and as its `stdout` and `stderr` what its script had written by then (`S11`), and `result` of `run_6b2d8f4a0c9e1735` the same for that run. Each run's folder is kept, with what was in its `out/`.
- telemetry has received no `run.finished` for either run and no `service.stopping`; the lines above are where they are recorded.
- `/run/ikigenba/scripts.sock` still exists, and connections made to it after scripts exited wait in the socket's queue for the next scripts to answer.

## The host stops scripts while runs are queued

A run waiting in the queue has not started, and scripts, which is going away, starts nothing more: a script started now would only be killed at the deadline. So at the signal scripts finishes every queued run at once, `failed` with reason `queue_abandoned`, and records each one's `run.finished`, under that run's own request id and user. A running run that ends within the drain frees its place, but no queued run takes it. The running runs go on and are waited for as in `The host stops scripts while a script finishes within the drain`. An abandoned run is not trouble and is not a request: the stop is silent and exits 0. A model that still wants the run asks for it again once a scripts is serving.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

scripts exits 0, once both running runs have ended and every event is sent, within 4 seconds of the signal. Nothing is on stdout or stderr.

Preconditions:

- scripts is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog, with `RUN_MAX_ACTIVE` and `RUN_MAX_QUEUED` unset.
- `run_8a2c6e1f9b3d5074`'s script and `run_6b2d8f4a0c9e1735`'s are running at the signal, and each exits 0 on its own, `run_6b2d8f4a0c9e1735`'s 2 seconds after the signal and `run_8a2c6e1f9b3d5074`'s 3 seconds after it.
- Two runs are queued behind them, both of `u_7f3a9c21`'s, each asked for by a `run` call answered `queued` (`S08`): `run_c4a8e2f6b0d93157`, of `nightly-report`, since `2026-10-05T09:31:50Z`, whose call carried `X-Request-Id: 5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5`, and after it `run_f1b5d9a3e7c20684`, of `sync-crm`, since `2026-10-05T09:31:55Z`, whose call carried `X-Request-Id: a7c1e5b9d3f04682b8e2c6a0f4d97153`. Each has its folder, with its `input.json`, its `tree/` and an empty `out/`.
- No request is running at the signal, and none arrives after it.

Postconditions:

- Neither queued run ever started: no process of either ran, and neither has a `stdout` or `stderr` file. Both are recorded `failed`: once a scripts is serving again, `result` of `run_c4a8e2f6b0d93157` answers `status` `failed`, `reason` `queue_abandoned`, no `exit_code`, `finished` the moment of the signal, and empty `stdout` and `stderr` (`S11`), and `result` of `run_f1b5d9a3e7c20684` the same for that run. Their folders are kept as they were.
- Both running runs are recorded as they ended, `exited` with `exit_code` `0`, as in `The host stops scripts while a script finishes within the drain`.
- telemetry has received, as the signal came, each queued run's `run.finished`, under that run's own request id and user, whose `duration_us` runs from the run's `started` to the signal:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"reason":"queue_abandoned","run":"run_c4a8e2f6b0d93157","status":"failed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"a7c1e5b9d3f04682b8e2c6a0f4d97153","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"reason":"queue_abandoned","run":"run_f1b5d9a3e7c20684","status":"failed","truncated":false}}
  ```

  the order of the two not fixed; then, as each running run ended, its `run.finished` with `status` `exited`; and last scripts' `service.stopping` with `reason` `SIGTERM`. telemetry has received no `run.started` for either queued run.
- No process scripts started is still running.
- `/run/ikigenba/scripts.sock` still exists, and connections made to it after scripts exited wait in the socket's queue for the next scripts to answer.

## The host stops scripts while a run call is still unpacking at the deadline

A `run` call does its git synchronously: it resolves the ref and unpacks the commit's tree before it answers (`S08`). A call accepted before the signal is a request scripts finishes if it can, but one whose git is still running at the drain deadline is cut off like any request `S02` cuts off: scripts kills its git and closes its connection without an answer. A call cut off at the drain deadline is not a failed start: scripts kills its git, removes the folder it had begun for the run, and records no run, so it leaves no record, no folder, and no `run.*` event, and the model, which had no answer, asks again once a scripts is serving. That sets it apart from a git that takes longer than `OPERATION_SECONDS`, which is a failed start recorded as a run, `failed` with reason `timed_out` (`S08`): there the call is answered, here scripts is going away and answers nothing. Losing the request is trouble, so it is counted, and scripts exits 1.

Command:

```
$ kill -TERM <pid>
```

Output:

```
scripts: undelivered event: {"time":"<time>","service":"scripts","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
scripts: stopped with 1 request unfinished
```

scripts exits 1, 5 seconds after the signal. The text is on stderr; stdout is empty. Any other event scripts had recorded and not yet delivered when the deadline came is written as an `undelivered event` line too, in the order the events were recorded, `service.stopping` last of them, as `S02` tells; the position of the `stopped with` line among them is not fixed, and stderr may also hold an `undelivered event` line carrying the cut-off call's `request.finished`, as `S02` allows.

Preconditions:

- scripts is serving as process `<pid>`, on the socket it was passed, over `S06`'s shared catalog, and no run is running.
- `u_7f3a9c21`'s `run` of `nightly-report`, with no `ref`, carrying `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, was accepted before the signal: it has resolved `main` to `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, and its `git archive` of that commit is still unpacking 5 seconds after the signal.
- No other request is running at the signal, and none arrives after it.

Postconditions:

- The call's git was killed, and its caller received no answer.
- No run is recorded for the call: once a scripts is serving again, `runs` of `nightly-report` does not list it, answering the runs it had before the call and no other. No folder for the call is left under `state/runs/scr_6d1f4a9b2e8c7035/`: no `input.json`, no part of `tree/`. No script was started.
- No git process scripts started is still running.
- The call's `request.started` is recorded (delivered to telemetry, written to stderr as undelivered, or both). telemetry has received no `run.started`, `run.finished`, `tool.called` or `request.finished` for it, and no `service.stopping`.
- `/run/ikigenba/scripts.sock` still exists, and connections made to it after scripts exited wait in the socket's queue for the next scripts to answer.

## The host starts scripts after runs were left running

A scripts that dies rather than stops records nothing for the runs it was running, so its catalog still says `running` for runs whose processes are gone. Nothing could ever end them, so the next scripts to start settles them before it is ready: it marks every run still recorded `running` as `killed`, `finished` at the moment it marks it, and records each one's `run.finished`, under that run's own request id and user, before its own `service.started`. A run so marked keeps its folder exactly as it was left, with whatever its script had written, so `result` and the run's page show the run as far as it got. The marking is not trouble and writes nothing to stderr. Pruning follows it (`S19`).

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed scripts, and `ikigenba-scripts.socket` is active, as in `S02`'s `The host starts scripts`; `ikigenba-scripts.service` is not running.
- The scripts that last ran died without stopping — it was killed with `SIGKILL` — while `run_8a2c6e1f9b3d5074`'s and `run_6b2d8f4a0c9e1735`'s scripts were running, and systemd ended what was left of the service, so no process of either run is running. `/opt/scripts/state/scripts.db` holds `S06`'s shared catalog, in which both runs are still recorded `running`, and their folders under `/opt/scripts/state/runs/` hold the output their scripts had written.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `ikigenba-scripts.service` is `active`, and scripts is serving on `/run/ikigenba/scripts.sock`.
- Both runs are recorded `killed`: `result` of `run_8a2c6e1f9b3d5074` answers `status` `killed`, no `exit_code`, `finished` the moment this start marked it, and as its `stdout` and `stderr` what its files held (`S11`), and `result` of `run_6b2d8f4a0c9e1735` the same for that run. No run is recorded `running`. Both folders are as they were left.
- telemetry has received, before scripts' `service.started`, one `run.finished` for each, under the request id and user of that run, whose `duration_us` runs from the run's `started` to the moment it was marked:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"9b1e4d7a2c5f8036e1a4d7b0c3f6e9a2","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_8a2c6e1f9b3d5074","status":"killed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"<request-id-2>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"run_6b2d8f4a0c9e1735","status":"killed","truncated":false}}
  ```

  where `<request-id-2>` is `run_6b2d8f4a0c9e1735`'s `request_id`; the order of the two is not fixed. Then `service.started`, as in `S02`. The trail holds no `service.stopping` from the scripts that died.
- scripts has written nothing to the journal.

## The host starts scripts after runs were left queued

A scripts that dies rather than stops records nothing for the runs waiting in its queue either, so its catalog still says `queued` for runs nothing will ever start. The next scripts to start does not start them: a run asked for before a crash is not run behind its caller's back after it. In the same step that marks the runs left `running` `killed`, before it is ready, it finishes every run still recorded `queued` as `failed`, with reason `queue_abandoned`, `finished` at the moment it marks it, as a stop would have, and records each one's `run.finished`, under that run's own request id and user, before its own `service.started`. A run so finished keeps its folder as it was left. The marking is not trouble and writes nothing to stderr. Pruning follows it (`S19`).

Command:

```
$ sudo systemctl start ikigenba-scripts.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed scripts, and `ikigenba-scripts.socket` is active, as in `S02`'s `The host starts scripts`; `ikigenba-scripts.service` is not running.
- The scripts that last ran died without stopping — it was killed with `SIGKILL` — while `run_8a2c6e1f9b3d5074`'s and `run_6b2d8f4a0c9e1735`'s scripts were running and `run_c4a8e2f6b0d93157`, of `u_7f3a9c21`'s `nightly-report`, asked for by a `run` call carrying `X-Request-Id: 5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5`, was queued behind them. systemd ended what was left of the service, so no process of any run is running. `/opt/scripts/state/scripts.db` holds `S06`'s shared catalog, in which the two running runs are still recorded `running` and `run_c4a8e2f6b0d93157` is still recorded `queued`; its folder under `/opt/scripts/state/runs/scr_6d1f4a9b2e8c7035/` holds its `input.json`, its `tree/` and an empty `out/`.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- `ikigenba-scripts.service` is `active`, and scripts is serving on `/run/ikigenba/scripts.sock`.
- `run_c4a8e2f6b0d93157` never started: `result` of it answers `status` `failed`, `reason` `queue_abandoned`, no `exit_code`, `finished` the moment this start marked it, and empty `stdout` and `stderr` (`S11`). Its folder is as it was left. The two running runs are recorded `killed`, as in `The host starts scripts after runs were left running`. No run is recorded `running` or `queued`.
- telemetry has received, before scripts' `service.started`, the two running runs' `run.finished`, `killed`, as in `The host starts scripts after runs were left running`, and `run_c4a8e2f6b0d93157`'s, whose `duration_us` runs from the run's `started` to the moment it was marked:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"5e8b2d4f7a1c3096e4b7d0a3c6f9e2b5","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"reason":"queue_abandoned","run":"run_c4a8e2f6b0d93157","status":"failed","truncated":false}}
  ```

  The order of the three is not fixed. Then `service.started`, as in `S02`. telemetry has received no `run.started` for `run_c4a8e2f6b0d93157`.
- scripts has written nothing to the journal.

## The host deploys scripts while a run is running

A deploy restarts `ikigenba-scripts.service` (`S02`), and a restart is a stop: the old scripts drains, and at its deadline kills every run still running and records it `killed`. A run is never carried across a deploy, and a deploy does not wait for runs beyond the drain: a run in flight when scripts is deployed ends `killed`, and a model that wants its result runs the script again once the new scripts is serving. This is accepted, not trouble: the old scripts exits 0 and the restart succeeds. As in `The host stops scripts while a script outlasts the drain`, the drain has used the whole deadline, so the old scripts' last events go to the journal as `undelivered event` lines. The new scripts finds no run recorded `running`, so it marks none.

Command:

```
$ sudo systemctl restart ikigenba-scripts.service
```

Output:

```
```

Exits 0, once the new scripts has reported that it is ready. Nothing is on stdout or stderr.

Preconditions:

- scripts is serving on `/run/ikigenba/scripts.sock` under `ikigenba-scripts.service`, over `S06`'s shared catalog; `/opt/scripts/bin/scripts` has been replaced by a new release.
- `run_8a2c6e1f9b3d5074`'s script and `run_6b2d8f4a0c9e1735`'s are running when the old scripts receives `SIGTERM`, and both would run on for minutes.
- No request the old scripts accepted is still running 5 seconds after its signal.
- The host's services file lists the telemetry service, which takes every event.

Postconditions:

- No process of either run is running. Both runs are recorded `killed`, `finished` 5 seconds after the old scripts' signal, as in `The host stops scripts while a script outlasts the drain`, and their folders are kept: `result` of either, asked of the new scripts, answers `status` `killed` with the output written before the kill (`S11`).
- The journal holds the old scripts' lines: an `undelivered event` line with each run's `run.finished`, `status` `killed`, under that run's request id and user, then one with its `service.stopping`, `reason` `SIGTERM`, and no `stopped with` line. telemetry has received none of those three events.
- A new scripts process is serving on `/run/ikigenba/scripts.sock`, over the same `state/scripts.db` and `state/runs/`; it marked no run, and telemetry has received its `service.started`, whose `version` is the version the new binary's `scripts --version` prints.
