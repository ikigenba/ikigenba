# Stories — assets

The files that give dummy's pages the platform's visual style, and how dummy
serves them. dummy carries its own copies of the style's files in the
checkout's hand-maintained `assets/` directory: `theme.css`, the platform
style's stylesheet, which loads its fonts through `@font-face` rules naming
the font files beside it; the Inter and JetBrains Mono fonts as `.woff2`
files; and `OFL.txt`, the fonts' licence. They are inside the binary: dummy
reads nothing from disk to answer for them, a host holds no `assets/`
directory, and the page needs nothing from any other host — no font service
and no third-party request of any kind. Every file `assets/` holds is served
at `/assets/<file name>`, and nothing else is: the namespace is flat, so
`/assets/` itself, a path with a further `/` in it such as `/assets/a/b`, and
any name `assets/` does not hold are paths that do not exist, whatever the
method. Only the path of a file `assets/` holds takes `GET` and `HEAD` and
refuses any other method. An asset's body is, byte for byte, the file of that
name in the checkout dummy was built from. Its `Content-Type` comes from the
file's extension, by a fixed table: `.css` is `text/css; charset=utf-8`,
`.woff2` is `font/woff2`, `.txt` is `text/plain; charset=utf-8`, and any other
extension is `application/octet-stream`.

Every asset answered 200 or 304 carries a strong `ETag` and `Cache-Control:
no-cache`, so a browser keeps its copy but asks each time whether it is still
current, and an unchanged asset costs a `304` rather than the bytes again. The
`ETag`'s value is opaque — no story fixes it, and `"<etag>"` below stands for
whatever the server sent. What is fixed is the relation: the same file always
yields the same value, and a dummy carrying different content for that file
yields a different one.

The asset routes are routes like any other. Every request reaching dummy
comes through the host's nginx gate, which sets `X-User-Id` and
`X-User-Email` on each upstream request, or from a sibling app that forwards
the ones it received (`S2`), and the identity check runs first here as on
every route (`S3`). The curl lines below therefore carry both headers
explicitly, and go to a dummy the developer serves with
`systemd-socket-activate -l 127.0.0.1:3000 dummy` (`S2`). A response block
shows the status line and the headers the story fixes; a header it does not
show, `Date` say, is not fixed.

## A browser fetches the panel's stylesheet

Every page dummy sends links `/assets/theme.css` as its stylesheet (`S3`), so
a browser drawing the panel asks for it next. The response is the style
itself, typed so the browser applies it, with the tag its next visit will
quote back.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/theme.css
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the platform style's stylesheet: byte for byte,
`assets/theme.css` as it stood in the checkout dummy was built from.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- dummy was built from a checkout whose `assets/` holds `theme.css`.

Postconditions:

- Nothing has changed.

## A browser fetches a font

The stylesheet's `@font-face` rules name the font files beside it, so a
browser applying the style asks dummy for each font it needs, at the same
flat `/assets/` path the stylesheet came from. `<font>.woff2` is any of the
font files `assets/` holds.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/<font>.woff2
```

Response:

```
HTTP/1.1 200 OK
Content-Type: font/woff2
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is, byte for byte, `assets/<font>.woff2` as it stood in
the checkout dummy was built from.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- dummy was built from a checkout whose `assets/` holds `<font>.woff2`.

Postconditions:

- Nothing has changed.

## A browser revalidates an asset that has not changed

This is the ordinary case once a browser has drawn the panel: `Cache-Control:
no-cache` has it ask again before reusing its copy, and it quotes the `ETag`
of the copy it holds in `If-None-Match`. The file is the one it fetched, so
the tag still matches, and dummy says so instead of re-sending bytes the
browser already has. The browser uses the copy it holds.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'If-None-Match: "<etag>"' http://127.0.0.1:3000/assets/theme.css
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

- dummy is serving on `127.0.0.1:3000`.
- `"<etag>"` is the `ETag` from a response for `/assets/theme.css` from the
  same dummy.

Postconditions:

- Nothing has changed.

## A client asks for an asset's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike,
with no body, as on every route. A client learns the type and the tag of an
asset without paying for its bytes.

Request:

```
$ curl -sI -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/theme.css
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

- dummy is serving on `127.0.0.1:3000`.
- dummy was built from a checkout whose `assets/` holds `theme.css`.

Postconditions:

- Nothing has changed.

## A caller asks for an asset that does not exist

The assets are the files `assets/` holds and nothing more, served flat, so the
directory itself, a nested path, and a name the directory does not hold all
name nothing, whatever the method: a `POST` to a missing asset's path is a 404
like a `GET`, never a 405. The caller is identified, so this is the same page
as any other path that does not exist (`S3`), in the chrome, with the way back
to the panel.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/nope.css
```

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/
```

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/a/b
```

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/nope.css
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/html; charset=utf-8
```

Status 404. The body is the page dummy sends for any path that does not exist
(`S3`): an HTML document in the same chrome as the panel — the mark,
`mg@example.com`, and the sign-out link, with the same title, stylesheet link,
and viewport as every page — whose visible text says the page was not found
and carries a link to `/widgets`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- `assets/` in the checkout dummy was built from holds no file named
  `nope.css`.

Postconditions:

- Nothing has changed.

## A caller sends an asset a method it does not take

An asset is read-only: dummy serves it and nothing changes it. This holds
only for the path of a file `assets/` holds; any method on a missing asset's
path is a 404. `Allow` names
the two methods an asset takes, and the refusal, from an identified caller, is
a page in the chrome.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/assets/theme.css
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
Content-Type: text/html; charset=utf-8
```

Status 405. The body is an HTML document in the same chrome as the panel,
with the same title, stylesheet link, and viewport as every page (`S3`),
whose visible text says the method is not allowed and carries a link to
`/widgets`. `PUT`, `DELETE`, and `PATCH` are refused the same way.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- dummy was built from a checkout whose `assets/` holds `theme.css`.

Postconditions:

- Nothing has changed.

## A request for an asset arrives without the identity headers

The nginx gate sets `X-User-Id` on every upstream request, and a sibling app
forwards the one it received, so a request without it came from neither as
it should and dummy cannot say who is asking. The identity check runs before
dummy looks at the path or the method, on the asset routes as on every other
(`S3`), so a stylesheet is never sent to a request that has no identity.

Request:

```
$ curl -si http://127.0.0.1:3000/assets/theme.css
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is one line of plain text saying the identity header is
missing. No stylesheet and no `ETag` are sent.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The request carries no `X-User-Id` header.

Postconditions:

- Nothing has changed.
- dummy wrote one line to stderr, `dummy: request -: X-User-Id is missing`,
  as it does for every request it answers with a 500 (`S3`).
