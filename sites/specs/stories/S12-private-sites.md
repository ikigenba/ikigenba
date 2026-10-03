# Stories — private sites

Serving a private site: what a running sites (`S02`) answers on the site paths of a site whose visibility is `private`. A private site is for the people of the space, not for the world: any signed-in user of the space may read it, its owner or anyone else, and a guest is sent to sign in first. sites answers a site path in the order `S11` gives; for a private site, once the method has been accepted and the slug found, a guest's request is answered with a redirect to auth's sign-in before anything else about the path is looked at — before the redirect to add the slug's `/`, before whether the site is published, and before the path is resolved in the tree — so a guest learns nothing of a private site but that it asks for a sign-in. The redirect is `302` with `Location: <proto>://auth.<space>/?return=<url>`: `<proto>` is the request's `X-Forwarded-Proto` in lower case when that is `http` or `https` in any letter case, and `https` otherwise, including when there is none; `<space>` is the request's `Host` with its leading `sites.` removed, port kept; and `<url>` is `<proto>://<Host><request URI>`, the address the guest asked for, path and query, percent-encoded as a query component (`:` as `%3A`, `/` as `%2F`, `?` as `%3F`, `=` as `%3D`, `&` as `%26`), so auth can send the guest back to it once they have signed in. What auth does with it is told in auth's own stories. The redirect is not a view of the site: it sets no cookie and records no `site.viewed`, and the request's `request.started` and `request.finished` are its whole trail. A user's request is served exactly as `S11` serves a public site's — files, redirects, the site's own `404.html` or sites' not-found page, `304`, the visitor cookie, and `site.viewed` — with one difference: every such answer carries `Cache-Control: private, no-cache`, so no shared cache keeps a copy a guest could be given.

Unless a story says otherwise, sites runs as in `S11`, with `S06`'s shared catalog, the suite's services file, and telemetry taking every event, and the requests carry `Host: sites.sbx.ikigenba.dev`, `X-Forwarded-Proto: https`, and nginx's `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`; a user's carry `X-User-Id` and `X-User-Email` too, and a guest's neither. The private site is `handbook`, id `sit_9a3c5e7b1d0f2468`, owned by `u_7f3a9c21` (`mg@example.com`), listed, tracking `main` of repository `rep_3f9a0c1d2e4b5a69`, and published at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`, whose tree, `cache/sites/sit_9a3c5e7b1d0f2468/a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d/`, holds `index.html` and `guide/index.html` and no `404.html`. `<blob:<path>>` below stands for the bytes `git --git-dir=<REPOS_DIR>/rep_3f9a0c1d2e4b5a69.git cat-file blob a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d:<path>` prints. A user's request in this group carries no cookie unless it says so, and is answered with `Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure` (`S14`). No answer in this group earns a line on stderr. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed.

## A guest opens a private site

Someone with a link to the handbook, not signed in, is sent to sign in to the space, and the sign-in will bring them back to the page they asked for.

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

Status 302. No story fixes the body. There is no `Set-Cookie` and no `ETag`, and nothing of the site is in the answer. A `HEAD` is answered the same way. `GET /handbook`, without the slash, is not first redirected to `/handbook/`: it is sent to sign in with the return URL it asked for, `https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fhandbook`. A path within the site that names nothing, `/handbook/nope.html` say, is sent to sign in the same way, with its own return URL, so a guest cannot learn which files the site holds.

Preconditions:

- sites is serving, and telemetry takes every event.
- The catalog holds `handbook`, private and published, and its tree is in the cache.
- The request carries no `X-User-Id` and no cookie.

Postconditions:

- Nothing has changed. No file of the tree was read.
- sites recorded no `site.viewed`. telemetry has received the request's two events, with an empty user:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/handbook/"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":302}}
  ```

## A guest follows a link deep into a private site

The return URL is the whole address the guest asked for, path and query, so a link into the middle of the handbook still lands there after signing in. Each reserved character of the address is encoded, so the return URL is one query value to auth.

Request:

```
GET /handbook/guide/?q=1 HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fhandbook%2Fguide%2F%3Fq%3D1
```

Status 302. No story fixes the body. With the query `?q=1&tab=2`, the return URL ends `%3Fq%3D1%26tab%3D2`.

Preconditions:

- The preamble's: the request carries no `X-User-Id`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`. The request's `request.started` has `path` `/handbook/guide/`, without the query, and its `request.finished` has `status` 302.

## A guest opens a private site in the sandbox, over plain HTTP

In the sandbox the space is a local name with a port, served over `http`. The sign-in address and the return URL follow the request: auth is at the same space, port kept, over the same scheme the request came in on.

Request:

```
GET /handbook/ HTTP/1.1
Host: sites.wip.localhost:7400
X-Forwarded-Proto: http
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: http://auth.wip.localhost:7400/?return=http%3A%2F%2Fsites.wip.localhost%3A7400%2Fhandbook%2F
```

Status 302. No story fixes the body. With `Host: sites.sbx.ikigenba.dev` and `X-Forwarded-Proto: http`, `Location` is `http://auth.sbx.ikigenba.dev/?return=http%3A%2F%2Fsites.sbx.ikigenba.dev%2Fhandbook%2F`.

Preconditions:

- The preamble's, with the request reaching sites at the sandbox's host (`S22`).

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## A guest's request arrives with no forwarded scheme

nginx always says which scheme the browser used. A request that does not, or names something other than `http` or `https`, is taken to have come over `https`, the scheme every host serves on, so the sign-in address is never downgraded by a missing header.

