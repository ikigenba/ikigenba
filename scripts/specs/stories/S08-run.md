# Stories — run

`run`, the tool that starts a run of one of the caller's scripts and answers at once, without waiting for the script. It takes, in this order, `name`, required, a string, the name of one of the caller's scripts; `ref`, optional, a string: a branch, a tag, or a sha; and `input`, optional, a JSON object. Without `ref` it takes the script's own ref (`S06`); with one, that ref is used for this run only and the script's ref is unchanged. A call does these things in this order. It looks the script up among the caller's own scripts (`S05`), and refuses the call while scripts is stopping (`S18`). It gives the run its id, `run_` and 16 lowercase hexadecimal digits, minted now and never changed, and its own folder, `state/runs/<script id>/<run id>/`, and writes there `input.json`, the `input` argument byte for byte as it appears in the request, with nothing added, or `{}` without one; scripts never looks inside it. In the script's repository, `<REPOS_DIR>/<repo>.git`, it resolves the ref with `git --git-dir=<dir> rev-parse --verify -q --end-of-options '<ref>^{commit}'` and unpacks the commit's tree with `git --git-dir=<dir> archive --format=tar <sha>` into the folder's `tree/`, file for file and byte for byte as `git archive` emits it, with no `.git`; then it removes write permission from every file and directory under `tree/`, and makes `out/`, empty. It starts `python3.12 main.py` in `tree/`, in its own process group, with the environment `S15` fixes, the script's standard output going to the folder's `stdout` and its standard error to `stderr`; and it answers. The answer has `id`, `status`, `sha`, and `reason`, in that order: for a run that started, its id, `running`, and the sha it runs. The run is recorded with its `script`, the script's id; its `sha`; its `ref`, the ref as given or, without one, the script's; its `user`, the caller's `X-User-Id`; its `request_id`, the call's request id (`S02`); its `trigger`, `manual`, which marks a run `run` started; and `started`, the time of the call. A run is also started, the same way but as the script's owner, at the script's own ref, with the event as its input and the trigger `event`, when the events app delivers an event the script is subscribed to (`S27`); this group tells the runs `run` starts. From then on the script runs on its own: `result` follows it and `cancel` ends it early (`S11`), and how it ends is `S15`'s and `S17`'s. Every git run is bounded by `OPERATION_SECONDS`, and the unpack by `TREE_MAX_BYTES` (`S17`). The repository's owner is not checked again: the script runs whatever its repository holds. A run that cannot start is not a refusal but a run: it is recorded `failed` with a `reason`, its folder holds what was produced, `input.json` always, and `run` answers its id, `failed`, its sha when the ref had resolved, and the reason, as a successful result. Those failures are told one per story below, but for a tree over `TREE_MAX_BYTES`, `too_large`, which is `S17`'s. A failed run has a `finished` time, the moment its failure was recorded, no `exit_code`, and no output of the script's: `result` answers its `stdout` and `stderr` as `""` unless a story says otherwise. Runs are concurrent: nothing serializes them, across scripts or within one.

The actor, the request shape, the result envelope, and the fixture are those of `S06`; the catalog is `S06`'s shared catalog. The caller `u_7f3a9c21` owns `nightly-report` (`scr_6d1f4a9b2e8c7035`, repository `rep_9c2e4b7a1d3f8e05`, ref `main`), whose run `run_8a2c6e1f9b3d5074`, started `2026-10-05T09:31:40Z` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, is still `running`; `rotate-keys` (`scr_5c9b1e3a7f2d4068`, repository `rep_7b3e9a0c5d1f2846`, ref `release`); and `backfill` (`scr_e8f2a6c0d4b19357`, repository `rep_0f6a2d9e8c4b7153`, ref `main`, never run). `u_2b8e1d04` owns `digest` (`scr_3b7f9d1c5e0a2846`). Under `../repos/state/repos`, in `rep_9c2e4b7a1d3f8e05`, `main` is at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` and the tag `v1` at `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`, and `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60` is an earlier commit; `rep_7b3e9a0c5d1f2846` has no `release`; there is no `rep_0f6a2d9e8c4b7153.git`. Each commit of `rep_9c2e4b7a1d3f8e05` has a `main.py` at its root. scripts runs with `REPOS_DIR` unset, `TREE_MAX_BYTES` `268435456` and `OPERATION_SECONDS` `600`, the manifest's values (`S01`), and `python3.12` on its `PATH`; now is `2026-10-05T09:32:00Z`. `run` is of kind `additive`. A run that starts records `run.started`, with `run`, the run's id, `script`, the script's id, `sha`, and `trigger`, when its process starts and before the call's `tool.called`; its `run.finished` comes when it ends, under the same user and request id (`S16`). A run that could not start records no `run.started`, only `run.finished`, with `run`, `status` `failed`, `duration_us`, the whole microseconds from the run's start to its recorded failure, `truncated` `false`, and `reason`, before the call's `tool.called`, whose `outcome` is `ok`. The script's name and the run's input are in no event. A refusal makes no run and no folder, runs no git, and records no `run.*` event. scripts writes nothing to stderr for any answer in this group.

