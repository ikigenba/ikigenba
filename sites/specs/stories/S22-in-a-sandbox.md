# Stories — in a sandbox

sites reached through a sandbox: the local runner a developer brings up from a worktree, which serves every app of the checkout in plain HTTP behind its own nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The stories share one sandbox: `wip`, on port `7400`, holding `auth`, `dummy`, `mcp`, `repos`, `sites`, and `telemetry`, all active, with sites at `http://sites.wip.localhost:7400`, repos at `http://repos.wip.localhost:7400`, auth at `http://auth.wip.localhost:7400`, and the gateway at `http://mcp.wip.localhost:7400`. Browsers, curl, and git resolve every name under `localhost` to the loopback address. The sandbox gives sites the environment of a host, `DRAIN_SECONDS`, `IKIGENBA_SERVICES`, and the three settings of its manifest's `[env]` at their manifest values, and variables a host never sets, none of which sites reads; sites runs the developer's own `git`, found on the `PATH` the sandbox gives it. Each app's working directory is the sandbox's own for that app, `<data>/apps/<app>/`, where `<data>` is the sandbox's data directory (sandbox's `S3-apps.md`), so sites' catalog is `<data>/apps/sites/state/sites.db` and its unpacked trees are under `<data>/apps/sites/cache/sites/` (`S18`), while repos keeps its bare repositories under `<data>/apps/repos/state/repos/` (repos' `S18-in-a-sandbox.md`). `REPOS_DIR`, at its manifest default `../repos/state/repos`, is resolved against sites' working directory, so it names `<data>/apps/sites/../repos/state/repos`, which is repos' directory: in a sandbox as on a host, sites reads exactly the repositories repos holds, with no setting changed. The catalog is created on the first `up` and kept across `down` and `up` until `wipe`. The sandbox's services file lists sites with `url` `http://sites.wip.localhost:7400`, so every site's URL sites gives in the sandbox is `http://sites.wip.localhost:7400/<slug>/`.

