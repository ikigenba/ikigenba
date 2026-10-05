# Stories — disk

What scripts reads and writes on disk. scripts is one of the consumers repos' `S15-disk.md` serves: it reads repos' repositories straight from their directories with the host's own `git`, running as the same `ikigenba` user, and never reaches repos over HTTP or through anything else of repos', so repos receives no request on scripts' account and records no event for it. repos keeps one bare repository per repository at `<REPOS_DIR>/<rep id>.git`, named by the repository's id (`rep_` and 16 lowercase hexadecimal digits), never its name, whose `HEAD` names `refs/heads/main` and whose own git config holds `ikigenba.id`, `ikigenba.name`, `ikigenba.owner`, and `ikigenba.created` (repos' `S15-disk.md`). `REPOS_DIR` is a setting read once, at start, from scripts' environment; unset or empty it is the manifest's `../repos/state/repos` (`S01`), and a relative value is resolved against scripts' working directory, so on a host, where scripts runs in `/opt/scripts` and repos in `/opt/repos`, it is `/opt/scripts/../repos/state/repos`, which is `/opt/repos/state/repos`, and in a sandbox `<data>/apps/scripts/../repos/state/repos` (`S25`). scripts does not check it at start, since repos may be installed later (`S02`, `S21`). scripts reads a repository four ways and no other: reading `ikigenba.owner`, reading `ikigenba.name`, resolving a ref to a commit, and unpacking a commit's tree with `git archive`; it never writes under `REPOS_DIR` — no ref, no object, no config, no lock left behind — so a repository's directory is byte for byte what it was before scripts read it. A script names its repository by id, so a script keeps running across a rename in repos, and a repository deleted in repos is gone for scripts too. The name is read afresh from the directory each time a page shows it, and a page shows the id, muted, when the directory is gone or the name cannot be read (`S03`, `S12`); the catalog keeps no name. What scripts writes is under its own working directory: its catalog `state/scripts.db`, which the host replicates (`S01`, `S21`), and one folder per run, `state/runs/<script id>/<run id>/`, holding `tree/`, the files of the run's commit exactly as `git archive` emits them — the same paths, the same bytes, symbolic links as links, and no `.git` — and beside it `out/`, where the script writes, `input.json`, `stdout`, and `stderr` (`S08`, `S15`), with the run's metadata as design chooses. Write permission is removed from everything under `tree/` once it is unpacked (`S15`). The run folder is the product, not a cache: it lives under `state/`, never `cache/`, nothing replicates it, and nothing rebuilds it. A run's row in the catalog survives its folder going missing, and a run whose files are gone is shown as such, never as an error. Pruning (`S19`) and deletion (`S10`) remove run folders. The actor is a model working through an MCP client, or an operator on the host; scripts runs on the host in `/opt/scripts` with `REPOS_DIR` unset unless a story sets it, and with the suite's services file (`S05`); telemetry takes every event; and the catalog holds `S06`'s shared catalog, among them the caller `u_7f3a9c21`'s `nightly-report`, `scr_6d1f4a9b2e8c7035`, over `rep_9c2e4b7a1d3f8e05` at `main`, and `backfill`, `scr_e8f2a6c0d4b19357`, over `rep_0f6a2d9e8c4b7153`, never run. `/opt/repos/state/repos/` holds `rep_9c2e4b7a1d3f8e05.git` (repos' `nightly-report`), its `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` and its tag `v1` at `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`, `rep_41d8f0a6b2c97e13.git` (repos' `crm-sync`), and `rep_7b3e9a0c5d1f2846.git` (repos' `ops-tools`), all with `ikigenba.owner` `u_7f3a9c21`, and `rep_d41c7a9e05b28f63.git` (repos' `journal`), with `ikigenba.owner` `u_2b8e1d04`; there is no `rep_0f6a2d9e8c4b7153.git`.

## A model creates a script over a repository in repos' directory

With `REPOS_DIR` unset, scripts finds the caller's repository where repos keeps it on the same host, by its id, and takes it as the caller's because its own config says so. It asks repos nothing.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

and a `content` array of one text block whose text is that object encoded compactly. `<created>` is the time of the call.

Preconditions:

- The preamble's: `REPOS_DIR` is unset, and `/opt/repos/state/repos/rep_41d8f0a6b2c97e13.git` is a bare repository whose `ikigenba.owner` is `u_7f3a9c21`. No script is named `crm-weekly`.

Postconditions:

- The catalog holds `crm-weekly`, never run, over `rep_41d8f0a6b2c97e13` (`S06`). No run was made and nothing was unpacked.
- `/opt/repos/state/repos/rep_41d8f0a6b2c97e13.git` is as it was. repos received no request.
- The request recorded `script.created` with `script` `<id>` (`S16`).

## An operator points scripts at another repositories directory

`REPOS_DIR` says where repos' repositories are, for a host that keeps them somewhere other than beside scripts, or a developer whose repositories are in a directory of their own. Once it is set, scripts looks there and only there: the default place is not consulted.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run","arguments":{"name":"nightly-report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"running","sha":"<sha>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, except that the operator has set `REPOS_DIR=/srv/ikigenba/repos` in `/opt/scripts/etc/env` and restarted scripts.
- `/srv/ikigenba/repos/rep_9c2e4b7a1d3f8e05.git` is a bare repository whose `main` is at `<sha>` and holds a `main.py`. `/opt/repos/state/repos/rep_9c2e4b7a1d3f8e05.git` still exists, its `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`.

Postconditions:

- The run `<id>` of `nightly-report` is at `<sha>`, unpacked from `/srv/ikigenba/repos/rep_9c2e4b7a1d3f8e05.git` into `/opt/scripts/state/runs/scr_6d1f4a9b2e8c7035/<id>/tree/` (`S08`).
- Neither repository changed.
- The request recorded `run.started` with `run` `<id>`, `script` `scr_6d1f4a9b2e8c7035`, `sha` `<sha>`, and `trigger` `manual` (`S16`).
- With `REPOS_DIR=../repos-b/state/repos` instead, scripts would have read `/opt/repos-b/state/repos/rep_9c2e4b7a1d3f8e05.git`, the relative value resolved against `/opt/scripts`.

## A model creates a script over a repository another user owns

Whose a repository is, scripts learns from the repository's own config, `ikigenba.owner`, which repos wrote when it made it. `rep_d41c7a9e05b28f63` is `u_2b8e1d04`'s, so for the caller it is not there: the answer is the one for a repository that does not exist, and tells the caller nothing of another user's. The owner is checked only here, at create; a script's later runs do not read it again.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly","repo":"rep_d41c7a9e05b28f63"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_d41c7a9e05b28f63'
```

Preconditions:

- The preamble's: `git config --file /opt/repos/state/repos/rep_d41c7a9e05b28f63.git/config ikigenba.owner` prints `u_2b8e1d04`. No script is named `crm-weekly`.

Postconditions:

- No script was created. The repository is as it was.
- The request recorded no `script.created`; its `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a script over a directory repos did not make

A bare repository under `REPOS_DIR` whose config holds no `ikigenba.owner` is no user's, so it is no one's to run: something other than repos put it there. scripts refuses it as it refuses a repository that does not exist.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly","repo":"rep_5e6f7a8b9c0d1e2f"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_5e6f7a8b9c0d1e2f'
```

Preconditions:

- The preamble's, and `/opt/repos/state/repos/rep_5e6f7a8b9c0d1e2f.git` is a bare repository with commits on `main` whose config holds no `ikigenba.` key. No script is named `crm-weekly`.

Postconditions:

- No script was created. The directory is as it was: scripts wrote no `ikigenba.owner` into it.
- The request recorded no `script.created`; its `tool.called` has `outcome` `error`.

## A model runs a script whose repository was renamed in repos

The owner renamed repos' `nightly-report` to `daily-report` (repos' `S08-rename.md`). Nothing moved on disk: the directory is named by the id, which the script holds, so running goes on as before. The script's own name is scripts' and does not follow the repository's; only the pages, which read the name afresh, show the new one.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
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

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's; the owner then renamed `nightly-report` to `daily-report` in repos, so `git config --file /opt/repos/state/repos/rep_9c2e4b7a1d3f8e05.git/config ikigenba.name` prints `daily-report`. Its `main` is still at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`.

Postconditions:

- The run `<id>` of `nightly-report` is at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, unpacked from `rep_9c2e4b7a1d3f8e05`; the script is still named `nightly-report`, still over `rep_9c2e4b7a1d3f8e05`.
- The request recorded `run.started` with `run` `<id>`, `script` `scr_6d1f4a9b2e8c7035`, `sha` `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, and `trigger` `manual` (`S16`); neither `nightly-report` nor `daily-report` is in any event.
- `GET /nightly-report/` now names the repository `daily-report`, with `rep_9c2e4b7a1d3f8e05` beside it (`S12`), and the catalog at `/` shows `daily-report` in the script's Repository cell (`S03`).

## A model runs a script whose repository was deleted in repos

The owner deleted repos' repository behind `backfill` (repos' `S09-delete.md`), and its directory is gone. There is nothing to unpack, so the script is not started; the run is still recorded, as a run that failed to start with the reason `repository_missing`, as `S08` tells. scripts creates nothing where the directory was.

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: run

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"run","arguments":{"name":"backfill"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","status":"failed","reason":"repository_missing"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `backfill` is over `rep_0f6a2d9e8c4b7153`, and `/opt/repos/state/repos/rep_0f6a2d9e8c4b7153.git` does not exist.

Postconditions:

- The catalog records the run `<id>` of `backfill`, `failed` with `reason` `repository_missing` and no `sha`; nothing was unpacked and no script started (`S08`). `backfill` is otherwise unchanged.
- scripts created nothing under `/opt/repos/state/repos/`.
- `GET /backfill/` shows the repository as its id, `rep_0f6a2d9e8c4b7153`, muted, since there is no directory to read a name from (`S12`).
- The request recorded the run's `run.finished` with `status` `failed` and no `run.started` (`S16`); its `tool.called` has `kind` `additive` and `outcome` `ok`.

## A model runs a script and the repository is left as it was

Every read scripts makes of a repository is a read. After a run, the repository's directory holds the same files, with the same bytes and the same modification times, as before it: scripts took no lock in it, wrote no ref or config, and left nothing behind. repos learned nothing of the run. A `ref` given to `run` is resolved for that run only (`S08`).

Request:

```
POST /mcp HTTP/1.1
Host: scripts.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

- The preamble's: the tag `v1` of `rep_9c2e4b7a1d3f8e05` is at `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`.
- Before the call, the operator recorded every path under `/opt/repos/state/repos/rep_9c2e4b7a1d3f8e05.git` with its size, its modification time, and a checksum of its bytes.

Postconditions:

- `/opt/scripts/state/runs/scr_6d1f4a9b2e8c7035/<id>/tree/` holds the files of `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`; `nightly-report`'s ref is still `main`.
- Recorded again once the run has ended, every path under `/opt/repos/state/repos/rep_9c2e4b7a1d3f8e05.git` has the size, modification time, and checksum it had; no path was added or removed. Its `main` and `v1` are where they were.
- repos received no request and recorded no event.

## An operator lists the files of a run's folder

An operator looking at a run on disk finds it in a folder named by the script's id and the run's id: the input the run was given, its kept output, the files the script wrote under `out/`, and the tree it ran in, laid out exactly as `git archive` emits the run's commit, with the same bytes. The operator names the run's own files; the metadata design keeps beside them is design's and is not listed here.

Command:

```
$ cd /opt/scripts/state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60 && find input.json stdout stderr out tree | LC_ALL=C sort
```

Output:

```
input.json
out
out/charts
out/charts/sales.svg
out/report.csv
out/report.html
stderr
stdout
tree
tree/main.py
tree/nightly
tree/nightly/__init__.py
tree/nightly/charts.py
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The preamble's: `nightly-report`'s run `run_3f9a1c2e8b7d4a60` exited 0 at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, having written `out/charts/sales.svg`, `out/report.csv`, and `out/report.html`, and its folder is kept.
- The commit `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` of `rep_9c2e4b7a1d3f8e05` holds `main.py`, `nightly/__init__.py`, and `nightly/charts.py`.
- The command runs on the host as the `ikigenba` user.

Postconditions:

- Nothing has changed.
- `input.json` holds the run's input, as `S06` gives it, `stdout` its 1229 bytes of standard output, and `stderr` is empty; `out/charts/sales.svg`, `out/report.csv`, and `out/report.html` are 12034, 7904, and 48211 bytes.
- Each file under `tree/` has the bytes `git --git-dir=/opt/repos/state/repos/rep_9c2e4b7a1d3f8e05.git archive --format=tar e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` emits for it, and no file or directory under `tree/` is writable (`S15`). There is no `tree/.git`.

## An operator removes a run's folder while scripts runs

A run folder is not a cache: nothing rebuilds it, and once it is gone the run's input, output and files are gone with it. An operator reclaiming disk may still remove one without stopping scripts, and scripts takes it in its stride: the run's row stays in the catalog, its details are answered as before, and where its files were, scripts says they are gone, never answering with an error. Because nothing under `tree/` is writable, the operator restores write permission there before removing the folder, as pruning does (`S19`).

Command:

```
$ chmod -R u+w /opt/scripts/state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60 && rm -rf /opt/scripts/state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60
```

Output: none.

Exits 0. Nothing is on stdout; stderr is empty.

Preconditions:

- The preamble's: scripts is running, and the folder of `nightly-report`'s run `run_3f9a1c2e8b7d4a60`, which exited 0, is kept. The command runs as the `ikigenba` user.

Postconditions:

- `state/runs/scr_6d1f4a9b2e8c7035/run_3f9a1c2e8b7d4a60/` is gone, and nothing brings it back. The catalog is unchanged: `runs` of `nightly-report` still lists `run_3f9a1c2e8b7d4a60` (`S11`).
- `result` with `{"run":"run_3f9a1c2e8b7d4a60"}` is answered with no `isError` member and a `structuredContent` of the run's details as before, with no `stdout`, `stderr`, or `files`, and `files_gone` `true` last (`S11`):

  ```
  {"id":"run_3f9a1c2e8b7d4a60","script":"scr_6d1f4a9b2e8c7035","sha":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"main","user":"u_7f3a9c21","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","trigger":"manual","status":"exited","exit_code":0,"started":"2026-10-05T09:14:02Z","finished":"2026-10-05T09:14:14Z","stdout_bytes":1229,"stderr_bytes":0,"truncated":false,"files_gone":true}
  ```

- `GET /nightly-report/runs/run_3f9a1c2e8b7d4a60/` is answered `200` with the run's details and the note `Files no longer kept`, and without its input, output, or files (`S13`); a download of its `stdout` is not found (`S14`).
- scripts recorded no event and wrote nothing to stderr.
