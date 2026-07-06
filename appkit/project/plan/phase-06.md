# Phase 06 — The `appkit/web` package

*Realizes design Decision 6 (templates + static assets over an on-disk root).
Depends on no earlier phase (a new standalone package; D5's path is consumed
only later, by Phase 07).*

A new package `appkit/web` with the D6 surface: `Load(root) (*Site, error)`
parsing every top-level `*.html`/`*.tmpl` into one `html/template` set
(erroring on a missing/unreadable root), `(*Site).Render(w, name, data)`
executing a named template with `Content-Type: text/html; charset=utf-8`, and
`(*Site).Static()` serving `<root>/static` from disk — `/static/` prefix
stripped, correct content types, `.woff2` mime registered in `init()`, and
directory requests answered 404 (no autoindex). Templates parse once at `Load`;
static bytes are read per request.

**Done when:** the suite is green (design Conventions commands, from `appkit/`)
and R-M0CJ-U84T, R-M1KG-7ZVI, R-M2SC-LRM7, R-M408-ZJCW, and R-M585-DB3L are
each covered by a clearly-named test in `appkit/web` driving a real
`t.TempDir()` root through `httptest`, genuinely asserting the behavior its D6
Verification line describes.
