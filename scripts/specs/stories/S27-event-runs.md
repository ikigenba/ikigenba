# Stories — event runs

What happens when the events app delivers an event to scripts. scripts accepts every event the events app delivers; how the events app reaches it, in what order, and how it retries are the events app's, and no story here fixes the wire of that delivery. An event is a record of eleven members: `id`, which begins `evt_`; `time`; `service`, the service that emitted it; `event`, its lowercase dotted name, `repo.pushed` say; `request_id` and `user`, the request and the user that caused it; `attrs`, a flat object of strings, numbers and booleans; `cause`, the id of the event that caused it, or empty; `depth`; `seq`, its place in the events app's log; and `received`. For every subscription (`S26`) whose `event` is exactly the event's `event`, scripts starts a run of the subscribed script as `run` does without a `ref` (`S08`): the same steps, the same folder `state/runs/<script id>/<run id>/`, the same git and the same process, with the environment `S15` fixes, bounded by the limits `S17` fixes, and pruned as `S19` tells. Such a run differs from one `run` starts in these ways only. Its `user` is the script's owner, whoever the event's `user` is. Its `ref` is the script's ref at delivery, so a later `update` (`S09`) does not move it. Its `input.json` holds the event record, a JSON object of exactly those eleven members, byte for byte as the events app sent it in the delivery. Its `trigger` is `event`, and its run object, as `result`, `runs` and `cancel` answer it (`S11`), has the member `event`, the event's id, right after `trigger`. Its `request_id`, which `result` answers and `IKIGENBA_REQUEST_ID` gives the script (`S15`), is the id of the request by which the events app delivered the event to scripts, the stories' `<delivery request id>`; the event's own `request_id`, that of the request that caused the event, is not carried forward, and the event's `id`, as the run's `event` and as `IKIGENBA_EVENT_ID` (`S15`), is the link back to it. Its `IKIGENBA_EVENT_ID` and `IKIGENBA_EVENT_DEPTH` are the event's `id` and `depth` (`S15`). scripts looks at the event's `event` and `id` only: neither its `attrs`, nor its `service`, nor its `user` decides whether a run starts, so a script subscribed to `repo.pushed` runs for a push to any repository by any user, and a script that cares which reads its input and decides for itself. One subscribed script gets at most one run from one event: scripts knows a run by its script's id and the event's id, so the same event delivered again, which the events app may do, starts no second run of that script, whether its first run is still running, has ended, or has since been pruned (`S19`). A script deleted and created again under the same name is a new script, with a new id, and has had no run from any event. A subscription applies to the events delivered while it exists: an event delivered before the script was subscribed to its name, or after it was unsubscribed (`S26`), starts nothing for that script. An event no subscription names starts nothing and is accepted all the same. While scripts is stopping (`S18`), or while it cannot reach its catalog, a delivery is refused and starts no run for any subscription, so the events app delivers it again later; once a delivery is accepted, every subscribed script has its run. A run that cannot start is a `failed` run with its `reason`, exactly as for `run` (`S08`), and its delivery counts as accepted. Several subscribed scripts each get their own run, with ids and folders of their own, from the one delivery; the order in which they start is not fixed, and no run waits for another.

The actor is the events app delivering an event, and a model working through an MCP client that follows what it started with `runs` and `result` (`S11`), as the subscribed script's owner, with the request shape and result envelope `S05` fixes and on revision `2026-07-28`. scripts is serving on the host (`S02`) with every setting at its default, telemetry takes every event, and it is now `2026-10-05T09:32:00Z`. The catalog holds `S06`'s shared catalog: the caller `u_7f3a9c21` (`mg@example.com`) owns `nightly-report` (`scr_6d1f4a9b2e8c7035`, over `rep_9c2e4b7a1d3f8e05` at `main`, which is `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`), whose seven runs `S11` lists, the newest `run_8a2c6e1f9b3d5074`, `running` since `2026-10-05T09:31:40Z`; `sync-crm` (`scr_a2e7c4f9b1d03856`, over `rep_41d8f0a6b2c97e13` at `main`, which is `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3`), whose one run `run_6b2d8f4a0c9e1735` is `running`; `rotate-keys` (`scr_5c9b1e3a7f2d4068`, over `rep_7b3e9a0c5d1f2846` at `release`, a branch that repository does not have), whose one run is `run_1e9c3a7f5b0d2864`; and `backfill`; and `u_2b8e1d04` (`ann@example.com`) owns `digest` (`scr_3b7f9d1c5e0a2846`, over `rep_d41c7a9e05b28f63` at `main`, which is `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`), whose one run is `run_0c4e8a2f6b1d9375`. Besides, `nightly-report` is subscribed to `repo.pushed`, since `2026-10-05T08:00:00Z`, and no other script is subscribed to anything unless a story says so. The caller has just pushed a commit to `main` of `rep_7b3e9a0c5d1f2846`, repos' `ops-tools`, moving it from `7d5b3f1e9c0a2846b8d4f6e1a3c5b7d9e0f2a4c6` to `5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4`, and repos emitted the event that push made, which the stories call the push event, `evt_8c3f1a6e2d9b4075`:

```
{"id":"evt_8c3f1a6e2d9b4075","time":"2026-10-05T09:31:58.204117Z","service":"repos","event":"repo.pushed","request_id":"6c2e9a4f1b7d3058e2a6c9f4b1d7e305","user":"u_7f3a9c21","attrs":{"new":"5e2a8c4f1b9d7036e2a4c8f0b6d3e1a5c7f9b2d4","old":"7d5b3f1e9c0a2846b8d4f6e1a3c5b7d9e0f2a4c6","ref":"refs/heads/main","repo":"rep_7b3e9a0c5d1f2846"},"cause":"","depth":0,"seq":4182,"received":"2026-10-05T09:31:58.209553Z"}
```

The events app delivers it to scripts at `2026-10-05T09:32:00Z` unless a story says otherwise. `<id>` is the id of the run it starts, `run_` and 16 lowercase hexadecimal digits, and `<started>` the moment of delivery. A run that starts records `run.started`, with `run`, `script`, `sha`, and `trigger` `event`, under its own request id and its user, the script's owner, and its `run.finished` when it ends under the same two (`S16`); a run that cannot start records only its `run.finished`, with `status` `failed`. A delivery records no `tool.called`, since no tool was called; what else the delivery's own request records is not fixed here. A refused delivery records no `run.*` event. Neither the event's `attrs` nor any part of the input is in any event. scripts writes nothing to stderr for any delivery or answer in this group.

## The events app delivers an event a script is subscribed to

The ordinary case: an agent wired `nightly-report` to react to every push, and a push happened. The events app delivers the push event, and scripts starts a run of `nightly-report` from its own ref, `main`, as its owner, with the event as its input; the model sees the run, first among the script's runs, marked as started by that event. The event names a push to another repository, `ops-tools`; the run is `nightly-report`'s all the same, from `nightly-report`'s own repository, since scripts does not look inside the event.

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

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of `{"runs":[...]}` whose first entry is

```
{"id":"<id>","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"running","started":"<started>","truncated":false}
```

followed by the seven entries `S11` shows for `nightly-report`, as they were, each with `trigger` `manual` and no `event`; and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `nightly-report` is subscribed to `repo.pushed`, and the events app delivered the push event once, at `2026-10-05T09:32:00Z`, and scripts accepted it. This call comes after.

Postconditions:

- The catalog has a new run `<id>` of `nightly-report`: script `scr_6d1f4a9b2e8c7035`, sha `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, ref `main`, user `u_7f3a9c21`, request id `<delivery request id>`, trigger `event`, event `evt_8c3f1a6e2d9b4075`, status `running`, started `<started>`. `show` (`S07`) answers it as the script's `last_run`. `nightly-report` and its subscription are as they were.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` holds `input.json`, the push event's record, a JSON object of its eleven members with the values the preamble shows; `tree/`, the tree of `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, read-only; `out/`; and `stdout` and `stderr`. `python3.12 main.py` is running in that `tree/`, in a process group of its own, as `u_7f3a9c21`'s run.
- `run_8a2c6e1f9b3d5074` runs on, untouched. No other script has a new run. Nothing was written under `REPOS_DIR`.
- When the run started, telemetry received its `run.started`:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"<delivery request id>","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","trigger":"event"}}
  ```

  When the script ends, the run's `run.finished` follows under the same request id and user (`S16`). No `tool.called` was recorded for the delivery. The name `nightly-report`, the event's `attrs`, and the event's id are in no event scripts recorded.
- This `runs` call recorded its `request.started`, its `tool.called` with `kind` `read`, `outcome` `ok` and `tool` `runs`, and its `request.finished`, and nothing else.

## A model reads the result of a run an event started

A model follows a run an event started as it follows any run, with `result`, and learns from it which event started it: `trigger` is `event` and `event` is the event's id, which the model can look up with the events app's own tools. The run's `user` is the script's owner and its `ref` the script's. Here the run started by the push event has ended, `exited` 0, having printed one line.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","user":"u_7f3a9c21","request_id":"<delivery request id>","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":5,"stderr_bytes":0,"truncated":false,"stdout":"done\n","stderr":"","files":[]}
```

