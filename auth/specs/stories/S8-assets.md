# Stories — assets

The files that give auth's pages the platform's visual style, and how auth
serves them. auth serves the style's files: `theme.css`, the platform
style's stylesheet, which loads its fonts through `@font-face` rules naming
the font files beside it; the Inter and JetBrains Mono fonts as `.woff2`
files; `OFL.txt`, the fonts' licence; and `TABLER-LICENSE.txt`, the licence of
the Tabler icons the pages draw. They are inside the binary: auth reads
nothing from disk to answer for them, a host holds no `assets/` directory, and
a page needs nothing from any other host — no font service and no third-party
request of any kind. Each is served at `/assets/<file name>`, and nothing
else is: the namespace is flat, so `/assets/` itself, a path with a further
`/` in it such as `/assets/a/b`, and any other name are paths that do not
exist, whatever the method. Only the path of an asset takes `GET` and `HEAD`
and refuses any other method. An asset's `Content-Type` comes from the
file's extension, by a fixed table: `.css` is `text/css; charset=utf-8`,
`.woff2` is `font/woff2`, `.txt` is `text/plain; charset=utf-8`, and any other
extension is `application/octet-stream`.

Every asset answered 200 or 304 carries a strong `ETag` and `Cache-Control:
no-cache`, so a browser keeps its copy but asks each time whether it is still
current, and an unchanged asset costs a `304` rather than the bytes again. The
`ETag`'s value is opaque — no story fixes it, and `"<etag>"` below stands for
whatever the server sent. What is fixed is the relation: the same file always
yields the same value, and an auth carrying different content for that file
yields a different one.

An asset is the same for everyone: a visitor drawing the sign-in page has no
session yet, so an asset needs no credential and is answered the same
whether or not the request carries a session cookie or a token. auth's own failures here are
one line of plain text, never a page with the banner, since the caller may be
signed out. The requests below go to auth a developer serves with
`systemd-socket-activate -l 127.0.0.1:3001 auth` (`S2-serve.md`), at
`http://localhost:3001`; on a space nginx proxies auth's hostname to auth's
socket instead. A response block shows the status line and the headers the
story fixes; a header it does not show, `Date` say, is not fixed.

## A browser fetches the stylesheet

Every HTML page auth sends links `/assets/theme.css` as its stylesheet
(`S3-sign-in.md`, `S5-tokens.md`), so a browser drawing the sign-in page or
the profile asks for it next. The response is the style itself, typed so the
browser applies it, with the tag its next visit will quote back.

Request:

```
$ curl -si http://localhost:3001/assets/theme.css
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the platform style's stylesheet.

Preconditions:

- auth is serving on `127.0.0.1:3001`.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- Nothing has changed.

## A browser fetches a font

The stylesheet's `@font-face` rules name the font files beside it, so a
browser applying the style asks auth for each font it needs, at the same
flat `/assets/` path the stylesheet came from. `<font>.woff2` is any of the
font files the stylesheet names.

Request:

```
$ curl -si http://localhost:3001/assets/<font>.woff2
```

Response:

```
HTTP/1.1 200 OK
Content-Type: font/woff2
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the font.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.

## A browser revalidates an asset that has not changed

This is the ordinary case once a browser has drawn one of auth's pages:
`Cache-Control: no-cache` has it ask again before reusing its copy, and it
quotes the `ETag` of the copy it holds in `If-None-Match`. The file is the one
it fetched, so the tag still matches, and auth says so instead of re-sending
bytes the browser already has. The browser uses the copy it holds.

Request:

```
$ curl -si -H 'If-None-Match: "<etag>"' http://localhost:3001/assets/theme.css
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "<etag>"
Cache-Control: no-cache
```

Status 304. The body is empty. The `ETag` returned is the one the request
quoted, and it and `Cache-Control: no-cache` are the same as the 200 carries.
Every asset is revalidated this way, the fonts included.

Preconditions:

- auth is serving on `127.0.0.1:3001`.
- `"<etag>"` is the `ETag` from a response for `/assets/theme.css` from the
  same auth.

Postconditions:

- Nothing has changed.

## A client asks for an asset's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike,
with no body. A client learns the type and the tag of an asset without paying
for its bytes.

Request:

```
$ curl -sI http://localhost:3001/assets/theme.css
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is empty. The `ETag` is the one a `GET` of the same
asset returns.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.

## A caller asks for an asset that does not exist

The assets are the stylesheet, the fonts, and the licences and nothing more,
served flat, so `/assets/` itself, a nested path, and any other name all
name nothing, whatever the method: a `POST` to a missing asset's path is a 404
like a `GET`, never a 405.

Request:

```
$ curl -si http://localhost:3001/assets/nope.css
```

```
$ curl -si http://localhost:3001/assets/
```

```
$ curl -si http://localhost:3001/assets/a/b
```

```
$ curl -si -X POST http://localhost:3001/assets/nope.css
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is one line of plain text.

Preconditions:

- auth is serving on `127.0.0.1:3001`.
- auth serves no asset named `nope.css`.

Postconditions:

- Nothing has changed.

## A caller sends an asset a method it does not take

An asset is read-only: auth serves it and nothing changes it. This holds only
for the path of an asset; any method on a missing asset's path
is a 404. `Allow` names the two methods an asset takes.

Request:

```
$ curl -si -X POST http://localhost:3001/assets/theme.css
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
Content-Type: text/plain; charset=utf-8
```

Status 405. The body is one line of plain text. `PUT`, `DELETE`, and `PATCH`
are refused the same way.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.
