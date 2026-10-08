# Stories — on a space

sites reached through a space: the file `S20` describes, deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at `sites.<space>` over TLS, so on the space `sbx.ikigenba.dev` sites answers at `sites.sbx.ikigenba.dev`. nginx on the space proxies to sites' socket, `/run/ikigenba/sites.sock` (`S02`); sites ships no `etc/nginx.conf` (`S20`). sites' manifest sets `guests = true` (`S01`), so the space's nginx asks auth's `GET /check/open` rather than `/check` for every path of sites but `/mcp`, the paths under `/mcp/`, and git's paths (opsctl's `S5-nginx.md`, `An operator reads the configuration of a host running an app that serves guests`): a request with no credential is passed to sites with no `X-User-Id` and no `X-User-Email` and never sent to sign in by nginx (auth's `S4-check.md`, `nginx checks an open request with no credential`; opsctl's `S5-nginx.md`, `A guest reaches an app that serves guests`), while a request with a session or a token auth honors is passed with the caller's `X-User-Id` and `X-User-Email`, and one whose credential auth refuses gets auth's 403 and never reaches sites. Every request nginx passes carries `X-Forwarded-Proto: https` and the `X-Request-Id` nginx gave it, the same id its `/check/open` or `/check` subrequest carried, so auth's check event and sites' records of the request share one request id. Deciding who may see a private site, and sending a guest to sign in for one, is sites' own doing (`S12`), not nginx's. The host's services file is `/var/lib/ikigenba/services.json`, which lists sites with `url` `https://sites.sbx.ikigenba.dev`, its socket, and marked for MCP since its manifest has `mcp = true`, so every site's URL sites gives is `https://sites.sbx.ikigenba.dev/<slug>/` and the MCP gateway offers sites' seven tools through `https://mcp.sbx.ikigenba.dev/mcp` (mcp's `S11-on-a-space.md`). sites runs as `/opt/sites/bin/sites` with `/opt/sites` as its working directory, so its catalog is `/opt/sites/state/sites.db`, its unpacked trees are under `/opt/sites/cache/sites/`, and `REPOS_DIR`, at its manifest default `../repos/state/repos`, names `/opt/sites/../repos/state/repos`, the directory where repos keeps its bare repositories, `/opt/repos/state/repos/` (repos' `S15-disk.md`; `S18`). The host keeps and replicates the declared database as it does auth's, which is opsctl's doing and is named here only by its effect; `cache/` is not backed up. `/opt/sites/etc/env` carries the three settings of the manifest's `[env]` beside the space's `DRAIN_SECONDS` and `IKIGENBA_SERVICES`, and the host provides the `git` sites runs (opsctl's `S4-init.md`).

The stories prove the whole path from checkout to browser, curl, and agent, and nothing about sites that the earlier groups do not already say. devctl and opsctl are named only by their published commands. The catalog on the space holds `S06`'s shared catalog — `blog` (public, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`) and `handbook` (private, published at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`), both owned by `u_7f3a9c21`, `mg@example.com`, among them — and repos on the space holds the repositories they are served from: `site`, `rep_8c21d4e0f7a3b915`, whose tree at `main` holds `index.html`, and `notes`, `rep_3f9a0c1d2e4b5a69`, owned by the same user. A guest is curl with no cookie and no `Authorization` header; a signed-in caller sends the token `ikp_<token>` (auth's `S5-tokens.md`), owned by `u_7f3a9c21`, as `Authorization: Bearer ikp_<token>`. The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. Trail events are named here by their attributes, as `S15` records them in full. The token's secret is in no record any story below leaves behind: not in the trail and not in nginx's logs on the host.

## A guest reaches a public site on a space

This is what sites is for from the outside: anyone, signed in or not, opens a public site's address and gets its files. nginx asks auth's `/check/open`, which admits the request with no identity, and passes it to sites as a guest's; sites serves `index.html` from the tree of the published commit, and gives the browser a visitor cookie it will carry on every later request to sites.

Request:

```
$ curl -si https://sites.sbx.ikigenba.dev/blog/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
etag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
cache-control: public, no-cache
set-cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly the bytes of `index.html` in the tree of `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` of `site`, as `git archive` gives them (`S11`). `<visitor>` is `vis_` followed by 16 lowercase hexadecimal digits, and the cookie is `Secure` because the request arrived over TLS (`S14`). Sent again with `-H 'Cookie: ikigenba_visitor=<visitor>'`, the request is answered the same but with no `set-cookie`, and with `-H 'If-None-Match: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"'` as well it is answered `HTTP/2 304` with no body.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance is `running`, and `opsctl` is installed on it.
- `devctl build sites`, run in a clean tree at the commit `<sha>`, wrote `sites/dist/sites-<sha>.tar.xz` (`S20`). No tag is needed.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev sites/dist/sites-<sha>.tar.xz` exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows sites' service and socket `active`, in the layout devctl's and opsctl's stories own.
- auth, repos, and telemetry are deployed and active on the space through their own chains, and the space's nginx was regenerated by the install with sites' `guests = true`.
- `blog` is public, listed, and published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` holds that commit.
- The request carries no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed but the trail and, at most, `/opt/sites/cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`, which holds the commit's tree, unpacked in this request if it was not there already (`S16`). The repository is as it was.
- The trail holds, under the id nginx gave the request: auth's `check.allowed` with `outcome=guest`, `credential=none`, `method=GET`, `host=sites.sbx.ikigenba.dev`, and `path=/blog/`, under no user (auth's `S4-check.md`); then sites' `request.started` with `method=GET` and `path=/blog/`, `site.viewed` with `site=sit_4e7a1c9b0d2f8635`, `visitor=<visitor>`, `path=/blog/`, `status=200`, `referrer_host` empty, and `commit=5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and `request.finished` with `status=200`, each under an empty user (`S14`, `S15`).

## A guest on a space asks for a private site

A private site is for the space's signed-in users. The guest reaches sites all the same, since nginx sends no one to sign in on sites' paths, and sites itself sends the guest to auth's sign-in, carrying the URL it asked for, percent-encoded, so auth brings it back once it has signed in (`S12`).

Request:

```
$ curl -si 'https://sites.sbx.ikigenba.dev/handbook/guide/?q=1'
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fhandbook%2Fguide%2F%3Fq%3D1
```

Status 302. The body is not fixed. The response sets no cookie and carries none of `handbook`'s files.

Preconditions:

- sites is deployed and active on `sbx.ikigenba.dev`, as in `A guest reaches a public site on a space`.
- `handbook` is private and published at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`.
- The request carries no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed but the trail. Nothing was unpacked and no git ran.
- The trail holds, under the id nginx gave the request: auth's `check.allowed` with `outcome=guest`, `credential=none`, `host=sites.sbx.ikigenba.dev`, and `path=/handbook/guide/`; then sites' `request.started` and `request.finished` with `status=302`, under an empty user, and no `site.viewed`.

## A signed-in user on a space reaches a private site

A user signed in to the space, with a session or, here, a token, is admitted by auth's `/check/open` as themselves, and sites serves the private site to them as it serves a public one, marked so that no shared cache keeps it.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://sites.sbx.ikigenba.dev/handbook/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
etag: "a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d"
cache-control: private, no-cache
set-cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly the bytes of `index.html` in the tree of `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d` of `notes`. Any user of the space is served the same; `ann@example.com`'s token would be answered alike, since a private site is the space's and not only its owner's (`S12`).

Preconditions:

- sites is deployed and active on `sbx.ikigenba.dev`, as in `A guest reaches a public site on a space`.
- `handbook` is private and published at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`.
- `ikp_<token>`, whose id is `<token-id>`, is a token auth honors, owned by `u_7f3a9c21`.

Postconditions:

- Nothing has changed but the trail and, at most, the commit's tree under `/opt/sites/cache/sites/sit_9a3c5e7b1d0f2468/`.
- The trail holds, under the id nginx gave the request and user `u_7f3a9c21`: auth's `check.allowed` with `outcome=allowed`, `credential=token`, `host=sites.sbx.ikigenba.dev`, `path=/handbook/`, and `token=<token-id>`; then sites' `request.started`, `site.viewed` with `site=sit_9a3c5e7b1d0f2468`, `path=/handbook/`, `status=200`, and `commit=a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`, and `request.finished` with `status=200`.

## A user on a space reaches sites' landing page