## A model runs a script

The ordinary case: `nightly-report` tracks `main`, so a run with no `ref` resolves `main`, unpacks its commit into a folder of the run's own, and starts the script there. With no `input`, `input.json` is `{}`. The answer comes as soon as the process has started; the script has not finished, and may not have printed anything yet.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id.

Preconditions:

- The preamble's.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`: script `scr_6d1f4a9b2e8c7035`, sha `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, ref `main`, user `u_7f3a9c21`, request id `<request-id>`, the request's id, trigger `manual`, status `running`, started `<started>`, the time of the call, and no `finished`. `runs` (`S11`) lists it first, and `show` (`S07`) answers it as the script's `last_run`, `{"id":"<id>","status":"running","started":"<started>"}`. `nightly-report`'s ref is still `main`.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` exists and holds `input.json`, whose content is exactly `{}`; `tree/`, the tree of `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` as `git archive` emitted it, with no write permission on any file or directory under it; `out/`, empty when the script started; and `stdout` and `stderr`, where the script's output goes.
- `python3.12 main.py` is running in that `tree/`, in a process group of its own; the call did not wait for it to end.
- `run_8a2c6e1f9b3d5074` runs on, untouched. The repository is untouched.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"run"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  When the script ends, the run's `run.finished` follows, under user `u_7f3a9c21` and `<request-id>` (`S16`). The name `nightly-report` is in none of them.

## A model runs a script with input

A model hands a script what to work on as `input`, a JSON object, which the script reads from the file its `IKIGENBA_INPUT` names (`S15`). scripts writes it as it came, byte for byte as it appears in the request, and never reads, reorders, or reformats it; here that is 69 bytes. An object of any shape is accepted, `{}` included.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","input":{"since":"2026-10-04","channels":["sales","support"],"dry_run":false}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, `running`, at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, ref `main`.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/input.json` holds exactly these 69 bytes, with no LF after them:

  ```
  {"since":"2026-10-04","channels":["sales","support"],"dry_run":false}
  ```

- The script is running with the folder's `tree/` as its working directory.
- telemetry has received, between the request's `request.started` and its `tool.called`, `run.started` with attributes `{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","trigger":"manual"}`; the input is in no event.

## A model runs a script from a ref it does not track

A model can run any branch, tag, or commit of the script's repository once, to try a change before moving the script to it, say, or to rerun a release, without changing what the script tracks. Here `nightly-report`, which tracks `main`, is run from the tag `v1`. The run records the ref as the model gave it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","ref":"v1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, `running`, sha `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`, ref `v1`. `nightly-report`'s ref is still `main`, and its next run with no `ref` resolves `main` again.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/tree/` holds the tree of `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`.
- scripts recorded no `script.updated`. telemetry has received, between the request's `request.started` and its `tool.called`, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37","trigger":"manual"}}
  ```

## A model runs a script from a commit sha

A model that knows exactly which commit it wants, to reproduce an earlier run, say, names it by its sha. The sha is resolved like any ref, so it must be a commit in the script's repository; the script's ref is unchanged, and the run records the sha as its ref, as the model gave it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","ref":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60` is a commit in `rep_9c2e4b7a1d3f8e05`.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, `running`, sha `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60`, ref `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60`. `nightly-report`'s ref is still `main`.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/tree/` holds the tree of `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60`.
- telemetry has received `run.started` with attributes `{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60","trigger":"manual"}`.

## A model runs a script while another run of it is running

Nothing serializes runs: a script may run any number of times at once, each run in its own folder, from its own tree, writing only to its own `out/`, `stdout`, and `stderr`, within its own folder. A new run does not wait for one in flight, does not end it, and is not refused for it. Here `run_8a2c6e1f9b3d5074` of `nightly-report` has been running since `2026-10-05T09:31:40Z` when the model runs the script again.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is not `run_8a2c6e1f9b3d5074`.

Preconditions:

- The preamble's: `run_8a2c6e1f9b3d5074` is `running`, its process alive, its folder `state/runs/scr_6d1f4a9b2e8c7035/run_8a2c6e1f9b3d5074/`.

Postconditions:

- Both runs are `running`, both at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, and both processes are alive, each in its own process group. `runs` (`S11`) lists `<id>` first, then `run_8a2c6e1f9b3d5074`.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` holds the new run's own `input.json`, `tree/`, and `out/`, `stdout`, and `stderr`; `run_8a2c6e1f9b3d5074`'s folder, its output so far, and its `out/` are untouched.
- telemetry has received `run.started` for `<id>` only; `run_8a2c6e1f9b3d5074` recorded nothing.

## A model runs a script whose ref names no commit

A branch that was never pushed, a tag that was deleted, a mistyped sha: a ref git cannot resolve to a commit in the script's repository makes a run that fails before the script starts. It is still a run, so the trail and the script's runs show the attempt. Here `rotate-keys` tracks `release`, a branch `rep_7b3e9a0c5d1f2846` has never had. A `ref` argument that names no commit fails the same way, recorded with that ref; one git would not accept as a name at all, `..bad` say, names no commit either and fails the same way, with `..bad` as the run's ref.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"run","arguments":{"name":"rotate-keys"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","reason":"commit_missing"}
```

and a `content` array of one text block whose text is that object encoded compactly. There is no `sha`: the ref never resolved.

Preconditions:

- The preamble's: `rep_7b3e9a0c5d1f2846` has no branch, tag, or commit `release`.

Postconditions:

- The catalog has a new run `<id>` of `rotate-keys`: script `scr_5c9b1e3a7f2d4068`, no sha, ref `release`, user `u_7f3a9c21`, request id `<request-id>`, trigger `manual`, status `failed`, reason `commit_missing`, no `exit_code`, started and finished at the time of the call. `show` (`S07`) answers it as `rotate-keys`' `last_run`. `rotate-keys` is otherwise as it was, its ref still `release`, and `run_1e9c3a7f5b0d2864` is as it was.
- `state/runs/scr_5c9b1e3a7f2d4068/<id>/` holds `input.json`, `{}`; there is no `tree/`. No script ran.
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`), and no `run.started`:

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"reason":"commit_missing","run":"<id>","status":"failed","truncated":false}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"run"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model runs a script whose repository is gone

The script names a repository that is no longer under `REPOS_DIR`: repos has deleted it, or repos is not installed. The script is not deleted with it, and a run of it fails before git is asked anything, naming no sha. `backfill` names `rep_0f6a2d9e8c4b7153`, which repos has deleted.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"run","arguments":{"name":"backfill"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","reason":"repository_missing"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `../repos/state/repos/rep_0f6a2d9e8c4b7153.git` does not exist, and `backfill` has never run.

Postconditions:

- The catalog has `backfill`'s first run, `<id>`: script `scr_e8f2a6c0d4b19357`, no sha, ref `main`, status `failed`, reason `repository_missing`, started and finished at the time of the call. `backfill` itself is still in the catalog, its repository still `rep_0f6a2d9e8c4b7153`.
- `state/runs/scr_e8f2a6c0d4b19357/<id>/` holds `input.json`, `{}`; there is no `tree/`. No git ran and no script ran.
- telemetry has received, between the request's `request.started` and its `tool.called`, whose `outcome` is `ok`, `run.finished` with attributes `{"duration_us":<n>,"reason":"repository_missing","run":"<id>","status":"failed","truncated":false}`, and no `run.started`.

## A model runs a script and git takes too long

Every git run scripts makes is bounded by `OPERATION_SECONDS`, so a repository on failing storage cannot hold a `run` call open for ever. A git still running when the time is up is killed, and the run fails with the reason `timed_out`, the word a run killed at `SCRIPT_SECONDS` also has as its status (`S17`); a reason stands only beside the status `failed`, so the two are never confused. The same reason comes whichever git run it was: killed at the resolve, the run has no sha; killed at the unpack, as here, it has the sha the ref had resolved to. What the unpack had written before git was killed stays in `tree/` as git left it; no script ran from it. A git still running when a stopping scripts reaches its drain deadline is another matter: that call is cut off without an answer and records no run (`S18`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","reason":"timed_out"}
```

and a `content` array of one text block whose text is that object encoded compactly. The answer comes about 600 seconds after the call.

Preconditions:

- The preamble's, except that `git archive` of `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` is still running 600 seconds after it started, the repository's storage stalling, say.

Postconditions:

- The git was killed; no git scripts started for the call is still running, and no script ran.
- The catalog has a new run `<id>` of `nightly-report`, sha `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, ref `main`, status `failed`, reason `timed_out`, no `exit_code`, finished when git was killed.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` holds `input.json`, `{}`, and whatever of `tree/` git had written.
- telemetry has received, between the request's `request.started` and its `tool.called`, whose `outcome` is `ok`, `run.finished` with attributes `{"duration_us":<n>,"reason":"timed_out","run":"<id>","status":"failed","truncated":false}`, and no `run.started`.

