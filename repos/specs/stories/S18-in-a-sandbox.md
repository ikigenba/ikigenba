# Stories — in a sandbox

repos reached through a sandbox: the local runner a developer brings up from a worktree, which serves every app of the checkout in plain HTTP behind its own nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The stories share one sandbox: `wip`, on port `7400`, holding `auth`, `dummy`, `events`, `mcp`, `repos`, and `telemetry`, all active, with repos at `http://repos.wip.localhost:7400` and the gateway at `http://mcp.wip.localhost:7400`. Browsers, curl, and git resolve every name under `localhost` to the loopback address. The sandbox gives repos the environment of a host, `DRAIN_SECONDS`, `IKIGENBA_SERVICES`, and the eight settings of its manifest's `[env]` at their manifest values, and variables a host never sets, none of which repos reads; repos runs the developer's own `git`, found on the `PATH` the sandbox gives it. Its working directory is the sandbox's own for the app, so its database is `<sandbox data>/apps/repos/state/repos.db` and its repositories are under `<sandbox data>/apps/repos/state/repos/` (`S15-disk.md`), created on the first `up` and kept across `down` and `up` until `wipe`. The sandbox's services file lists repos with `url` `http://repos.wip.localhost:7400`, so every clone URL repos gives in the sandbox is `http://repos.wip.localhost:7400/<name>.git`, and the credential guidance names `credential.http://*.wip.localhost:7400.helper` (`S06-create.md`). The sandbox's nginx includes repos' `etc/nginx.conf` from the checkout in repos' server, as a host's does, so a large push streams through to repos (`S16-package.md`). Every request to an app other than `auth` is first put to auth's `/check`; an agent sends the sandbox's token, the one `sandbox token` prints, as `Authorization: Bearer <token>`, and git sends it as the Basic password after the sandbox's nginx challenges it on a git path, as a host's nginx does (sandbox's `S4-routing.md`). The sandbox's nginx, its routing through `/check`, its challenge, and the token are the sandbox's own (a separate sub-project), named here only by their observable effect. repos also emits `repo.pushed` to the event bus, the `events` app (`S02-serve.md`); in the sandbox the events app runs as the user units `sandbox-wip-events.socket` and `sandbox-wip-events.service`, and an agent reads it through the gateway with its read tools `catalog` and `search`, called with the gateway's `call` tool (mcp's `S08-call.md`). Every request below is made to the sandbox; responses pass through the sandbox's nginx over HTTP/1.1. The developer's shell has the token in `IKIGENBA_TOKEN` and the helper the guidance gives installed once:

```
$ export IKIGENBA_TOKEN="$(sandbox token)"
$ git config --global credential.http://*.wip.localhost:7400.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'
```

The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05-mcp.md` fixes; the members every result carries on that revision are not repeated. As on a space (`S17-on-a-space.md`), the token is in no record any story below leaves behind: not in the trail, not in a clone's `.git/config`, and not in the developer's global git config.

## An agent reaches repos in a sandbox

An agent working in the worktree opens repos' own name in the sandbox with the stored token and gets the landing page, as on a space.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://repos.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03-landing.md`): an HTML page whose title is `repos`, whose visible text carries the heading `repos`, the heading `MCP tools` and the six tool names `list`, `show`, `status`, `create`, `rename`, and `delete`, the heading `Clone with git` and under it the credential guidance naming `credential.http://*.wip.localhost:7400.helper`, and the link `About repos` to `/about`, whose banner's profile link is titled with the email of the token's owner and leads to `http://auth.wip.localhost:7400/`, and whose footer reads `repos v<semver>`. Its banner carries the launcher button `Services`, since the sandbox's services file lists repos with its icon.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token a human created at `http://auth.wip.localhost:7400/` and stored with `sandbox token set`.
- auth's `/check` answers 200 for that token, naming its owner.

Postconditions:

- Nothing has changed but the trail: repos records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id the sandbox's nginx gave the request and the token's owner; auth records `check.allowed` for the same id with `host=repos.wip.localhost` and `path=/` (auth's `S4-check.md`).

## git in a sandbox asks for a repository without a credential

As on a space, git's first request sends no credential, and the sandbox's nginx answers auth's 401 on a git path with the Basic challenge rather than a redirect to sign in.

Request:

```
$ curl -si 'http://repos.wip.localhost:7400/notes.git/info/refs?service=git-upload-pack'
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Basic realm="ikigenba"
```

Status 401. The body is the sandbox's nginx's one line `authentication required: send your token as the password` (sandbox's `S4-routing.md`).

Preconditions:

- The sandbox above is up.
- The request carries no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed. Nothing reached repos' socket, and repos recorded no event.

## An agent creates a repository in a sandbox and pushes to it

The whole loop an agent runs in a sandbox: create a repo through the sandbox's gateway, clone it from the URL the result gives, commit, and push with plain git, the helper handing git the token. `create` is additive, so the agent calls it with the gateway's `mutate` tool (mcp's `S09-mutate.md`).

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"repos","tool":"create","args":{"name":"notes"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is repos' answer to `create` (`S06-create.md`), relayed by the gateway: no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"notes","default_branch":"main","size_bytes":<n>,"available":true,"created":"<created>","clone_url":"http://repos.wip.localhost:7400/notes.git","credentials":"<guidance>"}
```

and a `content` array of one text block holding that same object encoded compactly, where `<guidance>` is the credential guidance naming `credential.http://*.wip.localhost:7400.helper`. Then, in the developer's shell:

Command:

```
$ git clone -q http://repos.wip.localhost:7400/notes.git
$ echo hello > notes/README.md
$ git -C notes add README.md
$ git -C notes commit -q -m 'First commit'
$ git -C notes push -q origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `git clone` may warn on stderr that the repository is empty; the other commands write nothing to stderr. `<new>` is the sha `git -C notes rev-parse HEAD` prints.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token auth accepts, whose owner is the user `<user-id>` and has no repo named `notes` in this sandbox.
- The developer's shell is as the opening paragraph says, and the current directory holds nothing named `notes`.

Postconditions:

- `<user-id>` owns `notes` with id `<id>`; `<sandbox data>/apps/repos/state/repos/<id>.git` is its bare repository, and its `refs/heads/main` is `<new>`. `show` gives `<new>` as `head`.
- `git -C notes remote get-url origin` prints `http://repos.wip.localhost:7400/notes.git`, holding no credential.
- The trail holds repos' `repo.created` with `repo=<id>` and `owner=<user-id>` under the gateway request's id, and `repo.pushed` with `repo=<id>`, `ref=refs/heads/main`, `old` of 40 zeros, and `new=<new>` under the id the sandbox's nginx gave the `POST /notes.git/git-receive-pack`, both under user `<user-id>`; auth's check events for the git requests carry `credential=basic`.
- `sandbox logs repos` shows no line from repos: it wrote nothing to stderr.

## A developer pushes a large commit through a sandbox

As on a space, a commit far larger than git's 1 MiB buffer goes in a chunked request body, and the sandbox's nginx, carrying repos' fragment, passes it through without a size limit, so a 50 MiB push succeeds.

Command:

```
$ head -c 52428800 /dev/urandom > notes/blob.bin
$ git -C notes add blob.bin
$ git -C notes commit -q -m 'Add a large file'
$ git -C notes push -q origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `ls-remote` writes its one line to stdout; nothing is on stderr. `<new>` is the sha `git -C notes rev-parse HEAD` prints.

Preconditions:

- The clone of `An agent creates a repository in a sandbox and pushes to it` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- The sandbox's nginx was configured by an `up` run with `repos/etc/nginx.conf` in the checkout.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`; no `413` or other refusal reached git.
- The trail holds repos' `repo.pushed` with `ref=refs/heads/main`, `old=<old>`, and `new=<new>`, and `request.finished` with `status=200` and `request_bytes` of at least 52428800, under the id the sandbox's nginx gave the push and the token's owner.

## An agent in a sandbox reads the event catalog through the gateway

Before any push has happened, an agent can learn from events what repos emits. `catalog` is a read tool of events, so the agent calls it with the gateway's `call` tool.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `catalog`, relayed by the gateway: no `isError` member, and a `structuredContent` that lists the event `repo.pushed` as emitted by the service `repos`, with the four attributes `repo`, `ref`, `old`, and `new`.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token auth accepts.
- No repository in the sandbox has been pushed to since `up`.

Postconditions:

- Nothing has changed.

## An agent in a sandbox finds a developer's push among the events

Once git has accepted a push, repos emits `repo.pushed` to events, one for each ref the push moved, with the same attributes as the trail's record of it (`S02-serve.md`). An agent finds it with events' `search`.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"events":["repo.pushed"]}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `search`, relayed by the gateway: no `isError` member, and a `structuredContent` holding, for the push, exactly one event, since it moved one ref. That event's `id` begins `evt_`; its `service` is `repos` and its `event` `repo.pushed`; its `attrs` are `repo` `<id>`, `ref` `refs/heads/main`, `old` 40 zeros, and `new` `<new>`; its `request_id` is the id the sandbox's nginx gave the `POST /notes.git/git-receive-pack`; its `user` is `<user-id>`; its `cause` is empty; and its `depth` is 0, since the push sent no `X-Event-Cause` or `X-Event-Depth`.

Preconditions:

- The sandbox above is up, with `events` active throughout, and `sandbox token` prints a token auth accepts, owned by `<user-id>`.
- `An agent creates a repository in a sandbox and pushes to it` has run: `<user-id>` owns `notes` with id `<id>`, whose `main` the push created at `<new>`.

Postconditions:

- Nothing has changed. The push's `repo.pushed` is still in the trail, as `An agent creates a repository in a sandbox and pushes to it` says; the event sent to events is in addition to it.

## A developer in a sandbox pushes with an event as the cause

A push made in reaction to an event names that event with the headers `X-Event-Cause` and `X-Event-Depth` (`S02-serve.md`), and the sandbox's nginx passes them through to repos, since it sets neither itself. The developer pushes with the event's id `evt_0123456789abcdef` as the cause, at depth `0`.

Command:

```
$ echo caused >> notes/README.md
$ git -C notes commit -q -a -m 'Caused commit'
$ git -C notes -c http.extraHeader='X-Event-Cause: evt_0123456789abcdef' -c http.extraHeader='X-Event-Depth: 0' push -q origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `ls-remote` writes its one line to stdout; nothing is on stderr. `<new>` is the sha `git -C notes rev-parse HEAD` prints. The push behaves exactly as it does without the headers. Then the `search` request of `An agent in a sandbox finds a developer's push among the events` answers with an event for this push: `attrs` `repo` `<id>`, `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>`, under the id the sandbox's nginx gave this push and user `<user-id>`, with `cause` `evt_0123456789abcdef` and `depth` 1.

Preconditions:

- The clone of `An agent creates a repository in a sandbox and pushes to it` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- `events` is active throughout.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`.
- The trail holds repos' `repo.pushed` with `old=<old>` and `new=<new>` and `request.finished` with `status=200` under the push's id, exactly as for a push without the headers.

## A developer pushes in a sandbox while events is stopped

events being away is not a reason to refuse a push. repos keeps the event for a few minutes and delivers it once events is back (`S02-serve.md`), so a developer who starts events again within that time loses nothing.

Command:

```
$ systemctl --user stop sandbox-wip-events.socket sandbox-wip-events.service
$ echo again >> notes/README.md
$ git -C notes commit -q -a -m 'Second commit'
$ git -C notes push -q origin main
$ git -C notes ls-remote origin refs/heads/main
$ systemctl --user start sandbox-wip-events.socket sandbox-wip-events.service
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `ls-remote` writes its one line to stdout; nothing is on stderr. `<new>` is the sha `git -C notes rev-parse HEAD` prints. The push behaves exactly as it does with events active. Within a few minutes of the `start`, the `search` request of `An agent in a sandbox finds a developer's push among the events` answers with an event for this push: `attrs` `repo` `<id>`, `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>`, under the id the sandbox's nginx gave this push and user `<user-id>`, with `cause` empty and `depth` 0.

Preconditions:

- The clone of `An agent creates a repository in a sandbox and pushes to it` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- `events` is active when the first command runs, and the developer starts it again within a few minutes of the push.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`.
- The trail holds repos' `repo.pushed` with `old=<old>` and `new=<new>` and `request.finished` with `status=200` under the push's id, as with events active.
- events holds the push's `repo.pushed` once, delivered after it was started again. The trail holds no `event.lost` for it.

## A developer pushes in a sandbox while events stays stopped too long

repos keeps an event for events only a few minutes. When events is still away after that, the event is dropped, and the trail says so: repos records `event.lost` carrying the dropped event's `id` (`S02-serve.md`). The push itself is as unaffected as in the story above.

Command:

```
$ systemctl --user stop sandbox-wip-events.socket sandbox-wip-events.service
$ echo later >> notes/README.md
$ git -C notes commit -q -a -m 'Third commit'
$ git -C notes push -q origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `ls-remote` writes its one line to stdout; nothing is on stderr. `<new>` is the sha `git -C notes rev-parse HEAD` prints.

Preconditions:

- The clone of `An agent creates a repository in a sandbox and pushes to it` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- `events` is active when the first command runs, and the developer leaves it stopped well past the few minutes repos keeps an event before starting it again with `systemctl --user start sandbox-wip-events.socket sandbox-wip-events.service`.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`, and the trail holds repos' `repo.pushed` with `old=<old>` and `new=<new>` under the push's id, as with events active.
- Once events is active again, the `search` request of `An agent in a sandbox finds a developer's push among the events` answers with no event for this push, and no later request brings it back.
- The trail holds `event.lost` from `repos` whose attributes carry the `evt_` id repos gave the dropped `repo.pushed`.

## An agent asks a sandbox for repos' event paths

`/events` and `/declarations` are how events reaches repos, on repos' own socket only. At repos' public name in the sandbox the sandbox's nginx answers both 404 for every method, because the sandbox includes repos' `etc/nginx.conf` in its server, and the request never reaches repos. The forms below are answered the same way.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://repos.wip.localhost:7400/events
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -d '{}' http://repos.wip.localhost:7400/events
```

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://repos.wip.localhost:7400/declarations
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -d '{}' http://repos.wip.localhost:7400/declarations
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed. A request to either path with no token is answered 404 as well; this story does not fix which of the sandbox's refusals comes first, only that neither reaches repos.

Preconditions:

- The sandbox above is up from an `up` run with `repos/etc/nginx.conf` in the checkout, and `sandbox token` prints a token auth accepts.

Postconditions:

- Nothing has changed. repos records no event for any of the requests: none reached its socket.
