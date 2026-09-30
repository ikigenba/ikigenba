# D08-assets

dummy's pages take the platform's visual style and its service launcher from
files dummy does not author: the stylesheet every page links, the fonts that
stylesheet loads, the launcher's script, and the licences of the fonts and of
the Tabler icons the style draws. They are the platform's shared web files,
the same for every app, and they come from appkit
(`github.com/ikigenba/ikigenba/appkit`, `D01-layout-and-run-seam`), which
embeds them and serves them through the handler `appkit.Static()` returns,
under the prefix `appkit.StaticPrefix`, `/_appkit/`. This design is how dummy
mounts that handler and what a caller observes of it through dummy.

dummy carries no style file of its own any more. The hand-maintained
`assets/` directory, the root package that embedded it, the `/assets/` route
and dummy's own extension-to-`Content-Type` table are gone; a path under
`/assets/` is now an ordinary path that does not exist, answered by
`D04-panel`'s catch-all like any other.

## Mounting inside the identity gate

The shared files are routes like any other, served by `panel.Handler`
(`D04-panel`), which passes a request whose path begins with `/_appkit/` to
the handler `appkit.Static()` returns, unchanged: that handler compares the
request's whole `URL.Path` against `/_appkit/<name>` itself, so no prefix is
stripped. Everything `Handler` does before it looks at a path — the identity
check that answers a request without `X-User-Id` with the plain 500 and the
line on stderr — is `D04-panel`'s and applies here as on every route, so a
request with no identity is never answered by appkit at all. `D04-panel`'s
rule that a `HEAD` is answered with the status and headers of the matching
`GET` holds here too, and appkit's handler answers `HEAD` that way. Because
every such path is none of `/`, `/widgets` and `/widgets/table`,
`D04-panel`'s store-neutrality requirement for such paths already guarantees
that no request for a shared file changes the widgets.

Delegation is one requirement: through dummy, a request with identity under
`/_appkit/` gets exactly what appkit's handler gives it. The requirements after
it state what the stories fix about that answer, as dummy's own observable
behaviour, so that dummy's tests fail if a different appkit release stopped
giving it. Each was observed against the published appkit module by driving
`appkit.Static()` with `httptest` for every served name and method.

## What is served

Exactly seven paths name a file: `/_appkit/` followed by `theme.css`,
`launcher.js`, `InterVariable.woff2`, `InterVariable-Italic.woff2`,
`JetBrainsMono.woff2`, `OFL.txt` or `TABLER-LICENSE.txt`, compared byte for
byte. The path compared is the request's `URL.Path`, which `net/http` stores
decoded. A `GET` of one answers 200 with the file and a fixed `Content-Type`
per file; the body is the same bytes on every request to one binary, and no
requirement says what the bytes are. Every other path under `/_appkit/` —
the prefix itself, another name such as `banner.html`, a served name with a
further `/` or segment, a served name in other letter case — answers 404,
whatever the method; a method other than `GET` and `HEAD` on a served name
answers 405 with `Allow: GET, HEAD`. The bodies and other headers of the 404
and the 405 are appkit's and are not fixed here: they are not HTML documents
with the banner, and nothing under `/_appkit/` is.

## Revalidation

Every 200 and 304 for a served name carries one strong `ETag` and one
`Cache-Control: no-cache`, so a browser keeps its copy but asks each time. The
tag's value is opaque; what is fixed is that it is a well-formed strong entity
tag (RFC 9110 section 8.8.3) and that one binary gives one file the same tag
every time. The story's other half of the relation, that different content
yields a different tag, is appkit's property and not dummy's: dummy embeds no
content of its own under `/_appkit/`, and R-SDFO-JNO1 hands every such request
to `appkit.Static()`, so one dummy binary only ever serves one content for
each file and no dummy test could observe two. No dummy requirement fixes it.
A request whose single `If-None-Match` line is `*`, or a comma-separated list
of tags one of which, with any `W/` removed, is the file's tag, gets 304 with
an empty body and the same tag, whatever `If-Modified-Since` says; a
well-formed list that names no current tag gets the 200 as if the field were
absent. Several `If-None-Match` lines, a malformed list, `Range`,
`Last-Modified`, `If-Match`, `If-Unmodified-Since`, `If-Range`, and
`If-Modified-Since` without `If-None-Match` are not fixed by the stories and
not fixed here. No 404 or 405 under `/_appkit/` carries an `ETag`.

