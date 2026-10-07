# Stories — on a space

repos reached through a space: the file `S16-package.md` describes, deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at `repos.<space>` over TLS, so on the space `sbx.ikigenba.dev` repos answers at `repos.sbx.ikigenba.dev`. nginx on the space proxies to repos' socket, `/run/ikigenba/repos.sock` (`S02-serve.md`), and includes repos' own `etc/nginx.conf` in that server, so a push of any size streams through to repos and is refused, if at all, by repos' own limits (`S12-limits.md`). The space authenticates every request before it reaches repos and passes the caller on in `X-User-Id` and `X-User-Email`, with the request's id in `X-Request-Id`; repos has no unauthenticated case except its two bus paths, `/events` and `/declarations`, which nginx answers 404 and never passes on (the story "An agent asks a space for repos' event paths" below), so any other request that arrives at all is one of a known caller. git sends its credential as `Authorization: Basic` with the personal access token as the password, which auth's `/check` decides exactly as the same token sent as a bearer (auth's `S4-check.md`); and since git sends nothing until it is challenged, the host's nginx answers a request it cannot authenticate on a path ending `/info/refs`, `/git-upload-pack`, or `/git-receive-pack` with `401` and `WWW-Authenticate: Basic realm="ikigenba"` rather than a redirect to sign in (opsctl's `S5-nginx.md`). That challenge is the space's, on every app's server, and not repos' fragment's. The host's services file is `/var/lib/ikigenba/services.json`, which lists repos with `url` `https://repos.sbx.ikigenba.dev`, its socket, and marked for MCP since its manifest has `mcp = true`, so every clone URL repos gives is `https://repos.sbx.ikigenba.dev/<name>.git` and the MCP gateway offers repos' six tools through `https://mcp.sbx.ikigenba.dev/mcp` (mcp's `S11-on-a-space.md`). repos runs as `/opt/repos/bin/repos` with `/opt/repos` as its working directory, so its database is `/opt/repos/state/repos.db` and its repositories are under `/opt/repos/state/repos/` (`S15-disk.md`); the host keeps and replicates the declared database as it does auth's, which is opsctl's doing and is named here only by its effect. `/opt/repos/etc/env` carries the eight settings of the manifest's `[env]` beside the space's `DRAIN_SECONDS` and `IKIGENBA_SERVICES`, and the host provides the `git` repos runs (opsctl's `S4-init.md`). repos also emits `repo.pushed` to the event bus, the `events` app (`S02-serve.md`), deployed and active on the space through its own chain unless a story says otherwise; an agent reads the events app through the gateway with its read tools `catalog` and `search`, called with the gateway's `call` tool (mcp's `S08-call.md`). The developer's shell in these stories is on their own machine, with `git` installed, the token `ikp_<token>` (auth's `S5-tokens.md`), owned by the user `u_7f3a9c21`, `mg@example.com`, in the environment variable `IKIGENBA_TOKEN`, and the credential helper the guidance gives (`S06-create.md`) installed once:

```
$ git config --global credential.https://*.sbx.ikigenba.dev.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'
```