where `<finished>` is when the script ended; and a `content` array of one text block whose text is that object encoded compactly. The run's input is not in the result; the run's page shows it (`S13`).

Preconditions:

- The preamble's: the events app delivered the push event, which started the run `<id>` of `nightly-report`. Its script printed `done` and exited 0, writing nothing to standard error or `out/`.

Postconditions:

- Nothing has changed. No git ran.

## A script reads the event that started it

A script started by an event finds the event where any run finds its input: in the file `IKIGENBA_INPUT` names (`S15`). It reads it as JSON and finds the event's name, its id, and its attributes, here the repository a push moved and the ref and commits it moved between, and decides from them what to do. scripts does not filter by `attrs`, so a script that should react to one repository only checks `attrs.repo` itself.

`main.py` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`:

```python
import json
import os

with open(os.environ["IKIGENBA_INPUT"]) as f:
    event = json.load(f)
print(event["event"], event["id"])
attrs = event["attrs"]
print(attrs["repo"], attrs["ref"], attrs["old"][:7], attrs["new"][:7])
```

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","user":"u_7f3a9c21","request_id":"<delivery request id>","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":86,"stderr_bytes":0,"truncated":false,"stdout":"repo.pushed evt_8c3f1a6e2d9b4075\nrep_7b3e9a0c5d1f2846 refs/heads/main 7d5b3f1 5e2a8c4\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `main.py` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` is the one shown. The events app delivered the push event, which started the run `<id>` of `nightly-report`, and the run has ended.

Postconditions:

- Nothing has changed by the call. The run's `input.json` still holds the push event's record, which the run's page shows as its Input and offers for download (`S13`, `S14`).

## The events app delivers the same event again

The events app delivers each event at least once, so it may deliver one scripts has already taken, after a delivery whose answer it never got, say. scripts knows the event by its id: a subscribed script that already has a run from that event gets no second one, whether that run is still running, has ended, or has been pruned, and the delivery is accepted as the first was.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 carrying the answer of `The events app delivers an event a script is subscribed to`, as it was before the second delivery: eight entries, the first `<id>` with `event` `evt_8c3f1a6e2d9b4075`, and no other entry with that `event`.

Preconditions:

- The preamble's: the events app delivered the push event at `2026-10-05T09:32:00Z`, which started the run `<id>` of `nightly-report`, still running; then it delivered the same event, `evt_8c3f1a6e2d9b4075`, again, at `2026-10-05T09:32:05Z`. This call comes after both.

Postconditions:

- The second delivery changed nothing: no run was made, nothing was added under `state/runs/`, no git ran and no script started. `<id>` runs on untouched.
- The second delivery recorded no `run.*` event.

## The events app delivers an event no script is subscribed to

An event is delivered to scripts whatever its name, and most names no script reacts to. An event no subscription names starts nothing: no run, no folder, no git. It is accepted all the same, since there is nothing for the events app to retry. Here repos' push event carries a name nothing is subscribed to, `repo.created` say, with the push event's other members.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 carrying the answer `S11`'s `A model lists a script's runs` shows: the seven runs `nightly-report` had, and none started by an event.

Preconditions:

- The preamble's, except that the event the events app delivered at `2026-10-05T09:32:00Z` is `evt_2d7f0b4c9e1a6538`, whose `event` is `repo.created`. No subscription names `repo.created`; `nightly-report`'s names `repo.pushed`, and a subscription matches an event's name exactly, never as a prefix or a pattern.

Postconditions:

- Nothing has changed: no script of any user has a new run, and nothing was added under `state/runs/`. No git ran and no script started.
- The delivery recorded no `run.*` event.

## The events app delivers an event several scripts are subscribed to

Any number of scripts may be subscribed to one event name, and each gets its own run from one delivery: one event, one run per subscribed script. Each run is its own script's, from that script's own repository and ref, with its own id and folder, and each holds the same event as its input. Here the caller has subscribed `sync-crm` to `repo.pushed` too.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"runs","arguments":{"name":"sync-crm"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"runs":[{"id":"<id-2>","sha":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3","ref":"main","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"running","started":"<started>","truncated":false},{"id":"run_6b2d8f4a0c9e1735","sha":"3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3","ref":"main","trigger":"manual","status":"running","started":"2026-10-05T09:31:00Z","truncated":false}]}
```

