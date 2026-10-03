# Stories — trail

What sites' trail holds, and what it never holds. sites records what it does as events it sends to the platform's telemetry service, as repos does (repos' `S02-serve.md`); how it finds telemetry, and what it does with an event telemetry cannot take, is told in `S02`. An operator, or an agent working for one, follows what happened from one thing they know — a request id, a user, a site's id, a visitor's id, or a time. The stories show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"sites","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when sites recorded the event, in UTC to the microsecond, as `2026-10-02T14:03:07.123456Z`; `service` is always `sites`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`, each empty when there is none: `user` is empty for a guest, whose request carries no `X-User-Id`, and both are empty for a start or a stop. `attrs` holds the event's attributes, flat, their keys in alphabetical order. Attributes name what happened and the ids of what it touched, never data: no attribute carries a site's name or slug, a caller's email, a request's query string, the contents of a file, or a tool's arguments. A site is named by its id under the key `site`, a repository by its id under `repo`, and a visitor by its id under `visitor`, so the trail of a site survives anything done to its name. The one place a slug reaches the trail is a `path`, `/blog/about/` say, which is the URL path as it arrived, without its query, in `request.started` as on every app, and in `site.viewed`.

sites records these events and no others:

- `service.started`, once sites is serving, with `version`; and `service.stopping`, its last event, with `reason`, `SIGTERM` or `SIGINT` (`S02`);
- `request.started`, as each request arrives, with `method` and `path`; and `request.finished`, once its answer is complete, with `status`, `duration_us`, `request_bytes`, and `response_bytes`, shown as `<n>` and `<bytes>` unless a story fixes them (`S02`);
- `tool.called`, for each call of one of its seven tools answered with a result, with `tool`, `kind`, `outcome`, and `duration_us` (`S05`);
- `site.viewed`, for each answer to a known site's path from the trailing-slash redirect on — `301`, `200`, `304`, `404`, and `503` alike — with `site`; `visitor`, the visitor's id, the one just minted when the request carried none (`S14`); `path`; `status`, a number; `referrer_host`, the host of the request's `Referer` as the URL gives it, port kept, and empty when there is no `Referer`, it has no host, or it cannot be parsed; and `commit`, the site's published sha, empty only for a site never published (`S11`, `S12`);
- `site.created`, with `site`, `repo`, `visibility`, and `listed`, a JSON boolean (`S06`);
- `site.published`, with `site`, `commit`, and `ref`, the ref as the call gave it or else the site's own (`S08`);
- `site.updated`, once for each field an update changed, with `site`, `field`, and `value`, always a string, so a boolean is `"true"` or `"false"` (`S09`);
- `site.deleted`, with `site` (`S10`);
- `site.apex`, with `site`, the apex site's id, or empty when the apex was cleared (`S13`);
- `site.unavailable`, when a site's published commit cannot be served, with `site`, `commit`, and `reason`: `repository_missing`, `commit_missing`, `too_large`, `timed_out`, or `git_failed` (`S16`).

A request's events carry its request id and its caller and come in this order: `request.started`, then its `site.*` events, then its `tool.called` if it called a tool, then `request.finished`. A tool call that is refused records no `site.*` event. A request that never reaches a site — the landing page, the about page, `/_appkit/`, an unknown slug, a guest sent to sign in, a method other than `GET` and `HEAD`, or the apex host — records only its `request.started` and `request.finished`. Unless a story says otherwise, sites runs on the host with the suite's services file (`S05`), whose `sites` entry has the `url` `https://sites.sbx.ikigenba.dev`, telemetry takes every event, and the catalog holds `S06`'s shared catalog: the caller `u_7f3a9c21` (`mg@example.com`) owns `blog`, `sit_4e7a1c9b0d2f8635`, public and listed, over `rep_8c21d4e0f7a3b915` at `main`, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; `handbook`, `sit_9a3c5e7b1d0f2468`, private; and `scratch`, `sit_2d6f8a0c4e1b3957`, slug `scratch-7c1e9a4f`, public, unlisted, and never published; `u_2b8e1d04` (`ann@example.com`) owns `recipes`, `sit_6b1d3f5a7c9e0284`. No apex is set. The visitor's browser holds the cookie `ikigenba_visitor=vis_1a2b3c4d5e6f7081` unless a story says it holds none.

## An operator follows a signed-in user's visit to a page

A user follows a link from another site to `blog`'s about page. The trail ties the view to the request and to the user through the envelope, names the site by id, and keeps of the referring page only its host: the path and query of the page the user came from, and the query of the page they asked for, are not recorded. A `Referer` of `http://localhost:8080/x` would give `localhost:8080`.

Request:

```
GET /blog/about/?from=newsletter HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
Referer: https://example.org/post?x=1
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

Status 200. The body is exactly the bytes of `about/index.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail. telemetry has received the request's three events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/blog/about/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/about/","referrer_host":"example.org","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"vis_1a2b3c4d5e6f7081"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `mg@example.com`, `from=newsletter`, `/post`, and `x=1` are in none of them.

## An operator follows a guest's first visit

A guest, with no credential and no cookie yet, opens `blog` directly. The guest's events carry an empty `user`; the request id nginx gave the request still ties them together, and the visitor id sites mints for the guest's browser is the one the trail records, so the guest's later views can be followed by it. There is no `Referer`, so `referrer_host` is empty.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly the bytes of `index.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`. `<visitor>` is the id sites minted, `vis_` and 16 lowercase hexadecimal digits (`S14`); the cookie is `Secure` because `X-Forwarded-Proto` is `https`.

Preconditions:

- The preamble's, except that the browser holds no `ikigenba_visitor` cookie and the request carries no identity headers.

Postconditions:

- Nothing has changed but the trail. telemetry has received the request's three events, in this order, each with an empty `user`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"<visitor>"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `<visitor>` in `site.viewed` is the value of the cookie the response set.

## An operator sees a view whose referrer could not be read

A `Referer` that is not a URL sites can parse is not an error: the page is served as it would be without one, and `referrer_host` is empty, as it is for a `Referer` with no host, such as `about:blank`. What the header held is not recorded.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
Referer: http://[example.org/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

Status 200. The body is exactly the bytes of `index.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`. No `Set-Cookie`: the request carried a well-formed visitor cookie.

Preconditions:

- The preamble's; the request is a guest's.

Postconditions:

- Nothing has changed but the trail. Between the request's `request.started` and its `request.finished`, telemetry has received:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"vis_1a2b3c4d5e6f7081"}}
  ```

## An operator finds no query string in the trail

A query string can carry what a visitor would not want kept — a token, a search, an address. sites serves the file the path names, whatever the query, and records the path alone, in `request.started` and in `site.viewed` both.

Request:

```
GET /blog/notes.txt?session=a81f2c&email=mg%40example.com HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Cache-Control: public, no-cache
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

Status 200. The body is exactly the bytes of `notes.txt` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail. telemetry has received the request's three events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/blog/notes.txt"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/notes.txt","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"vis_1a2b3c4d5e6f7081"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `session`, `a81f2c`, and the email are in none of them.

## An operator sees a visit to a missing page of a site

A view is recorded whatever its answer, once the request has reached a site. A path the published commit has no file for is answered `404` with the site's own `404.html` (`S11`), and its `site.viewed` carries that status and the commit the site is published at, so an operator can tell a broken link inside a site from a missing site.

Request:

```
GET /blog/old-post/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
Referer: https://sites.sbx.ikigenba.dev/blog/
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
```

Status 404. The body is exactly the bytes of `404.html` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Preconditions:

- The preamble's; the tree at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` has no `old-post/`. The request is a guest's.

Postconditions:

- Nothing has changed but the trail. Between the request's `request.started` and its `request.finished`, whose `status` is 404, telemetry has received:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/old-post/","referrer_host":"sites.sbx.ikigenba.dev","site":"sit_4e7a1c9b0d2f8635","status":404,"visitor":"vis_1a2b3c4d5e6f7081"}}
  ```

## An operator sees a visit to a site not yet published

A site that has never been published has no commit, and its pages are answered `404` with sites' not-found page (`S11`). The view is still the site's, so it is recorded, and its `commit` is empty: the one case where it is.

Request:

```
GET /scratch-7c1e9a4f/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
```

Status 404. The body is sites' not-found page: an HTML page whose `h1` reads `Not found` and whose `p#notfound` reads `There is nothing at this address.`, with the footer and no banner.

Preconditions:

- The preamble's: `scratch` has no commit. The request is a guest's.

Postconditions:

- Nothing has changed but the trail. Between the request's `request.started` and its `request.finished`, whose `status` is 404, telemetry has received:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"","path":"/scratch-7c1e9a4f/","referrer_host":"","site":"sit_2d6f8a0c4e1b3957","status":404,"visitor":"vis_1a2b3c4d5e6f7081"}}
  ```

## An operator finds no view for a path that names no site

A path whose first segment is no site's slug reached no site, so there is no site to record a view of and no `site.viewed`: its trail is the request's own pair. The same holds for a guest sent to sign in for a private site (`S12`), a method other than `GET` and `HEAD` (`S11`), and the apex host (`S13`).

Request:

```
GET /nosuchsite/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: no-cache
```

Status 404. The body is sites' not-found page, as in `An operator sees a visit to a site not yet published`. No `Set-Cookie`.

Preconditions:

- The preamble's: no site has the slug `nosuchsite`. The request is a guest's.

Postconditions:

- Nothing has changed but the trail. telemetry has received the request's two events, and no `site.viewed`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/nosuchsite/"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":404}}
  ```

## An operator follows a site's creation

`create` (`S06`) records `site.created` with the new site's id, its repository's id, its visibility, and whether it is listed, the last as a JSON boolean. The name the model chose is in no event; the site's id is how the rest of its trail is found.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"name":"docs","repo":"rep_3f9a0c1d2e4b5a69"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","name":"docs","slug":"docs","url":"https://sites.sbx.ikigenba.dev/docs/","repo":"rep_3f9a0c1d2e4b5a69","ref":"main","visibility":"public","listed":true,"created":"<created>"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: no site is named `docs`, and `rep_3f9a0c1d2e4b5a69` is the caller's repository (`S18`).

Postconditions:

- The catalog holds `docs`, unpublished, as `S06` tells.
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.created","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"listed":true,"repo":"rep_3f9a0c1d2e4b5a69","site":"<id>","visibility":"public"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `docs` and `mg@example.com` are in none of them.

## An operator follows a publish from a tag

`publish` (`S08`) records `site.published` with the commit it published and the ref it resolved, as the call gave it. Here the model publishes `blog` from the tag `v1`; the event says `v1`, while the site goes on tracking `main`, so the trail tells a one-off publish from a ref apart from the site's own.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"v1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"<published>"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<published>` is the time of the call.

Preconditions:

- The preamble's: the tag `v1` in `rep_8c21d4e0f7a3b915` names `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Postconditions:

- `blog` is published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `<published>`, and still tracks `main`.
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.published","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","ref":"v1","site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"publish"}}
  ```

  Published again from `main` with no `ref` argument, the event's `ref` is `main`, the site's own.

## An operator finds no event for a refused publish

A tool call that is refused changed nothing, and the trail says only that the call was made and refused: its `tool.called` with `outcome` `error`, and no `site.*` event. The text of the refusal and the ref the model asked for are not recorded.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: publish

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"publish","arguments":{"name":"blog","ref":"nosuchbranch"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no commit for 'nosuchbranch'
```