The stories prove the whole path from checkout to browser, git, and agent, and nothing about repos that the earlier groups do not already say. devctl and opsctl are named only by their published commands. The MCP requests below are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05-mcp.md` fixes; the members every result carries on that revision are not repeated. The token's secret is in no record any story below leaves behind: not in the trail, not in nginx's logs on the host, not in a clone's `.git/config`, and not in the developer's global git config, which holds the helper and nothing of the token.

## A visitor reaches repos' landing page on a space

The visitor asks for the landing page over TLS at repos' hostname on the space. The gate in front of repos authenticates the request and hands repos the caller's identity, and repos renders the page for that caller.

Request:

```
$ curl -si https://repos.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03-landing.md`): an HTML page whose title is `repos`, whose banner's profile link is titled with the email address of the caller the gate authenticated, whose visible text carries the heading `repos`, the heading `MCP tools` and the six tool names `list`, `show`, `status`, `create`, `rename`, and `delete`, the heading `Clone with git` and under it the credential guidance naming `credential.https://*.sbx.ikigenba.dev.helper`, and a link `About repos` to `/about`, and whose footer reads `repos v<semver>`, the version the deployed binary's `repos --version` prints (`S01-bootstrap.md`), the same one `space status` reports for repos. Its stylesheet is `https://repos.sbx.ikigenba.dev/_appkit/theme.css`, and the fonts that stylesheet loads are under the same `https://repos.sbx.ikigenba.dev/_appkit/` (`S04-assets.md`): a browser showing the page requests its style from repos' own host and from no other origin. Its button feedback script is `https://repos.sbx.ikigenba.dev/_appkit/feedback.js` and its icon `https://repos.sbx.ikigenba.dev/_appkit/favicon.svg`, both from the same host. In the banner, the profile link leads to `https://auth.sbx.ikigenba.dev/`, and the `Sign out` button is in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout` (`S03-landing.md`).

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance is `running`, and `opsctl` is installed on it.
- A tag `repos/v<semver>` points at the commit `devctl build repos` was run at, and it wrote `repos/dist/repos-v<semver>.tar.xz`.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev repos/dist/repos-v<semver>.tar.xz` exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows `repos v<semver> active active -`.
- `/opt/repos/state/repos.db` is the database repos opened, created on its first start on this space or kept from an earlier release, and brought up to date with the migrations this release carries (`S01`).
- auth and telemetry are deployed and active on the space through their own chains; the space routes `repos.sbx.ikigenba.dev` through its authenticating gate, which sets `X-User-Id` and `X-User-Email` on what it passes to repos and refuses a request it cannot authenticate before repos sees it.
- The caller holds a credential the gate accepts, and the email that credential names is the one the page's profile link is titled with.

Postconditions:

- Nothing has changed but the trail: repos records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id nginx gave the request and the user the gate named (`S02-serve.md`).

## A visitor on a space opens the service launcher

On a space the host sets `IKIGENBA_SERVICES` in repos' environment to the path of its services file (`S02-serve.md`). That file lists every service installed on the host, and repos' entry carries an icon because repos' package ships `share/icon.svg` (`S16-package.md`), which is what puts repos in the launcher (`S03-landing.md`). The launcher's text and behavior are `S03-landing.md`'s; this story fixes only what the visitor sees on a space.

Request:

```
$ curl -si https://repos.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page of the story above, and its banner carries the launcher button labelled `Services` (`S03-landing.md`). In a browser, pressing the button opens a list of the space's services with a search box labelled `Find a service`; each entry shows a service's icon and name. repos' own entry is in the list and is marked as the current page. The launcher's script is `https://repos.sbx.ikigenba.dev/_appkit/launcher.js` (`S04-assets.md`), so the launcher, like the style, needs nothing from any other origin.

Preconditions:

- Everything the story above requires holds: repos `v<semver>` is deployed and active on `sbx.ikigenba.dev`, and the caller holds a credential the gate accepts.
- `repos/dist/repos-v<semver>.tar.xz` holds `share/icon.svg` (`S16-package.md`).
- The host sets `IKIGENBA_SERVICES` in repos' environment to the path of its services file, and that file lists repos with its icon.

Postconditions:

- Nothing has changed but repos' own two records of the request, as in the story above.

## git on a space asks for a repository without a credential

git's first request for a clone sends no credential. auth's `/check` answers it 401, and the host's nginx, on a git path, turns that into the Basic challenge rather than a redirect to sign in, so git knows to ask its credential helper. The same answer meets the pack requests when they arrive without a credential.

Request:

```
$ curl -si 'https://repos.sbx.ikigenba.dev/notes.git/info/refs?service=git-upload-pack'
```

```
$ curl -si -X POST https://repos.sbx.ikigenba.dev/notes.git/git-receive-pack
```

Response:

```
HTTP/2 401
content-type: text/plain
www-authenticate: Basic realm="ikigenba"
```

