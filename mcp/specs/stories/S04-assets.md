# Stories — assets

The files that give the connect page the platform's visual style, its service launcher, its button feedback and its favicon, and how mcp serves them. They are the platform's shared web files, the same for every platform app, and mcp does not author them. mcp serves exactly nine of them under `/_appkit/`, each with a fixed `Content-Type`: `theme.css`, the platform style's stylesheet, as `text/css; charset=utf-8`, whose `@font-face` rules name the font files beside it; `launcher.js`, the service launcher's script, and `feedback.js`, the button feedback script, both as `text/javascript; charset=utf-8`; `favicon.svg`, the platform's one favicon, as `image/svg+xml`; the Inter and JetBrains Mono fonts, `InterVariable.woff2`, `InterVariable-Italic.woff2`, and `JetBrainsMono.woff2`, as `font/woff2`; and two licences as `text/plain; charset=utf-8`: `OFL.txt`, the fonts' licence, and `TABLER-LICENSE.txt`, the licence of the Tabler icons the platform style draws. They are inside the binary: mcp reads nothing from disk to answer for them, a host holds no copy of them, and the page needs nothing from any other host — no font service and no third-party request of any kind. Nothing else under `/_appkit/` exists, whatever the method: not `/_appkit/` itself, not another file name, not a served name with a further `/` or segment after it, and not a served name in other letter case. A served file takes `GET` and `HEAD` and refuses any other method. mcp serves nothing under `/assets/`; a path there is an ordinary path that does not exist, answered as `S03`'s "A caller asks for a path that does not exist" says.

Every served file answered 200 or 304 carries a strong `ETag` and exactly one `Cache-Control: no-cache`, so a browser keeps its copy but asks each time whether it is still current, and an unchanged file costs a `304` rather than the bytes again. The `ETag`'s value is opaque — no story fixes it, and `"<etag>"` below stands for whatever the server sent. What is fixed is the relation: the value follows from the file's content alone, so the same content always yields the same value and different content a different one. A body is the same bytes on every request to the same mcp binary; no story fixes its content. The stories do not fix how mcp answers a `Range` request, whether it sends `Last-Modified`, how it treats `If-Match`, `If-Unmodified-Since`, `If-Range`, an `If-Modified-Since` with no `If-None-Match`, or an `If-None-Match` that is not a well-formed list of tags.

The shared files are served to guests and users alike, with no identity required: the files hold nothing of anyone's, and any page of the platform may ask for them. A request from a signed-in user carries `X-User-Id` and `X-User-Email`, and a guest's carries neither (`S03`); the answer is the same for both, byte for byte. The requests below carry a user's headers unless a story says otherwise, and go to a running mcp (`S02`). Every request here, whatever its answer, adds `request.started` and `request.finished` to the trail, and mcp writes nothing to stderr about it; only an event it cannot deliver reaches stderr, as on every route (`S03`). A response block shows the status line and the headers the story fixes; a header it does not show, `Date` say, is not fixed.

## A browser fetches the connect page's stylesheet

The connect page links `/_appkit/theme.css` as its stylesheet (`S03`), so a browser drawing it asks for it next. The response is the style itself, typed so the browser applies it, with the tag its next visit will quote back.

Request:

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the platform style's stylesheet. `Cache-Control` appears once.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A browser fetches a font

The stylesheet's `@font-face` rules name the font files beside it, so a browser applying the style asks mcp for each font it needs, under the same `/_appkit/` the stylesheet came from. `<font>` is `InterVariable`, `InterVariable-Italic`, or `JetBrainsMono`.

Request:

```
GET /_appkit/<font>.woff2 HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: font/woff2
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the font `<font>.woff2` names. `Cache-Control` appears once.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A browser fetches the launcher's script

The banner's service launcher runs a script, and the connect page loads it from `/_appkit/launcher.js` whenever it carries the launcher (`S03`), so a browser drawing the page asks mcp for it as it does for the stylesheet.

Request:

```
GET /_appkit/launcher.js HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/javascript; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the service launcher's script. `Cache-Control` appears once.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A browser fetches the button feedback script

The connect page loads the platform's button feedback script from `/_appkit/feedback.js` (`S03`), with or without a launcher, so a browser drawing the page asks mcp for it as it does for the stylesheet.

Request:

```
GET /_appkit/feedback.js HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/javascript; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the button feedback script. `Cache-Control` appears once.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A browser fetches the favicon

The connect page links the platform's favicon at `/_appkit/favicon.svg` as its icon (`S03`), with or without a launcher, so a browser drawing the page asks mcp for it to show in the page's tab.

Request:

```
GET /_appkit/favicon.svg HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: image/svg+xml
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the favicon, an SVG image. `Cache-Control` appears once.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A reader reads a licence

The fonts and the icons are shipped under licences that travel with them, and mcp serves both where the files they cover are served. `<licence>` is `OFL.txt`, the fonts' licence, or `TABLER-LICENSE.txt`, the Tabler icons' licence.

