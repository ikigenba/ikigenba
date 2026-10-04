# Stories — apex

The apex: the one site a space shows at the root domain. A space's sites are served at `sites.<space>`, `sites.sbx.ikigenba.dev` say, but a person who types the root domain the space sits under, `ikigenba.dev`, should land somewhere; the apex says where. It is a setting of the catalog, `apex`, which holds one site's id or nothing, and starts as nothing. The `apex` tool reads, sets, and clears it, and sites answers every request at the apex host from it. devctl's `apex set` and opsctl's existing `host.apex = sites` setting route the root domain, the parent of the space's `host.name`, to sites, through nginx's open gate, so a guest's request reaches sites there with no identity headers, as on a site path; how they do that is told in devctl's and opsctl's own stories. sites takes every request whose `Host`, its port dropped, contains a `.` and does not begin with `sites.` to be an apex request, whatever its method and whatever its path, `/`, `/about`, and `/mcp` included. A `Host` with no `.`, `backend` or `sites` say, is a sibling calling over the socket, and is never an apex request: the mcp gateway names no service in `Host`: it sends `Host: backend` to every service (`S05`). With an apex set, an apex request is answered `302` with `Location: <sites-url>/<slug>/<path>`, where `<sites-url>` is the `url` of the services file's entry named `sites`, less one trailing `/`, and, when the file has no such entry or its `url` is empty, `<proto>://sites.<Host>`, with `<proto>` the request's `X-Forwarded-Proto` when that is `http` or `https` and `https` otherwise, and `<Host>` the request's `Host` as it arrived, port kept; `<slug>` is the apex site's slug; and `<path>` is the request's path without its leading `/`, its query kept after it. With none, it is answered `404` with sites' not-found page (`S11`) and `Cache-Control: no-cache`. An apex answer looks at nothing but the setting and the apex site's slug — not the site's visibility, whether it is published, nor who is asking — so it sets no cookie and records no `site.viewed`; the request's `request.started` and `request.finished` are its whole trail, and the page the browser is sent to is answered as `S11` and `S12` say.

The `apex` tool takes two optional arguments: `name`, a string, a site's name; and `clear`, a boolean. With neither, or with `clear` `false` and no `name`, it reads the setting. With `name`, it sets the apex to that site, which must be one of the caller's own sites and public: a name that is not one of the caller's sites, another user's included, is refused `no site named '<name>'`, and a private one `apex site must be public`. A site with no published commit may be the apex; its address answers as an unpublished site does (`S11`) until it is published. With `clear` `true`, it unsets the apex, whoever set it. Any user may read the apex and clear it, since it belongs to the space rather than to a site's owner, and any user may set it to a public site of their own, replacing whatever was there. `name` and `clear` `true` together are refused `apex takes name or clear, not both`. Its result is always `{"apex":<site>}`, where `<site>` is the apex site's object, as `show` answers it (`S07`), or `null` when there is none. Every call that sets or clears records `site.apex` with `site`, the apex site's id, or the empty string when cleared, between `request.started` and `tool.called`, even when it leaves the setting as it was, setting the site already the apex or clearing an apex already unset, as publishing the same commit again records `site.published` again (`S08`). Making the apex site private is refused by `update` (`S09`), and deleting it clears the apex (`S10`). `apex` is of kind `additive`, reached through the gateway's `mutate`.

The request shape, the result envelope, the error form, `tool.called`, and the trail are as `S05` fixes them. Each tool request is the HTTP request a running sites (`S02`) receives on `/mcp`, carrying `X-User-Id` and `X-User-Email` and no `X-Request-Id`, so sites gives it an id of its own, `<request-id>` below, on revision `2026-07-28`; sites runs with the suite's services file (`S03`), whose `sites` entry has the `url` `https://sites.sbx.ikigenba.dev`, and telemetry takes every event. The catalog is `S06`'s shared catalog: `u_7f3a9c21` (`mg@example.com`) owns `blog`, public and published; `handbook`, private; and `scratch`, slug `scratch-7c1e9a4f`, public and unpublished; `u_2b8e1d04` (`ann@example.com`) owns `recipes`, public and published; and the apex is unset unless a story says otherwise. `blog`'s object, which several stories answer, is:

```
{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}
```

No answer in this group earns a line on stderr. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed.