Status 401. The body is the host's nginx's one line `authentication required: send your token as the password` (opsctl's `S5-nginx.md`). `/notes.git/git-upload-pack` and `/notes.git/info/refs?service=git-receive-pack` answer the same.

Preconditions:

- repos `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches repos' landing page on a space`.
- The request carries no `ikigenba_session` cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/repos.sock`, and repos recorded no event; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none` (auth's `S4-check.md`).

## A developer clones a repository on a space with the credential helper

This is what repos is for from the outside: plain git, with the token handed over by the helper the guidance gives and written nowhere. git asks for the refs, is challenged (the story above), asks the helper, which prints the token from the environment, and retries with it as the Basic password; auth admits it as the token's owner, and repos serves the repo named `notes` among that owner's repos (`S11-git.md`). The clone URL is the one `show` gives, and it holds no credential.

Command:

```
$ git clone https://repos.sbx.ikigenba.dev/notes.git
$ git -C notes remote get-url origin
$ git -C notes rev-parse HEAD
```

Output:

```
https://repos.sbx.ikigenba.dev/notes.git
<head>
```

Each command exits 0. `git clone` writes its own messages, beginning `Cloning into 'notes'...`, to stderr and nothing to stdout; the other two write their one line to stdout and nothing to stderr. `<head>` is the sha `show` gives as `head` for `notes`, and the clone's working tree holds that commit's files.

Preconditions:

- repos `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches repos' landing page on a space`.
- `u_7f3a9c21` owns `notes`, `rep_3f9a0c1d2e4b5a69`, and it has at least one commit on `main`.
- The developer's shell is as the opening paragraph says: `IKIGENBA_TOKEN` holds `ikp_<token>`, a token auth honors whose owner is `u_7f3a9c21`, and the helper is installed for `https://*.sbx.ikigenba.dev`. The current directory holds nothing named `notes`.

Postconditions:

- `./notes` is a clone of `notes` whose `origin` is `https://repos.sbx.ikigenba.dev/notes.git`.
- Nothing has changed in repos: the repo's refs and objects are as they were.
- The trail holds, for the two requests git sent with the token, `GET /notes.git/info/refs?service=git-upload-pack` and `POST /notes.git/git-upload-pack`: auth's `check.allowed` with `credential=basic`, `host=repos.sbx.ikigenba.dev`, and `token=<token-id>` for each; and repos' records for each, under the id nginx gave it and user `u_7f3a9c21`, the second with `repo.fetched` carrying `repo=rep_3f9a0c1d2e4b5a69` between its `request.started` and its `request.finished` with `status=200` (`S11-git.md`). The challenged first request left only auth's `check.refused`.
- The token's secret is in none of: the trail, the host's nginx logs, `notes/.git/config`, and the developer's `~/.gitconfig`.

## A developer pushes a large commit through a space

A commit far larger than the 1 MiB git buffers in memory is sent with a chunked request body. The host's nginx sets no body limit for repos and passes the body on as it arrives (`S16-package.md`), and repos streams it into git (`S11-git.md`), so a push of 50 MiB of incompressible data, well under `PUSH_MAX_BYTES`, succeeds as a small one does.

Command:

```
$ head -c 52428800 /dev/urandom > notes/blob.bin
$ git -C notes add blob.bin
$ git -C notes commit -q -m 'Add a large file'
$ git -C notes push origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `git push` writes its own messages to stderr, ending with the line `   <old7>..<new7>  main -> main`, where `<old7>` and `<new7>` are the abbreviated shas, and nothing to stdout. `ls-remote` writes its one line to stdout: `<new>`, the sha `git -C notes rev-parse HEAD` prints, a tab, and the ref name.

Preconditions:

- The clone of `A developer clones a repository on a space with the credential helper` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- `notes` is well under `REPO_MAX_BYTES` (1073741824 bytes) on disk.
- repos runs with the manifest's `[env]` defaults on the space: `PUSH_MAX_BYTES` is 104857600.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`, and `show` gives `<new>` as `head` and a `size_bytes` larger by roughly the size of the pushed pack.
- Neither the host's nginx nor repos refused the push: no `413` or other refusal reached git.
- The trail holds, under the id nginx gave the `POST /notes.git/git-receive-pack` and user `u_7f3a9c21`: repos' `request.started`, `repo.pushed` with `repo=rep_3f9a0c1d2e4b5a69`, `ref=refs/heads/main`, `old=<old>`, and `new=<new>`, and `request.finished` with `status=200` and `request_bytes` of at least 52428800 (`S02-serve.md`). No record names the file, the commit message, or the token.

## An agent on a space creates a repository through the gateway

The expected path for an agent: it asks the gateway to run repos' `create` on its behalf, and gets back the clone URL and the guidance for giving git the token, then pushes with plain git. `create` is an additive tool (`S05-mcp.md`), so the agent calls it with the gateway's `mutate` tool (mcp's `S09-mutate.md`); the gateway reaches repos directly on `/run/ikigenba/repos.sock` and relays its answer.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"repos","tool":"create","args":{"name":"drafts"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is repos' answer to `create` (`S06-create.md`), relayed: no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"drafts","default_branch":"main","size_bytes":<n>,"available":true,"created":"<created>","clone_url":"https://repos.sbx.ikigenba.dev/drafts.git","credentials":"<guidance>"}
```

and a `content` array of one text block holding that same object encoded compactly. `<id>` is `rep_` followed by 16 lowercase hexadecimal digits; `<guidance>` is the credential guidance of `S06-create.md`, naming `credential.https://*.sbx.ikigenba.dev.helper`; neither `clone_url` nor anything else in the result holds the token. The result's `io.modelcontextprotocol/serverInfo` is the gateway's, `{"name":"mcp","version":"v<semver>"}`, not repos'.

Preconditions:

- repos `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches repos' landing page on a space`, and the host's services file lists `repos` enabled, marked for MCP, with the socket `/run/ikigenba/repos.sock`.
- mcp is deployed and active on the space through its own `S11-on-a-space.md` chain, serving `https://mcp.sbx.ikigenba.dev/mcp`.
- The agent holds `ikp_<token>`, whose owner `u_7f3a9c21` has no repo named `drafts`.

Postconditions:

- `u_7f3a9c21` owns a new, empty repo `drafts` with id `<id>`, and `/opt/repos/state/repos/<id>.git` is its bare repository (`S15-disk.md`).
- With the helper of the opening paragraph, `git clone https://repos.sbx.ikigenba.dev/drafts.git` exits 0 and makes an empty clone, and a push to it of any commit on `main` succeeds.
- mcp wrote nothing to stderr, and neither did repos.
- The trail holds, under the id nginx gave the request and user `u_7f3a9c21`, auth's `check.allowed` with `credential=token`, the gateway's events for the call, and repos' own, among them `repo.created` with `repo=<id>` and `owner=u_7f3a9c21` and `tool.called` with `tool=create`, `kind=additive`, and `outcome=ok` (`S02-serve.md`). No event attribute holds the name `drafts` or the token.

## git on a space sends a token auth refuses

A token that is disabled, expired, unknown, or whose owner has not signed in for 30 days is refused by auth's `/check` with 403 whichever way it is sent (auth's `S4-check.md`), and the host's nginx passes that 403 through: it is not a challenge, so git does not ask again and the clone fails. A malformed Basic credential gets the same 403.

Request:

```
$ curl -si -u 'token:ikp_<refused>' 'https://repos.sbx.ikigenba.dev/notes.git/info/refs?service=git-upload-pack'
```

Response:

```
HTTP/2 403
```

Status 403. The body is not fixed, and the response carries no `WWW-Authenticate` header. With `IKIGENBA_TOKEN` holding `ikp_<refused>`, `git clone https://repos.sbx.ikigenba.dev/notes.git` exits 128, and git's last line on stderr is `fatal: unable to access 'https://repos.sbx.ikigenba.dev/notes.git/': The requested URL returned error: 403`.

Preconditions:

- repos `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches repos' landing page on a space`.
- `ikp_<refused>` is a value auth does not honor, for any of the reasons above.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/repos.sock`, and repos recorded no event.
- auth recorded `check.refused` with `outcome=forbidden`, `credential=basic`, `host=repos.sbx.ikigenba.dev`, and `path=/notes.git/info/refs`, under no user and with no `token` attribute; neither the password nor the encoded credential is in the trail or in the host's nginx logs; the host's access log holds the Basic username, as opsctl's `S5-nginx.md` log format records it.

## An agent on a space reads the event catalog through the gateway

Before any push has happened on the space, an agent can learn from events what repos emits. `catalog` is a read tool of events, so the agent calls it with the gateway's `call` tool; the gateway relays events' answer.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `catalog`, relayed: no `isError` member, and a `structuredContent` that lists the event `repo.pushed` as emitted by the service `repos`, with the four attributes `repo`, `ref`, `old`, and `new`. The result's `io.modelcontextprotocol/serverInfo` is the gateway's.

Preconditions:

- repos `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches repos' landing page on a space`, and mcp and events are deployed and active on the space.
- No repository on the space has been pushed to since repos and events were deployed.
- The agent holds `ikp_<token>`, a token the gate accepts.

Postconditions:

- Nothing has changed.

## An agent on a space finds a developer's push among the events

Once git has accepted a push, repos emits `repo.pushed` to events, one for each ref the push moved, with the same attributes as the trail's record of it (`S02-serve.md`). An agent finds the large push of `A developer pushes a large commit through a space` with events' `search`.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"events":["repo.pushed"]}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `search`, relayed: no `isError` member, and a `structuredContent` holding, for that push, exactly one event, since it moved one ref. That event's `id` begins `evt_`; its `service` is `repos` and its `event` `repo.pushed`; its `attrs` are `repo` `rep_3f9a0c1d2e4b5a69`, `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>`; its `request_id` is the id nginx gave the `POST /notes.git/git-receive-pack`; its `user` is `u_7f3a9c21`; its `cause` is empty; and its `depth` is 0.

