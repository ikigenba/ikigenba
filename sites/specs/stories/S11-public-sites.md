# Stories — public sites

Serving a public site: what a running sites (`S02`) answers at `/<slug>/` and every path beneath it on `sites.<space>`, for a site whose visibility is `public`. A public site is served to anyone. On a host, nginx's gate for sites is open because sites' manifest declares `guests = true` (`S01`): a guest's request reaches sites with no `X-User-Id` and no `X-User-Email`, and a signed-in user's with both, and nginx gives every request an `X-Request-Id`. A guest is a request with no `X-User-Id`; a user is a request with one. A public site answers a user exactly as it answers a guest, but for the `user` its events carry. Every path but `/`, `/about`, `/mcp`, and `/_appkit/<file>` is a site path, whose first segment is a slug (`S03`). sites answers a site path in this order: a method other than `GET` or `HEAD` is refused with `405`; a slug that names no site is answered with sites' not-found page; a private site sends a guest to sign in (`S12`); `/<slug>` without its trailing `/` is redirected to `/<slug>/`; a site with no published commit is answered with the not-found page; and otherwise the rest of the path is resolved in the site's tree. The tree of a site's published commit is `cache/sites/<site id>/<sha>/` under sites' working directory, unpacked from the site's repository (`S08`); in this group every published site's tree is already there, and a tree that is missing is `S16`'s. sites resolves the rest of the path in that tree and nowhere else: a path ending in `/` serves the `index.html` of that directory; a path naming a directory without a trailing `/` is redirected to add it, its query kept; a path naming a file serves the file; and a path that names nothing, a segment that is `.` or `..` or begins with `.`, or a symlink in the tree, is a missing file, answered `404` with the tree's own `/404.html` when the tree holds one, and with sites' not-found page when it does not. A served file's body is the file's bytes exactly as `git archive` emitted them, and its headers are `Content-Type`, from the file's extension, `text/html; charset=utf-8` for `.html` and `.htm`, the type with `; charset=utf-8` for every other `text/` type, and `application/octet-stream` when the extension is unknown; `Content-Length`, the file's size in bytes; `ETag`, the published commit's sha in double quotes, the same for every file of the site; and `Cache-Control: public, no-cache`, so a browser keeps its copy and asks each time whether it is still current. sites sends no `Last-Modified`. Every answer for a site path of a known site, from the redirect to `/<slug>/` on — `200`, `301`, `304`, and `404` alike — carries `Cache-Control: public, no-cache`, sets the visitor cookie when the request carried none, and records `site.viewed` between the request's `request.started` and `request.finished`. The visitor cookie is `S14`'s; a request in this group carries no cookie unless it says so, and is answered with `Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure`, where `<visitor>` is the id sites minted for it, `vis_` and 16 lowercase hexadecimal digits. `site.viewed`'s attributes are `site`, the site's id; `visitor`, the visitor id, the one just minted when the request carried none; `path`, the request's URL path as it arrived, without its query; `status`, the status of the answer; `referrer_host`, the host of the request's `Referer`, port kept, or the empty string when there is none or it cannot be parsed; and `commit`, the site's published sha, or the empty string when it has none. No query and no email is ever in the trail, and no attribute but `path` carries a site's name or slug. sites' not-found page is an HTML document, `Content-Type: text/html; charset=utf-8`, titled `Not found`, that links `/_appkit/theme.css`, whose heading is `Not found` and whose text is `There is nothing at this address.`, with the footer `sites v<semver>` and no banner and no script; an answer that is not a site's own carries no `ETag`.

