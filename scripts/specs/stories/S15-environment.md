# Stories — environment

What a script finds when it runs: the process scripts starts for a run, what it is given, where it may write, and how its end becomes the run's status. Once `run` has resolved the ref and unpacked the commit into the run's `tree/`, and a slot is free (`S08`), scripts starts the command `python3.12 main.py`, the host's `python3.12` found on scripts' own `PATH` (`S22`), with the run's `tree/` as its working directory. The tree is the commit's files exactly as `git archive` emits them, with no `.git` and no history; `main.py` at the repository's root is the entry point, and everything else in the repository is the script's own business: modules it imports, data it reads. The process runs as scripts' own unix user and group, whatever the script holds. Its standard output and standard error go to the run's `stdout` and `stderr`, each kept up to `OUTPUT_MAX_BYTES` (`S17`), and its standard input is empty. Its environment is scrubbed: it holds exactly these fourteen variables and no others. `PATH` is scripts' own, exactly as scripts was given it. `HOME` is the run's folder, the same as `IKIGENBA_RUN_DIR`. `LANG` is `C.UTF-8`, so the script reads and writes text as UTF-8 whatever the host's locale. `IKIGENBA_RUN_ID` is the run's id. `IKIGENBA_SCRIPT` is the script's id, `scr_` and 16 lowercase hexadecimal digits, not its name. `IKIGENBA_SHA` is the 40 lowercase hexadecimal digits of the commit the run resolved, the `sha` `run` answered. `IKIGENBA_RUN_DIR` is the run's folder. `IKIGENBA_OUT_DIR` is the run's `out/` folder, empty when the script starts, where the script leaves what it produces: its files are what `result` lists (`S11`) and the run's page offers for download (`S13`, `S14`). `IKIGENBA_INPUT` is the run's `input.json`, which is always written before the script starts: the `input` argument of `run` byte for byte as it appears in the request, when it was given, and `{}` otherwise (`S08`), or, for a run an event started, the event record, byte for byte as the events app sent it in the delivery (`S27`). `IKIGENBA_USER_ID` and `IKIGENBA_REQUEST_ID` are the run's user and request id: for a run `run` started, the `run` call's `X-User-Id` and `X-Request-Id`; for a run an event started, the script's owner, whoever caused the event, and the id of the request by which the events app delivered the event to scripts, not the event's own `request_id` (`S27`). `IKIGENBA_EVENT_ID` and `IKIGENBA_EVENT_DEPTH` are, for a run an event started, the event's `id` and its `depth` in decimal; for a run `run` started, the empty string and `0`. A script that calls a sibling sends them on, as it sends its user and request id, as `X-Event-Cause` and `X-Event-Depth` (`A script started by an event calls a sibling service with the event as cause`). `IKIGENBA_SERVICES` is the path of the services file, exactly as scripts was given it (`S02`). The four path variables, `HOME`, `IKIGENBA_RUN_DIR`, `IKIGENBA_OUT_DIR`, and `IKIGENBA_INPUT`, name real places as the script sees them, each a full path that holds from any working directory; nothing promises what those paths look like, or how they relate to one another or to where scripts keeps the run on its disk (`S20`), beyond `HOME` being `IKIGENBA_RUN_DIR`. So a script finds its input, its out folder, and its run's folder only through the variables, and its repository's files only as its working directory. The tree is read-only to the script: before the script starts, every file and directory under it has lost its write permission, a guard against an accidental write. The script writes to its out folder and to `/tmp`. The script runs in its own process group, and in a control group of its own that holds the run's limits (`S17`); scripts kills the process group whole, children included, whenever it ends a run itself: on `cancel` (`S11`), at `SCRIPT_SECONDS` (`S17`), at the drain deadline (`S18`), or when its script is deleted (`S10`). A run is `main.py`'s: it ends when `main.py`'s own process exits, not when every process it started has, and scripts then kills whatever `main.py` left running, in the run's process group or anywhere else in its control group, and removes the control group, so nothing a script starts outlives its run. Runs are concurrent, across scripts and within one script, up to `RUN_MAX_ACTIVE` at once; a run beyond that waits for a slot, as `S08` tells. Each has its own folder, tree, input, and out folder. When `main.py`'s process ends on its own, the run is `exited`, with that process's exit code, or, when the process died of a signal scripts did not send, 128 plus the signal's number; `killed`, `timed_out`, and `failed` are told in `S11`, `S10`, `S17`, `S18`, and `S08`. A script reaches the suite as a sibling service does, through the services file, and nothing more is built for it.

