# Stories — limits

What bounds a run, so that no script can fill scripts' disk with its tree or its output, or hold a process for ever. Four settings, each a positive whole number read once, at start, from scripts' environment, whose default is the manifest's `[env]` (`S01`), which the host writes into `/opt/scripts/etc/env`; an operator changes one there and restarts scripts, which ends every run then running (`S18`), and a developer sets it on the command line. Unset or empty, a setting is its default; a value that is not a positive whole number keeps scripts from starting (`S02`). `TREE_MAX_BYTES`, 268435456 (256 MiB), is the most a run's tree may hold: the sum of the sizes of the files of the commit, as `git archive` emits them, neither the repository's size on disk nor the archive's. A tree whose files sum to exactly `TREE_MAX_BYTES` is unpacked and run; one byte more, and it is not: scripts stops unpacking as soon as the sum passes the limit, leaves in `tree/` whatever of it had been written by then, starts no script, and records the run `failed` with `reason` `too_large` and the `sha` its ref resolved to. Every run unpacks its own tree, so the limit that applies is the one scripts started with, not the one in force when the commit was pushed or when an earlier run of it was refused. `OUTPUT_MAX_BYTES`, 1048576 (1 MiB), bounds a run's standard output and, separately, its standard error: of each stream the first `OUTPUT_MAX_BYTES` bytes are kept in the run's `stdout` or `stderr` file, cut where the count falls, mid-line or not, and the rest is read and dropped. A stream of exactly `OUTPUT_MAX_BYTES` is not cut. A run with either stream cut is `truncated`, one flag for both; the script is not killed, held up or told for it, and runs on to its own end. `stdout_bytes` and `stderr_bytes` count what was kept, never what was written. `OPERATION_SECONDS`, 600, is the longest one git run of a `run` call may take, resolving the ref or unpacking the tree, each timed on its own; a git still running at the deadline is killed and the run recorded `failed` with `reason` `timed_out`, as `S08` tells. `SCRIPT_SECONDS`, 600, is the longest a script may run, counted from when its process starts: a script still running then is killed with its whole process group, every process it started in that group with it, and its run recorded `timed_out`, with no exit code and `finished` the moment it was killed; what it wrote to its output streams and to `out/` before then is kept. Nothing bounds `out/`: a script writes there as much as the disk takes, and pruning answers the growth (`S19`). scripts has no memory setting of its own: the manifest's `[resources]` sets `memory_max` to `1G` for scripts' service unit (`S01`), one ceiling over scripts, every git it runs, and every script it runs together, with no limit per run and no cap on how many run at once; a script the kernel kills for memory has died of a signal scripts did not send, so its run is `exited` with `exit_code` 137, 128 plus `SIGKILL`'s 9 (`S15`).

The actor is a model working through an MCP client, starting a run with `run` as `S08` sends it and reading it with `result` as `S11` sends it; the request shape and the result envelope are `S05`'s. scripts runs on the host with the suite's services file (`S05`), telemetry takes every event, and the catalog holds `S06`'s shared catalog, among them the caller `u_7f3a9c21`'s `nightly-report`, `scr_6d1f4a9b2e8c7035`, over `rep_9c2e4b7a1d3f8e05` at `main`. The owner has since pushed one commit to that repository's `main`, `<sha>`, whose files each story gives, a `main.py` at its root among them; a run of `nightly-report` resolves `main` to `<sha>`. Every setting is unset unless a story sets it; a story that sets one has it in `/opt/scripts/etc/env`, and scripts was restarted since, which ended the shared catalog's two running runs `killed` (`S18`). No other run is running while a story's run does. Each story's `run` call carries the request id `4a8e2c6f0b9d4173c5e1a7f3b9d2c086`, which its run keeps as its `request_id`, and its `result` call, made once the run has ended, carries `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b`. `<id>` is the new run's id, `<started>` the time of the `run` call, and `<finished>` the moment the run ended. A run that ends on its own or at `SCRIPT_SECONDS` records its `run.finished` as it ends, under the run's own request id and user, with no request around it: `duration_us`, the whole microseconds from the run's start to its end; `exit_code`, only when `status` is `exited`; `reason`, only when it is `failed`; `run`; `status`; and `truncated` (`S16`). Nothing in this group earns a line on stderr: a run refused its tree, cut short or killed is recorded in the run and the trail, and what a script writes goes to its own run's files (`S02`).

## A model runs a script whose tree is exactly at the size limit

The limit is inclusive: a tree whose files sum to exactly `TREE_MAX_BYTES` is unpacked and run like any other. Only the files count; the directories that hold them, and how git stores them, do not.

