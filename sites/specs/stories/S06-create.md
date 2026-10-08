# Stories — create

`create`, the tool that adds a site to the catalog: a record naming one of the caller's repositories and the ref to publish from, unpublished until the caller publishes it (`S08`). It takes, in this order, `name` and `repo`, both required strings, and `ref`, a string, `visibility`, a string, and `listed`, a boolean, all three optional. A name is 1 to 64 characters, each a lowercase ASCII letter, a digit, or `-`, the first a letter or a digit, taken as given, never trimmed or folded; `about`, `mcp`, and `api` are not names, since `/about` and `/mcp` are sites' own paths and `/api` is reserved across the suite on every app's host, sites' included. Names are unique across the space, not per owner: every user's sites share one namespace because the slug is the URL, so a name is taken when any site, the caller's or another user's, has it as its name or as its slug. `repo` is a repository's id, `rep_` and 16 lowercase hexadecimal digits, whose bare repository `<REPOS_DIR>/<repo>.git` exists and whose `ikigenba.owner` config value, as repos writes it (repos' `S15-disk.md`), is the caller's `X-User-Id`; sites never asks repos, and a repository's name is not accepted in place of its id. One repository may back any number of sites. `ref` defaults to `main` and must be a string git accepts as a ref name; it is not resolved until a publish, so a ref that names nothing yet is accepted. `visibility` is `public` or `private`, default `public`; `listed` defaults to `true`. A listed site's slug is its name; an unlisted site's slug is its name, `-`, and 8 lowercase hexadecimal digits from the random source. The slug is fixed at create and never changes (`S09`). The arguments are checked in that order — the name against the rule, then whether it is taken; the repository; the ref; the visibility — and the first that fails is the whole answer, one line naming it. The result is the site object, members in this order: `id`, `sit_` and 16 lowercase hexadecimal digits, minted now and never changed; `name`; `slug`; `url`, `<sites-url>/<slug>/`, where `<sites-url>` is the `url`, less one trailing `/`, of the services file's entry named `sites`, else `<proto>://<Host>`; `repo`; `ref`; `visibility`; `listed`; and `created`, the time of the call, RFC 3339 UTC to the second. A new site is unpublished, so `commit` and `published` are absent. create reads the repository's owner and nothing else from it, and unpacks nothing.

The actor is a model working through an MCP client, or the gateway's `mutate` on its behalf; the request shape, the result envelope, the `invalid arguments:` wording, `tool.called`, and the trail are as `S05` fixes them. Each request is the HTTP request a running sites (`S02`) receives on `/mcp`, carrying `X-User-Id` and `X-User-Email` by hand, on revision `2026-07-28`. Unless a story says otherwise, sites runs with `REPOS_DIR` unset, so it reads repositories under `../repos/state/repos`, and with the suite's services file, whose `sites` entry has the `url` `https://sites.sbx.ikigenba.dev`; telemetry takes every event. Under `../repos/state/repos` are three bare repositories, part of `S06`'s shared catalog: `rep_8c21d4e0f7a3b915.git` (repos' `site`) and `rep_3f9a0c1d2e4b5a69.git` (repos' `notes`), whose `ikigenba.owner` is the caller `u_7f3a9c21` (`mg@example.com`), and `rep_d41c7a9e05b28f63.git` (repos' `journal`), whose `ikigenba.owner` is `u_2b8e1d04` (`ann@example.com`). In `rep_8c21d4e0f7a3b915`, `main` and the tag `v1` are at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` and the branch `preview` at `9d0c8b7a6f5e4d3c2b1a09f8e7d6c5b4a3928170`; in `rep_3f9a0c1d2e4b5a69`, `main` is at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`; in `rep_d41c7a9e05b28f63`, `main` is at `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`. There is no `rep_0a0b0c0d0e0f1a2b.git`. The catalog holds four sites, `S06`'s shared catalog, which every later group assumes unless it says otherwise: the caller's `blog`, id `sit_4e7a1c9b0d2f8635`, slug `blog`, repository `rep_8c21d4e0f7a3b915`, ref `main`, public, listed, commit `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, created `2026-10-01T09:30:00Z`, published `2026-10-01T10:00:00Z`; the caller's `handbook`, id `sit_9a3c5e7b1d0f2468`, slug `handbook`, repository `rep_3f9a0c1d2e4b5a69`, ref `main`, private, listed, commit `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`, created `2026-10-01T11:00:00Z`, published `2026-10-01T11:05:00Z`; the caller's `scratch`, id `sit_2d6f8a0c4e1b3957`, slug `scratch-7c1e9a4f`, repository `rep_8c21d4e0f7a3b915`, ref `preview`, public, unlisted, unpublished, created `2026-10-02T08:00:00Z`; and `u_2b8e1d04`'s `recipes`, id `sit_6b1d3f5a7c9e0284`, slug `recipes`, repository `rep_d41c7a9e05b28f63`, ref `main`, public, listed, commit `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`, created `2026-10-01T16:45:00Z`, published `2026-10-01T16:50:00Z`. Each published site's tree is at `cache/sites/<site id>/<commit>/`. No apex is set. `create` is of kind `additive`; a call that makes a site records `site.created`, with `site`, the new id, `repo`, `visibility`, and `listed`, a JSON boolean, before its `tool.called`; the name and slug are in no event. A refusal creates nothing and records no `site.*` event. sites writes nothing to stderr for any answer in this group.

## A model creates a site

The ordinary case: a name and a repository, everything else by default. The site is public, listed, so its slug is its name, and tracks `main`. Nothing is published yet: the answer has no `commit` and no `published`, and the site's URL answers not-found until the model calls `publish` (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_8c21d4e0f7a3b915"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"docs","slug":"docs","url":"https://sites.sbx.ikigenba.dev/docs/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"created":"<created>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the id sites minted, `sit_` and 16 lowercase hexadecimal digits, different from every other site's; `<created>` is the time of the call, RFC 3339 UTC to the second, `2026-10-02T15:04:05Z` say.

Preconditions:

- The preamble's: no site has the name or slug `docs`.

Postconditions:

- The catalog holds a fifth site: id `<id>`, name `docs`, slug `docs`, owner `u_7f3a9c21`, repository `rep_8c21d4e0f7a3b915`, ref `main`, public, listed, no commit, created `<created>`. `list` (`S07`) answers `blog`, `docs`, `handbook`, `scratch`; `show` with `docs` answers what `create` answered.
- Nothing was unpacked: there is no `cache/sites/<id>/`. The repository is untouched; `blog` and `scratch`, which use it too, are as they were.
- `GET /docs/` is answered 404 with sites' not-found page, as for any site not yet published (`S11`).
- telemetry has received the request's four events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"listed":true,"repo":"rep_8c21d4e0f7a3b915","site":"<id>","visibility":"public"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `docs` is in none of them.

## A model creates a site that tracks a tag

`ref` names what a publish takes when it is given no ref of its own: a branch, a tag, or a sha. Here the site tracks the tag `v1`. The ref is recorded as given and not resolved: a ref that names nothing in the repository yet, a branch the model has still to push, is accepted the same way, and only a publish finds out (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_8c21d4e0f7a3b915","ref":"v1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"docs","slug":"docs","url":"https://sites.sbx.ikigenba.dev/docs/","repo":"rep_8c21d4e0f7a3b915","ref":"v1","visibility":"public","listed":true,"created":"<created>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- The catalog holds `docs` as in `A model creates a site`, but that its ref is `v1`. A `publish` of `docs` with no `ref` resolves `v1` (`S08`).
- No git resolved `v1`. telemetry has received `site.created` with attributes `{"listed":true,"repo":"rep_8c21d4e0f7a3b915","site":"<id>","visibility":"public"}`; the ref is in no event.

## A model creates an unlisted site

An unlisted site's slug is its name with a random suffix, so its URL is hard to guess and the landing page shows it only to its owner (`S03`). Unlisted is a convention, not a protection: anyone with the URL can open a public unlisted site. The suffix is drawn now and kept for the life of the site, whatever later becomes of `listed` (`S09`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create","arguments":{"name":"draft","repo":"rep_8c21d4e0f7a3b915","listed":false},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"draft","slug":"<slug>","url":"https://sites.sbx.ikigenba.dev/<slug>/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":false,"created":"<created>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<slug>` is `draft-` followed by 8 lowercase hexadecimal digits from the random source, `draft-0e5b7c29` say.

