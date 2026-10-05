# Stories — create

`create`, the tool that adds a script to the catalog: a record naming one of the caller's repositories and the ref its runs resolve, which runs nothing until the caller calls `run` (`S08`). It takes, in this order, `name` and `repo`, both required strings, and `ref`, a string, optional. A name is 1 to 64 characters, each a lowercase ASCII letter, a digit, or `-`, the first a letter or a digit, taken as given, never trimmed or folded; `about` and `mcp` are not names, since `/about` and `/mcp` are scripts' own paths. Names are unique across the space, not per owner: every user's scripts share one namespace because a script's page is at its name, so a name is taken when any script, the caller's or another user's, has it. `repo` is a repository's id, `rep_` and 16 lowercase hexadecimal digits, whose bare repository `<REPOS_DIR>/<repo>.git` exists and whose `ikigenba.owner` config value, as repos writes it (repos' `S15-disk.md`), is the caller's `X-User-Id`; scripts never asks repos, and a repository's name is not accepted in place of its id. The id is kept as given, so a script keeps its repository when repos renames it. One repository may back any number of scripts. `ref` defaults to `main` and must be a string git accepts as a ref name; it is not resolved until a run, so a ref that names nothing yet is accepted. The arguments are checked in that order — the name against the rule, then whether it is taken; the repository; the ref — and the first that fails is the whole answer, one line naming it. The result is the script object (`S05`), members in this order: `id`, `scr_` and 16 lowercase hexadecimal digits, minted now and never changed; `name`; `repo`; `ref`; and `created`, the time of the call, RFC 3339 UTC to the second. A new script has never run, so `last_run` is absent. create reads the repository's owner and nothing else from it, unpacks nothing, and makes no run and no run folder.

