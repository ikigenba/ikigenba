# Phase 23 — Move github's landing + static from embedded internal/web to disk-served share/www

*Realizes design Decision 6 (landing/static, asset-serving half) and 15 (brand
icons). Touches the realizations of Decision 13 (`R-4LKF-FB23` boot smoke) and 14
(hermetic landing render). No dependency on any other pending phase.*

github stops embedding its web assets and serves them from disk under
`share/www`, the shape every other page-serving service in the suite already uses
(`crm`, `notify`, `ledger`, `gmail`). This is a consistency refactor:
`root project/design/D29.md` sanctions both the embedded and the disk-served
shape, so nothing here is a correctness fix — the goal is one serving model across
the suite.

**Observable end state.**

- `github/share/www/` exists and holds the served surface: `landing.html` (the
  canonical suite landing — a byte clone of crm's `share/www/landing.html` modulo
  github's three fields: the `<title>` suffix `· github`, the eyebrow
  `GitHub connector`, and the one-line description in D6) and `static/` carrying
  `tokens.css`, the four woff2 fonts (`space-grotesk`, `ibm-plex-sans`,
  `ibm-plex-mono-400`, `ibm-plex-mono-500`), and the three brand icons
  (`favicon.ico`, `favicon-32.png`, `apple-touch-icon.png`). These are github's
  own existing assets relocated out of `internal/web/static/`.
- The `internal/web` package is **gone**: `embed.go`, `web.go`, `landing.html`,
  `static/`, and every `*_test.go` under it are deleted. Nothing in the module
  imports `internal/web`.
- `internal/githubapp/spec.go` sets `Spec.WWW = true` and hand-wires only the
  landing route — `rt.Handle("GET /{$}", landingHandler(rt.WWW(), rt.Service(),
  rt.Version()))`, rendering `share/www/landing.html` through `rt.WWW().Render`
  with a `struct{ Service, Version string }`. It no longer registers `GET /static/`,
  a `web.StaticHandler`, or a `GET /favicon.ico` route: the chassis WWW mount
  provides `/static/` and the `/favicon.ico` alias, and `appkit/web` pins the
  static content types (including `.ico` → `image/x-icon`).
- The nginx-fragment content-assertion tests and the landing/static/brand-icon
  tests now live in `cmd/github/main_test.go`, driving github's real
  appkit-assembled router (with the loaded `share/www` site), matching notify's
  layout. Assembling the router sets `IKIGENBA_APP_ID`, `IKIGENBA_GITHUB_ORG`, and
  `IKIGENBA_APP_PRIVATE_KEY` from a per-test generated RSA key (hermetic).
- The install-layout boot smoke (`R-4LKF-FB23`) stages `share/<VERSION>/www` (a
  copy of the committed `github/share/www` tree) with a `share/current` symlink
  before booting, since a `WWW`-declaring service fails `serve` on a missing
  `share/www` root.

**Done when** — all mechanically checkable, run from `github/`:

- The **suite is green**: `GOWORK=off go build ./...` succeeds, `GOWORK=off go test
  ./...` passes with no failures and no SKIP, `gofmt -l .` prints nothing, and
  `GOWORK=off go vet ./...` is clean.
- `internal/web` is gone: `test ! -d internal/web`, and
  `grep -rn 'internal/web' . --include='*.go'` prints nothing.
- The eleven new D6 asset ids are each covered by a tagged test in
  `cmd/github/main_test.go` that drives the assembled router / rendered site, and
  pass:
  - `R-WI06-793Q` — `GET /` on the assembled router is `200 text/html; charset=utf-8`
    with the service name 3× and the version 1× for a distinct service/version pair.
  - `R-WJ82-L0UF` — `Spec.WWW = true` and the landing + `/static/` subtree are
    served from the on-disk `share/www` tree through `Router.WWW()`, with no
    `internal/web` embed in the module (a `WWW=false` or missing-root build fails).
  - `R-WKFY-YSL4` — the rendered landing links only `static/…` (no origin-absolute,
    no cross-service `dashboard`/`/srv/dashboard`, no `http(s)://` asset URL).
  - `R-WLNV-CKBT` — the head preloads `space-grotesk.woff2` and `ibm-plex-sans.woff2`
    as document-relative `static/fonts/…`, and served `tokens.css` references each.
  - `R-WMVR-QC2I` — the rendered page shows the canonical layout: eyebrow
    `GitHub connector`, the D6 description, `<h1 id="page-title">github</h1>` inside
    `<section aria-labelledby="page-title">`, and the Service/Version/API `<dl>` with
    `<code>POST /mcp</code>` and a `class="version"` cell.
  - `R-WO3O-43T7` — the leading `<a class="home" href="/">Home</a>` and its anchored
    `.home` styling (`position: absolute; top: var(--space-8)` inside a
    `position: relative` `main`; hover/focus rule).
  - `R-WPBK-HVJW` — `GET /static/tokens.css` is `200 text/css; charset=utf-8`, and
    each of the four woff2 is `200 font/woff2` with a non-empty body.
  - `R-WQJG-VNAL` — served `tokens.css` declares a self-served `url('fonts/…')`
    `@font-face` for all four fonts and no origin-absolute `url('/static/fonts/…')`.
  - `R-WRRD-9F1A` — every `@font-face` in served `tokens.css` uses
    `font-display: optional` (count equals the `@font-face` count) and none uses `swap`.
  - `R-WSZ9-N6RZ` — on the assembled router, `GET /{$}` answers `/` but does not
    shadow `POST /mcp`, `GET /health`, the PRM well-known, or an unknown path
    (which `404`s); there is no `/feed` route.
  - `R-WU76-0YIO` — `spec.go` sets `Spec.WWW = true`, mounts the landing at
    `GET /{$}` ungated via `rt.WWW().Render`, keeps MCP behind `RequireIdentity`, and
    hand-registers no `GET /static/` or favicon route.
- The relocated nginx-fragment and brand-icon tests stay green in
  `cmd/github/main_test.go`, still tagged with their unchanged ids:
  `R-EYEW-NH1D`, `R-1GOK-GA2F`, `R-1HWG-U1T4` (D6 nginx), `R-42HV-I1HS`,
  `R-43PR-VT8H`, `R-44XO-9KZ6` (D7), `R-1S5A-Z3ZD`, `R-1TD7-CVQ2`, `R-1UL3-QNGR`
  (D12), and `R-8MFA-HUNC`, `R-RYDN-YNR5`, `R-RZLK-CFHU` (D15).
- `R-4LKF-FB23` (boot smoke) stays green with the `share/<VERSION>/www` +
  `share/current` staging in place.
