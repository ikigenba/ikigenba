# Stories — visitors

The visitor cookie: how sites tells one browser from another across visits to a space's sites, without anyone signing in. A visitor is a browser, known by the cookie `ikigenba_visitor`, whose value is a visitor id, `vis_` and 16 lowercase hexadecimal digits that sites draws from its random source. sites sets it on every answer that is a view of a known site — every answer on a site path from the redirect to the slug's `/` on, as `S11` orders them: `200`, `301`, `304`, `404` within a site or for an unpublished one, and `503` when the site cannot be served (`S16`), for a guest and a user alike, to `GET` and `HEAD` alike — when the request carried no well-formed visitor cookie, and on no other answer. A request that carries one keeps it: it is answered with no `Set-Cookie`, and nothing about the cookie changes, not even its lifetime. A request whose `ikigenba_visitor` is not a well-formed visitor id — the wrong prefix, the wrong length, a character that is not a lowercase hexadecimal digit, or empty — is treated as carrying none, and is answered with a new one. The cookie is set as `Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax`, followed by `; Secure` when the request's `X-Forwarded-Proto` is `https`, in any letter case, and with no `; Secure` when the header is absent or holds anything else, `http` included: `Path=/` so the browser sends it for every site of the host; `Max-Age=34560000`, 400 days, the longest a browser keeps a cookie; `HttpOnly`, since no page needs to read it; and `SameSite=Lax`, so a visitor following a link from another site still sends it. It has no `Domain`, so it belongs to the host that set it, `sites.<space>`, and no other host, the root domain and the space's other apps included, ever receives it. The visitor id is what `site.viewed` records as `visitor` (`S11`): the cookie's when the request carried a well-formed one, and the one just minted otherwise, so a first visit and every later one share an id. sites keeps no record of visitors: the id is in the cookie and in the trail and nowhere else, and two first visits each get their own.

Unless a story says otherwise, sites runs as in `S11`, with `S06`'s shared catalog, the suite's services file, and telemetry taking every event, and the requests carry `Host: sites.sbx.ikigenba.dev`, `X-Forwarded-Proto: https`, and nginx's `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`. `blog`, id `sit_4e7a1c9b0d2f8635`, is public and published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and its tree is in the cache. A returning browser's cookie is `ikigenba_visitor=vis_1a2b3c4d5e6f7081`. No answer in this group earns a line on stderr. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed, but where a story says there is no `Set-Cookie`, there is none.

## A visitor's first visit gives the browser a visitor id

A browser that has never been to the space's sites opens one. sites mints a visitor id for it, sets it as a cookie that lasts 400 days, and records the view under that same id, so the visit and every later one from this browser can be counted as one visitor's.

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
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is `blog`'s `index.html` (`S11`). `<visitor>` is `vis_` and 16 lowercase hexadecimal digits sites drew from its random source. The `Set-Cookie` appears once, with exactly these attributes, in this order, and no `Domain` and no `Expires`. A signed-in user's first visit is answered the same way: the visitor is the browser, not the user.

Preconditions:

- sites is serving, and telemetry takes every event.
- The request carries no `Cookie` header.

Postconditions:

- Nothing has changed in the catalog or the cache; sites stored nothing about the visitor.
- telemetry has received the request's three events, in this order, the `site.viewed` naming the visitor id just set:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"<visitor>"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `<visitor>` is the same id as in the `Set-Cookie`.

## A visitor's first visit over plain HTTP gets a cookie the browser will send back

A cookie marked `Secure` is never sent over plain `http`, so on a space served over `http`, as the sandbox is, sites leaves `Secure` off, and the browser sends the cookie back on its next visit. The cookie still names no `Domain`: it belongs to `sites.wip.localhost:7400`'s host alone.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.wip.localhost:7400
X-Forwarded-Proto: http
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax
```

Status 200. The body is `blog`'s `index.html` (`S11`). The `Set-Cookie` has no `Secure`. `X-Forwarded-Proto: HTTPS` is `https`, and its cookie is `Secure`, as in `A visitor's first visit gives the browser a visitor id`.

Preconditions:

- The preamble's, with the request reaching sites at the sandbox's host (`S22`).
- The request carries no `Cookie` header.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` whose `visitor` is the id in the `Set-Cookie`.

## A visitor's first visit with no forwarded scheme gets a cookie without Secure

nginx always says which scheme the browser used. A request that does not, as a developer's request straight to the socket does not, or names something other than `https`, gets a cookie without `Secure`: sites marks the cookie `Secure` only when it knows the browser came over `https`.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: ftp
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax
```

Status 200. The body is `blog`'s `index.html` (`S11`). The `Set-Cookie` has no `Secure`.

Preconditions:

- The preamble's. The request carries no `Cookie` header.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` whose `visitor` is the id in the `Set-Cookie`.

## A returning visitor keeps their visitor id

A browser that has been here before sends its cookie, and keeps it: sites sets nothing, so the cookie is neither replaced nor renewed, and the view is recorded under the id the browser already had. Other cookies beside it change nothing.

