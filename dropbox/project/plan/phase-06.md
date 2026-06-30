# Phase 06 — Self-serve the landing fonts and eliminate the FOUT

*Realizes design Decision 8. Touches `internal/web/landing.html`,
`internal/web/static/tokens.css`, and `etc/nginx.conf` — no new dependency and no
new Go logic (`StaticHandler` is already wired at `GET /static/` in
`cmd/dropbox/main.go`). Independent of all other open work.*

The landing page flashes (fallback font → web font swap) and, worse, renders with
the **dashboard's** CSS/fonts rather than dropbox's own: `landing.html` links the
stylesheet origin-absolute (`/static/tokens.css`), which under the `/srv/dropbox/`
mount resolves to the apex (the dashboard), and the `@font-face` `src` is likewise
apex-absolute with `font-display: swap`. This phase makes the page load dropbox's
**own** embedded CSS/fonts, swap-free, and opens the nginx path so a browser
session can fetch them.

In **`internal/web/landing.html`**:
- Relativize the stylesheet link: `href="/static/tokens.css"` →
  `href="static/tokens.css"` (document-relative → `/srv/dropbox/static/tokens.css`,
  dropbox's own copy).
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
existing `= /srv/dropbox/` session gate and wiki's `/srv/wiki/static/`):
```nginx
location /srv/dropbox/static/ {
    auth_request /_session-authn;
    proxy_pass http://127.0.0.1:__PORT__/static/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
}
```
Leave the exact `= /srv/dropbox/` landing location, the bearer-gated
`/srv/dropbox/` prefix, the exact-match `= /srv/dropbox/content { return 404; }`
defence-in-depth block, the unauthenticated
`= /srv/dropbox/.well-known/oauth-protected-resource` PRM location, and the
`@dropbox_authn_500` rate-limit fallback unchanged.

**Update the two superseded assertions (keep their ids):**
- `web_test.go` **R-ASST-5K8L** (`TestLandingHTMLReferencesOwnEmbeddedStaticPath`):
  change the body containment from `/static/tokens.css` to `static/tokens.css` (the
  link now points at dropbox's own copy — D3's original intent, corrected form).
  The **same** test's cross-service guards (it forbids `/srv/`, `dashboard`, and
  `://` in the body) **stay as-is and remain green** — the relative
  `static/tokens.css` link and the relative `static/fonts/…` preloads contain none
  of those substrings, so no new guard is needed and no assertion there changes.
- `web_test.go` **R-ASST-3H6J** (`TestStaticHandlerServesTokensCSS`): change the
  asserted `tokens.css` substring from `url('/static/fonts/space-grotesk.woff2')`
  to `url('fonts/space-grotesk.woff2')`. The test's `text/css` content-type and
  Carbon-token assertions stay unchanged.
- `web_test.go` **R-ASST-7M1N** (`TestStaticHandlerServesEmbeddedFonts` —
  StaticHandler serves the four `/static/fonts/*.woff2` with `font/woff2`) stays
  **unchanged** — it remains the real-substrate proof that dropbox serves its own
  font bytes.

**Done when:** the suite is green (per design *Conventions*: `cd dropbox && go
build ./...`, `cd dropbox && go vet ./...`, `cd dropbox && gofmt -l .` clean,
`cd dropbox && go test ./...`, and `bin/check-migrations dropbox` all pass) and
these ids are covered by clearly-named tests:

- **R-LQXL-095Q** — dropbox's embedded `tokens.css` contains `font-display:
  optional` in every `@font-face` block and **no** `font-display: swap`
  occurrence. *(served `GET /static/tokens.css`)*
- **R-LS5H-E0WF** — dropbox's `tokens.css` contains **no** `url('/static/fonts/`
  occurrence; each `@font-face` `src` uses `url('fonts/…woff2')`. *(served `GET
  /static/tokens.css`)*
- **R-LTDD-RSN4** — `GET /` renders a `<head>` containing `href="static/tokens.css"`
  and **no** origin-absolute `href="/static/tokens.css"`. *(httptest `LandingHandler`)*
- **R-LULA-5KDT** — `GET /` renders, in `<head>`, a
  `<link rel="preload" as="font" type="font/woff2" crossorigin …>` for both
  `space-grotesk.woff2` and `ibm-plex-sans.woff2`, each `href` document-relative
  (`static/fonts/…`) matching the `@font-face` `src` target. *(httptest
  `LandingHandler`)*
- **R-LVT6-JC4I** — `etc/nginx.conf` contains a `location /srv/dropbox/static/`
  block whose body carries `auth_request /_session-authn;` and
  `proxy_pass http://127.0.0.1:__PORT__/static/;`; the existing exact landing,
  bearer prefix, content 404, PRM bootstrap, and `@dropbox_authn_500` locations
  are unchanged. *(fragment grep against `etc/nginx.conf`, mirroring the existing
  R-NGNX tests)*