## A model asks for the apex when none is set

A model checks what the space shows at the root domain before changing it. Nothing is set yet.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"apex","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"apex":null}
```

and a `content` array of one text block whose text is that object encoded compactly. The arguments `{"clear":false}` are answered the same way.

Preconditions:

- The preamble's: the apex is unset.

Postconditions:

- Nothing has changed.
- sites recorded no `site.apex`. telemetry has received the request's three events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"apex"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model makes one of the user's public sites the apex

The ordinary case: the user wants the blog to be what the space shows at the root domain. The answer is the site, so the model can tell the user the address it now answers at.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"apex","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"apex":{"id":"sit_4e7a1c9b0d2f8635","name":"blog","slug":"blog","url":"https://sites.sbx.ikigenba.dev/blog/","repo":"rep_8c21d4e0f7a3b915","ref":"main","visibility":"public","listed":true,"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","created":"2026-10-01T09:30:00Z","published":"2026-10-01T10:00:00Z"}}
```

and a `content` array of one text block whose text is that object encoded compactly. The arguments `{"name":"blog","clear":false}` are answered the same way.

Preconditions:

- The preamble's: the caller owns `blog`, which is public, and the apex is unset.

Postconditions:

- The apex is `sit_4e7a1c9b0d2f8635`: `apex` with no arguments answers the same object, and a request at the apex host is redirected to `/blog/` (`A visitor opens the root domain`). `blog` itself is unchanged.
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.apex","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"site":"sit_4e7a1c9b0d2f8635"}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"apex"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The name `blog` is in none of them.

## Another user asks for the apex

The apex belongs to the space, so any user may see what it is, including a site they do not own. The answer is the site's object as its owner's `show` gives it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"apex","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` that is `{"apex":<blog>}`, where `<blog>` is `blog`'s object from the preamble, and a `content` array of one text block whose text is that object encoded compactly. `show` for `blog` is still refused to this caller (`S07`); the apex is the one place another user's site object is answered.

Preconditions:

- The apex is `sit_4e7a1c9b0d2f8635`, `blog`, set by `u_7f3a9c21`. The caller, `u_2b8e1d04`, does not own `blog`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.apex`; the request's `tool.called` has `user` `u_2b8e1d04` and `outcome` `ok`.

## A model sets the apex in place of another user's site

The apex is the space's, not any one user's, so setting it replaces whatever was there, whoever set it. Here `u_2b8e1d04` made `recipes` the apex earlier.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"apex","arguments":{"name":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` that is `{"apex":<blog>}`, as in `A model makes one of the user's public sites the apex`, and a `content` array of one text block whose text is that object encoded compactly. Setting `blog` again, once it is the apex, answers the same.

Preconditions:

- The apex is `sit_6b1d3f5a7c9e0284`, `recipes`, owned by `u_2b8e1d04`.

Postconditions:

- The apex is `sit_4e7a1c9b0d2f8635`. `recipes` itself is unchanged and still served at `/recipes/`.
- telemetry has received, between the request's `request.started` and its `tool.called`, whose `outcome` is `ok`, one `site.apex`, naming the new apex:

  ```
  {"time":"<time>","service":"sites","event":"site.apex","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"site":"sit_4e7a1c9b0d2f8635"}}
  ```

  Had `blog` already been the apex, the call would have recorded the same `site.apex`.

## A model tries to make a private site the apex

