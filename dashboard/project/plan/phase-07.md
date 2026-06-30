# Phase 7 — Eliminate the web-font FOUT on the rendered web surface

*Realizes design Decision 8. Touches only `dashboard/ui` — the embedded
`static/tokens.css` and the two head-bearing templates `html/index.html` and
`html/profile.html` — no `internal/server` change, no migration, no view-model,
no schema, no new dependency. Independent of all other open work; `staticHandler`
(the apex `GET /static/` route) already serves `static/fonts/*.woff2`.*

The rendered web surface (the logged-out login page, the logged-in home/landing,
and the profile page) flashes: it paints with the fallback system font and then
visibly swaps to the web fonts. Cause: every `@font-face` in `tokens.css` uses
`font-display: swap` (paints fallback, swaps after load), and nothing preloads
the fonts. The dashboard is the **apex / `DEFAULT` app** served at the origin
root, so its origin-absolute `@font-face` `src: url("/static/fonts/…")` and its
`<link href="/static/tokens.css">` are **already correct** (they resolve to the
dashboard's own apex `/static/`) — there is **no** `src`-rewrite to do here. This
phase makes the font load swap-free and preloaded.

In **`ui/static/tokens.css`**:
- Change **`font-display: swap` → `font-display: optional`** in all four
  `@font-face` blocks (Space Grotesk, IBM Plex Sans, IBM Plex Mono 400, IBM Plex
  Mono 500). `optional` forbids a post-first-paint swap — this removes the jar.
  Leave the `src` URLs and all design-token *values* untouched.

In **both** head-bearing templates — **`ui/html/index.html`** and
**`ui/html/profile.html`** — inside the `<head>`, beside the existing
`<link rel="stylesheet" href="/static/tokens.css">`, add a font preload for the
two above-the-fold families:

```html
<link rel="preload" as="font" type="font/woff2" crossorigin
      href="/static/fonts/space-grotesk.woff2">
<link rel="preload" as="font" type="font/woff2" crossorigin
      href="/static/fonts/ibm-plex-sans.woff2">
```

Each `<head>` is independent (there is no shared head partial), so the preload
links must be added to **both** files. `crossorigin` is mandatory (fonts fetch in
CORS mode; without it the preload double-fetches and is wasted). The
origin-absolute `href` is byte-identical to the existing `@font-face` `src`, so
the preloaded bytes satisfy the `@font-face` request and first paint uses the
real font. Do **not** preload the mono family (minor/below the fold; `optional`
already prevents its jar). Do not touch any other CSS or page composition.

**Done when:** the suite is green — `cd dashboard && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), `go test ./...`, and
`bin/check-migrations dashboard` all succeed with zero failures (per design
*Conventions*) — and these ids are covered by clearly-named tests co-located in
`dashboard/internal/server/*_test.go` (`package server`):

- **R-P97M-GIJ1** — the embedded `ui/static/tokens.css` contains
  `font-display: optional` in every `@font-face` block and **no** `font-display:
  swap` occurrence anywhere — a block left as `swap` (the jar-causing value) fails
  this. *(read the embedded asset, or `GET /static/tokens.css`)*
- **R-PAFI-UA9Q** — `GET /` (logged-out and with a live session) and
  `GET /profile` (with a live session) each return a `<head>` containing a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` origin-absolute and
  identical to the `@font-face` `src` target (`/static/fonts/…`). A missing
  `crossorigin`, a missing family, a mismatched path, or a head left without the
  preload fails this. *(httptest via `testServer`/`do`; `/profile` uses a live
  session cookie)*
- **R-PBNF-820F** — `GET /static/fonts/space-grotesk.woff2` and
  `GET /static/fonts/ibm-plex-sans.woff2` through the real `staticHandler` each
  return `200` with a woff2 content type and a non-empty body — exercising the real
  embedded-FS route the preload/`src` point at. A 404 fails this. *(httptest
  against the real `GET /static/` route)*
