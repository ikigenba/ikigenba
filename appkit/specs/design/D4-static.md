# D4-static

Every app serves the same shared files — the stylesheet, the launcher
script, the fonts, and their licences — from the same path prefix,
`/_appkit/`, so a page's links and the banner's script tag are identical in
every app. `StaticPrefix` names the prefix and `Static` returns the handler
the app mounts at it with `mux.Handle(appkit.StaticPrefix, appkit.Static())`.
The handler sees the full request path, prefix included.

The handler serves exactly seven files, each the embedded asset's bytes
unaltered:

- `theme.css`, as `text/css; charset=utf-8`;
- `launcher.js`, as `text/javascript; charset=utf-8`;
- `InterVariable.woff2`, `InterVariable-Italic.woff2`, and
  `JetBrainsMono.woff2`, as `font/woff2`;
- `OFL.txt` and `TABLER-LICENSE.txt`, as `text/plain; charset=utf-8`.

Nothing else is served. `banner.html` is embedded but is a template, not a
file for the browser, so it is 404 like every other path, and so is the
prefix itself: there is no directory listing. The path is matched exactly as
the request carries it, after URL decoding; a query string does not matter.
GET and HEAD are allowed on the seven files; any other method on one of them
is 405 with `Allow: GET, HEAD`. An unknown path is 404 whatever the method,
since there is no resource there to have methods.

Browsers may keep the files but must check back before using them, so an
app upgrade that changes an asset is seen on the next page load while an
unchanged asset costs only an empty reply. Every 200 response for one of the
seven files, to GET or HEAD, carries a strong `ETag` and
`Cache-Control: no-cache`. The ETag is opaque and depends on the file's
bytes alone: the same content always yields the same tag — on every request,
from every handler `Static` returns, and in every build of appkit — and
different content yields a different tag.

The caching promise covers a *plain* request: a GET or HEAD for one of the
seven files carrying none of `Range`, `If-Match`, `If-Unmodified-Since`, or
`If-Range`. A plain request whose `If-None-Match` matches the tag is answered
304 with the same `ETag`, `Cache-Control`, and an empty body, even if it also
carries `If-Modified-Since`: as RFC 9110 requires, `If-None-Match` takes
precedence, so a browser that sends both still gets its empty reply. A plain
request whose non-empty `If-None-Match` does not match gets the normal 200,
again whatever its `If-Modified-Since`; so does one with no
`If-Modified-Since` whose `If-None-Match` is absent or does not match.
`If-None-Match` matches when it is exactly `*`, or when it is
a well-formed comma-separated list of entity-tags, empty elements ignored,
at least one of which equals the tag under RFC 9110's weak comparison; a
well-formed list with no such tag does not match.

Not fixed by this design, so the implementation may send or honour them or
not and no consumer may rely on either: which status, and what body, answers
a request carrying `Range`, `If-Match`, `If-Unmodified-Since`, or `If-Range`,
or a plain request carrying `If-Modified-Since` whose `If-None-Match` is
absent or empty — though any 200 or 304 such a request does receive for one
of the seven files still carries the `ETag`, `Cache-Control`, and (on a 200)
`Content-Type` the requirements state; `Last-Modified`, `Accept-Ranges`,
and other headers the requirements do not name; the `Content-Type` and other
headers of a 304; how a malformed `If-None-Match`, one mixing `*` with
entity-tags, or one split across several header lines is treated; and any
header or body on the 404 and 405 responses beyond what the requirements
state.

## REQUIREMENTS