```python
import os
print(os.path.getsize("data.bin"))
```

That is `main.py` at `<sha>`, 45 bytes; beside it is `data.bin`, 1048531 bytes, and nothing else.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":8,"stderr_bytes":0,"truncated":false,"stdout":"1048531\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, with `TREE_MAX_BYTES=1048576` set in `/opt/scripts/etc/env` and scripts restarted since.
- The files of the tree at `<sha>` are `main.py`, 45 bytes, and `data.bin`, 1048531 bytes: exactly 1048576 bytes.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, sha `<sha>`, ref `main`, status `exited`, exit code 0.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/tree/` holds `main.py` and `data.bin`, 1048576 bytes in all, as `git archive` emitted them.
- The `run` call recorded what `S08` tells: its `request.started`, the run's `run.started` with `sha` `<sha>`, its `tool.called` with `outcome` `ok`, and its `request.finished`.
- When the script exited, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":false}}
  ```
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A model runs a script whose tree is one byte over the size limit

One byte past `TREE_MAX_BYTES` and the script does not start. It is not a refusal: the run is made and recorded `failed` with `reason` `too_large`, and `run` answers it at once with the sha its ref resolved to, so the model can tell its owner which commit must shed files. The limit is not quoted in the answer; the run's page states it (`S13`). scripts stops the unpack as soon as the sum passes the limit; what it had written before then stays in `tree/` as it was left, and no script runs from it.

```python
import os
print(os.path.getsize("data.bin"))
```

That is `main.py` at `<sha>`, 45 bytes; beside it is `data.bin`, 1048532 bytes, and nothing else.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","sha":"<sha>","reason":"too_large"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"failed","started":"<started>","finished":"<finished>","stdout_bytes":0,"stderr_bytes":0,"truncated":false,"reason":"too_large","stdout":"","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<finished>` is the moment the failure was recorded, within the `run` call.

Preconditions:

- The preamble's, with `TREE_MAX_BYTES=1048576` set in `/opt/scripts/etc/env` and scripts restarted since.
- The files of the tree at `<sha>` are `main.py`, 45 bytes, and `data.bin`, 1048532 bytes: 1048577 bytes in all.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, sha `<sha>`, ref `main`, status `failed`, reason `too_large`, no exit code. `nightly-report`'s ref is still `main`.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` holds `input.json`, `{}`, and whatever of `tree/` had been written before scripts stopped the unpack at the limit. No script ran, and no git scripts started for the call is still running.
- The repository is as it was.
- telemetry has received the `run` call's four events, in this order, and no `run.started`:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"reason":"too_large","run":"<id>","status":"failed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"run"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A model runs a script again after the operator raises the size limit

The limit is the operator's to move. A script whose tree was too large under one limit runs under a higher one, from the same commit, with no change to the repository. The run that failed stays as it was recorded: a run is a record of what happened, and a new limit does not rewrite it.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's; the tree at `<sha>` is the one of `A model runs a script whose tree is one byte over the size limit`, 1048577 bytes in all.
- With `TREE_MAX_BYTES=1048576`, a run of `nightly-report` at `<sha>`, `<failed id>`, was recorded `failed` with `reason` `too_large`.
- The operator has since set `TREE_MAX_BYTES=2097152` in `/opt/scripts/etc/env` and restarted scripts.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, sha `<sha>`, ref `main`; `state/runs/scr_6d1f4a9b2e8c7035/<id>/tree/` holds `main.py` and `data.bin`, 1048577 bytes in all. Once the script has exited, `result` answers it `exited` with `exit_code` 0, `stdout` `"1048532\n"`, and `truncated` `false`.
- `<failed id>` is unchanged: `failed`, `reason` `too_large`, its folder as it was. `runs` (`S11`) lists both.
- The `run` call recorded what `S08` tells: its `request.started`, the run's `run.started` with `sha` `<sha>`, its `tool.called` with `outcome` `ok`, and its `request.finished`.

## A script writes exactly the output limit

The limit is inclusive: a stream of exactly `OUTPUT_MAX_BYTES` bytes is kept whole, and the run is not `truncated`.

```python
import sys

sys.stdout.write("x" * 1024)
```