## REQUIREMENTS

- R-SC7S-5VXC: dummy's design defines an **appkit path** as a request path — the value of the request's `URL.Path` field, which `net/http` stores decoded — that begins with `/_appkit/`, the value of `appkit.StaticPrefix`, and a **shared file path** as an appkit path that is exactly `/_appkit/` followed by one of `theme.css`, `launcher.js`, `InterVariable.woff2`, `InterVariable-Italic.woff2`, `JetBrainsMono.woff2`, `OFL.txt`, and `TABLER-LICENSE.txt`, compared byte for byte and so case-sensitively; and every requirement in dummy's design that names an appkit path or a shared file path MUST denote such a path.
- R-SDFO-JNO1: `Handler` MUST answer every request carrying a non-empty `X-User-Id` header whose path is an appkit path, whatever its method and other headers, exactly as the `http.Handler` that `appkit.Static()` returns answers the same request: with the same status, the same set of header fields with the same values, and the same body.
- R-SENK-XFEQ: `Handler` MUST answer a `GET` request carrying a non-empty `X-User-Id` header and no `If-None-Match` header, whose path is a shared file path, with status 200, a non-empty body, and exactly one `Content-Type` header, whose value is exactly `text/css; charset=utf-8` for `/_appkit/theme.css`, exactly `text/javascript; charset=utf-8` for `/_appkit/launcher.js`, exactly `font/woff2` for each of `/_appkit/InterVariable.woff2`, `/_appkit/InterVariable-Italic.woff2`, and `/_appkit/JetBrainsMono.woff2`, and exactly `text/plain; charset=utf-8` for each of `/_appkit/OFL.txt` and `/_appkit/TABLER-LICENSE.txt`.
- R-SFVH-B75F: A response `Handler` sends with status 200 or 304 to a request carrying a non-empty `X-User-Id` header whose path is a shared file path MUST carry exactly one `ETag` header, whose value is a strong entity tag — a `"`, then zero or more characters each of which is the byte 0x21 or in the byte ranges 0x23–0x7E and 0x80–0xFF, then a `"`, with nothing before or after — and exactly one `Cache-Control` header, whose value is exactly `no-cache`.
- R-SH3D-OYW4: Any two `GET` requests carrying a non-empty `X-User-Id` header and no `If-None-Match` header, whose path is the same shared file path, answered by `Handler` in one binary, whether by one handler or by two, MUST be answered with byte-identical bodies and the same `ETag` value.
- R-SJJ6-GIDI: `Handler` MUST answer a `GET` or `HEAD` request carrying a non-empty `X-User-Id` header, whose path is a shared file path, and which carries exactly one `If-None-Match` header line whose value either is exactly `*` or is a list of one or more elements separated by commas — each element being empty or, once leading and trailing spaces and tabs are removed, an entity tag of the form R-SFVH-B75F describes, optionally preceded by `W/` — at least one of whose entity tags, with any `W/` removed, is byte-identical to the `ETag` value a `GET` of that path carrying no `If-None-Match` header is answered with, with status 304, an empty body, and that same `ETag` value, whatever `If-Modified-Since` header the request carries.
- R-SKR2-UA47: `Handler` MUST answer a `GET` request carrying a non-empty `X-User-Id` header, whose path is a shared file path, and which carries exactly one `If-None-Match` header line whose value is not `*` and is a list of the form R-SJJ6-GIDI describes none of whose entity tags, with any `W/` removed, is byte-identical to the `ETag` value a `GET` of that path carrying no `If-None-Match` header is answered with, with status 200, that `ETag` value, and the body that `GET` is answered with, whatever `If-Modified-Since` header the request carries.
- R-SLYZ-81UW: `Handler` MUST answer a request carrying a non-empty `X-User-Id` header whose path is a shared file path and whose method is neither `GET` nor `HEAD`, whatever `If-None-Match` header it carries, with status 405, the header `Allow: GET, HEAD`, and no `ETag` header.
- R-4FAJ-45J6: `Handler` MUST answer a request carrying a non-empty `X-User-Id` header whose path is an appkit path and not a shared file path — `/_appkit/` itself, a shared file path followed by further characters, and a shared file path's name in other letter case included — whatever its method and whatever `If-None-Match` header it carries, with status 404 and no `ETag` header.