- R-7MDN-MICX: Package `appkit` MUST export `const StaticPrefix = "/_appkit/"`.
- R-7NLK-0A3M: Package `appkit` MUST export `func Static() http.Handler`, where `http` is the standard library's `net/http`.
- R-41EX-WQFL: The handler `Static` returns MUST answer a GET request whose URL path is exactly `StaticPrefix` followed by one of `theme.css`, `launcher.js`, `InterVariable.woff2`, `InterVariable-Italic.woff2`, `JetBrainsMono.woff2`, `OFL.txt`, or `TABLER-LICENSE.txt` (a *served path*), that carries none of the headers `Range`, `If-Match`, `If-Unmodified-Since`, and `If-Range` (a *plain request*), and whose `If-None-Match` header is absent or is treated as not matching that path's `ETag` under R-EJGR-G2QB — provided that, when the request also carries an `If-Modified-Since` header, the `If-None-Match` header is present with a non-empty value — with status 200 and a body equal byte-for-byte to the embedded file of that name under `assets/`.
- R-7Q1C-RTL0: The handler's 200 responses MUST carry exactly the `Content-Type` `text/css; charset=utf-8` for `theme.css`, `text/javascript; charset=utf-8` for `launcher.js`, `font/woff2` for `InterVariable.woff2`, `InterVariable-Italic.woff2`, and `JetBrainsMono.woff2`, and `text/plain; charset=utf-8` for `OFL.txt` and `TABLER-LICENSE.txt`.
- R-42MU-AI6A: The handler MUST answer a HEAD plain request for a served path, whose `If-None-Match` header is absent or is treated as not matching that path's `ETag` under R-EJGR-G2QB — provided that, when the request also carries an `If-Modified-Since` header, the `If-None-Match` header is present with a non-empty value — with status 200, the same `Content-Type` as the GET response, and an empty body.
- R-71YB-5NNS: The handler MUST answer a request with any method, whose URL path is not a served path, with status 404; this MUST include the paths `StaticPrefix` itself, `StaticPrefix` followed by `banner.html`, a served path with a trailing `/` or a further path segment, a served file name in different letter case, and any path that does not begin with `StaticPrefix`.
- R-7367-JFEH: The handler MUST answer a request whose method is neither GET nor HEAD, for a served path, with status 405, the header `Allow` with exactly the value `GET, HEAD`, and a body that is not the file's content.
- R-74E3-X756: Every 200 response the handler gives for a served path MUST carry exactly one `ETag` header whose value is a strong entity-tag as RFC 9110 section 8.8.3 defines it: a double-quoted opaque-tag with no `W/` prefix.
- R-EH0Y-OJ8X: Every 200 and 304 response the handler gives for a served path MUST carry exactly one `Cache-Control` header, with the value `no-cache`.
- R-EI8V-2AZM: A served path's `ETag` value MUST be determined by the bytes of its embedded file alone: across all 200 and 304 responses — whatever the method or request, from any handler `Static` returns, and in any build of `appkit`, including one whose `assets/` differ — two responses for served paths whose embedded files are byte-identical MUST carry the same `ETag` value, and two whose embedded files differ in content MUST carry different `ETag` values.
- R-EJGR-G2QB: For a request carrying exactly one `If-None-Match` header, the header MUST be treated as matching a served path's `ETag` when its value is exactly `*`, or when it is a well-formed comma-separated list of entity-tags — optional whitespace around each comma, empty list elements ignored as RFC 9110 section 5.6.1.2 requires — at least one of which, after removing any `W/` prefix, is character-for-character equal to that `ETag` (the weak comparison of RFC 9110 sections 8.8.3.2 and 13.1.2); a well-formed comma-separated list of entity-tags none of which is so equal MUST be treated as not matching.
- R-43UQ-O9WZ: The handler MUST answer a GET or HEAD plain request for a served path whose `If-None-Match` header is treated as matching that path's `ETag` under R-EJGR-G2QB, whether or not the request also carries an `If-Modified-Since` header, with status 304, exactly one `ETag` header whose value is the same `ETag` value the 200 response for that path carries, and an empty body.
- R-7UWY-AWJS: The handler `Static` returns MUST be safe to serve concurrent requests from multiple goroutines, and every call to `Static` MUST return a handler that behaves identically.