That is `main.py` at `<sha>`.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":1024,"stderr_bytes":0,"truncated":false,"stdout":"<stdout>","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<stdout>` stands for the run's whole `stdout`, 1024 `x` characters, as one JSON string.

Preconditions:

- The preamble's, with `OUTPUT_MAX_BYTES=1024` set in `/opt/scripts/etc/env` and scripts restarted since.

Postconditions:

- `state/runs/scr_6d1f4a9b2e8c7035/<id>/stdout` is exactly the 1024 bytes the script wrote; `stderr` is empty.
- The run is `exited` 0 and not `truncated`; its page does not mark its output truncated (`S13`).
- When the script exited, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":false}}
  ```
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A script writes more than the output limit to its standard output

A script that prints more than `OUTPUT_MAX_BYTES` keeps the head of what it printed: the first `OUTPUT_MAX_BYTES` bytes, cut where the count falls, here in the middle of a line. Everything after is dropped, and the run is `truncated`. The script is not killed, held up or told: its writes go on succeeding, and it runs on to its own end, here to the line it writes to standard error after its 10000 bytes of standard output, and exits 0.

```python
import sys

for i in range(1000):
    print(f"line {i:04d}")
print("done", file=sys.stderr)
```

That is `main.py` at `<sha>`. It writes 1000 lines, `line 0000` to `line 0999`, each 10 bytes with its LF.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":1024,"stderr_bytes":5,"truncated":true,"stdout":"<stdout>","stderr":"done\n","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<stdout>` stands for the run's whole kept `stdout`, 1024 bytes, as one JSON string: the 102 lines `line 0000\n` to `line 0101\n`, then `line`, the first four bytes of `line 0102`.

Preconditions:

- The preamble's, with `OUTPUT_MAX_BYTES=1024` set in `/opt/scripts/etc/env` and scripts restarted since.

Postconditions:

- `state/runs/scr_6d1f4a9b2e8c7035/<id>/stdout` is exactly the first 1024 bytes the script wrote to standard output, ending `line 0101\nline`; nothing of the other 8976 bytes is kept anywhere. `stderr` is `done\n`, whole.
- The run is `exited` 0 and `truncated`; its page marks its output truncated (`S13`).
- When the script exited, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":true}}
  ```
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A script writes more than the output limit to its standard error only

The two streams are bounded separately, each by the whole of `OUTPUT_MAX_BYTES`: a script that floods its standard error keeps all of its standard output. The run has one `truncated` flag, set when either stream was cut, so it is `truncated` here too, and `stdout_bytes` and `stderr_bytes` tell which stream reached the limit.

```python
import sys

sys.stderr.write("e" * 3000)
print("ok")
```

That is `main.py` at `<sha>`.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":3,"stderr_bytes":1024,"truncated":true,"stdout":"ok\n","stderr":"<stderr>","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<stderr>` stands for the run's whole kept `stderr`, 1024 `e` characters, as one JSON string.

Preconditions:

- The preamble's, with `OUTPUT_MAX_BYTES=1024` set in `/opt/scripts/etc/env` and scripts restarted since.

Postconditions:

- `state/runs/scr_6d1f4a9b2e8c7035/<id>/stdout` is `ok\n`, whole; `stderr` is exactly the first 1024 of the 3000 bytes the script wrote there.
- The run is `exited` 0 and `truncated`.
- When the script exited, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":true}}
  ```
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A script runs past the time limit

`run` never waits for a script, so a script that does not end would hold its process, and its share of the host, for ever. At `SCRIPT_SECONDS` after its process started, scripts kills it, and its run is `timed_out`: not `exited`, since the script did not end on its own, and with no exit code. What the script had written by then is kept; what it had buffered and not yet written is lost with the process, so a script that wants a line kept flushes it, as this one does.

```python
import time

print("starting", flush=True)
time.sleep(3600)
print("never printed")
```

That is `main.py` at `<sha>`.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"timed_out","started":"<started>","finished":"<finished>","stdout_bytes":9,"stderr_bytes":0,"truncated":false,"stdout":"starting\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<finished>` is about 60 seconds after `<started>`.

With `SCRIPT_SECONDS` unset, the run is killed about 600 seconds after it started.

Preconditions:

- The preamble's, with `SCRIPT_SECONDS=60` set in `/opt/scripts/etc/env` and scripts restarted since.

Postconditions:

- The catalog's run `<id>` is `timed_out`, with no exit code and `finished` `<finished>`; its page shows it timed out (`S13`).
- No process of the run is still running.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/stdout` is `starting\n`; `stderr` is empty. The folder is otherwise as the script left it.
- When scripts killed the script, about 60 seconds after it started, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"<id>","status":"timed_out","truncated":false}}
  ```

  `duration_us` is about 60000000. scripts wrote nothing to stderr.
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A script's child process is killed with it at the time limit

