# Stories — in a sandbox

scripts reached through a sandbox: the local runner a developer brings up from a worktree, which serves every app of the checkout in plain HTTP behind its own nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The stories share one sandbox: `wip`, on port `7400`, holding `auth`, `dummy`, `mcp`, `repos`, `scripts`, `sites`, and `telemetry`, all active, with scripts at `http://scripts.wip.localhost:7400`, repos at `http://repos.wip.localhost:7400`, auth at `http://auth.wip.localhost:7400`, and the gateway at `http://mcp.wip.localhost:7400`. Browsers, curl, and git resolve every name under `localhost` to the loopback address. The sandbox gives scripts the environment of a host, `DRAIN_SECONDS`, `IKIGENBA_SERVICES`, and the seven settings of its manifest's `[env]` at their manifest values, and variables a host never sets, none of which scripts reads; scripts runs the developer's own `git` and `python3.12`, found on the `PATH` the sandbox gives it, `python3.12` installed as `S22` tells, and every script it runs is handed that `PATH` and the sandbox's services file as `IKIGENBA_SERVICES` (`S15`). Each app's working directory is the sandbox's own for that app, `<data>/apps/<app>/`, where `<data>` is the sandbox's data directory (sandbox's `S3-apps.md`), so scripts' catalog is `<data>/apps/scripts/state/scripts.db` and its run folders are under `<data>/apps/scripts/state/runs/` (`S20`), while repos keeps its bare repositories under `<data>/apps/repos/state/repos/` (repos' `S18-in-a-sandbox.md`). `REPOS_DIR`, at its manifest default `../repos/state/repos`, is resolved against scripts' working directory, so it names `<data>/apps/scripts/../repos/state/repos`, which is repos' directory: in a sandbox as on a host, scripts reads exactly the repositories repos holds, with no setting changed. The catalog and the run folders are created on the first `up` and kept across `down` and `up` until `wipe`.

scripts' manifest declares no `guests`, so the sandbox's nginx puts every request to scripts to auth's `/check`: a request with no credential never reaches scripts, and is sent to sign in at a page or challenged at `/mcp` (sandbox's `S4-routing.md`). Every request the sandbox's nginx passes carries `X-Forwarded-Proto: http` and an `X-Request-Id` nginx made. An agent sends the sandbox's token, the one `sandbox token` prints, as `Authorization: Bearer <token>`, and git sends it as the Basic password after the sandbox's nginx challenges it on a git path (sandbox's `S4-routing.md`). The sandbox's nginx, its routing through `/check`, and the token are the sandbox's own (a separate sub-project), named here only by their observable effect. Every request below is made to the sandbox; responses pass through the sandbox's nginx over HTTP/1.1. The developer's shell has the token in `IKIGENBA_TOKEN` and the git credential helper repos' guidance gives installed once (repos' `S18-in-a-sandbox.md`):

```
$ export IKIGENBA_TOKEN="$(sandbox token)"
$ git config --global credential.http://*.wip.localhost:7400.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'
```

The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. As on a space (`S24`), the token is in no record any story below leaves behind.

## An agent reaches scripts' landing page in a sandbox

An agent working in the worktree opens scripts' own name in the sandbox with the stored token and gets the landing page, as on a space.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://scripts.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `scripts`, whose visible text carries the heading `scripts`, the heading `Your scripts`, the heading `MCP tools` and the nine tool names `list`, `show`, `create`, `update`, `delete`, `run`, `runs`, `result`, and `cancel`, and the link `About scripts` to `/about`, whose banner's profile link is titled with the email of the token's owner and leads to `http://auth.wip.localhost:7400/`, and whose footer reads `scripts v<semver>`. Its banner carries the launcher button `Services`, since the sandbox's services file lists scripts with its icon. With no script in the sandbox yet, the `Your scripts` section reads `No scripts yet`.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token a human created at `http://auth.wip.localhost:7400/` and stored with `sandbox token set`.
- auth's `/check` answers 200 for that token, naming its owner.
- scripts' catalog holds no script.

Postconditions:

- Nothing has changed but the trail: scripts records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id the sandbox's nginx gave the request and the token's owner; auth records `check.allowed` for the same id with `host=scripts.wip.localhost` and `path=/` (auth's `S4-check.md`).

## An agent runs a script in a sandbox from a repository it pushed

The whole loop an agent runs in a sandbox: create a repository in repos through the sandbox's gateway, push a `main.py` to it with plain git, then create a script from that repository, run it with an input, and read its result, all through the gateway. `create` and `run` are tools the gateway runs with `mutate` (mcp's `S09-mutate.md`), and `result` one it runs with `call` (mcp's `S08-call.md`). scripts never talks to repos: it reads the repository repos made from the directory both share.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"repos","tool":"create","args":{"name":"hello"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is repos' answer to `create` (repos' `S06-create.md`), relayed, whose `structuredContent` has `id` `<rep>`, `rep_` followed by 16 lowercase hexadecimal digits, `name` `hello`, and `clone_url` `http://repos.wip.localhost:7400/hello.git`. Then, in the developer's shell:

Command:

```
$ git clone -q http://repos.wip.localhost:7400/hello.git
$ cat > hello/main.py <<'PY'
import json
import os

with open(os.environ["IKIGENBA_INPUT"]) as f:
    who = json.load(f).get("who", "world")
print(f"hello, {who}")
with open(os.path.join(os.environ["IKIGENBA_OUT_DIR"], "greeting.txt"), "w") as f:
    f.write(f"hello, {who}\n")
PY
$ git -C hello add main.py
$ git -C hello commit -q -m 'Greet'
$ git -C hello push -q origin main
$ git -C hello rev-parse HEAD
```

Output:

```
<sha>
```

Each command exits 0. `git clone` may warn on stderr that the repository is empty; the other commands write nothing to stderr. `<sha>` is the 40 lowercase hexadecimal digits of the commit just pushed. Then the agent creates the script, runs it, and, once the run has ended, reads its result:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"scripts","tool":"create","args":{"name":"hello","repo":"<rep>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"scripts","tool":"run","args":{"name":"hello","input":{"who":"mg"}}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"scripts","tool":"result","args":{"run":"<run>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 2 whose `result` is scripts' answer to `create` (`S06`), relayed by the gateway: no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"hello","repo":"<rep>","ref":"main","created":"<created>"}
```

and a `content` array of one text block holding that same object encoded compactly. The second is a JSON-RPC response with `id` 3 whose `result` is scripts' answer to `run` (`S08`), given once the commit is unpacked and the script's process has started, with a `structuredContent` of

```
{"id":"<run>","status":"running","sha":"<sha>"}
```

The third is a JSON-RPC response with `id` 4 whose `result` is scripts' answer to `result` (`S11`), with a `structuredContent` of

```
{"id":"<run>","script":"<id>","sha":"<sha>","ref":"main","user":"<user-id>","request_id":"<run-request>","trigger":"manual","status":"exited","exit_code":0,"started":"<started>","finished":"<finished>","stdout_bytes":10,"stderr_bytes":0,"truncated":false,"stdout":"hello, mg\n","stderr":"","files":[{"path":"greeting.txt","size":10}]}
```

`<id>` is `scr_` followed by 16 lowercase hexadecimal digits, the same in the first and third; `<run>` is `run_` followed by 16 lowercase hexadecimal digits, the same in the second and third; `<sha>` is the commit the shell printed; and `<run-request>` is the id the sandbox's nginx gave the `run` request, which the gateway forwarded. Asked while the script still runs, `result` answers `status` `running`, with no `exit_code` and no `finished`, and the agent asks again.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token auth accepts, whose owner is the user `<user-id>`, who has no repository named `hello` in repos.
- `python3.12` is on the `PATH` the sandbox gives scripts (`S22`).
- No script in the sandbox is named `hello`.
- The developer's shell is as the opening paragraph says, and the current directory holds nothing named `hello`.

Postconditions:

- `<user-id>` owns the repository `hello` with id `<rep>`; `<data>/apps/repos/state/repos/<rep>.git` is its bare repository, its `ikigenba.owner` is `<user-id>`, and its `refs/heads/main` is `<sha>`. scripts wrote nothing there.
- `<user-id>` owns the script `hello` with id `<id>`, running `<rep>` at `main`, and its one run `<run>`, `exited` with exit code 0, in `<data>/apps/scripts/state/scripts.db`.
- `<data>/apps/scripts/state/runs/<id>/<run>/` is the run's folder: `tree/` holds `main.py` alone, as `<sha>` holds it, without write permission (`S15`); `input.json` holds the object `{"who":"mg"}`; `stdout` holds `hello, mg` and a newline; `stderr` is empty; and `out/` holds `greeting.txt`, whose content is the same line.
- The trail holds, under the id the sandbox's nginx gave each gateway request and user `<user-id>`: repos' `repo.created` with `repo=<rep>`; scripts' `script.created` with `script=<id>`, then `tool.called` with `tool=create` and `outcome=ok`; scripts' `run.started` with `run=<run>`, `script=<id>`, `sha=<sha>`, and `trigger=manual`, then `tool.called` with `tool=run` and `outcome=ok`; and scripts' `tool.called` with `tool=result` and `outcome=ok`. scripts' `run.finished` with `run=<run>` and `status=exited` and its exit code 0 is under `<run-request>` (`S16`). repos' `repo.pushed` with `repo=<rep>` and `new=<sha>` is under the id of the `POST /hello.git/git-receive-pack`.
- `sandbox logs scripts` shows no line from scripts: it wrote nothing to stderr.

## A guest in a sandbox asks for scripts' landing page

scripts serves nothing to guests, in a sandbox as on a space. The sandbox's nginx sends a guest who opens scripts' own name to sign in at the sandbox's auth, carrying the URL it asked for, and the request never reaches scripts.

Request:

```
$ curl -si http://scripts.wip.localhost:7400/
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/?return=http://scripts.wip.localhost:7400/
```

Status 302. The response carries no `WWW-Authenticate` header; the body is not fixed. `http://scripts.wip.localhost:7400/hello/` answers the same, with the `return` ending `/hello/`, and `curl -si -X POST http://scripts.wip.localhost:7400/mcp` is answered `401` with `WWW-Authenticate: Bearer realm="ikigenba"` (sandbox's `S4-routing.md`).

Preconditions:

- The sandbox above is up.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached scripts' socket, and scripts recorded no event.