Request:

```
GET /handbook/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /handbook/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: ftp
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fhandbook%2F
```

Status 302. No story fixes the body.

Preconditions:

- The preamble's: the request carries no `X-User-Id`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## A guest opens a private site that is not yet published

A private site asks a guest to sign in before anything else is looked at, so a guest learns nothing more of an unpublished private site than of a published one: not that it is unpublished, and not that it exists beyond asking for a sign-in.

Request:

```
GET /scratch-7c1e9a4f/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 302 Found
Location: https://auth.sbx.ikigenba.dev/?return=https%3A%2F%2Fsites.sbx.ikigenba.dev%2Fscratch-7c1e9a4f%2F
```

Status 302. No story fixes the body. There is no `Set-Cookie`.

Preconditions:

- `scratch`, slug `scratch-7c1e9a4f`, id `sit_2d6f8a0c4e1b3957`, has been made private with `update` (`S09`) and has no published commit.
- The request carries no `X-User-Id`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`.

## A user opens a private site that is not yet published

Once signed in, a user meets an unpublished private site as anyone meets an unpublished public one (`S11`): sites' not-found page, the cookie, and a `site.viewed` with an empty `commit`, but marked `private`.

Request:

```
GET /scratch-7c1e9a4f/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: private, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is sites' not-found page (`S11`). There is no `ETag`.

Preconditions:

- `scratch` is private and has no published commit, as in `A guest opens a private site that is not yet published`.

Postconditions:

- Nothing has changed. No git ran.
- telemetry has received, between the request's `request.started` and its `request.finished`, whose `status` is 404:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_2b8e1d04","attrs":{"commit":"","path":"/scratch-7c1e9a4f/","referrer_host":"","site":"sit_2d6f8a0c4e1b3957","status":404,"visitor":"<visitor>"}}
  ```

## The owner opens their private site

The owner, signed in, reads the handbook as any reader would. The answer is the public site's (`S11`) but for `Cache-Control`, which says `private`: the page was given to a signed-in user and no shared cache may hand it to anyone else.

Request:

```
GET /handbook/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Content-Length: <bytes>
ETag: "a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d"
Cache-Control: private, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly `<blob:index.html>`.

Preconditions:

- The preamble's: `handbook` is private and published at `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`, and its tree is in the cache.

Postconditions:

- Nothing has changed.
- telemetry has received the request's three events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/handbook/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"commit":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d","path":"/handbook/","referrer_host":"","site":"sit_9a3c5e7b1d0f2468","status":200,"visitor":"<visitor>"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

## A user who does not own a private site reads it

Private means private to the space, not to the owner: any user signed in to the space reads a private site exactly as its owner does. Ownership decides who manages a site through the tools, never who may read it.

Request:

```
GET /handbook/guide/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Content-Length: <bytes>
ETag: "a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d"
Cache-Control: private, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly `<blob:guide/index.html>`. `GET /handbook`, without the slash, is redirected to `Location: /handbook/` with `301` and `Cache-Control: private, no-cache`, and `GET /handbook/guide` to `Location: /handbook/guide/`, as `S11` redirects them for a public site.

Preconditions:

- The preamble's: `u_2b8e1d04` does not own `handbook`.

Postconditions:

- Nothing has changed.
- telemetry has received, between the request's `request.started` and its `request.finished`:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_2b8e1d04","attrs":{"commit":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d","path":"/handbook/guide/","referrer_host":"","site":"sit_9a3c5e7b1d0f2468","status":200,"visitor":"<visitor>"}}
  ```

  The email `ann@example.com` is in none of the request's events.

## A user revalidates a private page that has not changed

A user's browser revalidates a private page as it does a public one (`S11`), and the `304` carries the private site's `Cache-Control`. This user's browser has been here before, so it sends the visitor cookie, and no new one is set.

Request:

```
GET /handbook/guide/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Cookie: ikigenba_visitor=vis_1a2b3c4d5e6f7081
If-None-Match: "a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d"
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d"
Cache-Control: private, no-cache
```

Status 304. The body is empty. There is no `Set-Cookie`. A guest who sends the same `If-None-Match` is not answered `304`: it is sent to sign in, as in `A guest opens a private site`.

Preconditions:

- The preamble's. The request carries the well-formed visitor cookie `vis_1a2b3c4d5e6f7081`.

Postconditions:

- Nothing has changed.
- telemetry has received, between the request's `request.started` and its `request.finished`, whose `status` is 304:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_2b8e1d04","attrs":{"commit":"a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d","path":"/handbook/guide/","referrer_host":"","site":"sit_9a3c5e7b1d0f2468","status":304,"visitor":"vis_1a2b3c4d5e6f7081"}}
  ```

## A user asks for a page a private site does not have

Inside a private site, a path that names nothing is answered as `S11` answers one in a public site: here the tree has no `404.html`, so with sites' not-found page; and the answer is marked `private`.

Request:

```
GET /handbook/nope.html HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: private, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is sites' not-found page (`S11`). There is no `ETag`. A hidden file, a `..` or `.` segment, and a symlink are answered the same way, as in `S11`.

Preconditions:

- The preamble's: `handbook`'s tree holds no `nope.html` and no `404.html`.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `user` `u_7f3a9c21`, `site` `sit_9a3c5e7b1d0f2468`, `path` `/handbook/nope.html`, `status` 404, and `commit` `a3f1c9e27b4d6058e1c2a9b7d3f5e8016c4b2a9d`.
