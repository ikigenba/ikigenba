# Phase 16 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 13. Touches `internal/web/landing.tmpl`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (`StaticHandler` is already wired at `GET /static/` in
`cmd/prompts/main.go`). Independent of all other open work.*

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than prompts's own: `landing.tmpl` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/prompts/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load prompts's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them. `landing.tmpl` is a Go `html/template` executed by
`LandingHandler`; the link/preload assertions verify the **rendered** `<head>`.

In **`internal/web/landing.tmpl`**:
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/prompts/static/tokens.css`,
  prompts's own copy).
- Add two font preloads in `<head>`, beside the stylesheet link:
  ```html
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/space-grotesk.woff2">
  <link rel="preload" as="font" type="font/woff2" crossorigin
        href="static/fonts/ibm-plex-sans.woff2">
  ```
  `crossorigin` is mandatory; the document-relative `href` resolves to the same
  URL as the new `@font-face` `src`. Do not preload the mono family.

In **`internal/web/static/tokens.css`** (all four `@font-face` blocks):
- `font-display: swap` → `font-display: optional`.
- `src: url('/static/fonts/X.woff2')` → `url('fonts/X.woff2')`.

In **`etc/nginx.conf`**, add a session-gated static location (mirroring the
existing `= /srv/prompts/` session gate and the dashboard's own static gate):
```nginx
location /srv/prompts/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/prompts/` landing location, the bearer-gated
`/srv/prompts/` prefix, the `= /srv/prompts/feed` 404 denial, and the PRM
bootstrap location unchanged.

**Update the superseded assertion (keep its id):**
- `web_test.go` **R-LAND-CARB**: change the page-reference assertion from the
  origin-absolute `/static/tokens.css` to `static/tokens.css` (the link now points
  at prompts's own copy — D10's original intent, corrected form). The rest of
  R-LAND-CARB — that `tokens.css` and the `*.woff2` fonts are embedded and
  `StaticHandler` serves `GET /static/tokens.css` as `text/css` — stays
  **unchanged**: it remains the real-substrate proof that prompts serves its own
  assets.

**Done when:** the suite is green (per design *Conventions*: from the `prompts/`
directory, `go build ./...` compiles all packages without error, `gofmt -l .`
emits no output, and `go test ./...` passes — "the suite is green" means every
test passes and no race detector violations appear, `-race` implicit in CI) and
these ids are covered by clearly-named tests:

- **R-DFKP-IVZU** — prompts's embedded `tokens.css` contains `font-display:
  optional` in every `@font-face` block and **no** `font-display: swap`
  occurrence. *(served `GET /static/tokens.css`)*
- **R-DGSL-WNQJ** — prompts's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-DI0I-AFH8** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest
  `LandingHandler`, rendered `<head>`)*
- **R-DJ8E-O77X** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`, rendered `<head>`)*
- **R-DKGB-1YYM** — `etc/nginx.conf` contains a `location /srv/prompts/static/`
  block whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;`; the existing exact landing,
  bearer prefix, feed-denial, and PRM locations are unchanged. *(fragment grep
  against `etc/nginx.conf`)*
