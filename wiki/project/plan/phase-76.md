# Phase 76 — Eliminate the web-font FOUT on the read surface

*Realizes design Decision 50. Touches only `internal/web` — the embedded
`static/tokens.css` and the shared `layout.tmpl` `<head>` — no `cmd/wiki/main.go`
change, no migration, no LLM, no DB, no new dependency. Independent of all other
open work; `StaticHandler` (Phase 63/72) already serves `static/fonts/*.woff2`.*

The read surface (home, ask, subject pages) flashes: it paints with the fallback
system font and then visibly swaps to the web fonts. Cause: every `@font-face` in
`tokens.css` uses `font-display: swap` (paints fallback, swaps after load), the
font `src` is origin-absolute (`/static/fonts/…`, which under the `/srv/wiki/`
mount resolves to the apex/dashboard, not this service), and nothing preloads the
fonts. This phase makes the font load swap-free and self-contained.

In **`internal/web/static/tokens.css`**:
- Change **`font-display: swap` → `font-display: optional`** in all four
  `@font-face` blocks (Space Grotesk, IBM Plex Sans, IBM Plex Mono 400, IBM Plex
  Mono 500). `optional` forbids a post-first-paint swap — this removes the jar.
- Change each `src: url('/static/fonts/X.woff2')` → **`url('fonts/X.woff2')`**
  (relative to the stylesheet → `/srv/wiki/static/fonts/X.woff2`, this service's
  own embedded fonts), aligning the URL with the preload target below.

In **`internal/web/layout.tmpl`** (`<head>`, under the existing
`<base href="{{.Mount}}">`, beside the existing `tokens.css` link), add a font
preload for the two above-the-fold families:

```html
<link rel="preload" as="font" type="font/woff2" crossorigin
      href="static/fonts/space-grotesk.woff2">
<link rel="preload" as="font" type="font/woff2" crossorigin
      href="static/fonts/ibm-plex-sans.woff2">
```

`crossorigin` is mandatory (fonts fetch in CORS mode; without it the preload
double-fetches and is wasted). The base-relative `href` resolves to the same
`/srv/wiki/static/fonts/…` URL as the new `@font-face` `src`, so the preloaded
bytes satisfy the `@font-face` request and first paint uses the real font. Do
**not** preload the mono family (minor/below the fold; `optional` already prevents
its jar). Leave the design-token *values* and the `.prose` markdown CSS (D49)
untouched.

**Done when:** the suite is green (per design *Conventions*) and these ids are
covered by clearly-named tests:

- **R-KFVF-EMEO** — the embedded `internal/web/static/tokens.css` contains
  `font-display: optional` in every `@font-face` block and **no** `font-display:
  swap` occurrence anywhere — a block left as `swap` (the jar-causing value) fails
  this. *(read the embedded asset, or `GET /static/tokens.css`)*
- **R-KH3B-SE5D** — the embedded `tokens.css` contains **no** `url('/static/fonts/`
  (origin-absolute) occurrence and each `@font-face` `src` uses the mount-relative
  `url('fonts/…woff2')` form. *(read the embedded asset)*
- **R-KIB8-65W2** — `GET /` and `GET /subject/{type}/{slug}` (with a stub
  `PageFinder`) each return a `<head>` containing a `<link rel="preload" as="font"
  type="font/woff2" crossorigin …>` for both `space-grotesk.woff2` and
  `ibm-plex-sans.woff2`, each `href` resolving to the same `static/fonts/…` path as
  the `@font-face` `src`. A missing `crossorigin`, a missing family, or a
  mismatched path fails this. *(httptest, stub `PageFinder`)*
- **R-KJJ4-JXMR** — `GET /static/fonts/space-grotesk.woff2` and
  `GET /static/fonts/ibm-plex-sans.woff2` through the real `StaticHandler` each
  return `200` with a woff2 content type and a non-empty body — exercising the real
  embedded-FS route the preload/`src` now point at. A 404 fails this. *(httptest
  against the real `StaticHandler`)*
