# Stories — on a space

scripts reached through a space: the file `S23` describes, deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at `scripts.<space>` over TLS, so on the space `sbx.ikigenba.dev` scripts answers at `scripts.sbx.ikigenba.dev`. nginx on the space proxies to scripts' socket, `/run/ikigenba/scripts.sock` (`S02`), and includes scripts' own `etc/nginx.conf` in that server (`S23`), so `/events` and `/declarations` at the public name answer 404 while the same paths on the socket stay open to scripts' siblings. scripts' manifest declares no `guests` (`S01`), so the space's nginx asks auth's `/check` for every path of scripts: a request with no credential never reaches scripts, and is sent to sign in at a page or challenged at `/mcp` and the paths under `/mcp/` by nginx itself (opsctl's `S5-nginx.md`, `A browser reaches a wired app outside /mcp without signing in` and `An MCP client reaches a wired app without a credential`), one whose credential auth refuses gets auth's 403 and never reaches scripts, and one with a session or a token auth honors is passed with the caller's `X-User-Id` and `X-User-Email`. Every request nginx passes carries `X-Forwarded-Proto: https` and the `X-Request-Id` nginx gave it, the same id its `/check` subrequest carried, so auth's check event and scripts' records of the request share one request id. The host's services file is `/var/lib/ikigenba/services.json`, which lists scripts with `url` `https://scripts.sbx.ikigenba.dev`, its socket, and marked for MCP since its manifest has `mcp = true`, so the MCP gateway offers scripts' eleven tools through `https://mcp.sbx.ikigenba.dev/mcp` (mcp's `S11-on-a-space.md`), and every script scripts runs is handed that file's path as `IKIGENBA_SERVICES` (`S15`). scripts runs as `/opt/scripts/bin/scripts` with `/opt/scripts` as its working directory, so its catalog is `/opt/scripts/state/scripts.db`, its run folders are under `/opt/scripts/state/runs/`, and `REPOS_DIR`, at its manifest default `../repos/state/repos`, names `/opt/scripts/../repos/state/repos`, the directory where repos keeps its bare repositories, `/opt/repos/state/repos/` (repos' `S15-disk.md`; `S20`). The host keeps and replicates the declared database as it does auth's, which is opsctl's doing and is named here only by its effect; the run folders are kept on the host but not replicated (`S20`). `/opt/scripts/etc/env` carries the seven settings of the manifest's `[env]` beside the space's `DRAIN_SECONDS` and `IKIGENBA_SERVICES`; the host provides the `git` scripts runs (opsctl's `S4-init.md`) and the `python3.12` every script runs under, installed at the space's first boot or, on a space launched before that, by the operator (`S22`). A deploy restarts scripts and so kills any run in flight (`S18`).

The stories prove the whole path from checkout to browser, curl, and agent, and nothing about scripts that the earlier groups do not already say. devctl and opsctl are named only by their published commands. The catalog on the space holds `S06`'s shared catalog — `backfill`, `nightly-report`, `rotate-keys`, and `sync-crm`, owned by `u_7f3a9c21`, `mg@example.com`, and `digest`, owned by `u_2b8e1d04`, `ann@example.com`, with their runs — and repos on the space holds the repositories they run from, but `backfill`'s, which is gone. A guest is curl with no cookie and no `Authorization` header; a signed-in caller sends the token `ikp_<token>` (auth's `S5-tokens.md`), owned by `u_7f3a9c21`, as `Authorization: Bearer ikp_<token>`. The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. Trail events are named here by their attributes, as `S16` records them in full. The token's secret is in no record any story below leaves behind: not in the trail, not in nginx's logs on the host, and not in any run's folder.

## A user on a space reaches scripts' landing page

A signed-in user asks for scripts' own name and gets the catalog of their scripts and the tools that manage them.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://scripts.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `scripts`, whose banner's profile link is titled `mg@example.com`, the email of the token's owner, and leads to `https://auth.sbx.ikigenba.dev/`, whose banner's `Sign out` button is in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`, whose banner carries the launcher button `Services`, since the host's services file lists scripts with its icon, and whose visible text carries the heading `scripts`; the heading `Your scripts` and the table of the caller's scripts, `backfill`, `nightly-report`, `rotate-keys`, and `sync-crm`, in that order, each linking to its page at `/<name>/`, `backfill` saying `never run` and showing its repository by its id, since that repository is gone; not `digest`, which is `ann@example.com`'s; the heading `MCP tools` and the eleven tool names `list`, `show`, `create`, `update`, `delete`, `subscribe`, `unsubscribe`, `run`, `runs`, `result`, and `cancel`; and a link `About scripts` to `/about`; and whose footer reads `scripts v<semver>`, the version the deployed binary's `scripts --version` prints (`S01`), the same one `space status` reports for scripts. Its stylesheet is `https://scripts.sbx.ikigenba.dev/_appkit/theme.css` (`S04`): a browser showing the page requests its style from scripts' own host and from no other origin. Its button feedback script is `https://scripts.sbx.ikigenba.dev/_appkit/feedback.js`, from the same host.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance is `running`, and `opsctl` is installed on it.
- A tag `scripts/v<semver>` points at the commit `devctl build scripts` was run at, and it wrote `scripts/dist/scripts-v<semver>.tar.xz`.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev scripts/dist/scripts-v<semver>.tar.xz` exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows `scripts v<semver> active active -`.
- auth, repos, and telemetry are deployed and active on the space through their own chains, and `python3.12` is installed on the host (`S22`).
- `ikp_<token>`, whose id is `<token-id>`, is a token auth honors, owned by `u_7f3a9c21`, `mg@example.com`.
- The host sets `IKIGENBA_SERVICES` in scripts' environment to the path of its services file, and that file lists scripts with its icon, from `share/icon.svg` (`S23`).

Postconditions:

- Nothing has changed but the trail. scripts read the names of the listed scripts' repositories under `/opt/repos/state/repos/` (`S03`) and wrote nothing there; no script ran.
- The trail holds, under the id nginx gave the request and user `u_7f3a9c21`: auth's `check.allowed` with `outcome=allowed`, `credential=token`, `host=scripts.sbx.ikigenba.dev`, `path=/`, and `token=<token-id>`; then scripts' `request.started` with `method=GET` and `path=/`, and `request.finished` with `status=200`. The response set no cookie.

## A guest on a space asks for scripts' landing page

scripts serves nothing to guests, so the space's nginx sends a guest who opens scripts' own name to sign in, carrying the URL it asked for, and the request never reaches scripts. Once signed in, auth brings the browser back to the landing page.

Request:

```
$ curl -si https://scripts.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https://scripts.sbx.ikigenba.dev/
```

Status 302. The body is not fixed, and the response carries no `www-authenticate` header. `https://scripts.sbx.ikigenba.dev/about` and `https://scripts.sbx.ikigenba.dev/nightly-report/` answer the same, with the `return` ending `/about` and `/nightly-report/`.

Preconditions:

- scripts `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches scripts' landing page`.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/scripts.sock`, and scripts recorded no event; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none` (auth's `S4-check.md`).

## An MCP client on a space reaches scripts' /mcp without a credential

nginx keeps the strict `/check` on `/mcp` and everything under it, and answers an MCP client with no credential with the bearer challenge it gives at any app (opsctl's `S5-nginx.md`, `An MCP client reaches a wired app without a credential`), rather than a redirect it could not follow; scripts never sees the request.

Request:

```
$ curl -si -X POST https://scripts.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 401
content-type: text/plain
www-authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the host's nginx's one line `authentication required: send Authorization: Bearer <token>`.

Preconditions:

- scripts `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches scripts' landing page`.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/scripts.sock`, and scripts recorded no event; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none`.

## An agent asks a space for scripts' events and declarations paths

`/events` and `/declarations` are meant only for scripts' siblings on the socket. At scripts' public name the space's nginx answers each 404 for every method, because of the fragment scripts ships (`S23`); a credential makes no difference, since the answer is nginx's and the request never reaches scripts. Every form below is answered the same way.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' https://scripts.sbx.ikigenba.dev/events
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://scripts.sbx.ikigenba.dev/events
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' https://scripts.sbx.ikigenba.dev/declarations
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://scripts.sbx.ikigenba.dev/declarations
```

Response:

```
HTTP/2 404
```

Status 404. The body is not fixed. A request to either path with no credential is answered 404 as well; this story does not fix which of the space's refusals comes first, only that neither reaches scripts.

Preconditions:

- scripts `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches scripts' landing page`, so the host's nginx includes `/opt/scripts/etc/nginx.conf` in scripts' server.
- The agent holds `ikp_<token>`, a token auth honors, owned by `u_7f3a9c21`.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/scripts.sock`: scripts recorded no event for any of the requests, and no run started. The public 404 closes the paths to the outside only; what scripts answers on them on its socket is not this group's.

## An agent on a space creates and runs a script through the gateway

The expected path for an agent: it has pushed a `main.py` to one of its repositories in repos, asks the gateway to run scripts' `create` naming that repository, then `run` with an input, and follows the run with `result` until it has ended. `create` and `run` are additive tools (`S05`), so the agent calls them with the gateway's `mutate` tool (mcp's `S09-mutate.md`); `result` is a read tool, called with the gateway's `call` (mcp's `S08-call.md`). The gateway reaches scripts directly on `/run/ikigenba/scripts.sock` and relays its answer, forwarding the caller and the request id nginx gave the agent's request, so the run acts as `u_7f3a9c21` under the id of the `run` request (`S08`). The repository's `main` holds one file, `main.py`:

```
import json
import os

with open(os.environ["IKIGENBA_INPUT"]) as f:
    who = json.load(f).get("who", "world")
print(f"hello, {who}")
with open(os.path.join(os.environ["IKIGENBA_OUT_DIR"], "greeting.txt"), "w") as f:
    f.write(f"hello, {who}\n")
```

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"scripts","tool":"create","args":{"name":"hello","repo":"<rep>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"scripts","tool":"run","args":{"name":"hello","input":{"who":"mg"}}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"scripts","tool":"result","args":{"run":"<run>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 1 whose `result` is scripts' answer to `create` (`S06`), relayed: no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"hello","repo":"<rep>","ref":"main","created":"<created>","subscriptions":[]}
```

and a `content` array of one text block holding that same object encoded compactly; its `subscriptions` is empty, since nothing subscribes a new script to an event, and it has no `last_run`, since the script has never run. The second is a JSON-RPC response with `id` 2 whose `result` is scripts' answer to `run` (`S08`), relayed, given once the commit is unpacked and the script's process has started, without waiting for it, with a `structuredContent` of

```
{"id":"<run>","status":"running","sha":"<sha>"}
```

The third, sent once the script has ended, which this one does within a second, is a JSON-RPC response with `id` 3 whose `result` is scripts' answer to `result` (`S11`), relayed, with a `structuredContent` of

```
{"id":"<run>","script":"<id>","sha":"<sha>","ref":"main","user":"u_7f3a9c21","request_id":"<run-request>","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":10,"stderr_bytes":0,"truncated":false,"stdout":"hello, mg\n","stderr":"","files":[{"path":"greeting.txt","size":10}]}
```

Sent before the script has ended, the same call answers with `status` `running`, no `exit_code` and no `finished`, and the output so far, and the agent calls it again. `<id>` is `scr_` followed by 16 lowercase hexadecimal digits, the same in the first and third; `<run>` is `run_` followed by 16 lowercase hexadecimal digits, the same in the second and third; `<run-request>` is the id nginx gave the second request. Each result's `io.modelcontextprotocol/serverInfo` is the gateway's, `{"name":"mcp","version":"v<semver>"}`, not scripts'.

Preconditions:

- scripts `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches scripts' landing page`, and the host's services file lists `scripts` enabled, marked for MCP, with the socket `/run/ikigenba/scripts.sock`.
- mcp is deployed and active on the space through its own `S11-on-a-space.md` chain, serving `https://mcp.sbx.ikigenba.dev/mcp`.
- `python3.12` is installed on the host and on the `PATH` scripts runs with (`S22`).
- The agent holds `ikp_<token>`, owned by `u_7f3a9c21`, who owns the repository `hello`, `<rep>`, in repos, its `ikigenba.owner` `u_7f3a9c21` and its `main` at `<sha>`, a commit whose tree holds `main.py` above and nothing else, pushed with git as repos' `S17-on-a-space.md` tells.
- No script in the space is named `hello`.

Postconditions:

- `u_7f3a9c21` owns a new script `hello` with id `<id>`, running `<rep>` at `main`, and its one run `<run>`, `exited` with exit code 0, both in `/opt/scripts/state/scripts.db`.
- `/opt/scripts/state/runs/<id>/<run>/` is the run's folder: `tree/` holds `main.py` alone, as `<sha>` holds it, without write permission (`S15`); `input.json` holds the object `{"who":"mg"}`; `stdout` holds `hello, mg` and a newline; `stderr` is empty; and `out/` holds `greeting.txt`, whose content is the same line. `/opt/repos/state/repos/<rep>.git` is as it was.
- The landing page lists `hello` with its repository `hello` and its last run `exited 0` (`S03`), and `https://scripts.sbx.ikigenba.dev/hello/runs/<run>/` shows the run (`S13`).
- mcp wrote nothing to stderr, and neither did scripts.
- The trail holds, under the id nginx gave each request and user `u_7f3a9c21`, auth's `check.allowed` with `credential=token`, the gateway's events for the call, and scripts' own: for the first, `script.created` with `script=<id>`, then `tool.called` with `tool=create`, `kind=additive`, and `outcome=ok`; for the second, `run.started` with `run=<run>`, `script=<id>`, `sha=<sha>`, and `trigger=manual`, then `tool.called` with `tool=run`, `kind=additive`, and `outcome=ok`; for the third, `tool.called` with `tool=result`, `kind=read`, and `outcome=ok`. The run's `run.finished`, with `run=<run>` and `status=exited` and its exit code 0, is under `<run-request>`, the second request's id, whenever it was recorded (`S16`). No event attribute holds the name `hello`, the input, the output, or the token.