A signed-in user asks for sites' own name and gets the landing page, listing the space's sites and the tools that manage them.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://sites.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `sites`, whose banner's profile link is titled `mg@example.com`, the email of the token's owner, and leads to `https://auth.sbx.ikigenba.dev/`, whose banner's `Sign out` button is in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`, whose banner carries the launcher button `Services`, since the host's services file lists sites with its icon, and whose visible text carries the heading `sites`; the heading `Sites`, the line `Every site answers at https://sites.sbx.ikigenba.dev/<slug>/, where <slug> is its slug.`, and the table of the space's sites the caller may see, `blog` and `handbook` among them, each linked under `https://sites.sbx.ikigenba.dev/`; the heading `MCP tools` and the seven tool names `list`, `show`, `create`, `publish`, `update`, `delete`, and `apex`; and a link `About sites` to `/about`; and whose footer reads `sites <display>`, where `<display>` is whatever display string the host's environment gives sites: the string the deployed binary's `sites --version` prints under that same environment (`S01`), and empty when the host sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`. Its stylesheet is `https://sites.sbx.ikigenba.dev/_appkit/theme.css` (`S04`): a browser showing the page requests its style from sites' own host and from no other origin. Its button feedback script is `https://sites.sbx.ikigenba.dev/_appkit/feedback.js` and its icon `https://sites.sbx.ikigenba.dev/_appkit/favicon.svg`, both from the same host.

Preconditions:

- sites is deployed and active on `sbx.ikigenba.dev`, as in `A guest reaches a public site on a space`.
- `ikp_<token>` is a token auth honors, owned by `u_7f3a9c21`, `mg@example.com`.
- The host sets `IKIGENBA_SERVICES` in sites' environment to the path of its services file, and that file lists sites with its icon, from `share/icon.svg` (`S20`).

Postconditions:

- Nothing has changed but the trail: sites records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id nginx gave the request and user `u_7f3a9c21`, and no `site.viewed`. The response set no cookie.

## A guest on a space asks for sites' landing page

The landing page is for the space's signed-in users, so a guest who opens sites' own name is sent to sign in by sites, just as at a private site, and comes back to the landing page afterwards.

Request:

```
$ curl -si https://sites.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2F
```

Status 302. The body is not fixed, and the response sets no cookie. `https://sites.sbx.ikigenba.dev/about` answers the same, with `return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fabout`.

Preconditions:

- sites is deployed and active on `sbx.ikigenba.dev`, as in `A guest reaches a public site on a space`.
- The request carries no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed but the trail: auth's `check.allowed` with `outcome=guest`, `credential=none`, and `path=/`; then sites' `request.started` and `request.finished` with `status=302`, under the id nginx gave the request and an empty user.

## A visitor at the space's apex is sent to the apex site

The apex host is the root domain, `ikigenba.dev`, the address a person types without any service or space name in it. When the space holds the apex and routes it to sites, and an agent has made `blog` the apex site with `apex` (`S13`), a request there, by anyone, for any path, is sent to the same path under `blog`'s address, its query kept, and the visitor's browser follows it to the site. The address is `blog`'s URL under the services file's `url` for sites, `https://sites.sbx.ikigenba.dev`, since the root domain names no space.

Request:

```
$ curl -si https://ikigenba.dev/
```

```
$ curl -si 'https://ikigenba.dev/hello?x=1'
```

Response:

```
HTTP/2 302
location: https://sites.sbx.ikigenba.dev/blog/
```

Status 302. The body is not fixed, and the response sets no cookie. For the second request the location is `https://sites.sbx.ikigenba.dev/blog/hello?x=1`. Following the first, `curl -sL https://ikigenba.dev/` ends at `A guest reaches a public site on a space`'s answer, `index.html` of `blog`.

Preconditions:

- sites is deployed and active on `sbx.ikigenba.dev`, as in `A guest reaches a public site on a space`.
- `devctl apex set sites.sbx` has run (devctl's `S7-apex.md`): the root domain's `A` record points at this space, the host's certificate covers `ikigenba.dev`, and opsctl's `host.apex` is `sites`, so the space's nginx answers `ikigenba.dev` from sites' guest-admitting block, as it does `sites.sbx.ikigenba.dev` (opsctl's `S5-nginx.md`, `An operator reads the configuration of a host running an app that serves guests`).
- The host's services file lists sites with `url` `https://sites.sbx.ikigenba.dev`.
- The catalog's apex is `blog`, `sit_4e7a1c9b0d2f8635`, set by `apex` with `{"name":"blog"}` (`S13`).
- The requests carry no cookie and no `Authorization` header.

Postconditions:

- Nothing has changed but the trail. The redirect read no repository and no tree.
- The trail holds, for each request, under the id nginx gave it: auth's `check.allowed` with `outcome=guest`, `credential=none`, and `host=ikigenba.dev`; then sites' `request.started` with the request's `path` and `request.finished` with `status=302`, under an empty user, and no `site.viewed`.

## An MCP client on a space reaches sites' /mcp without a credential

Serving guests opens sites' pages and sites, never its tools. nginx keeps the strict `/check` on `/mcp` and everything under it, so an MCP client with no credential gets the same bearer challenge here as at any app (opsctl's `S5-nginx.md`, `An MCP client reaches an app that serves guests without a credential`), and sites never sees the request.

Request:

```
$ curl -si -X POST https://sites.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 401
content-type: text/plain
www-authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the host's nginx's one line `authentication required: send Authorization: Bearer <token>`.

Preconditions:

- sites is deployed and active on `sbx.ikigenba.dev`, as in `A guest reaches a public site on a space`.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/sites.sock`, and sites recorded no event; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none`, and no `/check/open` subrequest was made.

## An agent on a space creates and publishes a site through the gateway

The expected path for an agent: it has pushed a site's files to one of its repositories in repos, asks the gateway to run sites' `create` naming that repository, and then `publish`, and the site is live at the URL the result gives. Both are additive tools (`S05`), so the agent calls them with the gateway's `mutate` tool (mcp's `S09-mutate.md`); the gateway reaches sites directly on `/run/ikigenba/sites.sock` and relays its answer.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"sites","tool":"create","args":{"name":"portfolio","repo":"rep_8c21d4e0f7a3b915"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"sites","tool":"publish","args":{"name":"portfolio"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 1 whose `result` is sites' answer to `create` (`S06`), relayed: no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"portfolio","slug":"portfolio","url":"https://sites.sbx.ikigenba.dev/portfolio/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"created":"<created>"}
```

and a `content` array of one text block holding that same object encoded compactly. The second is a JSON-RPC response with `id` 2 whose `result` is sites' answer to `publish` (`S08`), relayed, with a `structuredContent` of

```
{"id":"<id>","name":"portfolio","slug":"portfolio","url":"https://sites.sbx.ikigenba.dev/portfolio/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"<created>","published":"<published>"}
```

`<id>` is `sit_` followed by 16 lowercase hexadecimal digits, the same in both. Each result's `io.modelcontextprotocol/serverInfo` is the gateway's, `{"name":"mcp","version":"<mcp display>"}`, where `<mcp display>` is the gateway's own display string (mcp's stories), not sites'.

Preconditions:

- sites is deployed and active on `sbx.ikigenba.dev`, as in `A guest reaches a public site on a space`, and the host's services file lists `sites` enabled, marked for MCP, with the socket `/run/ikigenba/sites.sock`.
- mcp is deployed and active on the space through its own `S11-on-a-space.md` chain, serving `https://mcp.sbx.ikigenba.dev/mcp`.
- The agent holds `ikp_<token>`, owned by `u_7f3a9c21`, who owns `site`, `rep_8c21d4e0f7a3b915`, in repos, its `ikigenba.owner` `u_7f3a9c21` and its `main` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, pushed with git as repos' `S17-on-a-space.md` tells.
- No site in the space is named `portfolio`.

Postconditions:

- `u_7f3a9c21` owns a new site `portfolio` with id `<id>`, public, listed, tracking `main`, and published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; `/opt/sites/cache/sites/<id>/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/` holds that commit's tree. `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` is as it was.
- A guest's `curl -si https://sites.sbx.ikigenba.dev/portfolio/` is answered `HTTP/2 200` with the bytes of `index.html` at that commit, `etag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"`, and `cache-control: public, no-cache`, as in `A guest reaches a public site on a space`; and the landing page lists `portfolio` with the badges `public`, `published`, and `yours`.
- mcp wrote nothing to stderr, and neither did sites.
- The trail holds, under the id nginx gave each request and user `u_7f3a9c21`, auth's `check.allowed` with `credential=token`, the gateway's events for the call, and sites' own: for the first, `site.created` with `site=<id>`, `repo=rep_8c21d4e0f7a3b915`, `visibility=public`, and `listed=true`, then `tool.called` with `tool=create`, `kind=additive`, and `outcome=ok`; for the second, `site.published` with `site=<id>`, `commit=5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and `ref=main`, then `tool.called` with `tool=publish`, `kind=additive`, and `outcome=ok` (`S15`). No event attribute holds the name `portfolio` or the token.