Preconditions:

- The preamble's: no site has the name `draft`.

Postconditions:

- The catalog holds `draft`, slug `<slug>`, listed `false`, unpublished. Its name `draft` is taken as well as its slug: a later `create` of either is refused as in `A model creates a site with a name its own site already has`.
- `GET /draft/` is answered 404 with sites' not-found page (`S11`): a site is reached by its slug only.
- telemetry has received `site.created` with attributes `{"listed":false,"repo":"rep_8c21d4e0f7a3b915","site":"<id>","visibility":"public"}`; neither the name nor the slug is in any event.

## A model creates a private site

A private site is served to signed-in users of the space only; a guest is sent to sign in (`S12`). It is otherwise a site like any other.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"name":"team","repo":"rep_3f9a0c1d2e4b5a69","visibility":"private"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"team","slug":"team","url":"https://sites.sbx.ikigenba.dev/team/","repo":"rep_3f9a0c1d2e4b5a69","ref":"main","visibility":"private","listed":true,"created":"<created>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `rep_3f9a0c1d2e4b5a69` already backs `handbook`.

Postconditions:

- The catalog holds `team`, private, listed, unpublished, on `rep_3f9a0c1d2e4b5a69`; `handbook` is as it was.
- telemetry has received `site.created` with attributes `{"listed":true,"repo":"rep_3f9a0c1d2e4b5a69","site":"<id>","visibility":"private"}`.

