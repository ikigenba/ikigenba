# Phase 14 — Move the landing + brand assets to disk-served `share/www`

*Realizes design Decision 9 (the landing page) and Decision 11 (the brand icon
contract), and the Decision 1 composition-root change that enables them
(`Spec.WWW`). All dependencies are the existing codebase; no earlier pending
phase.*

End state: artifacts serves its landing page and Carbon assets from disk under
`share/www/` through the shared chassis `WWW` mount, matching every other
current WWW service, and the embedded `internal/web` package is gone.

- `cmd/artifacts/main.go` sets `Spec.WWW = true`, so appkit loads the site from
  the configured `ARTIFACTS_WWW_PATH` (default `./share/www`) at boot and mounts
  `GET /static/` and `GET /favicon.ico`. The bespoke `artifactweb.StaticHandler`
  wiring, the hand-registered `GET /favicon.ico` route, and the `.ico`
  content-type pin are removed — the chassis provides all three.
- The one page route `GET /{$}` is registered in the composition root and
  rendered through `rt.WWW()`: a `landingHandler(store, rt.WWW(), rt.Service(),
  rt.Version())` builds `[]artifactRow` from the store (tier-correct download
  URLs per D4) and calls `Render(w, "landing.html", view)`.
- `artifacts/share/www/landing.html` (the cron-canonical template) and
  `artifacts/share/www/static/` (`tokens.css`, the four woff2 fonts,
  `landing.js`, and the three brand icons `favicon.ico` / `favicon-32.png` /
  `apple-touch-icon.png`) are committed, moved from the deleted
  `internal/web/{landing.html,static}` tree.
- `artifacts/internal/web/` (its `embed.go`, `web.go`, `landing.html`,
  `landing_test.go`, and `static/`) no longer exists.
- The D9 and D11 tests live in `cmd/artifacts/`, rendering through the committed
  `share/www` via `appkit/web` and driving the real assembled router.

**Done when:**

- Each of these ids appears verbatim as a tag comment in
  `cmd/artifacts/*_test.go` immediately above a genuine test asserting its
  behavior:
  - R-OSZJ-I2D8 — the committed `share/www/landing.html` byte-conforms to the
    cron canonical outside the named per-service slots, and on the assembled
    router `GET /static/tokens.css` and each committed woff2 answer `200` with
    content types `text/css` and `font/woff2`.
  - R-OU7F-VU3X — with one artifact seeded in a real store, `GET /{$}` against
    the assembled router (`Spec.WWW` loaded from the committed `share/www`)
    answers `200` and its body carries that artifact's filename as a row.
- These ids remain tagged above a genuine test (relocated to `cmd/artifacts/` as
  needed) and pass: R-53EO-JVJ9, R-54MK-XN9Y, R-55UH-BF0N, R-572D-P6RC,
  R-5AQ2-UHZF (D9) and R-RYDN-YNR5, R-RZLK-CFHU, R-8MFA-HUNC (D11).
- No test tags `R-59I6-GQ8Q` (its behavior — embedded assets — is gone):
  `grep -rF R-59I6-GQ8Q artifacts/ --include='*.go'` prints nothing.
- The embedded package is gone and the disk tree is present:
  `test ! -e artifacts/internal/web` and `test -f
  artifacts/share/www/landing.html` and `test -f
  artifacts/share/www/static/landing.js` and the three brand icons exist under
  `artifacts/share/www/static/`.
- `go test ./...` from `artifacts/` exits 0 (the tree's green gate — clean
  `go build`, `go vet`, silent `gofmt -l .`, tests pass).