Unless a story says otherwise, sites runs with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json`, the suite's services file with sites' entry (`S03`), and telemetry takes every event. The requests carry `Host: sites.sbx.ikigenba.dev`, `X-Forwarded-Proto: https`, and nginx's `X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`. The catalog is `S06`'s shared catalog: `blog`, id `sit_4e7a1c9b0d2f8635`, owned by `u_7f3a9c21`, public and listed, tracking `main` of repository `rep_8c21d4e0f7a3b915` and published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`; `handbook`, id `sit_9a3c5e7b1d0f2468`, private (`S12`); `scratch`, id `sit_2d6f8a0c4e1b3957`, slug `scratch-7c1e9a4f`, public, unlisted, and not yet published; and `recipes`, id `sit_6b1d3f5a7c9e0284`, owned by `u_2b8e1d04`, public and listed, published at `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92` of repository `rep_d41c7a9e05b28f63`, whose tree holds `index.html` alone. `blog`'s tree, `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/`, holds `index.html`, `about/index.html`, `style.css`, `logo.png`, `notes.txt`, `404.html`, `.env`, `link.html`, a symlink to `index.html`, and `data.blob`. `<blob:<path>>` below stands for the bytes `git --git-dir=<REPOS_DIR>/rep_8c21d4e0f7a3b915.git cat-file blob 5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02:<path>` prints. Serving a site path reads the catalog and the cache, runs no git while the tree is there, and never writes to the repository. No answer in this group earns a line on stderr. A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A guest opens a public site

The ordinary case: someone follows a link to the blog, with no account on the space. The site's home page is its tree's `index.html`, served as the repository holds it.

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
Content-Length: <bytes>
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly `<blob:index.html>`, and `Content-Length` is its length. There is no `Last-Modified`. sites adds nothing to the page: no banner, no footer, no script.

Preconditions:

- sites is serving, and telemetry takes every event.
- The catalog holds `blog`, public and published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`, and its tree is in the cache.
- The request carries no `X-User-Id` and no cookie.

Postconditions:

- Nothing has changed in the catalog, the cache, or the repository.
- sites wrote nothing to stderr. telemetry has received the request's three events, in this order, with an empty user:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"<visitor>"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `<visitor>` is the id in the `Set-Cookie`, and `response_bytes` is the length of `<blob:index.html>`.

## A guest follows a link to a page in a subdirectory

A path ending in `/` serves that directory's `index.html`. The visitor arrived from a post on another site, so the trail records the host they came from, and only the host: never the rest of the referring URL, and never the request's own query.

Request:

```
GET /blog/about/?from=post HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Referer: https://example.org/post?x=1
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Content-Length: <bytes>
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly `<blob:about/index.html>`. The query names nothing to sites and changes nothing in the answer.

Preconditions:

- The preamble's: `blog`'s tree holds `about/index.html`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started`, whose `path` is `/blog/about/`, and its `request.finished`, telemetry has received:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/about/","referrer_host":"example.org","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"<visitor>"}}
  ```

  With `Referer: http://localhost:8000/draft`, `referrer_host` is `localhost:8000`; with no `Referer`, or one that is not a URL with a host, such as `not a url`, it is the empty string.

## A guest's browser fetches a page's stylesheet and image

A page of the site links its own files, and the browser asks for each under the same `/blog/`. Each is typed from its extension so the browser uses it as it should.

Request:

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog/logo.png HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: <type>
Content-Length: <bytes>
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. For `/blog/style.css`, `<type>` is `text/css; charset=utf-8` and the body is exactly `<blob:style.css>`; for `/blog/logo.png`, `<type>` is `image/png` and the body is exactly `<blob:logo.png>`. The `ETag` is the same for both, and for every file of the site, since it names the commit, not the file.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- Each request recorded one `site.viewed` with its own `path`, `/blog/style.css` or `/blog/logo.png`, `status` 200, and `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

## A guest downloads a text file byte for byte

A text file is typed as text with its charset, and its bytes are the ones the commit holds: sites converts no line ending, adds no final newline, and transforms nothing. Here `notes.txt` has CRLF line endings and no newline at its end, so a change of a single byte would show.

Request:

```
GET /blog/notes.txt HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Content-Length: <bytes>
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly `<blob:notes.txt>`, CRLFs and all, with no newline after its last byte, and `Content-Length` is its length.

Preconditions:

- The preamble's: `notes.txt` at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` has CRLF line endings and does not end in a newline.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `path` `/blog/notes.txt` and `status` 200.

## A guest downloads a file of no known type

A file whose extension names no type is still served, as opaque bytes, so a browser saves it rather than guessing what it is.

Request:

```
GET /blog/data.blob HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/octet-stream
Content-Length: <bytes>
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly `<blob:data.blob>`. A file with no extension at all is typed the same way.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `path` `/blog/data.blob` and `status` 200.

## A guest types a site's address without the trailing slash

`/blog` is the site, but the pages of the site link each other relative to `/blog/`, so sites sends the browser to the address with the slash before serving anything. The query goes with it. The redirect is an answer about a known site, so it sets the cookie and is recorded like any other.

Request:

```
GET /blog?ref=card HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 301 Moved Permanently
Location: /blog/?ref=card
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 301. No story fixes the body. Without a query, `GET /blog` is redirected to `Location: /blog/`. A `HEAD` is redirected the same way.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received, between the request's `request.started`, whose `path` is `/blog`, and its `request.finished`, whose `status` is 301:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":301,"visitor":"<visitor>"}}
  ```

## A guest names a directory of a site without the trailing slash

A path that names a directory in the tree is redirected to the same path with a `/`, so the directory's `index.html` is served at an address its relative links resolve against. The query goes with it.

Request:

```
GET /blog/about HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 301 Moved Permanently
Location: /blog/about/
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 301. No story fixes the body. `GET /blog/about?x=1` is redirected to `Location: /blog/about/?x=1`.

Preconditions:

- The preamble's: `about` is a directory in `blog`'s tree.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `path` `/blog/about`, `status` 301, and `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

## A guest asks for a page a site with its own 404 page does not have

A site that ships a `404.html` at the root of its tree has it shown for every path within the site that names nothing, so a lost visitor stays inside the site's own look. The status is still 404. The page is the site's file, but it is not the file the visitor asked for, so it carries no `ETag`.

Request:

```
GET /blog/no-such-post.html HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog/drafts/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is exactly `<blob:404.html>`. There is no `ETag`. `/blog/drafts/` names a directory the tree does not have, which is the same as a file it does not have; so is a directory the tree has with no `index.html` in it.

Preconditions:

- The preamble's: `blog`'s tree holds `404.html` and no `no-such-post.html` and no `drafts`.

Postconditions:

- Nothing has changed.
- telemetry has received, between the request's `request.started` and its `request.finished`, whose `status` is 404:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/no-such-post.html","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":404,"visitor":"<visitor>"}}
  ```

## A guest asks for a page a site without a 404 page does not have

A tree with no `404.html` of its own leaves the answer to sites, which sends its own not-found page. `recipes`, `u_2b8e1d04`'s public site, holds `index.html` alone.

Request:

```
GET /recipes/soup.html HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is sites' not-found page: titled `Not found`, its heading `Not found` and its text `There is nothing at this address.`, with the footer `sites v<semver>` and no banner. There is no `ETag`.

Preconditions:

- The preamble's: `recipes` is public and published at `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`, whose tree, in the cache, holds `index.html` and no `404.html`.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `site` `sit_6b1d3f5a7c9e0284`, `path` `/recipes/soup.html`, `status` 404, and `commit` `e4d2b6f80a1c3e5d7f9b2a4c6e8d0f1a3b5c7e92`.

## A guest asks for a hidden file of a site

A file or directory whose name begins with `.` is never served, whatever it holds: a `.env`, a `.git`, a `.well-known`. Such a path is answered as one that names nothing, so a visitor cannot tell the file is there. Every segment of the path is held to the rule, not only the last.

Request:

```
GET /blog/.env HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog/.git/config HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is exactly `<blob:404.html>`, as for any path the site does not have; nothing of `.env` is in the answer.

Preconditions:

- The preamble's: `blog`'s tree holds `.env`.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with its `path`, `/blog/.env` or `/blog/.git/config`, `status` 404, and `commit` `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

## A guest's request climbs out of a site with dot segments

A path is resolved within the site's tree and never above it, and sites does not tidy a path into another one: a segment that is `..` or `.` makes the path name nothing, even where tidying would land on a real file. So a request can neither reach another site's tree, nor a file outside the cache, nor the same file under a second address.

Request:

```
GET /blog/about/../style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog/../recipes/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog/./style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is exactly `<blob:404.html>`. No form is redirected, and none is answered with `style.css` or with `recipes`' page. A segment percent-encoded as `%2e%2e` or `%2E` is the same segment and is answered the same way.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. No file outside `cache/sites/sit_4e7a1c9b0d2f8635/5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02/` was read.
- Each request recorded one `site.viewed` with `site` `sit_4e7a1c9b0d2f8635`, its `path` as it arrived, `/blog/about/../style.css` say, and `status` 404.

## A guest asks for a symlink in a site

A symlink in the tree is never followed, whether it points inside the tree or out of it, so a commit cannot make sites serve a file the commit does not hold, nor serve one file under two names. `link.html` points at `index.html`.

Request:

```
GET /blog/link.html HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is exactly `<blob:404.html>`, never `<blob:index.html>`. A path that passes through a symlinked directory is answered the same way.

