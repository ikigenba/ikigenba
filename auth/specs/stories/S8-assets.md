# Stories — assets

The files that give auth's pages the platform's visual style and its service
launcher, and how auth serves them. They are the platform's shared web files,
the same for every platform app, and auth does not author them. auth serves
exactly seven of them under `/_appkit/`, each with a fixed `Content-Type`:
`theme.css`, the platform style's stylesheet, as `text/css; charset=utf-8`,
whose `@font-face` rules name the font files beside it; `launcher.js`, the
service launcher's script, as `text/javascript; charset=utf-8`; the Inter and
JetBrains Mono fonts, `InterVariable.woff2`, `InterVariable-Italic.woff2`, and
`JetBrainsMono.woff2`, as `font/woff2`; and two licences as
`text/plain; charset=utf-8`: `OFL.txt`, the fonts' licence, and
`TABLER-LICENSE.txt`, the licence of the Tabler icons the platform style
draws. They are inside the binary: auth reads nothing from disk to answer for
them, a host holds no copy of them, and a page needs nothing from any other
host — no font service and no third-party request of any kind. Nothing else
under `/_appkit/` exists, whatever the method: not `/_appkit/` itself, not
another file name, not a served name with a further `/` or segment after it,
and not a served name in other letter case. A served file takes `GET` and
`HEAD` and refuses any other method. auth no longer serves anything under
`/assets/`; a path there is an ordinary path that does not exist, and auth
answers it 404.

Every served file answered 200 or 304 carries a strong `ETag` and exactly one
`Cache-Control: no-cache`, so a browser keeps its copy but asks each time
whether it is still current, and an unchanged file costs a `304` rather than
the bytes again. The `ETag`'s value is opaque — no story fixes it, and
`"<etag>"` below stands for whatever the server sent. What is fixed is the
relation: the value follows from the file's content alone, so the same
content always yields the same value and different content a different one.
A body is the same bytes on every request to the same auth binary; no story
fixes its content. The stories do not fix how auth answers a `Range`
request, whether it sends `Last-Modified`, how it treats `If-Match`,
`If-Unmodified-Since`, `If-Range`, an `If-Modified-Since` with no
`If-None-Match`, or an `If-None-Match` that is not a well-formed list of
tags.

A served file is the same for everyone. auth is the platform's identity
provider, so no identity gate stands in front of it, and a visitor drawing
the sign-in page has no session yet: a file under `/_appkit/` needs no
credential, is answered the same whether or not the request carries a
session cookie or a token, and no answer sets a cookie. The requests below
carry neither, and go to auth a developer serves with
`systemd-socket-activate -E GOOGLE_CLIENT_ID -E GOOGLE_CLIENT_SECRET -E WORKSPACE_DOMAIN -l 127.0.0.1:3001 auth` (`S2-serve.md`), at
`http://localhost:3001`; on a space nginx proxies auth's hostname to auth's
socket instead. A response block shows the status line and the headers the
story fixes; a header it does not show, `Date` say, is not fixed.

## A browser fetches the stylesheet

Every HTML page auth sends links `/_appkit/theme.css` as its stylesheet
(`S3-sign-in.md`, `S5-tokens.md`), so a browser drawing the sign-in page or
the profile asks for it next. The response is the style itself, typed so the
browser applies it, with the tag its next visit will quote back.

Request:

```
$ curl -si http://localhost:3001/_appkit/theme.css
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the platform style's stylesheet. `Cache-Control`
appears once.

Preconditions:

- auth is serving on `127.0.0.1:3001`.
- The request carries no `ikigenba_session` cookie and no `Authorization`
  header.

Postconditions:

- Nothing has changed.

## A browser fetches a font

The stylesheet's `@font-face` rules name the font files beside it, so a
browser applying the style asks auth for each font it needs, under the same
`/_appkit/` the stylesheet came from. `<font>` is `InterVariable`,
`InterVariable-Italic`, or `JetBrainsMono`.

Request:

```
$ curl -si http://localhost:3001/_appkit/<font>.woff2
```

Response:

```
HTTP/1.1 200 OK
Content-Type: font/woff2
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the font `<font>.woff2` names. `Cache-Control`
appears once.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.

## A browser fetches the launcher's script

The banner's service launcher runs a script, and every page auth draws with
the banner loads it from `/_appkit/launcher.js` (`S3-sign-in.md`), so a
browser drawing the profile asks auth for it as it does for the stylesheet.

Request:

```
$ curl -si http://localhost:3001/_appkit/launcher.js
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/javascript; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the service launcher's script. `Cache-Control`
appears once.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.

## A reader reads a licence

The fonts and the icons are shipped under licences that travel with them, and
auth serves both where the files they cover are served. `<licence>` is
`OFL.txt`, the fonts' licence, or `TABLER-LICENSE.txt`, the Tabler icons'
licence.

Request:

```
$ curl -si http://localhost:3001/_appkit/<licence>
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the text of the licence `<licence>` names.
`Cache-Control` appears once.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.