where `<id-2>` is the id of `sync-crm`'s new run; and a `content` array of one text block whose text is that object encoded compactly. `runs` of `nightly-report`, asked next, answers as in `The events app delivers an event a script is subscribed to`, its first entry `<id>`, with the same `event`.

Preconditions:

- The preamble's, and `sync-crm` is subscribed to `repo.pushed` too. The events app delivered the push event once, and scripts accepted it.

Postconditions:

- The catalog has two new runs, `<id>` of `nightly-report` from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` and `<id-2>` of `sync-crm` from `main` at `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3`, both `running`, both with user `u_7f3a9c21`, trigger `event` and event `evt_8c3f1a6e2d9b4075`. `<id>` and `<id-2>` differ.
- `state/runs/scr_6d1f4a9b2e8c7035/<id>/` and `state/runs/scr_a2e7c4f9b1d03856/<id-2>/` each hold their own `input.json`, both holding the push event's record, and their own `tree/`, `out/`, `stdout` and `stderr`. Both scripts are running, each in its own process group. `run_8a2c6e1f9b3d5074` and `run_6b2d8f4a0c9e1735` run on, untouched.
- telemetry has received a `run.started` with `trigger` `event` for each new run, one with `script` `scr_6d1f4a9b2e8c7035` and one with `script` `scr_a2e7c4f9b1d03856`, in an order that is not fixed.

## The events app delivers an event another user's script is subscribed to

Subscriptions are per script, and a script is its owner's: when scripts of two users are subscribed to one event, each user's script runs as its own owner, and neither user sees the other's run. Who caused the event does not matter: `u_2b8e1d04`'s `digest` runs for `u_7f3a9c21`'s push, as `u_2b8e1d04`, and `u_7f3a9c21`'s `nightly-report` runs as `u_7f3a9c21`. Here `u_2b8e1d04` follows their own run.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"runs","arguments":{"name":"digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"runs":[{"id":"<id-3>","sha":"e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92","ref":"main","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"running","started":"<started>","truncated":false},{"id":"run_0c4e8a2f6b1d9375","sha":"e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92","ref":"main","trigger":"manual","status":"exited","exit_code":0,"started":"2026-10-04T07:00:00Z","finished":"2026-10-04T07:00:05Z","truncated":false}]}
```

where `<id-3>` is the id of `digest`'s new run; and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, and `u_2b8e1d04`'s `digest` is subscribed to `repo.pushed` too. The events app delivered the push event, whose `user` is `u_7f3a9c21`, once, and scripts accepted it.

Postconditions:

- The catalog has two new runs from the event: `<id-3>` of `digest`, user `u_2b8e1d04`, from `main` at `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`; and `<id>` of `nightly-report`, user `u_7f3a9c21`, as in `The events app delivers an event a script is subscribed to`. Both have trigger `event` and event `evt_8c3f1a6e2d9b4075`.
- `digest`'s run acts as `u_2b8e1d04`: its `IKIGENBA_USER_ID` is `u_2b8e1d04` (`S15`), and its `run.started` and `run.finished` carry user `u_2b8e1d04`. Its `input.json` holds the push event's record whole, its `user` `u_7f3a9c21` included.
- `u_7f3a9c21`'s `runs` of `nightly-report` lists `<id>` and not `<id-3>`; `u_7f3a9c21`'s `result` of `<id-3>` is refused with `no run '<id-3>'`, and `u_2b8e1d04`'s `result` of `<id>` with `no run '<id>'` (`S11`).

## The events app delivers an event to a script whose ref names no commit

A run an event starts can fail to start for every reason a run `run` starts can (`S08`), and is then a `failed` run with its `reason`, recorded as `run` records it. The delivery is accepted all the same: the failure is the run's, not the delivery's, and delivering the event again would fail the same way. Here the caller subscribed `rotate-keys`, which tracks `release`, a branch its repository has never had.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id-4>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id-4>","script":"scr_5c9b1e3a7f2d4068","ref":"release","user":"u_7f3a9c21","request_id":"<delivery request id>","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"failed","started":"<started>","finished":"<started>","stdout_bytes":0,"stderr_bytes":0,"truncated":false,"reason":"commit_missing","stdout":"","stderr":"","files":[]}
```

where `<id-4>` is the id of `rotate-keys`' new run, as `runs` of `rotate-keys` answers it first; and a `content` array of one text block whose text is that object encoded compactly. There is no `sha`: the ref never resolved.

Preconditions:

- The preamble's, and `rotate-keys` is subscribed to `repo.pushed` too; `rep_7b3e9a0c5d1f2846` has no branch, tag, or commit `release`. The events app delivered the push event once.

Postconditions:

- The delivery was accepted, and the events app does not deliver the event again. `nightly-report` has its run `<id>` from it, `running`, as in `The events app delivers an event a script is subscribed to`.
- `state/runs/scr_5c9b1e3a7f2d4068/<id-4>/` holds `input.json`, the push event's record, and no `tree/`. No script ran for `rotate-keys`.
- telemetry has received, for `<id-4>`, no `run.started` and one `run.finished`, under user `u_7f3a9c21` and `<delivery request id>`, `<id-4>`'s own, the delivery's:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"<delivery request id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"reason":"commit_missing","run":"<id-4>","status":"failed","truncated":false}}
  ```