Preconditions:

- The preamble's: `link.html` in `blog`'s tree is a symlink to `index.html`.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `path` `/blog/link.html` and `status` 404.

## A browser revalidates a site page that has not changed

`Cache-Control: no-cache` has the browser ask again before it reuses its copy, quoting the `ETag` it holds. The tag is the published commit, so while the site stays at that commit every file's tag still matches, and sites says so instead of sending the bytes again. The header matches when it is `*`, or a comma-separated list one of whose tags equals the `ETag` once any `W/` prefix is removed; empty elements of the list are ignored.

Request:

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
If-None-Match: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
If-None-Match: W/"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
If-None-Match: "other", , "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
```

```
GET /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
If-None-Match: *
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 304. The body is empty. A `HEAD` that matches is answered the same. A path that names nothing in the site is answered `404` as above, whatever `If-None-Match` holds.

Preconditions:

- The preamble's: `blog` is published at `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02`.

Postconditions:

- Nothing has changed.
- telemetry has received, between the request's `request.started` and its `request.finished`, whose `status` is 304 and `response_bytes` 0:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/style.css","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":304,"visitor":"<visitor>"}}
  ```

## A browser revalidates a site page after the site was republished

The URL of a page does not change when its site is republished, which is why every answer says `no-cache`: the browser's next question quotes the old commit, which no longer matches, and it gets the new page at once.

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
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Content-Length: <bytes>
ETag: "<sha>"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is the `index.html` of `<sha>`, the commit `blog` is now published at, exactly as `git cat-file blob <sha>:index.html` prints it in its repository.

Preconditions:

- `blog` was republished (`S08`) at `<sha>`, a commit of `rep_8c21d4e0f7a3b915` other than `5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02` whose tree holds `index.html`; its tree is in the cache.
- The browser holds the copy it fetched before that.

Postconditions:

- Nothing has changed.
- The request recorded one `site.viewed` with `path` `/blog/`, `status` 200, and `commit` `<sha>`.

## A client asks for a site page's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body, and is a view of the site like a `GET`: it sets the cookie and is recorded.

Request:

```
HEAD /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Content-Length: <bytes>
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is empty. `Content-Length` is the length the `GET` would send, the length of `<blob:index.html>`. Every answer in this group answers `HEAD` the same way, with its own status and headers.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's three events, in this order:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"HEAD","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"<visitor>"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":0,"status":200}}
  ```

## A client sends a site path a method it does not take

A site is read-only to its visitors: a site path takes `GET` and `HEAD` and nothing else, whatever the path names. The method is checked before anything else, so the answer is the same for a known site, a file it does not have, and a slug that names no site; it is not a view of any site, so it sets no cookie and records no `site.viewed`.

Request:

```
POST /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
PUT /blog/style.css HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
DELETE /nosuch/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. There is no `Set-Cookie`. `PATCH`, `OPTIONS`, and every other method but `GET` and `HEAD` are refused the same way, on every site path, a private site's included, from a guest or a user alike.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`. telemetry has received the request's two events, the `request.finished` with `status` 405 and `response_bytes` 0.

## A guest asks for a site that does not exist

A slug that names no site in the catalog is answered with sites' not-found page. There is no site to view, so there is no cookie and no `site.viewed`; and the answer is not a site's, so it says only `no-cache`, neither `public` nor `private`. A deleted site's slug is answered the same way (`S10`).

Request:

```
GET /nosuch/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nosuch HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /nosuch/index.html HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: no-cache
```

Status 404. The body is sites' not-found page, as in `A guest asks for a page a site without a 404 page does not have`. There is no `Set-Cookie` and no `ETag`. `/nosuch` is not redirected, since there is no site to add a slash for. Slugs are matched exactly: `/Blog/` names no site. A signed-in user is answered the same way.

Preconditions:

- The preamble's: no site has the slug `nosuch` or `Blog`.

Postconditions:

- Nothing has changed.
- sites recorded no `site.viewed`. telemetry has received the request's two events:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/nosuch/"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":404}}
  ```

## A guest asks for a site that is not yet published

A site that has been created but never published has nothing to serve, and is answered with sites' not-found page, so a guest cannot tell it from a site that does not exist but by the cookie. It is a known site, though, so the answer is a view of it: the cookie is set and `site.viewed` is recorded, with an empty `commit`. `scratch` is unlisted, so its slug carries a suffix; the slug is the only way to reach it.

Request:

```
GET /scratch-7c1e9a4f/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /scratch-7c1e9a4f/index.html HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 404. The body is sites' not-found page, as in `A guest asks for a page a site without a 404 page does not have`. There is no `ETag`. `/scratch-7c1e9a4f`, without the slash, is first redirected to `/scratch-7c1e9a4f/` as for any site (`A guest types a site's address without the trailing slash`), with `commit` empty in its `site.viewed`. `scratch`'s ref, `preview`, is not looked at: sites serves a published commit or nothing, and no git runs.

Preconditions:

- The preamble's: `scratch`, slug `scratch-7c1e9a4f`, is public and has no published commit.

Postconditions:

- Nothing has changed. No git ran, and nothing was written under `cache/sites/sit_2d6f8a0c4e1b3957/`.
- telemetry has received, between the request's `request.started` and its `request.finished`, whose `status` is 404:

  ```
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"commit":"","path":"/scratch-7c1e9a4f/","referrer_host":"","site":"sit_2d6f8a0c4e1b3957","status":404,"visitor":"<visitor>"}}
  ```

## A signed-in user opens a public site

A public site is the same for everyone. A user who is signed in to the space, the site's owner or anyone else, is served exactly what a guest is, with the same `Cache-Control: public, no-cache`, since nothing in the answer depends on who asked. Only the trail differs: the request's events carry the user.

Request:

```
GET /blog/ HTTP/1.1
Host: sites.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /blog/ HTTP/1.1
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
ETag: "5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02"
Cache-Control: public, no-cache
Set-Cookie: ikigenba_visitor=<visitor>; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax; Secure
```

Status 200. The body is exactly `<blob:index.html>`, the same bytes a guest gets. Every other story in this group answers a user as it answers a guest.

Preconditions:

- The preamble's. The first request is `u_2b8e1d04`'s, who does not own `blog`; the second is its owner's.

Postconditions:

- Nothing has changed.
- telemetry has received the request's three events under the user, here for `u_2b8e1d04`:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_2b8e1d04","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"site.viewed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_2b8e1d04","attrs":{"commit":"5b9e2d7a1c3f4e6b8a0d2c4e6f8a1b3c5d7e9f02","path":"/blog/","referrer_host":"","site":"sit_4e7a1c9b0d2f8635","status":200,"visitor":"<visitor>"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_2b8e1d04","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  The email `ann@example.com` is in none of them.

## A visitor opens a site while sites cannot reach its catalog

sites finds a site by its slug in the catalog, so when it cannot read the catalog it cannot tell a site from a path that names none, nor a public site from a private one. It does not answer as if the site were gone, nor serve a tree it cannot vouch for; it says plainly that it cannot reach the catalog, as the landing page does (`S03`), quoting nothing of the database's own error, and the visitor may try again later. No site was found, so the answer is not a view of one: it sets no cookie and records no `site.viewed`.

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
Content-Type: text/plain; charset=utf-8
```

Status 503. The body is exactly the one line `cannot reach the catalog; try again later`, ending in a newline. There is no `Set-Cookie`. A `HEAD` is answered with the same status and headers and an empty body. Every site path is answered the same way, for a guest or a user alike.

Preconditions:

- sites is serving, and telemetry takes every event.
- sites' database cannot be read: `state/sites.db` has become unreadable since sites opened it, its storage failing reads, say.
- The request carries no `X-User-Id` and no cookie.

Postconditions:

- Nothing has changed. No file of any tree was read.
- sites wrote nothing to stderr and recorded no `site.viewed`. telemetry has received the request's two events, with an empty user:

  ```
  {"time":"<time>","service":"sites","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/blog/"}}
  {"time":"<time>","service":"sites","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":503}}
  ```

- sites is still serving.