The actor is a model working through an MCP client, or the gateway's `mutate` on its behalf; the request shape, the result envelope, the `invalid arguments:` wording, `tool.called`, and the trail are as `S05` fixes them. Each request is the HTTP request a running scripts (`S02`) receives on `/mcp`, carrying `X-User-Id` and `X-User-Email` by hand, on revision `2026-07-28`, and it is now `2026-10-05T09:32:00Z`. Unless a story says otherwise, scripts runs with `REPOS_DIR` unset, so it reads repositories under `../repos/state/repos`, and telemetry takes every event. Under `../repos/state/repos` are four bare repositories, part of `S06`'s shared catalog: `rep_9c2e4b7a1d3f8e05.git` (repos' `nightly-report`), `rep_41d8f0a6b2c97e13.git` (repos' `crm-sync`) and `rep_7b3e9a0c5d1f2846.git` (repos' `ops-tools`), whose `ikigenba.owner` is the caller `u_7f3a9c21` (`mg@example.com`), and `rep_d41c7a9e05b28f63.git` (repos' `journal`), whose `ikigenba.owner` is `u_2b8e1d04` (`ann@example.com`). In `rep_9c2e4b7a1d3f8e05`, `main` is at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d` and the tag `v1` at `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`, earlier commits are `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37` and `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60`, and nothing is named `release`; in `rep_41d8f0a6b2c97e13`, `main` is at `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3`; in `rep_7b3e9a0c5d1f2846`, `main` is at `7d5b3f1e9c0a2846b8d4f6e1a3c5b7d9e0f2a4c6` and there is no branch `release`; in `rep_d41c7a9e05b28f63`, `main` is at `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`. There is no `rep_0f6a2d9e8c4b7153.git`, which repos has deleted, and no `rep_0a0b0c0d0e0f1a2b.git`, which never existed. The catalog holds five scripts, `S06`'s shared catalog, which every later group assumes unless it says otherwise: the caller's `nightly-report`, id `scr_6d1f4a9b2e8c7035`, repository `rep_9c2e4b7a1d3f8e05`, ref `main`, created `2026-09-18T16:40:00Z`; the caller's `sync-crm`, id `scr_a2e7c4f9b1d03856`, repository `rep_41d8f0a6b2c97e13`, ref `main`, created `2026-09-25T10:00:00Z`; the caller's `rotate-keys`, id `scr_5c9b1e3a7f2d4068`, repository `rep_7b3e9a0c5d1f2846`, ref `release`, created `2026-09-28T08:00:00Z`; the caller's `backfill`, id `scr_e8f2a6c0d4b19357`, repository `rep_0f6a2d9e8c4b7153`, the one repos has deleted, ref `main`, created `2026-10-02T12:00:00Z`, which has never run; and `u_2b8e1d04`'s `digest`, id `scr_3b7f9d1c5e0a2846`, repository `rep_d41c7a9e05b28f63`, ref `main`, created `2026-10-01T16:45:00Z`.

The catalog also records ten runs, part of `S06`'s shared catalog. Every one has the trigger `manual` and acts as its script's owner, its output was not truncated unless said, and each has its folder at `state/runs/<script id>/<run id>/` but `run_72b0c8f5e3d1a946`, whose folder is gone. `nightly-report`'s seven, newest first: `run_8a2c6e1f9b3d5074`, `running`, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, started `2026-10-05T09:31:40Z`, with 214 bytes of stdout so far; `run_3f9a1c2e8b7d4a60`, `exited` with code 0, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, started `2026-10-05T09:14:02Z` and finished `2026-10-05T09:14:14Z`, request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, whose input is `{"since":"2026-10-04","channels":["sales","support"],"dry_run":false}`, whose stdout holds 1229 bytes and stderr none, and whose `out/` holds `charts/sales.svg`, 12034 bytes, `report.csv`, 7904 bytes, and `report.html`, 48211 bytes; `run_c71d0b5e4a2f9386`, `exited` with code 1, from `main` at `e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d`, started `2026-10-04T09:14:02Z` and finished `2026-10-04T09:14:11Z`; `run_5e8b3d7a1c0f6294`, `timed_out`, from `main` at `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`, started `2026-10-03T09:14:02Z` and finished `2026-10-03T09:24:02Z`, whose stdout was cut at 1048576 bytes, so it is truncated, and whose stderr holds 212 bytes; `run_19f6a4d2c8e3b705`, `killed` by a `cancel`, from the tag `v1` at `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37`, started `2026-10-02T09:14:02Z` and finished `2026-10-02T09:17:43Z`; `run_d4a7e2c9f1b8630a`, `failed` with reason `commit_missing`, from `release`, which named no commit, so it has no sha, started and finished `2026-10-01T09:14:02Z`; and `run_72b0c8f5e3d1a946`, `exited` with code 0, from `main` at `9a3c5f0e1b7d4c2a8f6e3d9b0c5a7e1f4d2b8c60`, started `2026-09-30T09:14:02Z` and finished `2026-09-30T09:14:13Z`. `sync-crm`'s one: `run_6b2d8f4a0c9e1735`, `running`, from `main` at `3c8e1f5a9d2b7064e1a3c5f7b9d0e2a4c6f8b1d3`, started `2026-10-05T09:31:00Z`. `rotate-keys`'s one: `run_1e9c3a7f5b0d2864`, `failed` with reason `commit_missing`, from `release`, with no sha, started and finished `2026-10-04T22:00:00Z`. `digest`'s one, `u_2b8e1d04`'s: `run_0c4e8a2f6b1d9375`, `exited` with code 0, from `main` at `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`, started `2026-10-04T07:00:00Z` and finished `2026-10-04T07:00:05Z`. No run is past keeping (`S19`). `create` is of kind `additive`; a call that makes a script records `script.created`, with `script`, the new id, before its `tool.called`; the name, the repository, and the ref are in no event. A refusal creates nothing and records no `script.*` event. scripts writes nothing to stderr for any answer in this group.

## A model creates a script

The ordinary case: a name and a repository, the ref by default. The script runs `main` of `rep_41d8f0a6b2c97e13`, the caller's `crm-sync`, whenever it is run. Nothing has run yet: the answer has no `last_run`, and nothing runs until the model calls `run` (`S08`).

Request:

```
POST /mcp HTTP/1.1
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
{"id":"<id>","name":"crm-weekly","repo":"rep_41d8f0a6b2c97e13","ref":"main","created":"2026-10-05T09:32:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the id scripts minted, `scr_` and 16 lowercase hexadecimal digits, different from every other script's; `created` is the time of the call.

Preconditions:

- The preamble's: no script has the name `crm-weekly`, and the call is made at `2026-10-05T09:32:00Z`.

Postconditions:

- The catalog holds a sixth script: id `<id>`, name `crm-weekly`, owner `u_7f3a9c21`, repository `rep_41d8f0a6b2c97e13`, ref `main`, created `2026-10-05T09:32:00Z`, and no run. `list` (`S07`) answers `backfill`, `crm-weekly`, `nightly-report`, `rotate-keys`, `sync-crm`, with `crm-weekly`'s entry having no `last_run`; `show` with `crm-weekly` answers what `create` answered.
- Nothing was unpacked and nothing ran: there is no `state/runs/<id>/`. The repository is untouched; `sync-crm`, which uses it too, is as it was, and its running run `run_6b2d8f4a0c9e1735` runs on.
- The script's page, `/crm-weekly/`, shows it with no runs yet (`S12`).
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"scripts","event":"script.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"script":"<id>"}}
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create"}}
  {"time":"<time>","service":"scripts","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `crm-weekly`, the repository, and the ref are in none of them.

## A model creates a script that runs from a tag

`ref` names what a run resolves when it is given no ref of its own: a branch, a tag, or a sha. Here the script runs the tag `v1` of `rep_9c2e4b7a1d3f8e05`, the repository `nightly-report` already runs `main` of. The ref is recorded as given and not resolved: a ref that names nothing in the repository yet, a branch the model has still to push, is accepted the same way, and only a run finds out, as `rotate-keys`, whose `release` names nothing in `rep_7b3e9a0c5d1f2846`, shows (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create","arguments":{"name":"nightly-report-v1","repo":"rep_9c2e4b7a1d3f8e05","ref":"v1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"nightly-report-v1","repo":"rep_9c2e4b7a1d3f8e05","ref":"v1","created":"2026-10-05T09:32:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: no script has the name `nightly-report-v1`, and `rep_9c2e4b7a1d3f8e05` already backs `nightly-report`.

Postconditions:

- The catalog holds `nightly-report-v1`, owner `u_7f3a9c21`, repository `rep_9c2e4b7a1d3f8e05`, ref `v1`, and no run. A `run` of it with no `ref` resolves `v1`, to `b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37` while the tag stays where it is (`S08`).
- `nightly-report` is as it was, ref `main`, and its running run `run_8a2c6e1f9b3d5074` runs on.
- No git resolved `v1`. telemetry has received `script.created` with attributes `{"script":"<id>"}`; the ref is in no event.

## A model creates a script with a name another user's script has

Names are unique across the space, not per owner: `digest` is `u_2b8e1d04`'s script and its page is at `/digest/`, so the caller cannot have a `digest` of its own. The answer says only that the name is taken; it says nothing of whose it is.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create","arguments":{"name":"digest","repo":"rep_41d8f0a6b2c97e13"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
a script named 'digest' already exists
```

Preconditions:

- The preamble's: `digest` is `u_2b8e1d04`'s.

Postconditions:

- No script was created. `digest` is as it was, still `u_2b8e1d04`'s, with its run `run_0c4e8a2f6b1d9375`, and the caller's `list` (`S07`) still does not show it.
- scripts recorded no `script.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create"}}
  ```

## A model creates a script with a name its own script already has

A taken name is refused, never read as a request for the script that has it, and the existing script is untouched, whatever repository and ref the call names. A model that means to change which ref a script runs calls `update` (`S09`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"name":"nightly-report","repo":"rep_9c2e4b7a1d3f8e05","ref":"v1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
a script named 'nightly-report' already exists
```

Preconditions:

- The preamble's: the caller owns `nightly-report`.

Postconditions:

- No script was created. `nightly-report` is unchanged: same id, `scr_6d1f4a9b2e8c7035`, ref `main`, its seven runs and their folders as they were, and `run_8a2c6e1f9b3d5074` still running.
- scripts recorded no `script.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a script with a name that is not allowed

A name is held to one rule, and every way of breaking it gets the same line, quoting the name as sent: upper-case letters or a space, as here; `_`, `.`, `/`, or any other character outside `a`-`z`, `0`-`9`, and `-`; a first character `-`, as in `-report`; the empty name; and more than 64 characters. Nothing is trimmed or folded, so ` report` and `Report` are refused rather than read as `report`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create","arguments":{"name":"CRM Weekly","repo":"rep_41d8f0a6b2c97e13"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid name 'CRM Weekly'
```

Preconditions:

- The preamble's.

Postconditions:

- No script was created.
- scripts recorded no `script.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create"}}
  ```

## A model creates a script named about or mcp

`/about` and `/mcp` are scripts' own paths, the about screen (`S03`) and the MCP endpoint (`S05`), so a script whose page is at `/about/` or `/mcp/` could never be reached. Both names keep to the naming rule, and both are refused with the line any name that is not allowed gets. `mcp` is refused the same way, with `invalid name 'mcp'`. Names that merely begin with them, `about-us` or `mcp-audit`, are ordinary names.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create","arguments":{"name":"about","repo":"rep_41d8f0a6b2c97e13"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid name 'about'
```

Preconditions:

- The preamble's.

Postconditions:

- No script was created. `GET /about` still answers the about screen (`S03`).
- scripts recorded no `script.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a script from a repository that does not exist

`repo` must name a bare repository under `REPOS_DIR`. One that is not there, because it was never created, as here, or because repos has deleted it, as `rep_0f6a2d9e8c4b7153`, which `backfill` still names, is refused, quoting the value as sent. A repository's name in place of its id, `crm-sync` say, names no repository and is refused the same way, with `no repository 'crm-sync'`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly","repo":"rep_0a0b0c0d0e0f1a2b"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_0a0b0c0d0e0f1a2b'
```

Preconditions:

- The preamble's: there is no `../repos/state/repos/rep_0a0b0c0d0e0f1a2b.git`.

Postconditions:

- No script was created, and nothing was written under `../repos/state/repos/` or `state/runs/`.
- scripts recorded no `script.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a script from another user's repository

A repository is the caller's when its `ikigenba.owner` says so; scripts reads that from the repository itself and asks repos nothing. Another user's repository gets exactly the answer a missing one gets, so the caller cannot learn that the id is in use. The owner is checked only here: a run later reads whatever the script's repository holds (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly","repo":"rep_d41c7a9e05b28f63"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_d41c7a9e05b28f63'
```

Preconditions:

- The preamble's: the `ikigenba.owner` config value of `rep_d41c7a9e05b28f63.git` is `u_2b8e1d04`.

Postconditions:

- No script was created. `rep_d41c7a9e05b28f63.git` is untouched, and `digest`, which runs from it, is as it was.
- scripts recorded no `script.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a script with a ref git would not accept

`ref` is not resolved at create, but it must be something git could resolve one day: a string git accepts as a ref name. `..bad` is not one; nor is the empty string, a ref with a space, `~`, `^`, `:`, `?`, `*`, `[`, or `\`, or one ending `/` or `.lock`. Each is refused quoting the value as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly","repo":"rep_41d8f0a6b2c97e13","ref":"..bad"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid ref '..bad'
```

Preconditions:

- The preamble's.

Postconditions:

- No script was created.
- scripts recorded no `script.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model calls create without saying which repository

`name` and `repo` are both required. A call that leaves one out is refused as its arguments are read, every offence in one answer, before any rule of scripts' is looked at (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- No script was created.
- scripts recorded no `script.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"scripts","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"create"}}
  ```

## A model sends arguments create does not take

A field of the wrong JSON type, or one the tool does not have, is refused as the arguments are read, every offence in one answer: the tool's fields first, in the order of its input schema, then each unknown field in the order it was sent (`S05`). Here the model sent `ref` as a number and tried to name the script's owner, which is always the caller.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm-weekly","repo":"rep_41d8f0a6b2c97e13","ref":1,"owner":"u_2b8e1d04"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
ref: expected string, got number
owner: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- No script was created.
- scripts recorded no `script.created`; the request's `tool.called` has `kind` `additive`, `outcome` `invalid_arguments`, and `duration_us` 0.