The actor is a model working through an MCP client, which starts a run with `run`, or has one started by an event its script is subscribed to (`S27`), and follows it with `result` until its status is final, with the request shape and result envelope `S05` fixes; the stories show what its script saw and produced. scripts is serving on the host (`S02`), started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, and that file holds the suite's services file (`S03`), whose entry named `repos` has the socket `/run/ikigenba/repos.sock`; telemetry takes every event. Every setting is its default but `RUN_MAX_ACTIVE`, which is 4. The catalog holds `S06`'s shared catalog, among it the caller `u_7f3a9c21`'s script `nightly-report`, `scr_6d1f4a9b2e8c7035`, over the repository `rep_9c2e4b7a1d3f8e05` at `main`, whose run `run_8a2c6e1f9b3d5074` is still `running` and runs on undisturbed by every story here, as `sync-crm`'s run `run_6b2d8f4a0c9e1735` does; with those two running and `RUN_MAX_ACTIVE` 4, a run a story starts starts at once and is never queued (`S08`). The owner has since pushed one commit to the repository's `main`, `<sha>`, whose files each story shows; the commit holds those files and no others. The `run` call carries `X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359` unless a story says otherwise, and that is the request id its run keeps; a `result` call's own request id is not fixed. `<id>` is the new run's id, `run_` and 16 lowercase hexadecimal digits, and `<started>` and `<finished>` its times. A run here records `run.started` in the `run` call's request, after its `request.started` and before its `tool.called`, and `run.finished` whenever the script ends, possibly before the `run` call is answered, under the `run` call's request id and user (`S16`). Each `result` call records its own `request.started`, `tool.called` with `tool` `result`, `kind` `read`, and `outcome` `ok`, and `request.finished`, and nothing else. After every run ends scripts prunes (`S19`); nothing in the fixture is past keeping. What a script writes goes into its run, never to scripts' stderr, and scripts writes nothing to stderr in this group.

## A model runs a script that prints its environment

A script's author needs to know what the script is given, and the variables are all of it: a script finds its run, its input, its out folder, and the suite through them alone. This script prints every variable it can see, by name, one per line.

`main.py`:

```python
import os

for name in sorted(os.environ):
    print(f"{name}={os.environ[name]}")
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
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
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
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
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":<bytes>,"stderr_bytes":0,"truncated":false,"stdout":"<stdout>","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<stdout>` is these fourteen lines, each ending with LF, and `stdout_bytes`, `<bytes>`, is their length in bytes:

```
HOME=<run folder>
IKIGENBA_EVENT_DEPTH=0
IKIGENBA_EVENT_ID=
IKIGENBA_INPUT=<input file>
IKIGENBA_OUT_DIR=<out folder>
IKIGENBA_REQUEST_ID=e7a3c9f1b5d24e68a0c4f8b2d6e1a359
IKIGENBA_RUN_DIR=<run folder>
IKIGENBA_RUN_ID=<id>
IKIGENBA_SCRIPT=scr_6d1f4a9b2e8c7035
IKIGENBA_SERVICES=/var/lib/ikigenba/services.json
IKIGENBA_SHA=<sha>
IKIGENBA_USER_ID=u_7f3a9c21
LANG=C.UTF-8
PATH=<path>
```