## A browser revalidates an asset that has not changed

This is the ordinary case once a browser has drawn one of auth's pages:
`Cache-Control: no-cache` has it ask again before reusing its copy, and it
quotes the `ETag` of the copy it holds in `If-None-Match`. The file is the one
it fetched, so the tag matches, and auth says so instead of re-sending bytes
the browser already has. The browser uses the copy it holds. The header
matches when it is `*`, or when it is a comma-separated list of tags, one of
which equals the file's `ETag` once any `W/` prefix is removed; empty elements
of the list are ignored. When the header matches, an `If-Modified-Since`
beside it is ignored.

Request:

```
$ curl -si -H 'If-None-Match: "<etag>"' http://localhost:3001/_appkit/theme.css
```

```
$ curl -si -H 'If-None-Match: W/"<etag>"' http://localhost:3001/_appkit/theme.css
```

```
$ curl -si -H 'If-None-Match: "other", , "<etag>"' http://localhost:3001/_appkit/theme.css
```

```
$ curl -si -H 'If-None-Match: *' http://localhost:3001/_appkit/theme.css
```

```
$ curl -si -H 'If-None-Match: "<etag>"' -H 'If-Modified-Since: Thu, 01 Jan 1970 00:00:00 GMT' http://localhost:3001/_appkit/theme.css
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "<etag>"
Cache-Control: no-cache
```

Status 304. The body is empty. The `ETag` returned is the one the 200 for the
same file carries, and `Cache-Control: no-cache` appears once, as on the 200.
Every served file is revalidated this way, and a `HEAD` that matches is
answered the same.

Preconditions:

- auth is serving on `127.0.0.1:3001`.
- `"<etag>"` is the `ETag` from a 200 for `/_appkit/theme.css` from the same
  auth.
- `"other"` is not that `ETag`.

Postconditions:

- Nothing has changed.

## A browser revalidates an asset whose copy is out of date

A browser holding a copy from an auth that carried different content for the
file quotes that copy's tag, which the file's current `ETag` no longer
equals. auth sends the file again, with its current tag. An
`If-Modified-Since` beside a non-empty `If-None-Match` is ignored, however
recent the date it names.

Request:

```
$ curl -si -H 'If-None-Match: "<old-etag>"' http://localhost:3001/_appkit/theme.css
```

```
$ curl -si -H 'If-None-Match: "<old-etag>"' -H 'If-Modified-Since: Fri, 31 Dec 9999 23:59:59 GMT' http://localhost:3001/_appkit/theme.css
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the platform style's stylesheet, as in "A browser
fetches the stylesheet", and `"<etag>"` is the file's current tag, not
`"<old-etag>"`.

Preconditions:

- auth is serving on `127.0.0.1:3001`.
- `"<old-etag>"` is a tag that is not the current `ETag` of
  `/_appkit/theme.css`.

Postconditions:

- Nothing has changed.

## A client asks for an asset's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike,
with no body. A client learns the type and the tag of a file without paying
for its bytes.

Request:

```
$ curl -sI http://localhost:3001/_appkit/theme.css
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is empty. The `ETag` is the one a `GET` of the same file
returns, and `Cache-Control` appears once. Every served file answers `HEAD`
this way, with its own `Content-Type`.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.

## A caller asks for an asset that does not exist

The files under `/_appkit/` are the seven above and nothing more, so the
prefix itself, any other name, a served name with more after it, and a served
name in other letter case all name nothing, whatever the method: a `POST` to
a missing file's path is a 404 like a `GET`, never a 405.
`/_appkit/banner.html` is one such path. The old `/assets/` paths, such as
`/assets/theme.css`, name nothing either.

Request:

```
$ curl -si http://localhost:3001/_appkit/nope.css
```

```
$ curl -si http://localhost:3001/_appkit/
```

```
$ curl -si http://localhost:3001/_appkit/banner.html
```

```
$ curl -si http://localhost:3001/_appkit/theme.css/
```

```
$ curl -si http://localhost:3001/_appkit/theme.css/x
```

```
$ curl -si http://localhost:3001/_appkit/THEME.CSS
```

```
$ curl -si -X POST http://localhost:3001/_appkit/nope.css
```

```
$ curl -si http://localhost:3001/assets/theme.css
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. No story fixes the body or any header of this response.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.

## A caller sends an asset a method it does not take

A served file is read-only: auth serves it and nothing changes it. This holds
only for the seven served paths; any method on a missing file's path is a
404. `Allow` names the two methods a served file takes.

Request:

```
$ curl -si -X POST http://localhost:3001/_appkit/theme.css
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. No story fixes the body or any other header of this response.
`PUT`, `DELETE`, `PATCH`, and every other method but `GET` and `HEAD` are
refused the same way, on every served file.

Preconditions:

- auth is serving on `127.0.0.1:3001`.

Postconditions:

- Nothing has changed.
