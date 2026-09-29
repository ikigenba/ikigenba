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

Caching headers, conditional and range requests, and the bodies of the 404
and 405 responses are not fixed by this design: the implementation may send
or honour them or not, and no consumer may rely on either.

## REQUIREMENTS

- R-7MDN-MICX: Package `appkit` MUST export `const StaticPrefix = "/_appkit/"`.
- R-7NLK-0A3M: Package `appkit` MUST export `func Static() http.Handler`, where `http` is the standard library's `net/http`.
- R-7OTG-E1UB: The handler `Static` returns MUST answer a GET request whose URL path is exactly `StaticPrefix` followed by one of `theme.css`, `launcher.js`, `InterVariable.woff2`, `InterVariable-Italic.woff2`, `JetBrainsMono.woff2`, `OFL.txt`, or `TABLER-LICENSE.txt` with status 200 and a body equal byte-for-byte to the embedded file of that name under `assets/`.
- R-7Q1C-RTL0: The handler's 200 responses MUST carry exactly the `Content-Type` `text/css; charset=utf-8` for `theme.css`, `text/javascript; charset=utf-8` for `launcher.js`, `font/woff2` for `InterVariable.woff2`, `InterVariable-Italic.woff2`, and `JetBrainsMono.woff2`, and `text/plain; charset=utf-8` for `OFL.txt` and `TABLER-LICENSE.txt`.
- R-7R99-5LBP: The handler MUST answer a HEAD request for any path R-7OTG-E1UB serves with status 200, the same `Content-Type` as the GET response, and an empty body.
- R-7SH5-JD2E: The handler MUST answer a request with any method, whose URL path is not one R-7OTG-E1UB serves, with status 404; this MUST include the paths `StaticPrefix` itself, `StaticPrefix` followed by `banner.html`, a served path with a trailing `/` or a further path segment, a served file name in different letter case, and any path that does not begin with `StaticPrefix`.
- R-7TP1-X4T3: The handler MUST answer a request whose method is neither GET nor HEAD, for a path R-7OTG-E1UB serves, with status 405, the header `Allow` with exactly the value `GET, HEAD`, and a body that is not the file's content.
- R-7UWY-AWJS: The handler `Static` returns MUST be safe to serve concurrent requests from multiple goroutines, and every call to `Static` MUST return a handler that behaves identically.