The apex host answers anyone, so the site it sends them to must be one anyone may read. A private site would send every visitor at the root domain to a sign-in.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"apex","arguments":{"name":"handbook"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
apex site must be public
```

Preconditions:

- The preamble's: the caller owns `handbook`, which is private; the apex is unset.

Postconditions:

- Nothing has changed: the apex is still unset.
- sites recorded no `site.apex`. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"apex"}}
  ```

## A model tries to make another user's site the apex

A user sets the apex only to a site of their own. To this caller, another user's site is as if it did not exist, as in every tool that names a site (`S07`), so the refusal is the one for a name that names nothing, whether or not the site is public.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"apex","arguments":{"name":"recipes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no site named 'recipes'
```

A name no site has, `nosuch` say, is refused the same way: `no site named 'nosuch'`. A site is named by its name, never its slug or id.

Preconditions:

- The preamble's: `recipes` is public and owned by `u_2b8e1d04`, not the caller; the apex is unset.

Postconditions:

- Nothing has changed: the apex is still unset.
- sites recorded no `site.apex`; the request's `tool.called` has `outcome` `error`.

## A user clears the apex

A user who no longer wants the root domain to lead anywhere clears the apex. Any user may: the apex is the space's, and clearing it takes nothing from any site. Here `u_2b8e1d04` clears the apex `u_7f3a9c21` set.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
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

and a `content` array of one text block whose text is that object encoded compactly. Clearing an apex already unset answers the same.

Preconditions:

- The apex is `sit_4e7a1c9b0d2f8635`, `blog`, set by `u_7f3a9c21`.

Postconditions:

- The apex is unset: `apex` with no arguments answers `{"apex":null}`, and a request at the apex host is answered `404` (`A visitor opens the root domain when no apex is set`). `blog` is unchanged and still served at `/blog/`.
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"<request-id>","user":"u_2b8e1d04","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"sites","event":"site.apex","request_id":"<request-id>","user":"u_2b8e1d04","attrs":{"site":""}}
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_2b8e1d04","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"apex"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"<request-id>","user":"u_2b8e1d04","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  Had the apex already been unset, the call would have recorded the same `site.apex`.

## A model asks to set and clear the apex at once

Setting and clearing contradict each other, and sites will not guess which was meant, so the call is refused and nothing changes. `clear` `false` beside a `name` is no contradiction: it sets.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"apex","arguments":{"name":"blog","clear":true},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
apex takes name or clear, not both
```

Preconditions:

- The preamble's: the apex is unset.

Postconditions:

- Nothing has changed: the apex is still unset.
- sites recorded no `site.apex`; the request's `tool.called` has `outcome` `error`.

## A model sends apex arguments of the wrong kind

Arguments are read against the tool's input schema first, with the platform's wording (`S05`): every offence in one answer, the tool's fields in the order of its schema, then each unknown field in the order it was sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"apex","arguments":{"name":3,"clear":"yes","site":"blog"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: expected string, got number
clear: expected boolean, got string
site: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- sites recorded no `site.apex`. Between the request's `request.started` and its `request.finished`, telemetry has received one event:

  ```
  {"time":"<time>","service":"sites","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"apex"}}
  ```

## A visitor opens the root domain

Someone types the root domain with nothing in front of it and lands on the apex site. sites does not serve the site at the apex host: it sends the browser to the site's own address, the one the services file publishes for sites, where it is served, cookie and trail included, as any visit is (`S11`). The root domain is not the space: on the space `sbx.ikigenba.dev` it is `ikigenba.dev`, and the redirect leads to `sites.sbx.ikigenba.dev`, never to `sites.ikigenba.dev`.

Request:

```
GET / HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://sites.sbx.ikigenba.dev/blog/
```

Status 302. No story fixes the body. There is no `Set-Cookie`. A `HEAD` is redirected the same way, and so is a signed-in user's request: who asks makes no difference at the apex host. `Host: ikigenba.dev:443` is the same apex request, answered the same way.

Preconditions:

- devctl's `apex set`, through opsctl's `host.apex = sites`, routes the root domain `ikigenba.dev` to sites through an open gate, so the request reaches sites with no identity headers.
- sites runs with the suite's services file, whose `sites` entry has the `url` `https://sites.sbx.ikigenba.dev`.
- The apex is `sit_4e7a1c9b0d2f8635`, `blog`, slug `blog`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`. telemetry has received the request's two events:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":302}}
  ```

## A visitor follows an old link at the root domain

A link that names a path at the root domain keeps it: the path and the query are carried into the apex site, so a link to a page of the site written against the root domain still reaches the page. The path is not looked at here; whether the site has it is for the site's own answer to say (`S11`). The root domain's `/about` and `/mcp` are paths like any other there, carried to the site, never sites' about screen or MCP endpoint.

Request:

```
GET /hello?x=1 HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://sites.sbx.ikigenba.dev/blog/hello?x=1
```

Status 302. No story fixes the body. `GET /about/team/` is redirected to `https://sites.sbx.ikigenba.dev/blog/about/team/`, and `GET /mcp` to `https://sites.sbx.ikigenba.dev/blog/mcp`.

Preconditions:

- As in `A visitor opens the root domain`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`. The request's `request.started` has `path` `/hello`, without the query, and its `request.finished` has `status` 302.

## A visitor opens the root domain where the services file names no sites

Without a `sites` entry to name its public address, sites builds the redirect from the request itself: the scheme `X-Forwarded-Proto` names, `https` when it names neither `http` nor `https`, and `sites.` in front of the `Host` as it arrived, port kept. That address is right only where sites sits directly under the apex host, as in a sandbox; on a space the services file always names sites.

Request:

```
GET / HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET / HTTP/1.1
Host: wip.localhost:7400
X-Forwarded-Proto: http
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: <location>
```

Status 302. No story fixes the body. For `Host: ikigenba.dev`, `<location>` is `https://sites.ikigenba.dev/blog/`; for `Host: wip.localhost:7400` over `http`, it is `http://sites.wip.localhost:7400/blog/`. With `Host: wip.localhost:7400` and no `X-Forwarded-Proto`, it is `https://sites.wip.localhost:7400/blog/`. A `sites` entry whose `url` is empty is the same as no entry.

Preconditions:

- The apex host is routed to sites, as in `A visitor opens the root domain`, and the apex is `blog`.
- sites runs with `IKIGENBA_SERVICES` naming no file, or a file with no entry named `sites`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## The mcp gateway reaches sites at its socket

The mcp gateway calls sites over its Unix socket with `Host: backend`: it names no service in `Host`: it sends `Host: backend` to every service, and that is not a name under any space. A `Host` with no `.` is a sibling calling over the socket, sites' own host, never the apex host, so the call reaches the MCP endpoint (`S05`) and is answered as it would be at `sites.sbx.ikigenba.dev`, whether or not an apex is set.

Request:

```
POST /mcp HTTP/1.1
Host: backend
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: apex

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"apex","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member and a `structuredContent` that is `{"apex":<blog>}`, as in `Another user asks for the apex`. It is not a `302`. `Host: backend:80` is the same host, and other bare names such as `sites` and `sites:80` are answered the same way.

Preconditions:

- The apex is `sit_4e7a1c9b0d2f8635`, `blog`.

Postconditions:

- Nothing has changed.
- The request's `tool.called` has `outcome` `ok`, and its `request.finished` has `status` 200.

## A client sends the root domain a method other than GET

Every request at the apex host is redirected, whatever its method: sites serves nothing there of its own, so there is nothing to refuse a method for. A form that posts to the root domain is sent on to the apex site.

Request:

```
POST /contact HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
DELETE / HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://sites.sbx.ikigenba.dev/blog/<path>
```

Status 302. No story fixes the body. `<path>` is `contact` for the `POST` and empty for the `DELETE`, which is redirected to `https://sites.sbx.ikigenba.dev/blog/`. Neither is a `405`.

Preconditions:

- As in `A visitor opens the root domain`.

Postconditions:

- Nothing has changed. The request's `request.finished` has `status` 302.

## A visitor opens the root domain of a space whose apex is an unpublished site

The apex answer looks only at the apex site's slug, so it redirects to a site that is not yet published as readily as to one that is. The visitor then meets the not-found page at the site's address until the site is published (`S11`).

Request:

```
GET / HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://sites.sbx.ikigenba.dev/scratch-7c1e9a4f/
```

Status 302. No story fixes the body.

Preconditions:

- As in `A visitor opens the root domain`, except that the apex is `sit_2d6f8a0c4e1b3957`, `scratch`, slug `scratch-7c1e9a4f`, public and with no published commit, set by its owner.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## A visitor opens the root domain when no apex is set

With no apex, the root domain leads nowhere, and sites says so with its not-found page, to anyone and for any method or path. The answer is no site's, so it says only `no-cache`.

Request:

```
GET / HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /hello?x=1 HTTP/1.1
Host: ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: no-cache
```

Status 404. The body is sites' not-found page (`S11`): titled `Not found`, its heading `Not found` and its text `There is nothing at this address.`, with the footer `sites v<semver>` and no banner. There is no `Set-Cookie`. A `POST` is answered the same way, never `405`.

Preconditions:

- devctl's `apex set`, through opsctl's `host.apex = sites`, routes the root domain to sites, as in `A visitor opens the root domain`.
- The apex is unset.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`. telemetry has received the request's two events, the `request.finished` with `status` 404.