sites' manifest sets `guests = true`, so the sandbox's nginx puts every request to sites but those under `/mcp` and git's paths to auth's `/check/open` instead of `/check`, and a request with no credential reaches sites with no `X-User-Id` and no `X-User-Email` (sandbox's `S3-apps.md`, `A developer brings up an app that welcomes guests`; sandbox's `S4-routing.md`, `A browser that has not signed in reaches an app that welcomes guests`); `/mcp` keeps `/check` and its bearer challenge. Every request the sandbox's nginx passes carries `X-Forwarded-Proto: http` and an `X-Request-Id` nginx made, so sites' answers name `http` addresses and its visitor cookie is not `Secure` (`S14`). An agent sends the sandbox's token, the one `sandbox token` prints, as `Authorization: Bearer <token>`, and git sends it as the Basic password after the sandbox's nginx challenges it on a git path (sandbox's `S4-routing.md`). The sandbox's nginx, its routing through `/check` and `/check/open`, and the token are the sandbox's own (a separate sub-project), named here only by their observable effect. Every request below is made to the sandbox; responses pass through the sandbox's nginx over HTTP/1.1. The developer's shell has the token in `IKIGENBA_TOKEN` and the git credential helper repos' guidance gives installed once (repos' `S18-in-a-sandbox.md`):

```
$ export IKIGENBA_TOKEN="$(sandbox token)"
$ git config --global credential.http://*.wip.localhost:7400.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'
```

The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. As on a space (`S21`), the token is in no record any story below leaves behind.

## An agent reaches sites' landing page in a sandbox

An agent working in the worktree opens sites' own name in the sandbox with the stored token and gets the landing page, as on a space.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://sites.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `sites`, whose visible text carries the heading `sites`, the heading `Sites` and the line `Every site answers at http://sites.wip.localhost:7400/<slug>/, where <slug> is its slug.`, the heading `MCP tools` and the seven tool names `list`, `show`, `create`, `publish`, `update`, `delete`, and `apex`, and the link `About sites` to `/about`, whose banner's profile link is titled with the email of the token's owner and leads to `http://auth.wip.localhost:7400/`, and whose footer reads `sites v<semver>`. Its banner carries the launcher button `Services`, since the sandbox's services file lists sites with its icon. With no site in the sandbox yet, the `Sites` section reads `No sites yet.`

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token a human created at `http://auth.wip.localhost:7400/` and stored with `sandbox token set`.
- auth's `/check/open` answers 200 for that token, naming its owner.
- sites' catalog holds no site.

Postconditions:

- Nothing has changed but the trail: sites records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id the sandbox's nginx gave the request and the token's owner; auth records `check.allowed` for the same id with `host=sites.wip.localhost` and `path=/` (auth's `S4-check.md`).

## An agent publishes a site in a sandbox from a repository it pushed

The whole loop an agent runs in a sandbox: create a repository in repos through the sandbox's gateway, push a page to it with plain git, then create a site from that repository and publish it, all through the gateway. Every tool here is one the gateway runs with `mutate` (mcp's `S09-mutate.md`). sites never talks to repos: it reads the repository repos made from the directory both share.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"repos","tool":"create","args":{"name":"pages"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is repos' answer to `create` (repos' `S06-create.md`), relayed, whose `structuredContent` has `id` `<rep>`, `rep_` followed by 16 lowercase hexadecimal digits, `name` `pages`, and `clone_url` `http://repos.wip.localhost:7400/pages.git`. Then, in the developer's shell:

Command:

```
$ git clone -q http://repos.wip.localhost:7400/pages.git
$ echo '<h1>hello</h1>' > pages/index.html
$ git -C pages add index.html
$ git -C pages commit -q -m 'First page'
$ git -C pages push -q origin main
$ git -C pages rev-parse HEAD
```

Output:

```
<sha>
```

Each command exits 0. `git clone` may warn on stderr that the repository is empty; the other commands write nothing to stderr. `<sha>` is the 40 lowercase hexadecimal digits of the commit just pushed. Then the agent creates the site and publishes it:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"sites","tool":"create","args":{"name":"pages","repo":"<rep>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"sites","tool":"publish","args":{"name":"pages"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 2 whose `result` is sites' answer to `create` (`S06`), relayed by the gateway: no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"pages","slug":"pages","url":"http://sites.wip.localhost:7400/pages/","repo":"<rep>","ref":"main","visibility":"public","listed":true,"created":"<created>"}
```

and a `content` array of one text block holding that same object encoded compactly. The second is a JSON-RPC response with `id` 3 whose `result` is sites' answer to `publish` (`S08`), with a `structuredContent` of

```
{"id":"<id>","name":"pages","slug":"pages","url":"http://sites.wip.localhost:7400/pages/","repo":"<rep>","ref":"main","visibility":"public","listed":true,"commit":"<sha>","created":"<created>","published":"<published>"}
```

`<id>` is `sit_` followed by 16 lowercase hexadecimal digits, the same in both, and `<sha>` is the commit the shell printed.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token auth accepts, whose owner is the user `<user-id>`, who has no repository named `pages` in repos.
- No site in the sandbox is named `pages`.
- The developer's shell is as the opening paragraph says, and the current directory holds nothing named `pages`.

Postconditions:

- `<user-id>` owns the repository `pages` with id `<rep>`; `<data>/apps/repos/state/repos/<rep>.git` is its bare repository, its `ikigenba.owner` is `<user-id>`, and its `refs/heads/main` is `<sha>`. sites wrote nothing there.
- `<user-id>` owns the site `pages` with id `<id>`, public, listed, tracking `main`, and published at `<sha>`, in `<data>/apps/sites/state/sites.db`; `<data>/apps/sites/cache/sites/<id>/<sha>/` holds the commit's tree, `index.html` alone.
- The trail holds, under the id the sandbox's nginx gave each gateway request and user `<user-id>`: repos' `repo.created` with `repo=<rep>`; sites' `site.created` with `site=<id>`, `repo=<rep>`, `visibility=public`, and `listed=true`, then `tool.called` with `tool=create` and `outcome=ok`; and sites' `site.published` with `site=<id>`, `commit=<sha>`, and `ref=main`, then `tool.called` with `tool=publish` and `outcome=ok`. repos' `repo.pushed` with `repo=<rep>` and `new=<sha>` is under the id of the `POST /pages.git/git-receive-pack`.
- `sandbox logs sites` shows no line from sites: it wrote nothing to stderr.

## A guest reaches a published site in a sandbox

Once published, the site answers anyone, with no credential at all, at the URL `publish` gave. The sandbox's nginx asks auth's `/check/open`, which admits the request with no identity, and sites serves the page and gives the browser a visitor cookie, without `Secure`, since the sandbox serves plain HTTP.

Request:

```
$ curl -si http://sites.wip.localhost:7400/pages/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
ETag: "<sha>"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax
```

Status 200. The body is exactly the line `<h1>hello</h1>` and its newline, the bytes of `index.html` at `<sha>`. `<visitor>` is `vis_` followed by 16 lowercase hexadecimal digits. `curl -si http://sites.wip.localhost:7400/pages` is redirected `301` to `/pages/` (`S11`).

Preconditions:

- `An agent publishes a site in a sandbox from a repository it pushed` has run, so `pages` is public and published at `<sha>`.
- The request carries no cookie and no `Authorization` header, so auth's `/check/open` answers 200 with no `X-User-Id` and no `X-User-Email`.

Postconditions:

- Nothing has changed but the trail.
- auth's `/check/open` received `X-Original-Method: GET`, `X-Original-Host: sites.wip.localhost` and `X-Original-URI: /pages/`; no `/check` subrequest was made (sandbox's `S4-routing.md`).
- The trail holds, under the id the sandbox's nginx made for the request and an empty user, sites' `request.started` with `path=/pages/`, `site.viewed` with `site=<id>`, `visitor=<visitor>`, `path=/pages/`, `status=200`, `referrer_host` empty, and `commit=<sha>`, and `request.finished` with `status=200`.

## A guest in a sandbox asks for a private site

A private site in a sandbox is for the sandbox's signed-in users, as on a space. sites sends the guest to sign in at the sandbox's auth, which it finds at `auth.` followed by the space the request's `Host` names, port kept, over the `http` the sandbox's nginx says the request arrived on, carrying the URL it asked for, percent-encoded.

Request:

```
$ curl -si 'http://sites.wip.localhost:7400/pages/?draft=1'
```

Response:

```
HTTP/1.1 302 Found
Location: http://auth.wip.localhost:7400/?return=http%3A%2F%2Fsites.wip.localhost%3A7400%2Fpages%2F%3Fdraft%3D1
```

Status 302. The body is not fixed, and the response sets no cookie. With the token, `curl -si -H "Authorization: Bearer $(sandbox token)" http://sites.wip.localhost:7400/pages/` is answered 200 with `index.html` and `Cache-Control: private, no-cache` (`S12`).

Preconditions:

- The site `pages` of `An agent publishes a site in a sandbox from a repository it pushed` has since been made private, by `update` with `{"name":"pages","visibility":"private"}` through the gateway's `mutate` (`S09`).
- The request carries no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed but the trail.
- The trail holds, under the id the sandbox's nginx made for the request and an empty user, sites' `request.started` with `path=/pages/` and `request.finished` with `status=302`, and no `site.viewed`.