Request:

```
GET /_appkit/<licence> HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the text of the licence `<licence>` names. `Cache-Control` appears once.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A browser revalidates an asset that has not changed

This is the ordinary case once a browser has drawn the connect page: `Cache-Control: no-cache` has it ask again before reusing its copy, and it quotes the `ETag` of the copy it holds in `If-None-Match`. The file is the one it fetched, so the tag matches, and mcp says so instead of re-sending bytes the browser already has. The browser uses the copy it holds. The header matches when it is `*`, or when it is a comma-separated list of tags, one of which equals the file's `ETag` once any `W/` prefix is removed; empty elements of the list are ignored. When the header matches, an `If-Modified-Since` beside it is ignored.

Request:

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: "<etag>"
```

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: W/"<etag>"
```

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: "other", , "<etag>"
```

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: *
```

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: "<etag>"
If-Modified-Since: Thu, 01 Jan 1970 00:00:00 GMT
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "<etag>"
Cache-Control: no-cache
```

Status 304. The body is empty. The `ETag` returned is the one the 200 for the same file carries, and `Cache-Control: no-cache` appears once, as on the 200. Every served file is revalidated this way, and a `HEAD` that matches is answered the same.

Preconditions:

- mcp is serving.
- `"<etag>"` is the `ETag` from a 200 for `/_appkit/theme.css` from the same mcp.
- `"other"` is not that `ETag`.

Postconditions:

- Nothing has changed.

## A browser revalidates an asset whose copy is out of date

A browser holding a copy from an mcp that carried different content for the file quotes that copy's tag, which the file's current `ETag` no longer equals. mcp sends the file again, with its current tag. An `If-Modified-Since` beside a non-empty `If-None-Match` is ignored, however recent the date it names.

Request:

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: "<old-etag>"
```

```
GET /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: "<old-etag>"
If-Modified-Since: Fri, 31 Dec 9999 23:59:59 GMT
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the platform style's stylesheet, as in "A browser fetches the connect page's stylesheet", and `"<etag>"` is the file's current tag, not `"<old-etag>"`.

Preconditions:

- mcp is serving.
- `"<old-etag>"` is a tag that is not the current `ETag` of `/_appkit/theme.css`.

Postconditions:

- Nothing has changed.

## A client asks for an asset's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body, as on every route. A client learns the type and the tag of a file without paying for its bytes.

Request:

```
HEAD /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is empty. The `ETag` is the one a `GET` of the same file returns, and `Cache-Control` appears once. Every served file answers `HEAD` this way, with its own `Content-Type`.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A caller asks for an asset that does not exist

The files under `/_appkit/` are the nine above and nothing more, so the prefix itself, any other name, a served name with more after it, and a served name in other letter case all name nothing, whatever the method: a `POST` to a missing file's path is a 404 like a `GET`, never a 405. `/_appkit/banner.html` is one such path.

Request:

```
GET /_appkit/nope.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /_appkit/ HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /_appkit/banner.html HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /_appkit/theme.css/ HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /_appkit/theme.css/x HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
GET /_appkit/THEME.CSS HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
POST /_appkit/nope.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. No story fixes the body or any header of this response.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A caller sends an asset a method it does not take

A served file is read-only: mcp serves it and nothing changes it. This holds only for the nine served paths; any method on a missing file's path is a 404. `Allow` names the two methods a served file takes.

Request:

```
POST /_appkit/theme.css HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. No story fixes the body or any other header of this response. `PUT`, `DELETE`, `PATCH`, and every other method but `GET` and `HEAD` are refused the same way, on every served file.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A guest's browser fetches the stylesheet

nginx lets a guest's browser through with no identity, as it does for every path of mcp but `/mcp`, the paths beneath it, and git's paths (`S03`), and mcp serves the stylesheet as it would to a signed-in user, rather than sending the guest to sign in for a file that holds nothing of anyone's.

Request:

```
GET /_appkit/theme.css HTTP/1.1
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/css; charset=utf-8
ETag: "<etag>"
Cache-Control: no-cache
```

Status 200. The body is the platform style's stylesheet, the same bytes as in `A browser fetches the connect page's stylesheet`, and the `ETag` is the same tag. Every served file is served to a guest this way, the fonts, the launcher's script, the button feedback script, the favicon, and the licences included, and a guest's `HEAD`, revalidation, refused method, and missing file are answered as the stories above answer a user's. An empty `X-User-Id`, or an `X-User-Email` with no `X-User-Id`, is a guest's request all the same.

Preconditions:

- mcp is serving.
- The request carries no `X-User-Id` and no `X-User-Email`, as nginx forwards a guest's request, and the `X-Request-Id` nginx gave it.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr. The trail holds two events for the request, under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user:

  ```
  request.started method=GET path=/_appkit/theme.css
  request.finished status=200
  ```