## A model runs a script and git fails

When git itself fails, a damaged object in the repository, say, scripts cannot say more than git did, so it keeps git's own words where the run's reader looks for what went wrong: in the run's `stderr`, exactly as git wrote them, unprefixed. They go nowhere else: not into the answer, not into an event, and not to scripts' own stderr (`S02`). The run fails with the reason `git_failed`, and has the sha when the ref had resolved, as here, where the unpack failed.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","reason":"git_failed"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, except that an object of the commit at `main` is damaged, so `git --git-dir=../repos/state/repos/rep_9c2e4b7a1d3f8e05.git archive --format=tar e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` exits 128 having written exactly these two lines to stderr:

  ```
  error: inflate: data stream error (incorrect header check)
  fatal: unable to read tree 7e1f0a3c5b9d2e4f6a8c0b1d3e5f7a9c2b4d6e8f
  ```

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, sha `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, ref `main`, status `failed`, reason `git_failed`, no `exit_code`, `stdout_bytes` 0, `stderr_bytes` 127.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` holds `input.json`, `{}`; whatever of `tree/` git had written; and `stderr`, holding exactly git's two lines, each ending with LF, 127 bytes. `result` (`S11`) answers them as its `stderr`, and `stdout` as `""`. No script ran.
- scripts wrote nothing to stderr. telemetry has received, between the request's `request.started` and its `tool.called`, whose `outcome` is `ok`, `run.finished` with attributes `{"duration_us":<n>,"reason":"git_failed","run":"<id>","status":"failed","truncated":false}`, and no `run.started`; git's words are in no event.

## A model runs a script whose process cannot start

The ref resolved and the tree is whole, but the process cannot be launched: `python3.12` was found on the `PATH` when scripts started (`S22`), but has since been removed from the host, say. The script never started, so the run fails with the reason `start_failed`, with the sha it would have run, and its tree is kept as it was unpacked.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","reason":"start_failed"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, except that `python3.12` has been removed from every directory on scripts' `PATH` since scripts started.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`, sha `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, ref `main`, status `failed`, reason `start_failed`, no `exit_code`.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` holds `input.json`, `{}`; `tree/`, the whole tree of `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, read-only; and `out/`, empty. No process of the run is alive.
- scripts is still serving and wrote nothing to stderr. telemetry has received, between the request's `request.started` and its `tool.called`, whose `outcome` is `ok`, `run.finished` with attributes `{"duration_us":<n>,"reason":"start_failed","run":"<id>","status":"failed","truncated":false}`, and no `run.started`.

## A model runs another user's script

Only a script's owner runs it. Another user's script does not exist for the caller, and gets exactly the answer a script that does not exist gets; no run is made and no git runs.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"run","arguments":{"name":"digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'digest'
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s, with one run, `run_0c4e8a2f6b1d9375`.

Postconditions:

- Nothing has changed: `digest` has the one run it had, and nothing was added under `state/runs/scr_3b7f9d1c5e0a2846/`.
- scripts recorded no `run.*` event; the request's `tool.called` has `kind` `additive` and `outcome` `error`, as for a script that does not exist.

## A model runs a script it does not have

A name among none of the caller's scripts is refused, quoting it as sent, before any git runs.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"run","arguments":{"name":"cleanup"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no script named 'cleanup'
```

Preconditions:

- The preamble's: no script is named `cleanup`.

Postconditions:

- Nothing has changed: no run was made and nothing was added under `state/runs/`.
- scripts recorded no `run.*` event. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"run"}}
  ```

## A model calls run without saying which script

`name` is required. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"run","arguments":{"ref":"main"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: no run was made. scripts recorded no `run.*` event; the request's `tool.called` has `tool` `run`, `kind` `additive`, `outcome` `invalid_arguments`, and `duration_us` 0.

## A model runs a script with input that is not an object

`input` is a JSON object, whatever its members; a string, a number, a boolean, or an array is refused as the arguments are read, with the platform's wording (`S05`), and no run is made. Here the model sent the date it meant to put inside an object as `input` itself; an array would be refused with `input: expected object, got array`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","input":"2026-10-04"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
input: expected object, got string
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: no run was made, nothing was added under `state/runs/scr_6d1f4a9b2e8c7035/`, and no git ran. scripts recorded no `run.*` event; the request's `tool.called` has `tool` `run`, `kind` `additive`, `outcome` `invalid_arguments`, and `duration_us` 0.