A script that starts processes of its own does not escape the limit through them: they are in the run's process group, and at `SCRIPT_SECONDS` the group is killed whole, the script and every process it started. A child's output goes where the script's goes, and is kept the same way.

```python
import subprocess

subprocess.run(["python3.12", "worker.py"])
```

That is `main.py` at `<sha>`. Beside it is `worker.py`:

```python
import os
import time

print(os.getpid(), flush=True)
time.sleep(3600)
```

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 15 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"timed_out","started":"<started>","finished":"<finished>","stdout_bytes":<pid bytes>,"stderr_bytes":0,"truncated":false,"stdout":"<pid>\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<pid>` is the process id `worker.py` printed, and `<pid bytes>` the length of that line in bytes; `<finished>` is about 60 seconds after `<started>`.

Preconditions:

- The preamble's, with `SCRIPT_SECONDS=60` set in `/opt/scripts/etc/env` and scripts restarted since.

Postconditions:

- Neither `main.py`'s process nor `worker.py`'s, `<pid>`, is still running: both were killed at the limit, and the run is `timed_out`, with no exit code.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/stdout` is `<pid>\n`.
- When scripts killed the run's process group, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"<id>","status":"timed_out","truncated":false}}
  ```
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A script writes more to its out folder than any limit

`out/` is the run's product, and nothing bounds it: a script writing a large file there is neither killed nor cut short, however the other limits are set, and the file is kept whole. Pruning answers the disk it takes (`S19`).

```python
import os

path = os.path.join(os.environ["IKIGENBA_OUT_DIR"], "big.bin")
with open(path, "wb") as f:
    f.write(bytes(4194304))
print("wrote", os.path.getsize(path))
```

That is `main.py` at `<sha>`. It writes 4194304 bytes, four times `TREE_MAX_BYTES` and 4096 times `OUTPUT_MAX_BYTES` as this story sets them.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 16 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 17 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":14,"stderr_bytes":0,"truncated":false,"stdout":"wrote 4194304\n","stderr":"","files":[{"path":"big.bin","size":4194304}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, with `TREE_MAX_BYTES=1048576` and `OUTPUT_MAX_BYTES=1024` set in `/opt/scripts/etc/env` and scripts restarted since.

Postconditions:

- `state/runs/scr_6d1f4a9b2e8c7035/<id>/out/big.bin` is the 4194304 bytes the script wrote, whole; the run's page offers it for download (`S14`).
- The run is `exited` 0 and not `truncated`.
- When the script exited, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":false}}
  ```
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.

## A script uses more memory than the host allows

scripts sets no memory limit of its own. On a host, the one ceiling is the manifest's `memory_max`, 1 GiB, over scripts' whole service unit: scripts, every git it runs, and every script it runs, together. A script that keeps taking memory past what is left is killed by the kernel, not by scripts, so by the status rule (`S15`) its run is `exited` with `exit_code` 137, 128 plus `SIGKILL`'s 9, not `killed`, which is kept for what scripts itself kills. Since the ceiling is shared, a script that takes much of it leaves less for scripts and for other runs; there is no limit per run and no cap on how many run at once.

```python
chunks = []
while True:
    chunks.append(b"x" * (64 * 1024 * 1024))
```

That is `main.py` at `<sha>`.

Request (run):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 4a8e2c6f0b9d4173c5e1a7f3b9d2c086
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (run):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 18 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Request (result):

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8d4b1f6e2a9c4e07b5d3a1f8c6e2094b
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response (result):

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 19 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","trigger":"manual","status":"exited","exit_code":137,"started":"<started>","finished":"<finished>","stdout_bytes":0,"stderr_bytes":0,"truncated":false,"stdout":"","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's. scripts runs on a host as `ikigenba-scripts.service`, whose memory ceiling is the manifest's `memory_max`, `1G` (`S01`).
- No other run is running, and the script's process is the one the kernel kills when the unit reaches its ceiling.

Postconditions:

- The run is `exited` with exit code 137, `finished` when the kernel killed the script; no process of the run is still running.
- scripts is still serving, and answers every tool as before.
- When the kernel killed the script, with no request around it, telemetry received the run's `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"4a8e2c6f0b9d4173c5e1a7f3b9d2c086","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":137,"run":"<id>","status":"exited","truncated":false}}
  ```
- The `result` call recorded its `request.started`, its `tool.called` with `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"result"}`, and its `request.finished`, under request id `8d4b1f6e2a9c4e07b5d3a1f8c6e2094b` and user `u_7f3a9c21`, and nothing else.