Request:

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: theme=dark; ikigenba_visitor=vis_1a2b3c4d5e6f7081
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
```

Status 200. The body is `blog`'s `style.css` (`S11`). There is no `Set-Cookie`.

Preconditions:

- The preamble's. The request carries the visitor cookie `vis_1a2b3c4d5e6f7081`, well formed.

Postconditions:

- Nothing has changed.
- telemetry has received, between the request's `request.started` and its `request.finished`:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/style.css","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"vis_1a2b3c4d5e6f7081"}}
  ```

  The cookie `theme` is in no event.

## A visitor's cookie that is not a visitor id is replaced

A cookie of the right name whose value sites did not mint — mangled by an extension, typed by hand, left by something else — is no visitor id, and sites will not record it as one. It answers as for a first visit: a new id, set as a cookie that overwrites the bad one, and the view recorded under the new id.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=hello
```

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1A2B3C4D5E6F7081
```

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f708
```

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is `blog`'s `index.html` (`S11`). `<visitor>` is a new id, never the value the request sent, upper-case digits and all: `vis_1A2B3C4D5E6F7081` is not folded into `vis_1a2b3c4d5e6f7081`.

Preconditions:

- The preamble's. The request's `ikigenba_visitor` is `hello`, `vis_1A2B3C4D5E6F7081` (upper case), `vis_1a2b3c4d5e6f708` (15 digits), or empty.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` whose `visitor` is `<visitor>`, the id in the `Set-Cookie`; the malformed value is in no event.

## A first-time visitor who meets a missing page still gets a visitor id

A visit that ends on a 404 inside a site is still a visit to that site, so the browser is given its id there as anywhere in the site, and the miss is recorded under it. The same holds for the redirect to a site's `/` and for a site that is not yet published (`S11`).

Request:

```
GET /blog/no-such-post.html HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 <status>
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404 for `/blog/no-such-post.html`, whose body is `blog`'s own `404.html`, and 301 for `/blog`, with `Location: /blog/`, as `S11` answers them; each sets the cookie.

Preconditions:

- The preamble's. The request carries no `Cookie` header.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` whose `status` is the answer's, 404 or 301, and whose `visitor` is the id in the `Set-Cookie`.

## A first-time visitor revalidating a page gets a visitor id

A browser can hold a cached copy of a page and no cookie, when its cookies were cleared and its cache was not. Its revalidation is a view of the site like any other, so the `304` sets the cookie too.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
If-None-Match: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 304. The body is empty.

Preconditions:

- The preamble's. The request carries no `Cookie` header.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `status` 304 and `visitor` the id in the `Set-Cookie`.

## A first-time visitor who meets an unavailable site still gets a visitor id

When a site's published commit cannot be served, the visitor gets the unavailable page (`S16`). It is still a visit to that site, so a browser with no cookie is given its id, and the visit is recorded under it.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 503 Service Unavailable
Retry-After: 60
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 503. The body is sites' unavailable page, as `S16` tells it.

Preconditions:

- `blog`'s tree is missing from the cache and cannot be rebuilt, as in `S16`.
- The request carries no `Cookie` header.

Postconditions:

- The request's `site.viewed` has `status` 503, `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and `visitor` the id in the `Set-Cookie`, and comes before the request's `site.unavailable`; the rest of its trail is `S16`'s.

## A visitor who reaches no site gets no cookie

A slug that names no site, and a method a site path does not take, are not views of any site, so a browser that has no cookie gets none, and one that has a cookie keeps it untouched (`S11`).

Request:

```
GET /nosuch/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
POST /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 <status>
```

Status 404 for `/nosuch/`, with sites' not-found page, and 405 for the `POST`, with `Allow: GET, HEAD` and an empty body, as `S11` answers them. Neither has a `Set-Cookie`.

Preconditions:

- The preamble's: no site has the slug `nosuch`. The request carries no `Cookie` header.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## A guest sent to sign in from a private site gets no cookie

The redirect to sign in is not a view of the private site (`S12`), so the guest's browser gets no visitor id from it. It gets one when it comes back signed in and is served the site.

Request:

```
GET /handbook/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fhandbook%2F
```

Status 302. No story fixes the body. There is no `Set-Cookie`.

Preconditions:

- `handbook` is private (`S12`). The request carries no `X-User-Id` and no `Cookie` header.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## A visitor at the apex host gets no cookie

The apex host only sends the browser on to the apex site (`S13`), so it sets nothing; and a cookie set there would belong to the root domain's host, never to `sites.<space>`. The browser gets its id from the site's own answer, at the address it is sent to.

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

Status 302. No story fixes the body. There is no `Set-Cookie`. With no apex set, the `404` is answered with no `Set-Cookie` either.

Preconditions:

- The apex is `blog` (`S13`). The request carries no `Cookie` header.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## A user on sites' own pages gets no cookie

The landing page, the about screen, the shared files under `/_appkit/`, and the MCP endpoint are sites' own, not any site's, so none of them sets the visitor cookie, for a browser with one or without. A cookie a request carries there is ignored.

Request:

```
GET / HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /about HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /_appkit/theme.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
```

Status 200. The bodies are the landing page and the about screen (`S03`) and the stylesheet (`S04`). None has a `Set-Cookie`. A guest's `GET /`, sent to sign in (`S03`), and every answer at `/mcp` (`S05`), a `tools/call` included, have none either.

Preconditions:

- The preamble's. The requests carry no `Cookie` header.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.