Preconditions:

- `A developer pushes a large commit through a space` has run, moving `notes`' `main` from `<old>` to `<new>`, with events active throughout.
- mcp and events are deployed and active on the space, and the agent holds `ikp_<token>`, whose owner is `u_7f3a9c21`.

Postconditions:

- Nothing has changed. The push's `repo.pushed` is still in the trail, as `A developer pushes a large commit through a space` says; the event sent to events is in addition to it.

## A developer pushes on a space while events is disabled

events being away is not a reason to refuse a push. An operator has disabled events on the space with `sudo opsctl disable events` (opsctl's `S7-apps.md`); the developer pushes and sees nothing different. repos keeps the event and delivers it once events is back (`S02-serve.md`): it sends events one at a time, in order, and each event's retry window of about five minutes runs from its first attempt, so an operator who enables events again within about five minutes of the push loses nothing.

Command:

```
$ echo again >> notes/README.md
$ git -C notes commit -q -a -m 'Second commit'
$ git -C notes push origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `git push` writes its own messages to stderr, ending with the line `   <old7>..<new7>  main -> main`, where `<old7>` and `<new7>` are the abbreviated shas, and nothing to stdout, exactly as with events active. `ls-remote` writes its one line to stdout: `<new>`, the sha `git -C notes rev-parse HEAD` prints, a tab, and the ref name.

Preconditions:

- The clone of `A developer clones a repository on a space with the credential helper` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- events is deployed on the space and disabled, and the operator runs `sudo opsctl enable events` within about five minutes of the push.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`, and the trail holds repos' `repo.pushed` with `repo=rep_3f9a0c1d2e4b5a69`, `ref=refs/heads/main`, `old=<old>`, and `new=<new>` and `request.finished` with `status=200`, under the id nginx gave the push and user `u_7f3a9c21`, as with events active.
- Once events is enabled again, the `search` request of `An agent on a space finds a developer's push among the events` answers with an event for this push, with those four attributes, that request id and user, `cause` empty and `depth` 0.
- The trail holds no `event.lost` for it.

## A developer pushes on a space while events stays disabled too long

repos keeps an event for events only for its retry window of about five minutes, which runs from its first delivery attempt, so an event queued behind another is kept longer. When events is still disabled after that, the event is dropped, and the trail says so: repos records `event.lost` carrying the dropped event's `id` (`S02-serve.md`). The push itself is as unaffected as in the story above.

Command:

```
$ echo later >> notes/README.md
$ git -C notes commit -q -a -m 'Third commit'
$ git -C notes push origin main
$ git -C notes ls-remote origin refs/heads/main
```

Output:

```
<new>	refs/heads/main
```

Each command exits 0. `git push` writes its own messages to stderr, ending with the line `   <old7>..<new7>  main -> main`, where `<old7>` and `<new7>` are the abbreviated shas, and nothing to stdout, exactly as with events active. `ls-remote` writes its one line to stdout: `<new>`, the sha `git -C notes rev-parse HEAD` prints, a tab, and the ref name.

Preconditions:

- The clone of `A developer clones a repository on a space with the credential helper` exists, and its `main` is `<old>`, the sha `notes`' `main` points at in repos.
- events is deployed on the space and disabled, and stays disabled well past the retry window repos keeps an event before the operator runs `sudo opsctl enable events`.

Postconditions:

- `notes`' `refs/heads/main` in repos is `<new>`, and the trail holds repos' `repo.pushed` with `repo=rep_3f9a0c1d2e4b5a69`, `ref=refs/heads/main`, `old=<old>`, and `new=<new>` under the id nginx gave the push and user `u_7f3a9c21`, as with events active.
- Once events is enabled again, the `search` request of `An agent on a space finds a developer's push among the events` answers with no event for this push, and no later request brings it back.
- The trail holds `event.lost` from `repos` whose attributes carry the `evt_` id repos gave the dropped `repo.pushed`.

## An agent asks a space for repos' event paths

`/events` and `/declarations` are how events reaches repos, on `/run/ikigenba/repos.sock` only. At repos' public name the space's nginx answers both 404 for every method, because of the fragment repos ships (`S16-package.md`); a credential makes no difference, since the answer is nginx's and the request never reaches repos. The forms below are answered the same way.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://repos.sbx.ikigenba.dev/events
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -d '{}' https://repos.sbx.ikigenba.dev/events
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://repos.sbx.ikigenba.dev/declarations
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -d '{}' https://repos.sbx.ikigenba.dev/declarations
```

Response:

```
HTTP/2 404
```

Status 404. The body is not fixed. A request to either path with no credential is answered 404 as well; this story does not fix which of the space's refusals comes first, only that neither reaches repos.

Preconditions:

- repos `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches repos' landing page on a space`, so the host's nginx includes `/opt/repos/etc/nginx.conf` in repos' server.
- The agent holds a valid token `ikp_<token>` (auth's `S5-tokens.md`) the gate accepts.

Postconditions:

- Nothing has changed. repos records no event for any of the requests: none reached `/run/ikigenba/repos.sock`.