## The events app delivers an event while scripts is stopping

scripts starts no new run once it has been told to stop (`S18`), and an event is no exception: a run started now would only be killed at the drain deadline. The delivery is refused, so the events app keeps the event and delivers it again later; a scripts serving again then takes it and starts the run, as for a first delivery. The model, still reaching the draining scripts, sees no new run.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 carrying the answer `S11`'s `A model lists a script's runs` shows: the seven runs `nightly-report` had, and none started by an event.

Preconditions:

- The preamble's. scripts received `SIGTERM` less than 4 seconds before the events app delivered the push event, and is draining, waiting for `run_8a2c6e1f9b3d5074`; this call reaches it after the delivery, within the drain.

Postconditions:

- The delivery was refused: no run was made for `nightly-report` or any script, nothing was added under `state/runs/`, no git ran and no script started. The delivery recorded no `run.*` event.
- `run_8a2c6e1f9b3d5074` is untouched by the delivery.
- The events app delivers the push event again. Once a scripts is serving again and accepts it, `nightly-report` has one run from it, as in `The events app delivers an event a script is subscribed to`, its `started` the moment of that delivery.

## The events app delivers an event while scripts cannot reach its catalog

scripts finds the subscriptions in its catalog, so when it cannot read the catalog it cannot tell which scripts an event concerns, nor record a run. It does not treat the event as one nothing is subscribed to, which would lose the run for good; it refuses the delivery, starts nothing, and the events app delivers the event again later. A model calling a tool meanwhile is answered `cannot reach the catalog; try again later` (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: runs

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"runs","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 carrying the answer `S11`'s `A model lists a script's runs` shows: the seven runs `nightly-report` had, and none started by an event.

Preconditions:

- The preamble's. scripts' database could not be read when the events app delivered the push event, its storage failing reads, say; it can be read again by this call, which comes before the events app delivers the event again.

Postconditions:

- The delivery was refused: no run was made, nothing was added under `state/runs/`, no git ran and no script started. The delivery recorded no `run.*` event. scripts is still serving.
- When the events app delivers the push event again and scripts accepts it, `nightly-report` has one run from it, as in `The events app delivers an event a script is subscribed to`.

## A run an event started runs past the time limit

A run an event starts is a run like any other, under the same limits (`S17`): its tree is bounded by `TREE_MAX_BYTES`, its output by `OUTPUT_MAX_BYTES`, each git by `OPERATION_SECONDS`, and the script by `SCRIPT_SECONDS`. Nobody is waiting on such a run, so the time limit is what stops a script that never ends. It is pruned as any run is (`S19`), and counts among its script's runs alike. The owner ends one early with `cancel` (`S11`), as any of their runs.

`main.py` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`:

```python
import time

print("starting", flush=True)
time.sleep(3600)
```

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","user":"u_7f3a9c21","request_id":"<delivery request id>","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"timed_out","started":"<started>","finished":"<finished>","stdout_bytes":9,"stderr_bytes":0,"truncated":false,"stdout":"starting\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<finished>` is about 600 seconds after `<started>`.

Preconditions:

- The preamble's: `main.py` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` is the one shown, and `SCRIPT_SECONDS` is unset. The events app delivered the push event, which started the run `<id>` of `nightly-report`, more than 600 seconds before this call.

Postconditions:

- The run `<id>` is `timed_out`, with no exit code; no process of it is still running. Its folder's `stdout` is `starting\n`.
- When scripts killed the script, telemetry received the run's `run.finished`, under the run's own request id, the delivery's, and its user:

  ```
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"<delivery request id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"run":"<id>","status":"timed_out","truncated":false}}
  ```

- scripts pruned when the run ended, as when any run ends (`S19`).