`<run folder>` names the run's folder, the same in both lines; `<out folder>` names its out folder, an empty directory when the script started; `<input file>` names the file holding the run's input, here `{}`; `<path>` is scripts' own `PATH`, exactly as scripts was given it. `IKIGENBA_EVENT_ID` is empty, with nothing after its `=`, and `IKIGENBA_EVENT_DEPTH` is `0`, since `run` started the run, not an event. Each of the three paths names a place that exists while the script runs, as a full path; what the paths look like is not fixed. There is no other line: none of scripts' own settings, no `DRAIN_SECONDS`, no other variable systemd gave scripts (`NOTIFY_SOCKET`, `LISTEN_*`, …), and nothing else of scripts' environment is passed on, and no caller's email is in it.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` of `nightly-report` is recorded `exited` with exit code 0, at `<sha>` from `main`, as user `u_7f3a9c21` under request id `e7a3c9f1b5d24e68a0c4f8b2d6e1a359`, trigger `manual`. Its folder, `state/runs/scr_6d1f4a9b2e8c7035/<id>/`, holds its `tree/` with `main.py` alone, its empty `out/`, its `input.json` holding `{}`, and its `stdout`, the fourteen lines.
- telemetry has received the run's `run.started`, in the `run` call's request, and, when the script ended, its `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":false}}
  ```

- No variable reached the script that the preamble does not name, and scripts wrote nothing to stderr.

## A script reads the input its run was given

A run's input is a file, not an argument list and not standard input: the model passes `input`, a JSON object, and the script reads the file `IKIGENBA_INPUT` names, from wherever in its code it needs it. scripts writes the file byte for byte as the input appears in the request, never reading or reformatting it (`S08`); a script reads it as JSON all the same, since what the model sends is JSON, not a layout.

`main.py`:

```python
import json
import os

with open(os.environ["IKIGENBA_INPUT"]) as f:
    data = json.load(f)
print(data["since"], ",".join(data["regions"]))
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","input":{"since":"2026-10-04","regions":["eu","us"]}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":17,"stderr_bytes":0,"truncated":false,"stdout":"2026-10-04 eu,us\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0. Its folder's `input.json` holds exactly the 44 bytes `{"since":"2026-10-04","regions":["eu","us"]}`, as they appear in the request, which the run's page shows as its Input and offers for download (`S13`, `S14`).
- The input is in no event: the `run.started` and `run.finished` telemetry has received carry only the ids, status, and numbers the preamble names.

## A script run without input reads an empty object

`input.json` is always written, so a script never has to ask whether it has input. A run asked for without `input` gets `{}`, and a script reading it as JSON gets an empty object.

`main.py`:

```python
import json
import os

with open(os.environ["IKIGENBA_INPUT"]) as f:
    print(json.load(f))
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
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
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":3,"stderr_bytes":0,"truncated":false,"stdout":"{}\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0, and its folder's `input.json` holds `{}`.

## A script started by an event finds the event as its input

A script subscribed to an event (`S26`) is started by the events app's delivery, not by a model's `run` (`S27`), and finds the event where every run finds its input: in the file `IKIGENBA_INPUT` names. It reads it as JSON, an object of the event record's eleven members, and finds in it what happened and who caused it. It runs as its owner, so `IKIGENBA_USER_ID` is the owner's id, whoever caused the event; here that is the same user. The rest of its environment is as for any run.

`main.py`:

```python
import json
import os

with open(os.environ["IKIGENBA_INPUT"]) as f:
    event = json.load(f)
print(os.environ["IKIGENBA_USER_ID"])
print(sorted(event))
print(event["event"], event["user"], event["attrs"]["repo"])
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":31,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 31 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"<delivery request id>","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":158,"stderr_bytes":0,"truncated":false,"stdout":"u_7f3a9c21\n['attrs', 'cause', 'depth', 'event', 'id', 'received', 'request_id', 'seq', 'service', 'time', 'user']\nrepo.pushed u_7f3a9c21 rep_7b3e9a0c5d1f2846\n","stderr":"","files":[]}
```

where `<delivery request id>` is the run's request id, that of the request by which the events app delivered the event to scripts (`S27`); and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone. `nightly-report` is subscribed to `repo.pushed` (`S26`), and the events app delivered the push event `S27`'s preamble shows, `evt_8c3f1a6e2d9b4075`, whose `user` is `u_7f3a9c21` and whose `attrs.repo` is `rep_7b3e9a0c5d1f2846`. That started the run `<id>` of `nightly-report`, which has ended. No model called `run`.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0, as user `u_7f3a9c21`, trigger `event`, event `evt_8c3f1a6e2d9b4075`. Its folder's `input.json` holds the event record, byte for byte as the events app sent it, which the run's page shows as its Input (`S13`).
- The event record is in no event scripts recorded: the run's `run.started`, with `trigger` `event`, and its `run.finished` carry only the ids, status, and numbers the preamble names.

## A script started by an event finds the event's id and depth

A script started by an event also finds the event's `id` and `depth` in its environment, without reading its input: `IKIGENBA_EVENT_ID` and `IKIGENBA_EVENT_DEPTH`. They are what it passes on when it calls a sibling (`A script started by an event calls a sibling service with the event as cause`). A run `run` started has them too, empty and `0` (`A model runs a script that prints its environment`). Its `IKIGENBA_REQUEST_ID` is the id of the request by which the events app delivered the event, `<delivery request id>`, not the push's own `6c2e9a4f1b7d3058e2a6c9f4b1d7e305`.

`main.py`:

```python
import os