## A model creates a site with a name another user's site has

Names are unique across the space, not per owner: `recipes` is `u_2b8e1d04`'s site and its slug is the URL `/recipes/`, so the caller cannot have a `recipes` of its own. The answer says only that the name is taken; it says nothing of whose it is.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create","arguments":{"name":"recipes","repo":"rep_8c21d4e0f7a3b915"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
a site named 'recipes' already exists
```

Preconditions:

- The preamble's: `recipes` is `u_2b8e1d04`'s.

Postconditions:

- No site was created. `recipes` is as it was, still `u_2b8e1d04`'s, and the caller's `list` (`S07`) still does not show it.
- sites recorded no `site.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create"}}
  ```

## A model creates a site with a name its own site already has

A taken name is refused, never read as a request for the site that has it, and the existing site is untouched. A name that is another site's slug is taken in the same way: `scratch-7c1e9a4f`, the slug of the caller's unlisted `scratch`, is refused with `a site named 'scratch-7c1e9a4f' already exists`, since a listed site by that name would have the same URL.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create","arguments":{"name":"blog","repo":"rep_8c21d4e0f7a3b915"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
a site named 'blog' already exists
```

Preconditions:

- The preamble's: the caller owns `blog`.

Postconditions:

- No site was created. `blog` is unchanged: same id, ref `main`, still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.
- sites recorded no `site.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a site with a name that is not allowed

A name is held to one rule, and every way of breaking it gets the same line, quoting the name as sent: upper-case letters or a space, as here; `_`, `.`, `/`, or any other character outside `a`-`z`, `0`-`9`, and `-`; a first character `-`, as in `-docs`; the empty name; and more than 64 characters. Nothing is trimmed or folded, so ` docs` and `Docs` are refused rather than read as `docs`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create","arguments":{"name":"My Docs","repo":"rep_8c21d4e0f7a3b915"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid name 'My Docs'
```