Preconditions:

- The preamble's: `rep_8c21d4e0f7a3b915` has no ref `nosuchbranch`.

Postconditions:

- `blog` is unchanged, published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` as of `2026-10-01T10:00:00Z`.
- telemetry has received the request's three events, in this order, and no `site.published`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"publish"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## An operator follows a change to whether a site is listed

`update` (`S09`) records one `site.updated` for each field it changed, with the field's name and its new value as a string: a boolean is the string `"false"` or `"true"`, never a JSON boolean, so every `site.updated` has the same shape whatever the field.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"update","arguments":{"name":"blog","listed":false},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":false,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `blog` is listed.

Postconditions:

- `blog` is unlisted; its slug is still `blog` (`S09`).
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.updated","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"field":"listed","site":"sit_4e7a1c9b0d2f8635","value":"false"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  ```

## An operator follows a site's deletion

`delete` (`S10`) records `site.deleted` with the site's id. The site's earlier events still name that id, so its whole trail, from `site.created` to `site.deleted`, can be followed after it is gone.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"delete","arguments":{"name":"scratch"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"sit_2d6f8a0c4e1b3957"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: the caller owns `scratch`, which is not the apex.

Postconditions:

- The catalog holds no `scratch`, as `S10` tells.
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.deleted","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"site":"sit_2d6f8a0c4e1b3957"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  ```

  Neither `scratch` nor `scratch-7c1e9a4f` is in any event.

## An operator follows the apex being set

`apex` (`S13`) records `site.apex` with the id of the site the apex now sends visitors to.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"apex","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"apex":{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: no apex is set.

Postconditions:

- The apex is `blog` (`S13`).
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.apex","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"apex"}}
  ```

## An operator follows the apex being cleared

Clearing the apex records `site.apex` with an empty `site`, so the trail shows when the apex host stopped sending visitors anywhere. Deleting the apex site clears it the same way, with this event after its `site.deleted` (`S10`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"apex","arguments":{"clear":true},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"apex":null}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, except that the apex is `blog`.

Postconditions:

- No apex is set (`S13`).
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"sites","event":"site.apex","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"site":""}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"apex"}}
  ```

## An operator learns why a site could not be served

When a site's published commit cannot be served, the visitor is answered `503` (`S16`), and the trail records both the view, with status 503, and `site.unavailable`, whose `reason` says what went wrong without the operator reading a log: here the site's repository is gone from repos' directory (`S18`). Each request answered this way records its own pair.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Retry-After: 60
```

Status 503. The body is sites' unavailable page, as `S16` fixes it.

Preconditions:

- The preamble's, except that `cache/sites/sit_4e7a1c9b0d2f8635/` holds no tree for `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and `/opt/repos/state/repos/rep_8c21d4e0f7a3b915.git` does not exist. The request is a guest's.

Postconditions:

- `blog` is still published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` (`S16`).
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":503,"visitor":"vis_1a2b3c4d5e6f7081"}}
  {"time":"<time>","service":"sites","event":"site.unavailable","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","reason":"repository_missing","site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":503}}
  ```

- sites wrote nothing to stderr: an unavailable site is a fact of the trail, not trouble.