print(os.environ["IKIGENBA_EVENT_ID"], os.environ["IKIGENBA_EVENT_DEPTH"])
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":32,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 32 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"<delivery request id>","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":23,"stderr_bytes":0,"truncated":false,"stdout":"evt_8c3f1a6e2d9b4075 0\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone. `nightly-report` is subscribed to `repo.pushed` (`S26`), and the events app delivered the push event `S27`'s preamble shows, `evt_8c3f1a6e2d9b4075`, whose `depth` is `0`, in a request whose id is `<delivery request id>`. That started the run `<id>` of `nightly-report`, which has ended.

Postconditions:

- Nothing has changed by the call. The run `<id>` is recorded `exited` with exit code 0, trigger `event`, event `evt_8c3f1a6e2d9b4075`, request id `<delivery request id>`.

## A script reads the files of its own repository

The script's working directory is its tree, so it opens its repository's files by their paths in the repository and imports the modules beside `main.py`, as it would in a checkout. There is no `.git` in the tree: the commit the run is at is `IKIGENBA_SHA`.

`main.py`:

```python
import csv

from fmt import line

with open("data/regions.csv", newline="") as f:
    for row in csv.reader(f):
        print(line(row))
```

`fmt.py`:

```python
def line(row):
    return " | ".join(row)
```

`data/regions.csv`:

```
eu,Frankfurt
us,Virginia
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":29,"stderr_bytes":0,"truncated":false,"stdout":"eu | Frankfurt\nus | Virginia\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py`, `fmt.py`, and `data/regions.csv`, and nothing else.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0.
- Its folder's `tree/` holds `main.py`, `fmt.py`, and `data/regions.csv`, each with the bytes it has at `<sha>`, and nothing else: no `.git`, and nothing the run added. Python could not keep its compiled copy of `fmt.py` beside it in the read-only tree, and ran the module all the same.

## A script tries to write into its tree

The tree is the commit as it was pushed, and it stays that way: a script that writes into its working directory, by mistake or on purpose, is refused by the system, as any program writing where it may not is, and Python raises an `OSError` naming the file. The script ends as its code lets it; here nothing catches the error, so Python prints its traceback to standard error and exits 1. The run is a run that started and ended, `exited` with that code, not one that failed to start. Changing a file the commit holds, `main.py` say, is refused the same way.

`main.py`:

```python
with open("notes.txt", "w") as f:
    f.write("hello\n")
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
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
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":1,"started":"<started>","finished":"<finished>","stdout_bytes":0,"stderr_bytes":<bytes>,"truncated":false,"stdout":"","stderr":"<stderr>","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<stderr>` is Python's traceback, `stderr_bytes` `<bytes>` bytes long, whose last line names an `OSError` and `notes.txt`; which `OSError`, and its wording, are not fixed.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 1.
- Its folder's `tree/` holds `main.py` alone, as it is at `<sha>`; there is no `notes.txt` in the tree or anywhere in the run's folder, and its `out/` is empty.
- telemetry has received the run's `run.started`, in the `run` call's request, and, when the script ended, its `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":1,"run":"<id>","status":"exited","truncated":false}}
  ```

## A script writes its results to its out folder and to /tmp

A script leaves what it produces in its out folder, under any names and subfolders it chooses, and works in `/tmp` for what it does not keep. What is in the out folder when the script ends is the run's product: `result` lists it, and the run's page offers each file for download (`S13`, `S14`), for as long as the run is kept (`S19`). Nothing in `/tmp` is part of the run: `result` does not list it. This script works through a temporary directory of its own under `/tmp`, where Python makes one when no variable names another place, removes it, and writes two files to its out folder.

`main.py`:

```python
import os
import tempfile

out = os.environ["IKIGENBA_OUT_DIR"]
with tempfile.TemporaryDirectory() as tmp:
    scratch = os.path.join(tmp, "regions.txt")
    with open(scratch, "w") as f:
        f.write("region\neu\nus\n")
    with open(scratch) as f:
        regions = f.read()
os.makedirs(os.path.join(out, "charts"))
with open(os.path.join(out, "report.csv"), "w") as f:
    f.write(regions)
with open(os.path.join(out, "charts", "sales.svg"), "w") as f:
    f.write('<svg xmlns="http://www.w3.org/2000/svg"/>\n')
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":0,"stderr_bytes":0,"truncated":false,"stdout":"","stderr":"","files":[{"path":"charts/sales.svg","size":42},{"path":"report.csv","size":13}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `files` lists each file under the out folder by its path there, sorted by path.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0.
- Its folder's `out/` holds `charts/sales.svg`, 42 bytes, and `report.csv`, 13 bytes, holding `region`, `eu`, and `us`, each line ending with LF. Its `tree/` holds `main.py` alone.
- The script's temporary directory is gone from `/tmp`; scripts neither made it nor removed it.

## A script reads its standard input

A run's input is `input.json`, never standard input. A script that reads standard input anyway, a program written for a pipe, say, finds it empty: it reads end of file at once, and does not wait for anything.

`main.py`:

```python
import sys

data = sys.stdin.read()
print(len(data))
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","input":{"since":"2026-10-04"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":2,"stderr_bytes":0,"truncated":false,"stdout":"0\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

The script read nothing from standard input though the run had input; that input is in `input.json`.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0, ended as soon as it started, and its folder's `input.json` holds `{"since":"2026-10-04"}`.

## A script calls a sibling service as the run's user

A running script reaches the suite the way a sibling service does, and nothing more is built for it: it finds the service in the services file `IKIGENBA_SERVICES` names, dials the entry's socket, and posts MCP to `/mcp`, as the gateway does, with no nginx, no gateway, no auth, and no token. It sends the run's user and request id, from `IKIGENBA_USER_ID` and `IKIGENBA_REQUEST_ID`, as `X-User-Id` and `X-Request-Id` on every call, so the sibling acts for the user whose `run` caused the run, sees only what that user may see, and records its work under the request that caused it: a trace of that one request id joins the model's `run`, the run, and every call the script made. The environment holds no email, so the script sends no `X-User-Email`. This script lists the caller's repositories in repos.

`main.py`:

```python
import http.client
import json
import os
import socket


class UnixConnection(http.client.HTTPConnection):
    def __init__(self, socket_path):
        super().__init__("backend")
        self.socket_path = socket_path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.socket_path)


with open(os.environ["IKIGENBA_SERVICES"]) as f:
    services = json.load(f)["services"]
repos = next(s for s in services if s["name"] == "repos")

body = json.dumps({
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "list",
        "arguments": {},
        "_meta": {
            "io.modelcontextprotocol/protocolVersion": "2026-07-28",
            "io.modelcontextprotocol/clientCapabilities": {},
        },
    },
})
conn = UnixConnection(repos["socket"])
conn.request("POST", "/mcp", body, {
    "Content-Type": "application/json",
    "MCP-Protocol-Version": "2026-07-28",
    "Mcp-Method": "tools/call",
    "Mcp-Name": "list",
    "X-User-Id": os.environ["IKIGENBA_USER_ID"],
    "X-Request-Id": os.environ["IKIGENBA_REQUEST_ID"],
})
answer = json.load(conn.getresponse())
for repo in answer["result"]["structuredContent"]["repos"]:
    print(repo["name"])
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 15 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 16 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":34,"stderr_bytes":0,"truncated":false,"stdout":"crm-sync\nnightly-report\nops-tools\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. The names are `u_7f3a9c21`'s repositories, as repos' `list` gives them; `ann@example.com`'s `journal` is not among them.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.
- repos is serving on `/run/ikigenba/repos.sock` and holds `S06`'s shared repositories: `u_7f3a9c21` owns `crm-sync`, `nightly-report`, and `ops-tools`, and `u_2b8e1d04` owns `journal`.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0.
- telemetry has received repos' events for the script's call, under the `run` call's request id and user, between the run's `run.started` and its `run.finished`:

  ```
  {"time":"<time>","service":"repos","event":"request.started","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"repos","event":"tool.called","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"repos","event":"request.finished","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- scripts recorded no event for the call: its only events about the run are `run.started` and `run.finished`, and scripts served no request for the script.

## A script started by an event calls a sibling service with the event as cause

A script started by an event calls a sibling as any script does (`A script calls a sibling service as the run's user`), and also sends the event that started it as the call's cause: `IKIGENBA_EVENT_ID` as `X-Event-Cause` and `IKIGENBA_EVENT_DEPTH` as `X-Event-Depth`, beside `X-User-Id` and `X-Request-Id`. scripts sends nothing for it; the script sends them, as it sends the others. What a sibling does with them is the sibling's: a service that honors them stamps any event it emits to the events app during that call with `cause` `evt_8c3f1a6e2d9b4075` and `depth` `1`, the depth sent plus one, which the events app's `search` shows. Here the call is repos' `list`, which emits no event, so nothing is stamped. The same script started by `run` sends them empty and `0`.

`main.py` is the one of `A script calls a sibling service as the run's user`, but the headers of its call are:

```python
{
    "Content-Type": "application/json",
    "MCP-Protocol-Version": "2026-07-28",
    "Mcp-Method": "tools/call",
    "Mcp-Name": "list",
    "X-User-Id": os.environ["IKIGENBA_USER_ID"],
    "X-Request-Id": os.environ["IKIGENBA_REQUEST_ID"],
    "X-Event-Cause": os.environ["IKIGENBA_EVENT_ID"],
    "X-Event-Depth": os.environ["IKIGENBA_EVENT_DEPTH"],
}
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":33,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 33 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"<delivery request id>","trigger":"event","event":"evt_8c3f1a6e2d9b4075","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":34,"stderr_bytes":0,"truncated":false,"stdout":"crm-sync\nnightly-report\nops-tools\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone. repos is serving on `/run/ikigenba/repos.sock` and holds `S06`'s shared repositories.
- `nightly-report` is subscribed to `repo.pushed` (`S26`), and the events app delivered the push event `S27`'s preamble shows, `evt_8c3f1a6e2d9b4075`, whose `depth` is `0`, in a request whose id is `<delivery request id>`. That started the run `<id>` of `nightly-report`, which has ended.

Postconditions:

- repos received the script's call with `X-User-Id: u_7f3a9c21`, `X-Request-Id: <delivery request id>`, `X-Event-Cause: evt_8c3f1a6e2d9b4075`, and `X-Event-Depth: 0`.
- telemetry has received repos' `request.started`, `tool.called` with `tool` `list`, and `request.finished` for the call, under request id `<delivery request id>` and user `u_7f3a9c21`, between the run's `run.started` and its `run.finished`.
- scripts recorded no event for the call.

## A script prints text outside ASCII

`LANG` is `C.UTF-8`, so Python reads and writes text as UTF-8 whatever the host's own locale, and text outside ASCII reaches the run's output as UTF-8, byte for byte.

`main.py`:

```python
print("Zürich — 東京")
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 17 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 18 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":19,"stderr_bytes":0,"truncated":false,"stdout":"Zürich — 東京\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `stdout_bytes` is 19, the length of the line in UTF-8.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone, encoded in UTF-8.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0, and its folder's `stdout` holds the 19 bytes of `Zürich — 東京` and LF in UTF-8.

## A script exits with an error

A script says it failed the way any program does, with a non-zero exit code, and the run records that code: the run is `exited` with it, and what the script wrote to standard error is kept as the run's `stderr`. A script that ends with an exception Python does not catch exits 1 the same way. The run's page shows the status `exited 3` (`S13`).

`main.py`:

```python
import sys

print("no data for 2026-10-04", file=sys.stderr)
sys.exit(3)
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 19 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 20 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":3,"started":"<started>","finished":"<finished>","stdout_bytes":0,"stderr_bytes":23,"truncated":false,"stdout":"","stderr":"no data for 2026-10-04\n","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 3, and its folder's `stderr` holds `no data for 2026-10-04` and LF.
- telemetry has received the run's `run.started`, in the `run` call's request, and, when the script ended, its `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":3,"run":"<id>","status":"exited","truncated":false}}
  ```

## A script kills itself with a signal

A process that dies of a signal scripts did not send ended on its own as far as scripts is concerned, so the run is `exited`, with the exit code 128 plus the signal's number, the convention shells use: 143 for `SIGTERM`, as here, and 137 for `SIGKILL`, which is also how a run the kernel kills for memory reads (`S17`). `killed` is kept for the runs scripts itself ended (`S11`, `S10`, `S18`). What the script wrote before it died is kept.

`main.py`:

```python
import os
import signal

print("giving up", flush=True)
os.kill(os.getpid(), signal.SIGTERM)
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 21 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 22 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":143,"started":"<started>","finished":"<finished>","stdout_bytes":10,"stderr_bytes":0,"truncated":false,"stdout":"giving up\n","stderr":"","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 143, not `killed`.
- telemetry has received the run's `run.started`, in the `run` call's request, and, when the script ended, its `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":143,"run":"<id>","status":"exited","truncated":false}}
  ```

## A script leaves a process running when it exits

A run is `main.py`'s: it ends when `main.py`'s own process exits, whatever else the script started, and its status is `main.py`'s end, by the status rule. What `main.py` started and left running is in the run's process group and its control group, and scripts kills everything still in the control group as the run ends, and removes the group, so nothing a script starts outlives its run, writes to its output after it, or holds it open. Killing what was left is not ending the run early: the run is `exited` with `main.py`'s code, not `killed`, and `finished` is the moment `main.py` exited. This script starts a ticker that would print a line every second for ever, waits until the ticker has written its process id to the out folder, and exits 0.

`main.py`:

```python
import os
import subprocess
import time

pid_file = os.path.join(os.environ["IKIGENBA_OUT_DIR"], "ticker.pid")
subprocess.Popen(["python3.12", "ticker.py"])
while not os.path.exists(pid_file):
    time.sleep(0.1)
print("main done", flush=True)
```

`ticker.py`:

```python
import os
import time

print("tick", flush=True)
with open(os.path.join(os.environ["IKIGENBA_OUT_DIR"], "ticker.pid"), "w") as f:
    f.write(f"{os.getpid()}\n")
while True:
    time.sleep(1)
    print("tick", flush=True)
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":29,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 29 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 30 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":15,"stderr_bytes":0,"truncated":false,"stdout":"tick\nmain done\n","stderr":"","files":[{"path":"ticker.pid","size":<pid bytes>}]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<pid bytes>` is the length in bytes of the ticker's process id and its LF. `<finished>` is the moment `main.py` exited, within a second of `<started>`; the ticker's second `tick` was never written. A `result` asked for a minute later answers the same object: nothing reached the run's output after it ended.

Preconditions:

- The preamble's: `<sha>` holds `main.py` and `ticker.py`, and nothing else.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 0, `main.py`'s, not `killed` and not `timed_out`, with `finished` `<finished>`.
- The ticker, the process whose id its folder's `out/ticker.pid` holds, is no longer running, and no process of the run is: scripts killed what `main.py` left in the run's control group when `main.py` exited, and removed the group.
- Its folder's `stdout` holds `tick` and `main done`, each with its LF, 15 bytes, and nothing more; its `out/` holds `ticker.pid` alone.
- telemetry has received the run's `run.started`, in the `run` call's request, and, when `main.py` exited, its `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":0,"run":"<id>","status":"exited","truncated":false}}
  ```

## A model runs a script whose repository has no main.py

`main.py` at the repository's root is the entry point, and scripts does not look for it: it unpacks the commit and starts `python3.12 main.py` whatever the tree holds. With no `main.py`, `python3.12` itself says it cannot open the file and exits 2, so the run started and ended, `exited` 2, with Python's complaint as its `stderr`; it is not a run that failed to start. A `main.py` in a subfolder is not the entry point.

`report.py`:

```python
print("report")
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 23 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the new run's id. Then the model follows the run with `result` until its status is final. The last answer is:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":24,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 24 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":2,"started":"<started>","finished":"<finished>","stdout_bytes":0,"stderr_bytes":<bytes>,"truncated":false,"stdout":"","stderr":"<stderr>","files":[]}
```

and a `content` array of one text block whose text is that object encoded compactly. `<stderr>` is one line from `python3.12`, `stderr_bytes` `<bytes>` bytes long with its LF, saying it can't open the file `main.py` in the run's tree, and ending `[Errno 2] No such file or directory`.

Preconditions:

- The preamble's: `<sha>` holds `report.py` alone.

Postconditions:

- The run `<id>` is recorded `exited` with exit code 2 and the sha `<sha>`; its folder's `tree/` holds `report.py` alone, and its `out/` is empty.
- telemetry has received the run's `run.started`, in the `run` call's request, and, when the script ended, its `run.finished`, under the `run` call's request id and user:

  ```
  {"time":"<time>","service":"scripts","event":"run.started","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"run":"<id>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","trigger":"manual"}}
  {"time":"<time>","service":"scripts","event":"run.finished","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"exit_code":2,"run":"<id>","status":"exited","truncated":false}}
  ```

## A model starts two runs of one script at once

Runs are concurrent, within one script as across scripts, up to `RUN_MAX_ACTIVE` at once: a second `run` of a script whose first run is still running starts at once while fewer than `RUN_MAX_ACTIVE` runs are running, as here, where the shared catalog's two running runs and these two make four. A run beyond that waits for a slot, and one beyond `RUN_MAX_QUEUED` waiting runs is refused, as `S08` tells. Each run has its own folder, its own tree, its own input, and its own out folder, so two runs of one script never see each other's files. The model starts two runs within a second of each other, each with its own input, while each script sleeps five seconds.

`main.py`:

```python
import json
import os
import time

with open(os.environ["IKIGENBA_INPUT"]) as f:
    region = json.load(f)["region"]
time.sleep(5)
with open(os.path.join(os.environ["IKIGENBA_OUT_DIR"], "region.txt"), "w") as f:
    f.write(region + "\n")
print(region)
```

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e7a3c9f1b5d24e68a0c4f8b2d6e1a359
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":25,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","input":{"region":"eu"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 8b1d4f7a2c9e4053b6a0d8f2e4c1a795
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":26,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report","input":{"region":"us"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each, and each answers before the other's script has ended. The first body is a JSON-RPC response with `id` 25 whose `result` has no `isError` member and a `structuredContent` of `{"id":"<id1>","status":"running","sha":"<sha>"}`, and the second one with `id` 26 and a `structuredContent` of `{"id":"<id2>","status":"running","sha":"<sha>"}`, each with a `content` array of one text block whose text is that object encoded compactly. `<id1>` and `<id2>` are two different run ids. Then the model follows each run with `result` until its status is final:

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":27,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id1>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: result

{"jsonrpc":"2.0","id":28,"method":"tools/call","params":{"name":"result","arguments":{"run":"<id2>"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 27 whose `result` has no `isError` member and a `structuredContent` of

```
{"id":"<id1>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"e7a3c9f1b5d24e68a0c4f8b2d6e1a359","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":3,"stderr_bytes":0,"truncated":false,"stdout":"eu\n","stderr":"","files":[{"path":"region.txt","size":3}]}
```

and the second one with `id` 28 and a `structuredContent` of

```
{"id":"<id2>","script":"scr_6d1f4a9b2e8c7035","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"8b1d4f7a2c9e4053b6a0d8f2e4c1a795","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":3,"stderr_bytes":0,"truncated":false,"stdout":"us\n","stderr":"","files":[{"path":"region.txt","size":3}]}
```

each with a `content` array of one text block whose text is that object encoded compactly. Each run's `started` and `finished` are its own; the two runs' times overlap, and neither ran after the other.

Preconditions:

- The preamble's: `<sha>` holds `main.py` alone.

Postconditions:

- The runs `<id1>` and `<id2>` of `nightly-report` are both recorded `exited` with exit code 0, at `<sha>`: `<id1>` under request id `e7a3c9f1b5d24e68a0c4f8b2d6e1a359` and `<id2>` under `8b1d4f7a2c9e4053b6a0d8f2e4c1a795`, both as user `u_7f3a9c21`. While both ran, `runs` of `nightly-report` listed both as `running`, among its other runs (`S11`); neither was ever `queued`.
- Each run has its own folder under `state/runs/scr_6d1f4a9b2e8c7035/`: `<id1>/`, whose `input.json` holds `{"region":"eu"}` and whose `out/` holds `region.txt` with `eu` and LF; and `<id2>/`, whose `input.json` holds `{"region":"us"}` and whose `out/` holds `region.txt` with `us` and LF. Each script saw its own run's id, folder, input, out folder, and request id in its environment.
- telemetry has received a `run.started` and a `run.finished` for each run, each run's under its own `run` call's request id.