Preconditions:

- The preamble's.

Postconditions:

- No site was created.
- sites recorded no `site.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create"}}
  ```

## A model creates a site named about, mcp, or api

`/about` and `/mcp` are sites' own paths, the about screen (`S03`) and the MCP endpoint (`S05`), so a site at `/about/` or `/mcp/` could never be reached. `/api` is not one of sites' pages but is reserved across the suite: on every app's host, sites' included, the host's nginx keeps `/api` and every path under it behind the strict identity check, answering a request with no credential `401` rather than passing it on as a guest's, so a site at `/api/` could never serve a guest. All three names keep to the naming rule, and all three are refused with the line any name that is not allowed gets. `mcp` is refused the same way, with `invalid name 'mcp'`, and `api` with `invalid name 'api'`. Names that merely begin with them, `about-us`, `mcp-notes`, or `api-docs`, are ordinary names.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"create","arguments":{"name":"about","repo":"rep_8c21d4e0f7a3b915"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid name 'about'
```

Preconditions:

- The preamble's.

Postconditions:

- No site was created. `GET /about` still answers the about screen (`S03`).
- sites recorded no `site.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a site with a visibility sites does not have

A site is `public` or `private`, nothing else. The value is not folded, so `Public` is refused as `secret` is.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_8c21d4e0f7a3b915","visibility":"secret"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
visibility must be public or private
```

Preconditions:

- The preamble's.

Postconditions:

- No site was created.
- sites recorded no `site.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a site from a repository that does not exist

`repo` must name a bare repository under `REPOS_DIR`. One that is not there, because it was never created or repos has deleted it, is refused, quoting the value as sent. A repository's name in place of its id, `site` say, names no repository and is refused the same way, with `no repository 'site'`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_0a0b0c0d0e0f1a2b"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_0a0b0c0d0e0f1a2b'
```

Preconditions:

- The preamble's: there is no `../repos/state/repos/rep_0a0b0c0d0e0f1a2b.git`.

Postconditions:

- No site was created, and nothing was written under `../repos/state/repos/` or `cache/`.
- sites recorded no `site.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a site from another user's repository

A repository is the caller's when its `ikigenba.owner` says so; sites reads that from the repository itself and asks repos nothing. Another user's repository gets exactly the answer a missing one gets, so the caller cannot learn that the id is in use. The owner is checked only here: a publish later reads whatever the site's repository holds (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_d41c7a9e05b28f63"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no repository 'rep_d41c7a9e05b28f63'
```

Preconditions:

- The preamble's: the `ikigenba.owner` config value of `rep_d41c7a9e05b28f63.git` is `u_2b8e1d04`.

Postconditions:

- No site was created. `rep_d41c7a9e05b28f63.git` is untouched.
- sites recorded no `site.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a site with a ref git would not accept

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

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_8c21d4e0f7a3b915","ref":"..bad"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid ref '..bad'
```

Preconditions:

- The preamble's.

Postconditions:

- No site was created.
- sites recorded no `site.created`; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model calls create without saying which repository

`name` and `repo` are both required. A call that leaves one out is refused as its arguments are read, every offence in one answer, before any rule of sites' is looked at (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
repo: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- No site was created.
- sites recorded no `site.created`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"create"}}
  ```

## A model sends arguments create does not take

A field of the wrong JSON type, or one the tool does not have, is refused as the arguments are read, every offence in one answer: the tool's fields first, in the order of its input schema, then each unknown field in the order it was sent (`S05`). Here the model sent `listed` as a string and tried to choose the slug, which sites alone sets.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_8c21d4e0f7a3b915","listed":"no","slug":"docs"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
listed: expected boolean, got string
slug: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- No site was created.
- sites recorded no `site.created`; the request's `tool.called` has `kind` `additive`, `outcome` `invalid_arguments`, and `duration_us` 0.
